package pathauthority

import "strings"

// LegacyLocatorCandidate proposes only lookup spelling justified by the
// recorded encoding. The result still requires fresh exact re-observation.
// Darwin v1 folded keys are ambiguous even when they contain only ASCII.
func (authority Exact) LegacyLocatorCandidate() (string, bool) {
	if err := authority.Validate(); err != nil || len(rootedComponents(authority.key)) == 0 {
		return "", false
	}
	switch {
	case authority.witness == exactWitnessPrefix:
	case strings.HasPrefix(authority.witness, darwinNativeWitnessPrefix):
		// Native capture retains a component as u whenever its folded lookup
		// cannot be proved to name the same entry.
	case strings.HasPrefix(authority.witness, darwinCaseWitnessPrefix) &&
		!strings.ContainsRune(darwinWitnessModes(authority.witness), 'i'):
	default:
		return "", false
	}
	return authority.key, true
}
