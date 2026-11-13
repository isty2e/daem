package adopt

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/recoverygate"
)

func TestCaptureRecoveryQualifiesOnlyTheSelectedGeneration4Candidate(t *testing.T) {
	candidate := recoverygate.Combine(nil, fileset.ErrFileSetEvidenceInvalid)
	for _, test := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "generic reader rejects generation4", err: fmt.Errorf("wrapped: %w", candidate), want: true},
		{name: "legacy reader rejects generation4", err: recoverygate.LegacyUserStateRecoveryError{Err: candidate}},
		{name: "joined legacy failure", err: errors.Join(candidate, recoverygate.LegacyUserStateRecoveryError{Err: candidate})},
		{name: "cancellation", err: errors.Join(candidate, context.Canceled)},
		{name: "deadline", err: errors.Join(candidate, context.DeadlineExceeded)},
		{name: "global fence", err: errors.Join(candidate, recoverygate.ErrGlobalMetadataFence)},
		{name: "unclassified journal failure", err: recoverygate.Combine(errors.New("journal unavailable"), fileset.ErrFileSetEvidenceInvalid)},
		{name: "generation3 publication", err: recoverygate.Combine(nil, fileset.ErrInterruptedFileSetTransaction)},
		{name: "clear"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if captureFileSet4Candidate(test.err) != test.want {
				t.Fatal("recovery admitted a different owner or failure family")
			}
		})
	}
}
