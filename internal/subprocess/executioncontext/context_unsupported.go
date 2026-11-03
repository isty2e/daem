//go:build !darwin && !linux

package executioncontext

import (
	"context"
	"errors"

	"github.com/isty2e/daem/internal/executionidentity"
)

func readExecutionContext(context.Context) (executionidentity.Context, error) {
	return executionidentity.Context{}, errors.New("execution context observation is unsupported")
}

func requireRecordedProcessGroupAbsent(int64) error {
	return &ExecutionObservationError{kind: ExecutionContextUnavailable}
}

func ObserveGroup(int) (GroupOccupancy, error) {
	return GroupAbsent, &ExecutionObservationError{kind: ExecutionContextUnavailable}
}
