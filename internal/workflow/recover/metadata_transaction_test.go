package recover

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/test/testkit/metadatatx"
)

func TestRecoverNoJournalDoesNotHijackFileSetFence(t *testing.T) {
	t.Run("published marker", func(t *testing.T) {
		manifestPath := filepath.Join(t.TempDir(), "daem.toml")
		paths, err := daempaths.Resolve(manifestPath)
		if err != nil {
			t.Fatal(err)
		}
		metadatatx.WriteInterrupted(t, paths.StateDir)

		_, err = Plan(context.Background(), PlanInput{ManifestPath: manifestPath})
		if !errors.Is(err, journal.ErrNoRecoverableJournal) {
			t.Fatalf("error = %v, want ErrNoRecoverableJournal", err)
		}
		if errors.Is(err, transaction.ErrAbandonedFileSetResidue) ||
			strings.Contains(err.Error(), "interrupted file-set transaction") {
			t.Fatalf("error = %v, must not replace missing journal with file-set fence", err)
		}
	})

	t.Run("markerless residue", func(t *testing.T) {
		manifestPath := filepath.Join(t.TempDir(), "daem.toml")
		paths, err := daempaths.Resolve(manifestPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(paths.StateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		residue := filepath.Join(paths.StateDir, ".daem-tmp-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		if err := os.Mkdir(residue, 0o700); err != nil {
			t.Fatal(err)
		}

		_, err = Plan(context.Background(), PlanInput{ManifestPath: manifestPath})
		if !errors.Is(err, journal.ErrNoRecoverableJournal) {
			t.Fatalf("error = %v, want ErrNoRecoverableJournal", err)
		}
		if errors.Is(err, transaction.ErrAbandonedFileSetResidue) {
			t.Fatalf("error = %v, must not hijack missing journal with residue", err)
		}
		if _, statErr := os.Lstat(residue); statErr != nil {
			t.Fatalf("residue disappeared: %v", statErr)
		}
	})
}

func TestRecoverPlansActiveJournalBesideAbandonedResidue(t *testing.T) {
	fixture := prepareRecoveryFixture(t, true)
	paths, err := daempaths.Resolve(fixture.input.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	residue := filepath.Join(paths.StateDir, ".daem-tmp-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := os.Mkdir(residue, 0o700); err != nil {
		t.Fatal(err)
	}

	prepared, err := Plan(t.Context(), fixture.input)
	if err != nil {
		t.Fatalf("Plan with residue: %v", err)
	}
	if got := prepared.Disclosure().AuthorityKind(); got != journal.RecoveryAuthorityActiveJournal {
		t.Fatalf("authority kind = %q, want active journal", got)
	}
	if fence, present := prepared.ContinuingFileSetFence(); !present ||
		fence != transaction.FileSetFenceAbandonedResidue {
		t.Fatalf("planned continuing fence = (%q, %t)", fence, present)
	}
	execution, err := Execute(t.Context(), prepared, ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute with residue: %v", err)
	}
	if fence, present := execution.ContinuingFileSetFence(); !present ||
		fence != transaction.FileSetFenceAbandonedResidue {
		t.Fatalf("terminal continuing fence = (%q, %t)", fence, present)
	}
	if _, err := os.Lstat(residue); err != nil {
		t.Fatalf("residue after recover: %v", err)
	}
}

func TestRecoverUnprovableStateDirDoesNotRecommendRecover(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "daem.toml")
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.StateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.StateDir, []byte("not-a-directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = Plan(context.Background(), PlanInput{ManifestPath: manifestPath})
	if err == nil || !errors.Is(err, transaction.ErrFileSetFenceUnprovable) {
		t.Fatalf("error = %v, want ErrFileSetFenceUnprovable", err)
	}
	if strings.Contains(err.Error(), "run: daem recover") &&
		!strings.Contains(err.Error(), "do not run daem recover") {
		t.Fatalf("error = %q, want restore-access without recover-first", err)
	}
}

func TestCleanupOnlyRecoveryIgnoresUnrelatedMetadataTransaction(t *testing.T) {
	fixture := prepareCleanupRecoveryFixture(t, retirement.PhaseFinalizing, true)
	metadatatx.WriteInterrupted(t, fixture.paths.StateDir)

	prepared, err := Plan(t.Context(), fixture.input)
	if err != nil {
		t.Fatalf("Plan cleanup-only recovery: %v", err)
	}
	if got := prepared.Disclosure().AuthorityKind(); got != journal.RecoveryAuthorityJournalCleanup {
		t.Fatalf("authority kind = %q, want journal cleanup", got)
	}
	if _, err := Execute(t.Context(), prepared, ExecuteOptions{}); err != nil {
		t.Fatalf("Execute cleanup-only recovery: %v", err)
	}
	assertRecoverPathAbsent(t, fixture.controlDir)
	assertRecoverPathAbsent(t, fixture.residueDir)
}

func TestBlockedJournalPreservesInventoryAuthority(t *testing.T) {
	fixture := prepareCleanupRecoveryFixture(t, retirement.PhaseFinalizing, true)
	foreign := filepath.Join(fixture.controlDir, "foreign")
	if err := os.WriteFile(foreign, []byte("foreign\n"), retirement.RecordMode); err != nil {
		t.Fatal(err)
	}
	metadatatx.WriteInterrupted(t, fixture.paths.StateDir)

	_, err := Plan(t.Context(), fixture.input)
	if err == nil || !strings.Contains(err.Error(), "recovery inventory is blocked") {
		t.Fatalf("Plan error = %v, want blocked journal inventory", err)
	}
	if errors.Is(err, journal.ErrNoRecoverableJournal) {
		t.Fatalf("blocked inventory must not collapse to ErrNoRecoverableJournal: %v", err)
	}
	assertRecoverPathPresent(t, fixture.controlDir)
	assertRecoverPathPresent(t, fixture.residueDir)
	assertRecoverPathAbsent(t, fixture.garbageDir)
}
