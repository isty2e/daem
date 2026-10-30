//go:build darwin || linux

package executioncontext

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// ObserveGroup returns raw occupancy in the caller's current syscall view.
// It does not establish a stored binding's context, ownership or quiescence.
func ObserveGroup(group int) (GroupOccupancy, error) {
	if group <= 1 || group > 1<<31-1 {
		return GroupAbsent, fmt.Errorf("invalid process group id")
	}
	err := unix.Kill(-group, 0)
	occupancy, recognized := classifyGroupProbe(err)
	if !recognized {
		return GroupAbsent, fmt.Errorf("probe process group: %w", err)
	}
	return occupancy, nil
}

func classifyGroupProbe(err error) (GroupOccupancy, bool) {
	switch {
	case err == nil:
		return GroupPresent, true
	case errors.Is(err, unix.ESRCH):
		return GroupAbsent, true
	case errors.Is(err, unix.EPERM):
		return GroupUnsignalable, true
	default:
		return GroupAbsent, false
	}
}

func requireRecordedProcessGroupAbsent(group int64) error {
	if group <= 1 || group > 1<<31-1 {
		return &ExecutionObservationError{kind: ExecutionIdentityInvalid}
	}
	occupancy, err := ObserveGroup(int(group))
	if err != nil {
		return &ExecutionObservationError{kind: ExecutionGroupUnknown, cause: err}
	}
	switch occupancy {
	case GroupAbsent:
		return nil
	case GroupUnsignalable:
		return &ExecutionObservationError{kind: ExecutionGroupUnsignalable}
	default:
		return &ExecutionObservationError{kind: ExecutionGroupOccupied}
	}
}
