package apply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
)

func TestPlanWriteMissingLockfileReturnsPartialPathResult(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "missing.lock.toml")
	writeApplyManifestFile(t, manifestPath)

	result, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		LockfilePath: lockfilePath,
	})
	if err == nil {
		t.Fatalf("PlanWrite returned nil error")
	}
	if !errors.Is(err, ErrReadLockfile) || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error = %v, want read-lockfile not-exist error", err)
	}
	if result.ManifestPath != manifestPath {
		t.Fatalf("ManifestPath = %q, want %q", result.ManifestPath, manifestPath)
	}
	if result.LockfilePath != lockfilePath {
		t.Fatalf("LockfilePath = %q, want %q", result.LockfilePath, lockfilePath)
	}
	if result.StatefilePath == "" {
		t.Fatalf("StatefilePath is empty")
	}
	if result.ReconciliationReady {
		t.Fatalf("PlanReady = true, want false before lockfile load succeeds")
	}
}

func TestPlanWriteNormalizesExplicitLockfilePathAtIngress(t *testing.T) {
	root := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	manifestPath := filepath.Join(root, "daem.toml")
	writeApplyManifestFile(t, manifestPath)

	result, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		LockfilePath: "relative.lock.toml",
	})
	if err == nil {
		t.Fatal("PlanWrite returned nil error for missing lockfile")
	}
	want, err := filepath.Abs("relative.lock.toml")
	if err != nil {
		t.Fatal(err)
	}
	if got := result.LockfilePath; got != want {
		t.Fatalf("LockfilePath = %q, want %q", got, want)
	}
}

func TestPlanWriteUnsupportedReadinessReturnsPartialPlanResult(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	writeApplyManifestFile(t, manifestPath)
	writeApplyLockfile(t, lockfilePath, lock.File{Version: lock.CurrentVersion})

	result, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		LockfilePath: lockfilePath,
	})
	if err == nil {
		t.Fatalf("PlanWrite returned nil error")
	}
	if !result.ReconciliationReady {
		t.Fatalf("PlanReady = false, want partial plan for JSON error rendering")
	}
	if result.StatefilePath == "" {
		t.Fatalf("StatefilePath is empty")
	}
	if len(result.Reconciliation.ManagedPaths()) != 1 {
		t.Fatalf("managed paths = %#v, want one readiness error decision", result.Reconciliation.ManagedPaths())
	}
	decision := result.Reconciliation.ManagedPaths()[0]
	if !decision.IsBlocked() || decision.Reason() != reconcile.ReasonMissingLock {
		t.Fatalf("decision = %#v, want missing-lock blocked decision", decision)
	}
}
