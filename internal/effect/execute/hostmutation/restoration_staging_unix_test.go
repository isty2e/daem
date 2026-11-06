//go:build darwin || linux

package hostmutation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/execute/applyprogram"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestRollbackScratchUsesPrivateSystemLocation(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	scratch, err := CreateRollbackScratch(storagecommit.Adapter{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(scratch.Directory()) })
	rollbackDir := scratch.Directory()
	if filepath.Clean(filepath.Dir(rollbackDir)) != filepath.Clean(os.TempDir()) {
		t.Fatalf("rollback scratch parent = %q, want system temp %q", filepath.Dir(rollbackDir), os.TempDir())
	}
	relativeToState, err := filepath.Rel(stateDir, rollbackDir)
	if err != nil {
		t.Fatalf("relativize rollback scratch against state root: %v", err)
	}
	if relativeToState != ".." && !strings.HasPrefix(relativeToState, ".."+string(filepath.Separator)) {
		t.Fatalf("rollback scratch %q is inside selected state root %q", rollbackDir, stateDir)
	}
	info, err := os.Stat(rollbackDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("rollback scratch mode = %04o, want 0700", got)
	}
	if err := scratch.Abort(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(rollbackDir); !os.IsNotExist(err) {
		t.Fatalf("rollback scratch stat after cleanup = %v, want absence", err)
	}
}

func TestRecoveryRollbackCleanupRejectsScratchGrowth(t *testing.T) {
	root := t.TempDir()
	after := []byte("expected after\n")
	writeRestorationTestFile(t, filepath.Join(root, ".codex", "AGENTS.md"), after)
	fixture := newRestorationTestOperation(t, root, storagecommit.Adapter{})
	action := RestorationAction{
		Kind: recovery.ActionKindRestoreDelete, Scope: target.ScopeProject, Destination: ".codex/AGENTS.md",
		ExpectedAfter: recovery.ExpectedPathState{
			Existed: true, PathMode: recovery.NewPermissionMode(0o600),
			Kind: recovery.PathKindFile, ContentHash: string(artifact.HashFileContent(after)),
		},
	}
	restoration, err := fixture.operation.StageRestoration(t.Context(), fixture.original, fixture.control, fixture.sources, []RestorationAction{action}, testAggregateCodecs())
	if err != nil {
		t.Fatal(err)
	}
	rollbackDir := restoration.rollback.scratch.Directory()
	t.Cleanup(func() { _ = os.RemoveAll(rollbackDir) })
	if err := os.WriteFile(filepath.Join(rollbackDir, "unexpected"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	beginRestorationExecutionForTest(t, fixture)
	err = restoration.Cleanup(t.Context())
	if err == nil || (!strings.Contains(err.Error(), "changed before cleanup") && !strings.Contains(err.Error(), "exceeds 1 entries")) {
		t.Fatalf("rollback cleanup error = %v, want scratch-growth rejection", err)
	}
	if _, err := os.Stat(filepath.Join(rollbackDir, "unexpected")); err != nil {
		t.Fatalf("unexpected entry changed after rejected cleanup: %v", err)
	}
	if _, err := os.Stat(rollbackDir); err != nil {
		t.Fatalf("scratch changed after rejected cleanup: %v", err)
	}
}

func TestRecoveryRollbackRejectsReplacedStageArtifact(t *testing.T) {
	root := t.TempDir()
	before, after := []byte("before recovery\n"), []byte("expected after\n")
	destination := filepath.Join(root, ".codex", "AGENTS.md")
	writeRestorationTestFile(t, destination, after)
	fixture := newRestorationTestOperation(t, root, storagecommit.Adapter{})
	work, err := workmeasure.NewArtifactWork(0, int64(len(before)))
	if err != nil {
		t.Fatal(err)
	}
	mode := recovery.NewPermissionMode(0o600)
	action := RestorationAction{
		Kind: recovery.ActionKindRestoreWrite, Scope: target.ScopeProject, Destination: ".codex/AGENTS.md",
		BackupHash: string(artifact.HashFileContent(before)), BackupKind: recovery.PathKindFile,
		BackupWork: work, BeforePathMode: mode, BeforePathExisted: true,
		ExpectedAfter: recovery.ExpectedPathState{
			Existed: true, PathMode: mode, Kind: recovery.PathKindFile,
			ContentHash: string(artifact.HashFileContent(after)),
		},
	}
	restoration, err := fixture.operation.StageRestoration(t.Context(), fixture.original, fixture.control, fixture.sources, []RestorationAction{action}, testAggregateCodecs())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restoration.Cleanup(context.Background()) })
	writeRestorationTestFile(t, destination, before)
	entry := &restoration.rollback.entries[0]
	entry.attempted, entry.effectKnown = true, true
	entry.effectState = recoveryWholePathState{
		existed: true, kind: recovery.PathKindFile, contentHash: string(artifact.HashFileContent(before)), fileMode: 0o600,
	}
	if err := os.WriteFile(entry.backupPath, []byte(strings.Repeat("x", len(after))), 0o600); err != nil {
		t.Fatal(err)
	}
	budget := beginRestorationExecutionForTest(t, fixture)
	err = restoration.Restore(t.Context(), budget, applyprogram.VisibilityGate{})
	if err == nil || !strings.Contains(err.Error(), "does not match expected hash") {
		t.Fatalf("rollback.Restore error = %v, want replaced-stage rejection", err)
	}
	content, err := os.ReadFile(destination)
	if err != nil || string(content) != string(before) {
		t.Fatalf("destination = %q, %v; want unchanged attempted effect", content, err)
	}
}

