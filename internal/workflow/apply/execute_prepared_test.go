package apply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/reconcile"
	targetpkg "github.com/isty2e/daem/internal/target"
)

func TestExecuteWithOptionsPassesExecuteEvents(t *testing.T) {
	tempDir := t.TempDir()
	paths := applyTestPaths(t, tempDir)
	writeApplyManifestFile(t, paths.ManifestPath)
	writeApplyFile(t, filepath.Join(tempDir, "instructions", "AGENTS.md"), "shared instructions\n")
	instructionHash := hashApplyPath(t, filepath.Join(tempDir, "instructions", "AGENTS.md"))
	writeApplyLockfile(t, paths.LockfilePath, applyInstructionLockfile(t, "project", "local:instructions/AGENTS.md?mode=vendor", instructionHash))
	planned, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: paths.ManifestPath,
		LockfilePath: paths.LockfilePath,
	})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}

	var events []execute.Event
	result, err := ExecuteWithOptions(context.Background(), planned, ExecuteOptions{
		ExecuteEvents: func(event execute.Event) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("ExecuteWithOptions returned error: %v", err)
	}
	if result.ActionCount != 1 {
		t.Fatalf("ActionCount = %d, want 1", result.ActionCount)
	}
	assertApplyFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "shared instructions\n")
	assertWorkflowApplyEventKinds(t, events, execute.EventJournalCaptureStarted, execute.EventActionStarted, execute.EventJournalCleaned)
}

func TestExecuteWithOptionsLeavesPrivateRecoveryResidueUntouched(t *testing.T) {
	tempDir := t.TempDir()
	paths := applyTestPaths(t, tempDir)
	writeApplyManifestFile(t, paths.ManifestPath)
	sourcePath := filepath.Join(tempDir, "instructions", "AGENTS.md")
	writeApplyFile(t, sourcePath, "shared instructions\n")
	writeApplyLockfile(t, paths.LockfilePath, applyInstructionLockfile(
		t,
		"project",
		"local:instructions/AGENTS.md?mode=vendor",
		hashApplyPath(t, sourcePath),
	))
	privateDirectory := filepath.Join(paths.RecoveryDir, ".private-build-residue")
	if err := os.MkdirAll(privateDirectory, 0o700); err != nil {
		t.Fatalf("create private recovery directory: %v", err)
	}
	privateFile := filepath.Join(paths.RecoveryDir, ".foreign-private-file")
	if err := os.WriteFile(privateFile, []byte("foreign"), 0o600); err != nil {
		t.Fatalf("write private recovery file: %v", err)
	}

	planned, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: paths.ManifestPath,
		LockfilePath: paths.LockfilePath,
	})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	result, err := ExecuteWithOptions(context.Background(), planned, ExecuteOptions{})
	if err != nil {
		t.Fatalf("ExecuteWithOptions returned error: %v", err)
	}
	if result.ActionCount != 1 {
		t.Fatalf("ActionCount = %d, want 1", result.ActionCount)
	}
	assertApplyFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "shared instructions\n")
	if _, err := os.Stat(paths.StatefilePath); err != nil {
		t.Fatalf("statefile stat after apply: %v", err)
	}
	for _, path := range []string{privateDirectory, privateFile} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("private recovery residue %q changed: %v", path, err)
		}
	}
	entries, err := os.ReadDir(paths.RecoveryDir)
	if err != nil {
		t.Fatalf("read recovery directory: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("recovery entries = %#v, want only two private residues", entries)
	}
}

func TestExecuteWithOptionsRejectsChangedDisclosedPlanBeforeEffects(t *testing.T) {
	tempDir := t.TempDir()
	paths := applyTestPaths(t, tempDir)
	writeApplyManifestFile(t, paths.ManifestPath)
	sourcePath := filepath.Join(tempDir, "instructions", "AGENTS.md")
	writeApplyFile(t, sourcePath, "first\n")
	writeApplyLockfile(t, paths.LockfilePath, applyInstructionLockfile(
		t,
		"project",
		"local:instructions/AGENTS.md?mode=vendor",
		hashApplyPath(t, sourcePath),
	))
	planned, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: paths.ManifestPath,
		LockfilePath: paths.LockfilePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	writeApplyFile(t, sourcePath, "changed after disclosure\n")

	_, err = ExecuteWithOptions(context.Background(), planned, ExecuteOptions{PlanWasDisclosed: true})
	var stale mutation.StalePlanError
	if !errors.As(err, &stale) {
		t.Fatalf("ExecuteWithOptions error = %v, want StalePlanError", err)
	}
	if _, statErr := os.Stat(filepath.Join(tempDir, "AGENTS.md")); !os.IsNotExist(statErr) {
		t.Fatalf("destination stat error = %v, want absent", statErr)
	}
	if _, statErr := os.Stat(paths.StatefilePath); !os.IsNotExist(statErr) {
		t.Fatalf("statefile stat error = %v, want absent", statErr)
	}
	if _, statErr := os.Stat(paths.RecoveryDir); !os.IsNotExist(statErr) {
		t.Fatalf("recovery directory stat error = %v, want absent", statErr)
	}
}

