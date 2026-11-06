package nativeconfig

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/workflow/readiness"
)

func assessRetainedInputs(ctx context.Context, input readiness.Input, operation *AssessmentOperation) (readiness.Assessment, error) {
	if operation == nil || input.PersistenceEpoch == nil {
		return readiness.Assessment{}, fmt.Errorf("CLI assessment requires its selected operation and persistence epoch")
	}
	if _, err := input.PersistenceEpoch.Snapshot(); err != nil {
		return readiness.Assessment{}, err
	}
	codecs, err := Codecs(ctx)
	if err != nil {
		return readiness.Assessment{}, err
	}
	input.Codecs = codecs
	if input.SourceEpoch == nil {
		epoch, err := ResolveSourceEpoch(ctx, input.Paths, input.Environment, input.Lockfile, input.Selection)
		if err != nil {
			return readiness.Assessment{}, err
		}
		input.SourceEpoch = &epoch
	}
	operation.documents = &DocumentObservation{}
	input.Native, err = observeRetainedDocumentInputs(ctx, input, operation.documents)
	if err != nil {
		return readiness.Assessment{}, err
	}
	result, err := readiness.Assess(ctx, input)
	if err != nil {
		return readiness.Assessment{}, err
	}
	if input.Native != nil {
		result.NativeApproval, err = input.Native.Documents.Evidence().ApprovalEvidence()
	}
	return result, err
}
