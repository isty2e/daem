//go:build darwin || linux

package workspacecleanup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/fileset"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/recoverygate"
)

func TestMayQualifyRequiresBothKnownEligibleAxes(t *testing.T) {
	for _, test := range []struct {
		name        string
		journal     error
		declaration error
		want        bool
	}{
		{name: "clear"},
		{name: "declaration alone", declaration: fileset.ErrInterruptedFileSetTransaction, want: true},
		{name: "journal alone", journal: journal.ErrIncompleteJournalCleanup, want: true},
		{name: "both", journal: journal.ErrIncompleteJournalCleanup, declaration: fileset.ErrInterruptedFileSetTransaction, want: true},
		{name: "unknown journal", journal: errors.New("could not read inventory"), declaration: fileset.ErrInterruptedFileSetTransaction},
		{name: "active journal", journal: journal.ErrInterruptedApply, declaration: fileset.ErrInterruptedFileSetTransaction},
		{name: "cancellation", journal: context.Canceled, declaration: fileset.ErrInterruptedFileSetTransaction},
		{name: "abandoned residue", journal: journal.ErrIncompleteJournalCleanup, declaration: fileset.ErrAbandonedFileSetResidue},
		{name: "incomplete census", journal: journal.ErrIncompleteJournalCleanup, declaration: fileset.ErrFileSetFenceCensusLimit},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := recoverygate.Combine(test.journal, test.declaration)
			if got := MayQualify(err); got != test.want {
				t.Fatalf("qualification = %t, want %t for %v", got, test.want, err)
			}
		})
	}
	if MayQualify(fileset.ErrInterruptedFileSetTransaction) {
		t.Fatal("a declaration fence without a known journal axis granted qualification")
	}
}

func TestCleanupOverlapProtectsContainersAndFutureReferents(t *testing.T) {
	root := t.TempDir()
	retiring := filepath.Join(root, "retiring")
	if err := os.Mkdir(retiring, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(retiring, alias); err != nil {
		t.Fatal(err)
	}
	budget := physicalwork.NewPathBudget()
	entry, err := mutation.ObserveDirectoryEntryAuthorityBounded(retiring, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	retirements := []mutation.DirectoryEntryAuthorityObservation{entry}
	for _, path := range []string{root, alias, filepath.Join(alias, "missing", "source")} {
		if err := rejectOverlap(path, retirements, budget); !errors.Is(err, errOverlap) {
			t.Errorf("expected refusal for %s: %v", path, err)
		}
	}
	if err := rejectOverlap(filepath.Join(root, "retiring-sibling", "source"), retirements, budget); err != nil {
		t.Fatalf("protected sibling was treated as overlap: %v", err)
	}
}
