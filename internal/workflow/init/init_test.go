package initworkflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/effect/mutation"
	daempaths "github.com/isty2e/daem/internal/paths"
)

func TestBuildPlanCreatesStarterManifest(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "daem.toml")

	plan, err := BuildPlan(context.Background(), Input{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("BuildPlan returned error: %v", err)
	}
	if plan.ManifestPath != manifestPath {
		t.Fatalf("ManifestPath = %q, want %q", plan.ManifestPath, manifestPath)
	}
	if plan.Action != ActionCreate {
		t.Fatalf("Action = %q, want %q", plan.Action, ActionCreate)
	}
	if string(plan.Content) != string(declarationmanifest.StarterContent()) {
		t.Fatalf("Content = %q, want starter manifest", plan.Content)
	}
}

func TestBuildPlanRejectsExistingManifestWithoutForce(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "daem.toml")
	if err := os.WriteFile(manifestPath, []byte("version = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := BuildPlan(context.Background(), Input{ManifestPath: manifestPath})
	if err == nil {
		t.Fatal("BuildPlan returned nil error for existing manifest without force")
	}
}

func TestWritePlanForceReplacesMalformedManifestWithExistingMode(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "daem.toml")
	if err := os.WriteFile(manifestPath, []byte("[malformed\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	plan := Plan{
		ManifestPath: manifestPath,
		Content:      declarationmanifest.StarterContent(),
		Action:       ActionOverwrite,
	}

	if err := writePlan(context.Background(), plan, true); err != nil {
		t.Fatalf("writePlan returned error: %v", err)
	}
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(declarationmanifest.StarterContent()) {
		t.Fatalf("content = %q, want starter manifest", content)
	}
	info, err := os.Stat(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v, want 0640", info.Mode().Perm())
	}
}

func TestWritePlanWithoutForceDoesNotOverwriteFileCreatedAfterPlanning(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "daem.toml")
	plan := Plan{
		ManifestPath: manifestPath,
		Content:      declarationmanifest.StarterContent(),
		Action:       ActionCreate,
	}
	if err := os.WriteFile(manifestPath, []byte("existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := writePlan(context.Background(), plan, false)
	if err == nil {
		t.Fatal("writePlan returned nil error for existing manifest without force")
	}
	content, readErr := os.ReadFile(manifestPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "existing\n" {
		t.Fatalf("content = %q, want existing content", content)
	}
}

func TestExecuteWaitCancellationLeavesManifestAbsent(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	manifestPath := filepath.Join(root, "project", "daem.toml")
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	store, err := mutation.NewStore(paths.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	domain, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{
		Path: manifestPath, Access: mutation.AccessExclusive, Effect: mutation.PathEffectDirectoryEntry,
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
	_, err = Execute(ctx, Input{ManifestPath: manifestPath})
	var canceled mutation.CancellationError
	if !errors.As(err, &canceled) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Execute error = %v, want typed deadline cancellation", err)
	}
	if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
		t.Fatalf("manifest exists after canceled wait: %v", err)
	}
}
