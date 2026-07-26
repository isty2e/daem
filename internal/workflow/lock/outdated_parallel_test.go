package lock

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRunOutdatedReportsChangesWithoutWritingLockfile(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeWorkflowTestFile(t, tempDir, "instructions/project.md", "before\n")
	writeWorkflowTestFile(t, tempDir, "daem.toml", `version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/project.md"
targets = ["codex"]
`)

	written, err := RunLock(context.Background(), LockInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	beforeLockfile, err := os.ReadFile(written.LockfilePath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	writeWorkflowTestFile(t, tempDir, "instructions/project.md", "after\n")

	result, err := RunOutdated(context.Background(), OutdatedInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("RunOutdated returned error: %v", err)
	}
	if result.LockfilePath != written.LockfilePath {
		t.Fatalf("LockfilePath = %q, want %q", result.LockfilePath, written.LockfilePath)
	}
	if !result.PreviousFound {
		t.Fatal("PreviousFound = false, want true")
	}
	if !result.Delta.HasChanges() {
		t.Fatal("Delta.HasChanges() = false, want true")
	}
	afterLockfile, err := os.ReadFile(written.LockfilePath)
	if err != nil {
		t.Fatalf("ReadFile after outdated returned error: %v", err)
	}
	if string(afterLockfile) != string(beforeLockfile) {
		t.Fatal("RunOutdated changed lockfile bytes")
	}
}

func TestRunLockDefaultParallelismWritesSameLockfileAsSequential(t *testing.T) {
	tempDir := t.TempDir()
	sequentialManifestPath := writeLockCommandParallelismFixture(t, tempDir, "sequential")
	defaultManifestPath := writeLockCommandParallelismFixture(t, tempDir, "default")

	sequential, err := RunLock(context.Background(), LockInput{
		ManifestPath:         sequentialManifestPath,
		MaxParallelSourceOps: 1,
	})
	if err != nil {
		t.Fatalf("RunLock sequential returned error: %v", err)
	}
	defaulted, err := RunLock(context.Background(), LockInput{ManifestPath: defaultManifestPath})
	if err != nil {
		t.Fatalf("RunLock default returned error: %v", err)
	}
	sequentialContent, err := os.ReadFile(sequential.LockfilePath)
	if err != nil {
		t.Fatalf("ReadFile sequential lockfile returned error: %v", err)
	}
	defaultContent, err := os.ReadFile(defaulted.LockfilePath)
	if err != nil {
		t.Fatalf("ReadFile default lockfile returned error: %v", err)
	}
	if string(defaultContent) != string(sequentialContent) {
		t.Fatalf("default lockfile bytes differ from sequential:\n%s\n---\n%s", defaultContent, sequentialContent)
	}
}

func TestRunOutdatedSequentialParallelismPreservesNoWriteBehavior(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeWorkflowTestFile(t, tempDir, "instructions/project.md", "before\n")
	writeWorkflowTestFile(t, tempDir, "daem.toml", `version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/project.md"
targets = ["codex"]
`)

	written, err := RunLock(context.Background(), LockInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	beforeLockfile, err := os.ReadFile(written.LockfilePath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	writeWorkflowTestFile(t, tempDir, "instructions/project.md", "after\n")

	result, err := RunOutdated(context.Background(), OutdatedInput{
		ManifestPath:         manifestPath,
		MaxParallelSourceOps: 1,
	})
	if err != nil {
		t.Fatalf("RunOutdated returned error: %v", err)
	}
	if result.LockfilePath != written.LockfilePath {
		t.Fatalf("LockfilePath = %q, want %q", result.LockfilePath, written.LockfilePath)
	}
	if !result.PreviousFound {
		t.Fatal("PreviousFound = false, want true")
	}
	if !result.Delta.HasChanges() {
		t.Fatal("Delta.HasChanges() = false, want true")
	}
	afterLockfile, err := os.ReadFile(written.LockfilePath)
	if err != nil {
		t.Fatalf("ReadFile after outdated returned error: %v", err)
	}
	if string(afterLockfile) != string(beforeLockfile) {
		t.Fatal("RunOutdated with MaxParallelSourceOps=1 changed lockfile bytes")
	}
}

func TestCommandMaxParallelSourceOpsUsesDefaultOnlyForCommandInputs(t *testing.T) {
	if got := commandMaxParallelSourceOps(0); got != defaultCommandMaxParallelSourceOps {
		t.Fatalf("commandMaxParallelSourceOps(0) = %d, want default %d", got, defaultCommandMaxParallelSourceOps)
	}
	if got := commandMaxParallelSourceOps(-3); got != defaultCommandMaxParallelSourceOps {
		t.Fatalf("commandMaxParallelSourceOps(-3) = %d, want default %d", got, defaultCommandMaxParallelSourceOps)
	}
	if got := commandMaxParallelSourceOps(1); got != 1 {
		t.Fatalf("commandMaxParallelSourceOps(1) = %d, want 1", got)
	}
}