func TestExecuteWithOptionsRejectsDeclarationRevisionChangeAfterDisclosure(
	t *testing.T,
) {
	for _, selected := range []string{"manifest", "lockfile"} {
		t.Run(selected, func(t *testing.T) {
			tempDir := t.TempDir()
			paths := applyTestPaths(t, tempDir)
			writeApplyManifestFile(t, paths.ManifestPath)
			sourcePath := filepath.Join(tempDir, "instructions", "AGENTS.md")
			writeApplyFile(t, sourcePath, "unchanged\n")
			writeApplyLockfile(t, paths.LockfilePath, applyInstructionLockfile(
				t,
				"project",
				"local:instructions/AGENTS.md?mode=vendor",
				hashApplyPath(t, sourcePath),
			))
			planned, err := PlanWrite(t.Context(), CommandInput{
				ManifestPath: paths.ManifestPath,
				LockfilePath: paths.LockfilePath,
			})
			if err != nil {
				t.Fatal(err)
			}

			selectedPath := paths.ManifestPath
			if selected == "lockfile" {
				selectedPath = paths.LockfilePath
			}
			content, err := os.ReadFile(selectedPath)
			if err != nil {
				t.Fatalf("read selected declaration: %v", err)
			}
			writeApplyFile(t, selectedPath, string(content)+"\n# changed after disclosure\n")

			_, err = ExecuteWithOptions(
				t.Context(),
				planned,
				ExecuteOptions{PlanWasDisclosed: true},
			)
			var stale mutation.StalePlanError
			if !errors.As(err, &stale) {
				t.Fatalf("ExecuteWithOptions error = %v, want StalePlanError", err)
			}
			if _, statErr := os.Stat(filepath.Join(tempDir, "AGENTS.md")); !os.IsNotExist(statErr) {
				t.Fatalf("destination stat error = %v, want absent", statErr)
			}
		})
	}
}

func TestExecuteWithOptionsRejectsIdenticalReplacementRootAfterDisclosure(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create project root: %v", err)
	}
	t.Setenv("XDG_DATA_HOME", filepath.Join(parent, "data"))
	paths := applyTestPaths(t, root)
	writeApplyManifestFile(t, paths.ManifestPath)
	sourcePath := filepath.Join(root, "instructions", "AGENTS.md")
	writeApplyFile(t, sourcePath, "unchanged\n")
	locked := applyInstructionLockfile(
		t,
		"project",
		"local:instructions/AGENTS.md?mode=vendor",
		hashApplyPath(t, sourcePath),
	)
	writeApplyLockfile(t, paths.LockfilePath, locked)
	planned, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: paths.ManifestPath,
		LockfilePath: paths.LockfilePath,
	})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}

	original := filepath.Join(parent, "original-project")
	if err := os.Rename(root, original); err != nil {
		t.Fatalf("move disclosed project root: %v", err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create replacement project root: %v", err)
	}
	replacementPaths := applyTestPaths(t, root)
	writeApplyManifestFile(t, replacementPaths.ManifestPath)
	replacementSource := filepath.Join(root, "instructions", "AGENTS.md")
	writeApplyFile(t, replacementSource, "unchanged\n")
	writeApplyLockfile(t, replacementPaths.LockfilePath, locked)

	_, err = ExecuteWithOptions(context.Background(), planned, ExecuteOptions{PlanWasDisclosed: true})
	var stale mutation.StalePlanError
	if !errors.As(err, &stale) {
		t.Fatalf("ExecuteWithOptions error = %v, want StalePlanError", err)
	}
	for _, path := range []string{
		filepath.Join(root, "AGENTS.md"),
		filepath.Join(original, "AGENTS.md"),
		replacementPaths.StatefilePath,
		filepath.Join(original, ".daem", "state.json"),
	} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("%s stat error = %v, want absent", path, statErr)
		}
	}
}

func TestExecuteWithOptionsRejectsClosedWritePlanBeforeEffects(t *testing.T) {
	tempDir := t.TempDir()
	paths := applyTestPaths(t, tempDir)
	writeApplyManifestFile(t, paths.ManifestPath)
	sourcePath := filepath.Join(tempDir, "instructions", "AGENTS.md")
	writeApplyFile(t, sourcePath, "content\n")
	writeApplyLockfile(t, paths.LockfilePath, applyInstructionLockfile(
		t,
		"project",
		"local:instructions/AGENTS.md?mode=vendor",
		hashApplyPath(t, sourcePath),
	))
	planned, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: paths.ManifestPath,
		LockfilePath: paths.LockfilePath,
	})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	if err := planned.Close(); err != nil {
		t.Fatalf("close write plan: %v", err)
	}

	_, err = ExecuteWithOptions(context.Background(), planned, ExecuteOptions{})
	if !errors.Is(err, ErrPreparedWriteClosed) {
		t.Fatalf("ExecuteWithOptions error = %v, want ErrPreparedWriteClosed", err)
	}
	for _, path := range []string{filepath.Join(tempDir, "AGENTS.md"), paths.StatefilePath, paths.RecoveryDir} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("%s stat error = %v, want absent", path, statErr)
		}
	}
}

