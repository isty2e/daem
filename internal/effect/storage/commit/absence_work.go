package commit

import (
	"fmt"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

// MaximumRootedAbsenceConfirmationPathWork bounds the Unix absence algorithm
// for any physical root cut of an absolute path. It excludes destination
// capture/acquisition and assumes an unbudgeted capability.
func MaximumRootedAbsenceConfirmationPathWork(pathComponents int) (int, error) {
	if pathComponents <= 0 {
		return 0, fmt.Errorf("rooted absence work requires positive path depth")
	}
	maximumInt := int(^uint(0) >> 1)
	left, right := pathComponents/2, pathComponents-pathComponents/2
	if left != 0 && right > maximumInt/left {
		return 0, fmt.Errorf("rooted absence work overflows")
	}
	product := left * right
	if pathComponents == maximumInt || product > maximumInt-pathComponents-1 {
		return 0, fmt.Errorf("rooted absence work overflows")
	}
	// One observation: two root validations, a full root validation at each
	// relative parent handle, the relative walk and root opening. With
	// D=R+P+1 this is R*(D-R)+D+1, maximized at floor(D*D/4)+D+1.
	observation := product + pathComponents + 1
	if observation > maximumInt/mutationfs.RootedAbsencePathObservationCount {
		return 0, fmt.Errorf("rooted absence work overflows")
	}
	return mutationfs.RootedAbsencePathObservationCount * observation, nil
}
