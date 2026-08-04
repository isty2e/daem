package lock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/realization/lockfile"
)

func TestRunLockWriteCreatesLockfileAndResult(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	writeWorkflowTestFile(t, tempDir, "instructions/project.md", "project instructions\n")
	writeWorkflowTestFile(t, tempDir, "daem.toml", `version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/project.md"
targets = ["codex"]
`)

	result, err := RunLock(context.Background(), LockInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	if result.ManifestPath != manifestPath || result.LockfilePath != lockfilePath {
		t.Fatalf("result paths = %q/%q, want %q/%q", result.ManifestPath, result.LockfilePath, manifestPath, lockfilePath)
	}
	if result.PreviousFound {
		t.Fatal("PreviousFound = true, want false")
	}
	subjects := result.Lockfile.Locked.Subjects()
	if len(subjects) != 2 {
		t.Fatalf("locked subjects = %#v, want Instructions Supply and projection", subjects)
	}
	for _, subject := range subjects {
		if subject.EntityID().Kind() != entity.KindInstructions {
			t.Fatalf("locked subject = %#v, want Instructions entity", subject)
		}
	}
	if !result.Delta.HasChanges() {
		t.Fatal("Delta.HasChanges() = false, want true")
	}
	if _, err := lockfile.Load(lockfilePath); err != nil {
		t.Fatalf("Load written lockfile returned error: %v", err)
	}
}

func TestRunLockDryRunDoesNotWriteExplicitLockfile(t *testing.T) {
	tempDir := t.TempDir()
	dataRoot := filepath.Join(tempDir, "data")
	t.Setenv("XDG_DATA_HOME", dataRoot)
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "locks", "custom.lock.toml")
	writeWorkflowTestFile(t, tempDir, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: oracle\n---\n")
	writeWorkflowTestFile(t, tempDir, "daem.toml", `version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]
`)

	result, err := RunLock(context.Background(), LockInput{
		ManifestPath: manifestPath,
		LockfilePath: lockfilePath,
		DryRun:       true,
	})
	if err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	if result.LockfilePath != lockfilePath {
		t.Fatalf("LockfilePath = %q, want %q", result.LockfilePath, lockfilePath)
	}
	if result.PreviousFound {
		t.Fatal("PreviousFound = true, want false")
	}
	if _, err := os.Stat(lockfilePath); !os.IsNotExist(err) {
		t.Fatalf("dry-run lockfile exists or stat failed unexpectedly: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataRoot, "daem", "locks", "mutation")); !os.IsNotExist(err) {
		t.Fatalf("dry-run mutation store exists or stat failed unexpectedly: %v", err)
	}
}

func TestRunLockReplacesSchemaV5WithoutInterpretingPriorContents(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(tempDir, "data"))
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	writeWorkflowTestFile(t, tempDir, "instructions/project.md", "project instructions\n")
	writeWorkflowTestFile(t, tempDir, "daem.toml", `version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/project.md"
targets = ["codex"]
`)
	const legacy = "version = 5\nlegacy_payload = \"unread\"\n"
	writeWorkflowTestFile(t, tempDir, "daem.lock.toml", legacy)

	preview, err := RunLock(context.Background(), LockInput{
		ManifestPath: manifestPath,
		DryRun:       true,
	})
	if err != nil {
		t.Fatalf("dry-run RunLock returned error: %v", err)
	}
	if !preview.PreviousFound {
		t.Fatal("dry-run PreviousFound = false for replaceable v5 lockfile")
	}
	if got, err := os.ReadFile(lockfilePath); err != nil || string(got) != legacy {
		t.Fatalf("v5 lockfile after successful dry-run = %q, %v", got, err)
	}

	result, err := RunLock(context.Background(), LockInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	if !result.PreviousFound {
		t.Fatal("PreviousFound = false for replaced v5 lockfile")
	}
	loaded, err := lockfile.Load(lockfilePath)
	if err != nil {
		t.Fatalf("Load replaced lockfile returned error: %v", err)
	}
	if loaded.Version != contractversion.LockfileSchema {
		t.Fatalf("replaced lockfile version = %d, want %d", loaded.Version, contractversion.LockfileSchema)
	}
}

func TestRunLockReplacesReleasedSchemaV3WithoutInterpretingPriorContents(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(tempDir, "data"))
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	writeWorkflowTestFile(t, tempDir, "instructions/project.md", "project instructions\n")
	writeWorkflowTestFile(t, tempDir, "daem.toml", `version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/project.md"
targets = ["codex"]
`)
	const released = "version = 3\nlegacy_payload = \"unread\"\n"
	writeWorkflowTestFile(t, tempDir, "daem.lock.toml", released)

	result, err := RunLock(context.Background(), LockInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	if !result.PreviousFound {
		t.Fatal("PreviousFound = false for replaced released schema-v3 lockfile")
	}
	loaded, err := lockfile.Load(lockfilePath)
	if err != nil {
		t.Fatalf("Load replaced lockfile returned error: %v", err)
	}
	if loaded.Version != contractversion.LockfileSchema {
		t.Fatalf("replaced lockfile version = %d, want %d", loaded.Version, contractversion.LockfileSchema)
	}
}

func TestRunLockRejectsFutureSchemaWithoutInterpretingOrReplacingIt(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(tempDir, "data"))
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	writeWorkflowTestFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")
	const future = "version = 7\nfuture_payload = { malformed = [\n"
	writeWorkflowTestFile(t, tempDir, "daem.lock.toml", future)

	for _, dryRun := range []bool{true, false} {
		_, err := RunLock(context.Background(), LockInput{
			ManifestPath: manifestPath,
			DryRun:       dryRun,
		})
		if err == nil {
			t.Fatalf("RunLock(dry-run=%t) returned nil error", dryRun)
		}
		for _, want := range []string{"unsupported lockfile version 7", "newer daem"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("RunLock(dry-run=%t) error = %q, want %q", dryRun, err, want)
			}
		}
		if got, readErr := os.ReadFile(lockfilePath); readErr != nil || string(got) != future {
			t.Fatalf("future lockfile after RunLock(dry-run=%t) = %q, %v", dryRun, got, readErr)
		}
	}
}