func TestExecuteWithOptionsUsesStaleSnapshotForUndisclosedCandidate(t *testing.T) {
	tempDir := t.TempDir()
	paths := applyTestPaths(t, tempDir)
	writeApplyManifestFile(t, paths.ManifestPath)
	sourcePath := filepath.Join(tempDir, "instructions", "AGENTS.md")
	writeApplyFile(t, sourcePath, "first\n")
	writeApplyLockfile(t, paths.LockfilePath, applyInstructionLockfile(
		t,
		"project",
		"local:instructions/AGENTS.md?mode=vendor",
		hashApplyPath(t, sourcePath),
	))
	planned, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: paths.ManifestPath,
		LockfilePath: paths.LockfilePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	writeApplyFile(t, sourcePath, "changed before execution\n")

	_, err = ExecuteWithOptions(context.Background(), planned, ExecuteOptions{})
	var stale mutation.StaleSnapshotError
	if !errors.As(err, &stale) {
		t.Fatalf("ExecuteWithOptions error = %v, want StaleSnapshotError", err)
	}
}

func TestExecuteIgnoresMutatedDisclosureAndUsesPrivatePreparedPlan(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(tempDir, "data"))
	paths := applyTestPaths(t, tempDir)
	writeApplyManifestFile(t, paths.ManifestPath)
	sourcePath := filepath.Join(tempDir, "instructions", "AGENTS.md")
	writeApplyFile(t, sourcePath, "content\n")
	writeApplyLockfile(t, paths.LockfilePath, applyInstructionLockfile(
		t,
		"project",
		"local:instructions/AGENTS.md?mode=vendor",
		hashApplyPath(t, sourcePath),
	))
	planned, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: paths.ManifestPath,
		LockfilePath: paths.LockfilePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	planned.Reconciliation.ManagedPaths()[0] = reconcile.ManagedPathDecision{}

	result, err := ExecuteWithOptions(context.Background(), planned, ExecuteOptions{})
	if err != nil {
		t.Fatalf("ExecuteWithOptions returned error: %v", err)
	}
	if result.ActionCount != 1 {
		t.Fatalf("action count = %d, want 1", result.ActionCount)
	}
	content, err := os.ReadFile(filepath.Join(tempDir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read canonical destination: %v", err)
	}
	if string(content) != "content\n" {
		t.Fatalf("canonical destination content = %q, want %q", content, "content\n")
	}
	if _, statErr := os.Stat(filepath.Join(tempDir, "OTHER.md")); !os.IsNotExist(statErr) {
		t.Fatalf("OTHER.md stat error = %v, want absent", statErr)
	}
	if _, statErr := os.Stat(paths.StatefilePath); statErr != nil {
		t.Fatalf("statefile stat error = %v, want present", statErr)
	}
}

func TestExecuteCancellationWhileWaitingForDestinationLeaseStartsNoEffect(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(tempDir, "data"))
	paths := applyTestPaths(t, tempDir)
	writeApplyManifestFile(t, paths.ManifestPath)
	sourcePath := filepath.Join(tempDir, "instructions", "AGENTS.md")
	writeApplyFile(t, sourcePath, "content\n")
	writeApplyLockfile(t, paths.LockfilePath, applyInstructionLockfile(
		t,
		"project",
		"local:instructions/AGENTS.md?mode=vendor",
		hashApplyPath(t, sourcePath),
	))
	planned, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: paths.ManifestPath,
		LockfilePath: paths.LockfilePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := mutation.NewStore(paths.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(tempDir, "AGENTS.md")
	domain, err := mutation.NewPhysicalPathDomain(mutation.PhysicalPathRequest{
		Path: destination, Access: mutation.AccessExclusive, Effect: mutation.PathEffectDirectoryEntry,
		Target: string(targetpkg.TargetCodex), Scope: string(targetpkg.ScopeProject),
	})
	if err != nil {
		t.Fatal(err)
	}
	holder, err := store.Acquire(context.Background(), domain)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = ExecuteWithOptions(ctx, planned, ExecuteOptions{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ExecuteWithOptions error = %v, want deadline cancellation", err)
	}
	for _, path := range []string{destination, paths.StatefilePath, paths.RecoveryDir} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("%s stat error = %v, want absent", path, statErr)
		}
	}
}
