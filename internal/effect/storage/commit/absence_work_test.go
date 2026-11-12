package commit

import (
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

func TestRootedAbsenceConfirmationWorkBoundsEveryRootCut(t *testing.T) {
	for depth := 2; depth <= 64; depth++ {
		actual, err := MaximumRootedAbsenceConfirmationPathWork(depth)
		if err != nil {
			t.Fatal(err)
		}
		largest := 0
		for root := 1; root < depth; root++ {
			parent := depth - root - 1
			observation := 2*root + parent*root + parent + 2
			largest = max(largest, mutationfs.RootedAbsencePathObservationCount*observation)
		}
		if actual != largest {
			t.Fatalf("absence work at depth %d: got %d, want %d", depth, actual, largest)
		}
	}
	for _, depth := range []int{0, -1, int(^uint(0) >> 1)} {
		if work, err := MaximumRootedAbsenceConfirmationPathWork(depth); err == nil || work != 0 {
			t.Fatal("invalid or overflowing absence work accepted", depth, work, err)
		}
	}
}
