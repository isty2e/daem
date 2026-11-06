//go:build darwin || linux

package hostmutation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/execute/applyprogram"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/realization/aggregate"
	hookcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	commandhook "github.com/isty2e/daem/internal/realization/aggregate/hook"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestAggregateRecoveryRejectsNewlyAppearedEmptyDocument(t *testing.T) {
	root := t.TempDir()
	destinationPath := filepath.Join(root, ".codex", "hooks.json")
	writeRestorationTestFile(t, destinationPath, nil)
	fixture := newRestorationTestOperation(t, root, storagecommit.Adapter{})
	destination, err := fixture.operation.destinations.ResolveProject(outputtest.Parse(t, ".codex/hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	placement, ok := aggregate.HookPlacementFor(target.TargetCodex, target.ScopeProject)
	if !ok {
		t.Fatal("Codex project Hook placement is missing")
	}
	canonical, err := hookcodec.CanonicalHookContribution(commandhook.ContributionInput{
		Event: "Stop", Type: "command", Command: "echo guard",
	})
	if err != nil {
		t.Fatal(err)
	}
	contribution, err := placement.Contribution(canonical)
	if err != nil {
		t.Fatal(err)
	}
	contract := contribution.Contract()
	action := RestorationAction{
		Kind: recovery.ActionKindRestoreDelete, Scope: target.ScopeProject,
		Destination: ".codex/hooks.json", ContentPath: aggregate.HooksContentPath, AggregateContract: &contract,
	}
	empty, err := workmeasure.NewArtifactWork(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for range 16 {
		if err := fixture.original.ReserveGeneralFileObservation(empty); err != nil {
			t.Fatal(err)
		}
	}
	reservation := recoveryGeneralPathReservation{budget: fixture.original}
	for range recoveryHostDestinationAccesses {
		if err := fixture.operation.destinations.ReserveDestinationAccess(destination, reservation); err != nil {
			t.Fatal(err)
		}
	}
	if err := fixture.original.ConcludeScratchCleanupNotApplicable(); err != nil {
		t.Fatal(err)
	}
	budget := beginRestorationExecutionForTest(t, fixture)
	owner := &Restoration{restorationState: &restorationState{
		operation: fixture.operation, control: fixture.control, sources: fixture.sources, original: fixture.original,
	}}
	_, err = executeRecoveryAggregateActionGroup(
		t.Context(), owner, budget, []RestorationAction{action}, []hostRollbackEntry{{destination: destination}},
		recoveryAggregateActionGroup{indexes: []int{0}, destination: destination},
		nil, nil, testAggregateCodecs(), applyprogram.VisibilityGate{},
	)
	if err == nil || !strings.Contains(err.Error(), "changed between recovery actions") {
		t.Fatalf("executeRecoveryAggregateActionGroup error = %v, want appeared-document rejection", err)
	}
	content, readErr := os.ReadFile(destinationPath)
	if readErr != nil || len(content) != 0 {
		t.Fatalf("unmanaged empty aggregate = %q, %v; want preserved empty file", content, readErr)
	}
}
