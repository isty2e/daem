//go:build darwin || linux

package hostmutation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/isty2e/daem/internal/effect/execute/applyprogram"
	"github.com/isty2e/daem/internal/effect/execute/mutationnamespace"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestRestorationOwnsAdmittedInputsAndVisitOrder(t *testing.T) {
	root := t.TempDir()
	content := []byte("expected after\n")
	writeRestorationTestFile(t, filepath.Join(root, "CLAUDE.md"), content)
	fixture := newRestorationTestOperation(t, root, storagecommit.Adapter{})
	actions := []RestorationAction{{
		Kind: recovery.ActionKindRestoreDelete, Scope: target.ScopeProject, Destination: "CLAUDE.md",
		ExpectedAfter: recovery.ExpectedPathState{
			Existed: true, Kind: recovery.PathKindFile, PathMode: recovery.NewPermissionMode(0o600),
			ContentHash: string(artifact.HashFileContent(content)),
		},
	}}
	restoration, err := fixture.operation.StageRestoration(t.Context(), fixture.original, fixture.control, fixture.sources, actions, testAggregateCodecs())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restoration.Cleanup(context.Background()) })
	copied := *restoration
	actions[0].Destination = "../outside"
	budget := beginRestorationExecutionForTest(t, fixture)
	visits, err := restoration.PrepareExecution()
	if err != nil {
		t.Fatalf("prepare owned action facts after input mutation: %v", err)
	}
	if !reflect.DeepEqual(visits, []int{0}) {
		t.Fatalf("compiled visits = %v, want [0]", visits)
	}
	visits[0] = 99
	stop := errors.New("stop before host mutation")
	visited := -1
	err = copied.Execute(t.Context(), budget, func(index int) error {
		visited = index
		return stop
	}, nil, applyprogram.VisibilityGate{})
	if !errors.Is(err, stop) || visited != 0 {
		t.Fatalf("copied execution = index %d, %v; want original prepared action and callback error", visited, err)
	}
	current, err := copied.PrepareExecution()
	if err != nil || !reflect.DeepEqual(current, []int{0}) {
		t.Fatalf("copied handle's prepared visits = %v, %v; want [0]", current, err)
	}
}

func TestRestorationOwnershipGuardReceivesIndependentActionFacts(t *testing.T) {
	for _, grouped := range []bool{false, true} {
		name := "whole path"
		if grouped {
			name = "aggregate group"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			content := []byte("expected after\n")
			action := RestorationAction{
				Kind: recovery.ActionKindRestoreDelete, Scope: target.ScopeProject, Destination: "CLAUDE.md",
				ExpectedAfter: recovery.ExpectedPathState{
					Existed: true, Kind: recovery.PathKindFile, PathMode: recovery.NewPermissionMode(0o600),
					ContentHash: string(artifact.HashFileContent(content)),
				},
			}
			if grouped {
				contract := restorationMCPContractForTest(t, "context7")
				canonical, err := mcpcodec.CanonicalClaudeProjectMCPServerEntry(mcpcodec.ClaudeProjectMCPServerProjection{
					ServerID: "context7", Command: "npx", AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
				})
				if err != nil {
					t.Fatal(err)
				}
				content = []byte(fmt.Sprintf(`{"mcpServers":{"context7":%s}}`, canonical))
				action.Destination = contract.Address().Document().AggregateRoot().String()
				action.ContentPath = string(outputtest.LegacyContentPath(t, contract.Address()))
				action.AggregateContract = &contract
				action.ExpectedAfter.PathExisted = true
				action.ExpectedAfter.ContentHash = string(artifact.HashFileContent(canonical))
			}
			path := filepath.Join(root, action.Destination)
			writeRestorationTestFile(t, path, content)
			fixture := newRestorationTestOperation(t, root, storagecommit.Adapter{})
			restoration, err := fixture.operation.StageRestoration(t.Context(), fixture.original, fixture.control, fixture.sources, []RestorationAction{action}, testAggregateCodecs())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = restoration.Cleanup(context.Background()) })
			budget := beginRestorationExecutionForTest(t, fixture)
			if _, err := restoration.PrepareExecution(); err != nil {
				t.Fatal(err)
			}
			stop := errors.New("stop at ownership guard")
			guardCalls := 0
			guard := func(_ context.Context, observed RestorationAction, _ mutationnamespace.Destination) error {
				guardCalls++
				if observed.ExpectedAfter.PathMode == nil || observed.ExpectedAfter.PathMode.FileMode().Perm() != 0o600 {
					t.Fatal("ownership guard changed retained action mode")
				}
				*observed.ExpectedAfter.PathMode = *recovery.NewPermissionMode(0o644)
				if observed.AggregateContract != nil {
					*observed.AggregateContract = aggregate.ProjectionContract{}
				}
				return stop
			}
			for range 2 {
				if err := restoration.Execute(t.Context(), budget, nil, guard, applyprogram.VisibilityGate{}); !errors.Is(err, stop) {
					t.Fatalf("guarded execution = %v, want original guard refusal", err)
				}
			}
			if guardCalls != 2 {
				t.Fatalf("guard calls = %d, want two unchanged action snapshots", guardCalls)
			}
			current, err := os.ReadFile(path)
			if err != nil || string(current) != string(content) {
				t.Fatalf("guard-refused host content = %q, %v; want unchanged input", current, err)
			}
		})
	}
}
