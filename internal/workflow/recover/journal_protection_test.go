//go:build darwin || linux

package recover

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/fileset"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	"github.com/isty2e/daem/internal/output/ownership"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/test/ownershiptest"
	"github.com/isty2e/daem/test/testkit/metadatatx"
)

func TestRecoverActiveJournalBeforeDisjointPermissionRecovery(t *testing.T) {
	fixture := prepareRecoveryFixture(t, true)
	paths, err := daempaths.Resolve(fixture.input.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	safety := ownershiptest.ArmPermissionRecovery(t, paths, ".codex/config.toml")
	protected := safety.Documents[0]
	protectedContent, err := os.ReadFile(protected)
	if err != nil {
		t.Fatal(err)
	}
	registryBefore, err := os.ReadFile(paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatal(err)
	}

	planned, err := Plan(t.Context(), fixture.input)
	if err != nil {
		t.Fatalf("plan disjoint journal recovery: %v", err)
	}
	t.Cleanup(func() { _ = planned.Close() })
	if plan := planned.Disclosure(); plan == nil || plan.AuthorityKind() != journal.RecoveryAuthorityActiveJournal || plan.HasErrors() {
		t.Fatal("expected an executable journal plan")
	}
	if _, selected := planned.PermissionDisclosure(); selected {
		t.Fatal("journal plan also selected permission recovery")
	}
	if got, err := os.ReadFile(fixture.hostPath); err != nil || !bytes.Equal(got, fixture.newContent) {
		t.Fatalf("planning changed the live output: %q, %v", got, err)
	}
	result, err := Execute(t.Context(), planned, ExecuteOptions{})
	if err != nil || result.Phase() != ExecutionPhaseCompleted || result.AuthorityRetained() {
		t.Fatalf("journal recovery: phase=%v, err=%v", result.Phase(), err)
	}
	if got, err := os.ReadFile(fixture.hostPath); err != nil || !bytes.Equal(got, fixture.oldContent) {
		t.Fatalf("journal did not restore output: %q, %v", got, err)
	}
	if got, err := os.ReadFile(paths.OwnershipRegistryPath); err != nil || !bytes.Equal(got, registryBefore) {
		t.Fatal("journal recovery changed the unrelated protection or binding", err)
	}
	if info, err := os.Stat(protected); err != nil || info.Mode().Perm() != 0o644 {
		t.Fatal("journal recovery changed protected file permissions", err)
	}

	next, err := Plan(t.Context(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = next.Close() })
	if plan, selected := next.PermissionDisclosure(); !selected || plan.HasErrors() || next.Disclosure() != nil {
		t.Fatal("new recovery did not select the retained permission work")
	}
	final, err := Execute(t.Context(), next, ExecuteOptions{})
	permission, selected := final.PermissionResult()
	if err != nil || !selected || !permission.Complete() {
		t.Fatal("permission recovery failed", err)
	}
	if got, err := os.ReadFile(protected); err != nil || !bytes.Equal(got, protectedContent) {
		t.Fatal("permission recovery rewrote the protected file", err)
	}
	if info, err := os.Stat(protected); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("permission recovery did not restrict access", err)
	}
	if rows, err := safety.Store.LoadPendingProtections(t.Context()); err != nil || len(rows) != 0 {
		t.Fatal("permission recovery did not retire its protection", err)
	}
}

func TestRecoverRefusesProtectionThatIntersectsJournalRecovery(t *testing.T) {
	for _, afterPreview := range []bool{false, true} {
		name := "at preview"
		if afterPreview {
			name = "after preview"
		}
		t.Run(name, func(t *testing.T) {
			fixture := prepareRecoveryFixture(t, true)
			paths, err := daempaths.Resolve(fixture.input.ManifestPath)
			if err != nil {
				t.Fatal(err)
			}
			var plan *PreparedRecovery
			if afterPreview {
				plan, err = Plan(t.Context(), fixture.input)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = plan.Close() })
			}
			safety := ownershiptest.ArmPermissionRecovery(t, paths, filepath.Base(fixture.hostPath))
			if afterPreview {
				_, err = Execute(t.Context(), plan, ExecuteOptions{})
			} else {
				var blocked *PreparedRecovery
				blocked, err = Plan(t.Context(), fixture.input)
				if blocked != nil {
					_ = blocked.Close()
				}
			}
			var conflict *ownership.ProtectionPendingError
			if !errors.As(err, &conflict) || !conflict.Protection.Equal(safety.Protection) {
				t.Fatalf("want the actual protection conflict, got %v", err)
			}
			if got, err := os.ReadFile(fixture.hostPath); err != nil || !bytes.Equal(got, fixture.newContent) {
				t.Fatalf("refusal changed the live output: %q, %v", got, err)
			}
			if _, err := os.Stat(fixture.operationDir); err != nil {
				t.Fatal("refusal removed the journal", err)
			}
			rows, err := safety.Store.LoadPendingProtections(t.Context())
			if err != nil || len(rows) != 1 || !rows[0].Equal(safety.Protection) {
				t.Fatal("refusal changed the protection", err)
			}
		})
	}
}

