package recoverygate

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/journal"
)

func TestCombinePreservesOrthogonalRecoveryBarrierState(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		journalErr  error
		fileSetErr  error
		wantJournal journal.InterruptionKind
		wantFileSet fileset.FileSetFenceKind
		continuing  bool
		blocks      bool
	}{
		{
			name:        "active with residue",
			journalErr:  journal.ErrInterruptedApply,
			fileSetErr:  fileset.ErrAbandonedFileSetResidue,
			wantJournal: journal.InterruptionActiveApply,
			wantFileSet: fileset.FileSetFenceAbandonedResidue,
			continuing:  true,
		},
		{
			name:        "cleanup with published transaction",
			journalErr:  journal.ErrIncompleteJournalCleanup,
			fileSetErr:  fileset.ErrInterruptedFileSetTransaction,
			wantJournal: journal.InterruptionCleanupOnly,
			wantFileSet: fileset.FileSetFencePublishedTransaction,
			continuing:  true,
		},
		{
			name:        "active with access failure",
			journalErr:  journal.ErrInterruptedApply,
			fileSetErr:  fileset.ErrFileSetAccessUnprovable,
			wantJournal: journal.InterruptionActiveApply,
			wantFileSet: fileset.FileSetFenceAccessUnprovable,
			blocks:      true,
		},
		{
			name:        "cleanup with invalid evidence",
			journalErr:  journal.ErrIncompleteJournalCleanup,
			fileSetErr:  fileset.ErrFileSetEvidenceInvalid,
			wantJournal: journal.InterruptionCleanupOnly,
			wantFileSet: fileset.FileSetFenceInvalidEvidence,
			blocks:      true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := Combine(test.journalErr, test.fileSetErr)
			if err == nil {
				t.Fatal("expected joint error")
			}
			state := StateOf(err)
			if state.Journal() != test.wantJournal || state.FileSet() != test.wantFileSet {
				t.Fatalf("state = (%q, %q), want (%q, %q)", state.Journal(), state.FileSet(), test.wantJournal, test.wantFileSet)
			}
			if state.HasContinuingFileSetFence() != test.continuing {
				t.Fatalf("continuing = %t, want %t", state.HasContinuingFileSetFence(), test.continuing)
			}
			if state.blocksJournalRecovery() != test.blocks {
				t.Fatalf("blocks = %t, want %t", state.blocksJournalRecovery(), test.blocks)
			}
			if !errors.Is(err, test.journalErr) || !errors.Is(err, test.fileSetErr) {
				t.Fatalf("joint error lost a cause: %v", err)
			}
		})
	}
}

func TestCombineCancellationHasHighestPrecedence(t *testing.T) {
	t.Parallel()
	for _, err := range []error{
		Combine(journal.ErrInterruptedApply, context.Canceled),
		Combine(context.DeadlineExceeded, fileset.ErrAbandonedFileSetResidue),
	} {
		if !IsCancellation(err) {
			t.Fatalf("error = %v, want cancellation", err)
		}
		if strings.Contains(err.Error(), "recover") || strings.Contains(err.Error(), "file-set") {
			t.Fatalf("cancellation error leaked lower-precedence remediation: %v", err)
		}
	}
}

func TestAccessFailureMessagePrecedesObservedJournalAuthority(t *testing.T) {
	t.Parallel()
	err := Combine(journal.ErrInterruptedApply, fileset.ErrFileSetAccessUnprovable)
	if err == nil {
		t.Fatal("expected joint error")
	}
	text := err.Error()
	if !strings.HasPrefix(text, fileset.ErrFileSetAccessUnprovable.Error()) ||
		!strings.Contains(text, "cannot be acted on") {
		t.Fatalf("error = %q, want access-first diagnosis", text)
	}
}

func TestUnknownJournalObservationDoesNotRecommendRecovery(t *testing.T) {
	t.Parallel()
	journalErr := errors.New("recovery inventory inspection failed")
	err := Combine(journalErr, fileset.ErrAbandonedFileSetResidue)
	if err == nil {
		t.Fatal("expected joint error")
	}
	if strings.Contains(err.Error(), "daem recover") {
		t.Fatalf("error = %q, want no recoverable-authority claim", err)
	}
	if !errors.Is(err, journalErr) ||
		!errors.Is(err, fileset.ErrAbandonedFileSetResidue) {
		t.Fatalf("joint error lost a cause: %v", err)
	}
}

func TestCombinePreservesSingleAxisCausesAndPeerKnowledge(t *testing.T) {
	t.Parallel()
	journalErr := errors.New("journal observation failed")
	journalOnly := Combine(journalErr, nil)
	if !errors.Is(journalOnly, journalErr) {
		t.Fatalf("journal-only error lost original cause: %v", journalOnly)
	}
	journalState := StateOf(journalOnly)
	if !journalState.JournalObserved() || journalState.JournalKnown() ||
		!journalState.FileSetKnown() || journalState.FileSet() != fileset.FileSetFenceClear {
		t.Fatalf("journal-only state = %#v", journalState)
	}

	fileSetErr := errors.New("file-set observation failed")
	fileSetOnly := Combine(nil, fileSetErr)
	if !errors.Is(fileSetOnly, fileSetErr) {
		t.Fatalf("file-set-only error lost original cause: %v", fileSetOnly)
	}
	fileSetState := StateOf(fileSetOnly)
	if !fileSetState.FileSetObserved() || fileSetState.FileSetKnown() ||
		!fileSetState.JournalKnown() || fileSetState.Journal() != journal.InterruptionClear {
		t.Fatalf("file-set-only state = %#v", fileSetState)
	}
}

func TestRequireFileSetClearRejectsNilContext(t *testing.T) {
	t.Parallel()
	err := RequireFileSetClear(nil, t.TempDir())
	if err == nil {
		t.Fatal("expected context error")
	}
	if !strings.Contains(err.Error(), "context is required") {
		t.Fatalf("error = %v, want required context", err)
	}
}

func TestCaptureStateDirRejectsNilContext(t *testing.T) {
	t.Parallel()
	_, err := CaptureStateDir(nil, t.TempDir())
	if err == nil {
		t.Fatal("expected context error")
	}
	if !strings.Contains(err.Error(), "context is required") {
		t.Fatalf("error = %v, want required context", err)
	}
}

func TestCaptureStateDirBoundedRejectsNilContext(t *testing.T) {
	t.Parallel()
	_, err := CaptureStateDirBounded(nil, t.TempDir(), 256, nil)
	if err == nil {
		t.Fatal("expected context error")
	}
	if !strings.Contains(err.Error(), "context is required") {
		t.Fatalf("error = %v, want required context", err)
	}
}

func TestCombinePreservesKnownPeerBesideUnknownAxis(t *testing.T) {
	t.Parallel()
	journalErr := errors.New("recovery inventory inspection failed")
	err := Combine(journalErr, fileset.ErrAbandonedFileSetResidue)
	state := StateOf(err)
	if !state.JournalObserved() || state.JournalKnown() ||
		!state.FileSetKnown() || state.FileSet() != fileset.FileSetFenceAbandonedResidue {
		t.Fatalf("partial state = %#v", state)
	}
	if !state.Observed() || state.JournalKnown() || !state.HasContinuingFileSetFence() {
		t.Fatalf("partial state queries = %#v", state)
	}
}