func TestRecoveryRollbackStagesOneBaselinePerSharedDocument(t *testing.T) {
	root := t.TempDir()
	const actionCount = 64
	servers := make(map[string]json.RawMessage, actionCount)
	actions := make([]RestorationAction, 0, actionCount)
	for index := range actionCount {
		serverID := fmt.Sprintf("server-%03d", index)
		canonical, err := mcpcodec.CanonicalClaudeProjectMCPServerEntry(mcpcodec.ClaudeProjectMCPServerProjection{
			ServerID: serverID, Command: "npx", AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
		})
		if err != nil {
			t.Fatal(err)
		}
		servers[serverID] = json.RawMessage(canonical)
		contract := restorationMCPContractForTest(t, serverID)
		actions = append(actions, RestorationAction{
			Scope: target.ScopeProject, Destination: contract.Address().Document().AggregateRoot().String(),
			ContentPath: string(outputtest.LegacyContentPath(t, contract.Address())), AggregateContract: &contract,
			ExpectedAfter: recovery.ExpectedPathState{
				Existed: true, PathExisted: true, PathMode: recovery.NewPermissionMode(0o600),
				Kind: recovery.PathKindFile, ContentHash: string(artifact.HashFileContent(canonical)),
			},
		})
	}
	content, err := json.Marshal(map[string]map[string]json.RawMessage{"mcpServers": servers})
	if err != nil {
		t.Fatal(err)
	}
	writeRestorationTestFile(t, filepath.Join(root, actions[0].Destination), content)
	fixture := newRestorationTestOperation(t, root, storagecommit.Adapter{})
	restoration, err := fixture.operation.StageRestoration(t.Context(), fixture.original, fixture.control, fixture.sources, actions, testAggregateCodecs())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restoration.Cleanup(context.Background()) })
	entries := restoration.rollback.entries
	if len(entries) != actionCount {
		t.Fatalf("rollback entries = %d, want %d action-aligned entries", len(entries), actionCount)
	}
	backupPath := entries[0].backupPath
	if backupPath == "" {
		t.Fatal("shared document has no staged baseline")
	}
	for index, entry := range entries {
		if entry.backupPath != backupPath {
			t.Fatalf("rollback entry[%d] backup = %q, want shared %q", index, entry.backupPath, backupPath)
		}
	}
	staged, err := os.ReadDir(restoration.rollback.scratch.Directory())
	if err != nil {
		t.Fatal(err)
	}
	if len(staged) != 1 {
		t.Fatalf("staged rollback artifacts = %d, want one per physical document", len(staged))
	}
}

func writeRestorationTestFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}
