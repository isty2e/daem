package journal

import (
	"errors"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/retirement"
)

func TestCleanupFailureProjectsExecutionFactsWithoutCauseText(t *testing.T) {
	cause := errors.New("remove /private/recovery/control: permission denied")
	err := WrapCleanupFailure(retirement.ActionFinalizeJournalCleanup, cause)

	var failure *CleanupFailure
	if !errors.As(err, &failure) {
		t.Fatalf("errors.As(%v, *CleanupFailure) = false", err)
	}
	if failure.Action() != retirement.ActionFinalizeJournalCleanup ||
		failure.Phase() != CleanupFailurePhaseExecution {
		t.Fatalf(
			"cleanup failure action=%q phase=%q",
			failure.Action(),
			failure.Phase(),
		)
	}
	const want = "journal cleanup failed: phase=execution action=finalize_journal_cleanup"
	if err.Error() != want {
		t.Fatalf("cleanup failure = %q, want %q", err, want)
	}
	if strings.Contains(err.Error(), "/private/") ||
		strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("cleanup failure exposed boundary cause: %q", err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("cleanup failure lost internal cause: %v", err)
	}
}

func TestCleanupFailureProjectsFinalizedGCStateWithoutCauseText(t *testing.T) {
	cause := errors.New("unlink /private/recovery/gc: input/output error")
	finalized := finalizedWithGCResidue(cause)
	err := WrapCleanupFailure(
		retirement.ActionMigrateLegacyJournalTombstone,
		finalized,
	)

	var failure *CleanupFailure
	if !errors.As(err, &failure) {
		t.Fatalf("errors.As(%v, *CleanupFailure) = false", err)
	}
	if failure.Action() != retirement.ActionMigrateLegacyJournalTombstone ||
		failure.Phase() != CleanupFailurePhaseGarbageCollection {
		t.Fatalf(
			"cleanup failure action=%q phase=%q",
			failure.Action(),
			failure.Phase(),
		)
	}
	const want = "journal cleanup incomplete: phase=garbage_collection action=migrate_legacy_journal_tombstone; semantic retirement is committed and no recovery action remains"
	if err.Error() != want {
		t.Fatalf("cleanup failure = %q, want %q", err, want)
	}
	if strings.Contains(err.Error(), "/private/") ||
		strings.Contains(err.Error(), "input/output error") {
		t.Fatalf("cleanup failure exposed boundary cause: %q", err)
	}
	if !IsRetirementFinalizedWithGCResidue(err) || !errors.Is(err, cause) {
		t.Fatalf("cleanup failure lost finalized state or internal cause: %v", err)
	}
}

func TestWrapCleanupFailureIsIdempotentForTheSelectedAction(t *testing.T) {
	cause := errors.New("failure")
	first := WrapCleanupFailure(retirement.ActionFinalizeJournalCleanup, cause)
	second := WrapCleanupFailure(retirement.ActionFinalizeJournalCleanup, first)
	if second != first {
		t.Fatalf("WrapCleanupFailure duplicated an existing typed failure")
	}
	if WrapCleanupFailure(retirement.ActionFinalizeJournalCleanup, nil) != nil {
		t.Fatalf("WrapCleanupFailure(nil) returned an error")
	}
}