func TestRunLockDryRunAndFailedRelockPreserveSchemaV5Bytes(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(tempDir, "data"))
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	const legacy = "version = 5\nlegacy_payload = \"unread\"\n"
	writeWorkflowTestFile(t, tempDir, "daem.lock.toml", legacy)
	writeWorkflowTestFile(t, tempDir, "daem.toml", `version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/missing.md"
targets = ["codex"]
`)

	if _, err := RunLock(context.Background(), LockInput{
		ManifestPath: manifestPath,
		DryRun:       true,
	}); err == nil {
		t.Fatal("dry-run relock succeeded with missing source")
	}
	if got, err := os.ReadFile(lockfilePath); err != nil || string(got) != legacy {
		t.Fatalf("v5 lockfile after failed dry-run = %q, %v", got, err)
	}

	if _, err := RunLock(context.Background(), LockInput{ManifestPath: manifestPath}); err == nil {
		t.Fatal("relock succeeded with missing source")
	}
	if got, err := os.ReadFile(lockfilePath); err != nil || string(got) != legacy {
		t.Fatalf("v5 lockfile after failed write = %q, %v", got, err)
	}
}

func TestRunLockRejectsManifestDriftDuringBuildWithoutReplacingLockfile(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(tempDir, "data"))
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	writeWorkflowTestFile(t, tempDir, "instructions/project.md", "project instructions\n")
	manifest := `version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/project.md"
targets = ["codex"]
`
	writeWorkflowTestFile(t, tempDir, "daem.toml", manifest)
	if _, err := RunLock(context.Background(), LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatalf("create baseline lockfile: %v", err)
	}
	baseline, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatal(err)
	}

	var mutateOnce sync.Once
	_, err = RunLock(context.Background(), LockInput{
		ManifestPath: manifestPath,
		LockEvents: func(ProgressEvent) {
			mutateOnce.Do(func() {
				if writeErr := os.WriteFile(manifestPath, []byte(manifest+"\n# external edit\n"), 0o600); writeErr != nil {
					t.Errorf("mutate manifest: %v", writeErr)
				}
			})
		},
	})
	var stale mutation.StaleSnapshotError
	if !errors.As(err, &stale) {
		t.Fatalf("RunLock error = %v, want StaleSnapshotError", err)
	}
	content, readErr := os.ReadFile(lockfilePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != string(baseline) {
		t.Fatal("lockfile changed after stale manifest detection")
	}
}

func TestLockRevisionRequestsGuardReadReferentAndReplacedEntry(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "daem.toml")
	lockfilePath := filepath.Join(t.TempDir(), "daem.lock.toml")
	metadataTransactionPath := filepath.Join(t.TempDir(), "metadata-transaction")
	requests := lockRevisionRequests(manifestPath, lockfilePath, metadataTransactionPath, nil)
	for _, path := range []string{manifestPath, lockfilePath} {
		wantEffects := map[mutation.PathEffect]bool{
			mutation.PathEffectDirectoryEntry: false,
			mutation.PathEffectReferent:       false,
		}
		for _, request := range requests {
			if request.Path == path {
				wantEffects[request.Effect] = true
			}
		}
		for effect, found := range wantEffects {
			if !found {
				t.Fatalf("path %q revision effect %d missing from %#v", path, effect, requests)
			}
		}
	}
}

func TestLockManifestEntryLeaseConflictsWithSymlinkReplacement(t *testing.T) {
	root := t.TempDir()
	store, err := mutation.NewStore(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	referent := filepath.Join(root, "manifest-target.toml")
	manifestPath := filepath.Join(root, "daem.toml")
	if err := os.WriteFile(referent, []byte("version = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(referent, manifestPath); err != nil {
		t.Fatal(err)
	}
	writer, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{
		Path: manifestPath, Access: mutation.AccessExclusive, Effect: mutation.PathEffectDirectoryEntry,
	})
	if err != nil {
		t.Fatal(err)
	}
	holder, err := store.Acquire(context.Background(), writer)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()

	domains, err := lockMutationDomains(
		manifestPath,
		filepath.Join(root, "daem.lock.toml"),
		filepath.Join(root, "metadata-transaction"),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = store.Acquire(ctx, domains...)
	var canceled mutation.CancellationError
	if !errors.As(err, &canceled) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Acquire error = %v, want manifest entry cancellation", err)
	}
}

func TestProgressEventSinkKeepsNilAndPanicSemantics(t *testing.T) {
	var nilSink ProgressEventSink
	nilSink.emit(ProgressEvent{Kind: "ignored"})

	defer func() {
		if recovered := recover(); recovered != "sink panic" {
			t.Fatalf("recovered = %#v, want sink panic", recovered)
		}
	}()
	ProgressEventSink(func(ProgressEvent) {
		panic("sink panic")
	}).emit(ProgressEvent{Kind: "resource_locked"})
}
