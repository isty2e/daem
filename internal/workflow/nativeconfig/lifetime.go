package nativeconfig

import (
	"context"
	"errors"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

// AssessmentOperation owns selected observation roots until its containing
// execution finishes. It supplies no mutation authority.
type AssessmentOperation struct {
	assessment readiness.Assessment
	native     *readiness.NativeAssessmentInput
	roots      []*rootedpath.CapturedRoot
	closed     bool
}

func AssessRetained(ctx context.Context, input readiness.Input) (*AssessmentOperation, error) {
	operation := &AssessmentOperation{}
	assessment, err := assessRetainedInputs(ctx, input, operation)
	if err != nil {
		return nil, errors.Join(err, operation.Close())
	}
	operation.assessment = assessment
	return operation, nil
}

// Assess closes its observation roots before returning static planning facts.
func Assess(ctx context.Context, input readiness.Input) (result readiness.Assessment, resultErr error) {
	operation, err := AssessRetained(ctx, input)
	if err != nil {
		return readiness.Assessment{}, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, operation.Close())
		if resultErr != nil {
			result = readiness.Assessment{}
		}
	}()
	return operation.assessment, nil
}

func (operation *AssessmentOperation) Assessment() readiness.Assessment {
	return operation.assessment
}

func (operation *AssessmentOperation) NativeInput() (readiness.NativeAssessmentInput, bool) {
	if operation == nil || operation.closed || operation.native == nil {
		return readiness.NativeAssessmentInput{}, false
	}
	return *operation.native, true
}

func (operation *AssessmentOperation) Close() error {
	if operation == nil || operation.closed {
		return nil
	}
	operation.closed = true
	operation.native = nil
	var err error
	for _, root := range operation.roots {
		err = errors.Join(err, root.Close())
	}
	operation.roots = nil
	return err
}
