package extension

import (
	"fmt"
	"strings"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
)

// PiSourceInterpretationDifference records a documented generic-Git/Pi-local
// interpretation difference, not an observation of the installed Pi runtime.
type PiSourceInterpretationDifference uint8

const (
	PiSourceDifferenceNone PiSourceInterpretationDifference = iota
	PiSourceDifferenceGitHubShorthand
	PiSourceDifferenceGitPlusURL
	PiSourceDifferenceBareSCP
)

// PiPackageSourceInterpretationDifference accepts only a Pi package carrier key.
// Existing carrier interpretation remains the authority on its source class.
func PiPackageSourceInterpretationDifference(key desiredextension.CarrierKey) (PiSourceInterpretationDifference, error) {
	if key.Carrier() != desiredextension.CarrierPiPackage {
		return PiSourceDifferenceNone, fmt.Errorf("Pi source interpretation difference requires a pi-package carrier")
	}
	interpreted, err := InterpretCarrierSource(key)
	if err != nil {
		return PiSourceDifferenceNone, err
	}
	if interpreted.Class() != CarrierSourceGit {
		return PiSourceDifferenceNone, nil
	}

	ref := key.Source().Ref()
	switch {
	case strings.HasPrefix(ref, "github:"):
		return PiSourceDifferenceGitHubShorthand, nil
	case strings.HasPrefix(ref, "git+http://"),
		strings.HasPrefix(ref, "git+https://"),
		strings.HasPrefix(ref, "git+ssh://"):
		return PiSourceDifferenceGitPlusURL, nil
	case !strings.HasPrefix(ref, "git:") && !strings.Contains(ref, "://"):
		// Remaining unprefixed, non-URL sources in the admitted Git grammar are SCP locators.
		return PiSourceDifferenceBareSCP, nil
	default:
		return PiSourceDifferenceNone, nil
	}
}