func TestRecoverCanRetireJournalWithoutChangingGuardedProtectedFile(t *testing.T) {
	fixture := prepareRecoveryFixture(t, false)
	paths, err := daempaths.Resolve(fixture.input.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	safety := ownershiptest.ArmPermissionRecovery(t, paths, filepath.Base(fixture.hostPath))
	planned, err := Plan(t.Context(), fixture.input)
	if err != nil {
		t.Fatal("observing an unchanged file is not a conflicting write", err)
	}
	result, err := Execute(t.Context(), planned, ExecuteOptions{})
	if err != nil || result.Phase() != ExecutionPhaseCompleted {
		t.Fatalf("retire without host or registry writes: %s, %v", result.Phase(), err)
	}
	if rows, err := safety.Store.LoadPendingProtections(t.Context()); err != nil || len(rows) != 1 || !rows[0].Equal(safety.Protection) {
		t.Fatal("retirement did not retain the protection", err)
	}
	if info, err := os.Stat(fixture.hostPath); err != nil || info.Mode().Perm() != 0o644 {
		t.Fatal("retirement changed permissions", err)
	}
}

func TestRecoverRetiredJournalCleanupPrecedesPermissionRecovery(t *testing.T) {
	fixture := prepareCleanupRecoveryFixture(t, retirement.PhaseFinalizing, true)
	safety := ownershiptest.ArmPermissionRecovery(t, fixture.paths, ".codex/config.toml")
	planned, err := Plan(t.Context(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	if plan := planned.Disclosure(); plan == nil || plan.AuthorityKind() != journal.RecoveryAuthorityJournalCleanup {
		t.Fatal("expected cleanup for the already-retired journal first")
	}
	if _, err := Execute(t.Context(), planned, ExecuteOptions{}); err != nil {
		t.Fatal(err)
	}
	if rows, err := safety.Store.LoadPendingProtections(t.Context()); err != nil || len(rows) != 1 || !rows[0].Equal(safety.Protection) {
		t.Fatal("retired-journal cleanup changed protection", err)
	}
}

func TestRecoverPreservesIndependentFileSetFenceBeforePermissionRecovery(t *testing.T) {
	fixture := prepareRecoveryFixture(t, true)
	paths, err := daempaths.Resolve(fixture.input.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	safety := ownershiptest.ArmPermissionRecovery(t, paths, ".codex/config.toml")
	metadatatx.WriteInterrupted(t, paths.StateDir)

	active, err := Plan(t.Context(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	if fence, observed := active.ContinuingFileSetFence(); !observed || fence != fileset.FileSetFencePublishedTransaction {
		t.Fatalf("independent transaction was not disclosed: %s", fence)
	}
	result, err := Execute(t.Context(), active, ExecuteOptions{})
	if err != nil || result.Phase() != ExecutionPhaseCompleted || result.FileSetFenceObservation().Kind() != fileset.FileSetFencePublishedTransaction {
		t.Fatalf("journal did not settle independently of the file set: %s, %v", result.Phase(), err)
	}
	blocked, err := Plan(t.Context(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	if safetyPlan, selected := blocked.PermissionDisclosure(); !selected || !safetyPlan.HasErrors() {
		t.Fatal("permission recovery bypassed an independent file-set fence")
	}
	if _, err := Execute(t.Context(), blocked, ExecuteOptions{}); fileset.FileSetFenceKindOf(err) != fileset.FileSetFencePublishedTransaction {
		t.Fatalf("permission recovery did not retain the actual file-set blocker: %v", err)
	}
	if rows, err := safety.Store.LoadPendingProtections(t.Context()); err != nil || len(rows) != 1 || !rows[0].Equal(safety.Protection) {
		t.Fatal("file-set refusal changed protection", err)
	}
	if info, err := os.Stat(safety.Documents[0]); err != nil || info.Mode().Perm() != 0o644 {
		t.Fatal("file-set refusal changed permissions", err)
	}
}
