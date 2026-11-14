package recover

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/effect/fileset"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	"github.com/isty2e/daem/internal/effect/mutation"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
	"github.com/isty2e/daem/test/testkit/metadatatx"
)

func TestRecoverRefusesGlobalMetadataTransaction(t *testing.T) {
	fixture := prepareRecoveryFixture(t, true)
	paths, err := daempaths.Resolve(fixture.input.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	metadatatx.WriteInterrupted(t, paths.GlobalMetadataDir())
	for _, legacy := range []bool{false, true} {
		input := fixture.input
		input.LegacyUserState = legacy
		prepared, err := Plan(t.Context(), input)
		if prepared != nil || !errors.Is(err, recoverygate.ErrGlobalMetadataFence) {
			t.Fatalf("recovery with legacy=%t bypassed the shared transaction: %v", legacy, err)
		}
	}
	if _, err := os.Lstat(fixture.operationDir); err != nil {
		t.Fatal("blocked recovery changed the active journal", err)
	}
}

func TestRecoverExecutionPreservesGlobalMetadataRefusal(t *testing.T) {
	fixture := prepareRecoveryFixture(t, true)
	prepared, err := Plan(t.Context(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = prepared.Close() })
	paths, err := daempaths.Resolve(fixture.input.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	metadatatx.WriteInterrupted(t, paths.GlobalMetadataDir())
	_, err = Execute(t.Context(), prepared, ExecuteOptions{})
	var stale mutation.StaleSnapshotError
	if !errors.Is(err, recoverygate.ErrGlobalMetadataFence) || errors.As(err, &stale) {
		t.Fatal("global metadata refusal became stale recovery evidence", err)
	}
}

func TestRecoveryAuthoritySharesGlobalMetadataLease(t *testing.T) {
	for _, cleanup := range []bool{false, true} {
		t.Run(fmt.Sprintf("cleanup=%t", cleanup), func(t *testing.T) {
			var fixture recoveryFixture
			if cleanup {
				fixture = prepareCleanupRecoveryFixture(t, retirement.PhasePrepared, true).recoveryFixture
			} else {
				fixture = prepareRecoveryFixture(t, true)
			}
			prepared, err := Plan(t.Context(), fixture.input)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = prepared.Close() })
			planned := prepared.lifecycle.planned.(recoveryPreparation)
			marker, err := recoverygate.GlobalMetadataAuthorityPath(planned.paths)
			if err != nil {
				t.Fatal(err)
			}
			writer, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{Path: marker, Access: mutation.AccessExclusive, Effect: mutation.PathEffectDirectoryEntry})
			if err != nil {
				t.Fatal(err)
			}
			store, err := mutation.NewStore(planned.paths.DataDir)
			if err != nil {
				t.Fatal(err)
			}
			lease, err := store.Acquire(t.Context(), writer)
			if err != nil {
				t.Fatal(err)
			}
			defer lease.Release()
			ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
			defer cancel()
			peer, err := store.Acquire(ctx, planned.authorityEvidence.domains...)
			if err == nil {
				_ = peer.Release()
				t.Fatal("journal recovery bypassed the global publisher lease")
			}
			var canceled mutation.CancellationError
			if !errors.As(err, &canceled) {
				t.Fatal("recovery lease failed for an unrelated reason", err)
			}
		})
	}
}

func TestRecoverNoJournalRetainsFileSetEvidenceWithoutRecoveryAuthority(t *testing.T) {
	t.Run("published marker", func(t *testing.T) {
		manifestPath := filepath.Join(t.TempDir(), "daem.toml")
		paths, err := daempaths.Resolve(manifestPath)
		if err != nil {
			t.Fatal(err)
		}
		metadatatx.WriteInterrupted(t, paths.StateDir)

		prepared, err := Plan(context.Background(), PlanInput{ManifestPath: manifestPath})
		if !errors.Is(err, journal.ErrNoRecoverableJournal) || prepared != nil {
			t.Fatalf("Plan = %v, %v, want no recovery authority and ErrNoRecoverableJournal", prepared, err)
		}
		if got := fileset.FileSetFenceKindOf(err); got != fileset.FileSetFencePublishedTransaction {
			t.Fatalf("fence = %q, want retained published transaction", got)
		}
		if _, err := os.Stat(filepath.Join(paths.StateDir, "metadata-transaction")); err != nil {
			t.Fatalf("metadata marker changed: %v", err)
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

		prepared, err := Plan(context.Background(), PlanInput{ManifestPath: manifestPath})
		if !errors.Is(err, journal.ErrNoRecoverableJournal) || prepared != nil {
			t.Fatalf("Plan = %v, %v, want no recovery authority and ErrNoRecoverableJournal", prepared, err)
		}
		if !errors.Is(err, fileset.ErrAbandonedFileSetResidue) {
			t.Fatalf("error = %v, want retained secondary residue evidence", err)
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
		fence != fileset.FileSetFenceAbandonedResidue {
		t.Fatalf("planned continuing fence = (%q, %t)", fence, present)
	}
	execution, err := Execute(t.Context(), prepared, ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute with residue: %v", err)
	}
	terminalFence := execution.FileSetFenceObservation()
	if !terminalFence.Observed() || !terminalFence.Known() ||
		terminalFence.Kind() != fileset.FileSetFenceAbandonedResidue {
		t.Fatalf(
			"terminal continuing fence = observed:%t known:%t kind:%q",
			terminalFence.Observed(),
			terminalFence.Known(),
			terminalFence.Kind(),
		)
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
	if err == nil || !errors.Is(err, fileset.ErrFileSetFenceUnprovable) {
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
