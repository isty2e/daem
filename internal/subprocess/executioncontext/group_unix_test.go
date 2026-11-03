//go:build darwin || linux

package executioncontext

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestGroupProbePreservesUnsignalableAndUnknownStates(t *testing.T) {
	for _, test := range []struct {
		err        error
		occupancy  GroupOccupancy
		recognized bool
	}{
		{nil, GroupPresent, true},
		{unix.ESRCH, GroupAbsent, true},
		{unix.EPERM, GroupUnsignalable, true},
		{unix.EINVAL, GroupAbsent, false},
	} {
		occupancy, recognized := classifyGroupProbe(test.err)
		if occupancy != test.occupancy || recognized != test.recognized {
			t.Fatalf("group probe collapsed occupancy or uncertainty: %v", test.err)
		}
	}
}

func TestGroupProbeRefusesSpecialProcessSelectors(t *testing.T) {
	for _, group := range []int{-1, 0, 1} {
		if _, err := ObserveGroup(group); err == nil {
			t.Fatal("non-group process selector reached a usable observation")
		}
	}
}
