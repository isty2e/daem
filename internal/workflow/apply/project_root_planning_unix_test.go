//go:build darwin || linux

package apply

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"golang.org/x/sys/unix"
)

func TestPlanWriteCapturesProjectRootBeforeManifestLoad(t *testing.T) {
	root, manifestPath, lockfilePath, missingInventory, _, _ := writeApplyCodexPluginCarrierCommandFixture(t)
	manifestContent, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest fixture: %v", err)
	}
	lockfileContent, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatalf("read lockfile fixture: %v", err)
	}
	if err := os.Remove(manifestPath); err != nil {
		t.Fatalf("remove manifest fixture: %v", err)
	}
	if err := unix.Mkfifo(manifestPath, 0o600); err != nil {
		t.Fatalf("create blocking manifest fifo: %v", err)
	}

	type planOutcome struct {
		result *PreparedWrite
		err    error
	}
	outcomes := make(chan planOutcome, 1)
	go func() {
		result, planErr := PlanWrite(context.Background(), CommandInput{
			ManifestPath:         manifestPath,
			LockfilePath:         lockfilePath,
			TargetValues:         []string{"codex"},
			RelationObservations: &missingInventory,
		})
		outcomes <- planOutcome{result: result, err: planErr}
	}()

	writer, err := os.OpenFile(manifestPath, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open manifest fifo after planner reader: %v", err)
	}
	moved := root + "-captured"
	t.Cleanup(func() { _ = os.RemoveAll(moved) })
	if err := os.Rename(root, moved); err != nil {
		_ = writer.Close()
		t.Fatalf("move selected root while manifest load is blocked: %v", err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		_ = writer.Close()
		t.Fatalf("create replacement root: %v", err)
	}
	if err := os.WriteFile(manifestPath, manifestContent, 0o600); err != nil {
		_ = writer.Close()
		t.Fatalf("write replacement manifest: %v", err)
	}
	if err := os.WriteFile(lockfilePath, lockfileContent, 0o600); err != nil {
		_ = writer.Close()
		t.Fatalf("write replacement lockfile: %v", err)
	}
	if _, err := writer.Write(manifestContent); err != nil {
		_ = writer.Close()
		t.Fatalf("release captured manifest read: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close manifest fifo writer: %v", err)
	}

	outcome := <-outcomes
	defer outcome.result.Close()
	if !hasRootedPathFailureKind(outcome.err, rootedpath.FailureRootReplaced) {
		t.Fatalf("PlanWrite error = %v, want %s", outcome.err, rootedpath.FailureRootReplaced)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".daem", "state.json")); !os.IsNotExist(statErr) {
		t.Fatalf("replacement statefile stat error = %v, want absent", statErr)
	}
}
