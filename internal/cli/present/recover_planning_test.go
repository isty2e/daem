package clipresent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/recoverygate"
)

func TestRecoverPlanningFailurePreservesIndependentFencePrecedence(t *testing.T) {
	for _, test := range []struct {
		name string
		peer error
		want string
		note bool
	}{
		{name: "clear", want: "recover failed: no recoverable journal operation", note: true},
		{name: "published", peer: fileset.ErrInterruptedFileSetTransaction, want: "continuing file-set fence: published_transaction", note: true},
		{name: "residue", peer: fileset.ErrAbandonedFileSetResidue, want: "do not delete reserved names by prefix", note: true},
		{name: "census", peer: fileset.ErrFileSetFenceCensusLimit, want: "inspect the bounded StateDir inventory", note: true},
		{name: "invalid", peer: fileset.ErrFileSetEvidenceInvalid, want: "recover failed: file-set transaction evidence"},
		{name: "access", peer: fileset.ErrFileSetAccessUnprovable, want: "recover failed: file-set state directory access"},
		{name: "unknown", peer: errors.New("unclassified peer evidence"), want: "unclassified peer evidence"},
		{name: "cancel", peer: context.Canceled, want: "recover failed: context canceled"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			PrintRecoverPlanningFailure(&output, recoverygate.Combine(journal.ErrNoRecoverableJournal, test.peer), HumanOptions{})
			if !strings.Contains(output.String(), test.want) || strings.Contains(output.String(), "note: journal recovery") != test.note {
				t.Fatalf("output=%q, want %q, scope note=%t", &output, test.want, test.note)
			}
		})
	}
}

func TestRecoverPlanningFailureBoundsVerboseEvidenceAndKeepsDefaultPathNeutral(t *testing.T) {
	const private = "/private/selected-state"
	cause := errors.Join(fileset.ErrAbandonedFileSetResidue, fmt.Errorf("%s\n\x1b[31m%s", private, strings.Repeat("x", 8192)))
	err := recoverygate.Combine(journal.ErrNoRecoverableJournal, cause)
	var output bytes.Buffer
	PrintRecoverPlanningFailure(&output, err, HumanOptions{})
	if strings.Contains(output.String(), private) || strings.Contains(output.String(), "\x1b") {
		t.Fatalf("default failure leaked evidence: %q", &output)
	}

	output.Reset()
	PrintRecoverPlanningFailure(&output, err, HumanOptions{Verbose: true})
	if !strings.Contains(output.String(), private) || strings.Contains(output.String(), "\x1b") || output.Len() > 5000 {
		t.Fatalf("verbose evidence not bounded or escaped: %q", &output)
	}
}
