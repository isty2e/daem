package delegate

import (
	"encoding/hex"
	"strings"

	pep440 "github.com/aquasecurity/go-pep440-version"
	"golang.org/x/mod/semver"
)

// selectorAssurance classifies what a package selector actually constrains.
type selectorAssurance string

const (
	selectorAssuranceFloating        selectorAssurance = "floating"
	selectorAssuranceExactVersion    selectorAssurance = "exact_version"
	selectorAssuranceImmutableDigest selectorAssurance = "immutable_digest"
)

func deriveSelectorAssurance(ecosystem PackageEcosystem, selector string) selectorAssurance {
	switch ecosystem {
	case EcosystemNPM:
		if isExactNPMVersion(selector) {
			return selectorAssuranceExactVersion
		}
	case EcosystemPython:
		if isExactPythonVersion(selector) {
			return selectorAssuranceExactVersion
		}
	case EcosystemContainer:
		if isCanonicalSHA256Digest(selector) {
			return selectorAssuranceImmutableDigest
		}
	}
	return selectorAssuranceFloating
}

// PinPolicy projects selector assurance onto the stable lock/report policy.
func (ref PackageRef) PinPolicy() PinPolicy {
	switch ref.assurance {
	case selectorAssuranceExactVersion, selectorAssuranceImmutableDigest:
		return PinPinned
	default:
		return PinFloating
	}
}

// npm treats partial versions as ranges. Require a complete SemVer release
// before claiming exact-version assurance.
// https://docs.npmjs.com/cli/v11/using-npm/package-spec/
func isExactNPMVersion(selector string) bool {
	if selector == "" {
		return false
	}
	candidate := selector
	if !strings.HasPrefix(candidate, "v") {
		candidate = "v" + candidate
	}
	if !semver.IsValid(candidate) {
		return false
	}
	release := strings.TrimPrefix(candidate, "v")
	if suffix := strings.IndexAny(release, "-+"); suffix >= 0 {
		release = release[:suffix]
	}
	return strings.Count(release, ".") == 2
}

// PEP 440 exact versions include epochs, pre/post/dev releases, and local
// versions. Operators, wildcards, and arbitrary equality remain floating.
// https://packaging.python.org/en/latest/specifications/version-specifiers/
func isExactPythonVersion(selector string) bool {
	if selector == "" {
		return false
	}
	if strings.HasPrefix(selector, "==") {
		selector = strings.TrimPrefix(selector, "==")
	}
	if strings.ContainsAny(selector, "*,") || strings.HasPrefix(selector, "=") ||
		strings.HasPrefix(selector, "!") || strings.HasPrefix(selector, "<") ||
		strings.HasPrefix(selector, ">") || strings.HasPrefix(selector, "~") {
		return false
	}
	_, err := pep440.Parse(selector)
	return err == nil
}

// OCI digests identify content; tags do not. daem currently admits only the
// canonical lowercase sha256 form used by its delegated package contract.
// https://github.com/opencontainers/distribution-spec/blob/main/spec.md
func isCanonicalSHA256Digest(selector string) bool {
	const prefix = "sha256:"
	const encodedSHA256Bytes = 64

	if !strings.HasPrefix(selector, prefix) {
		return false
	}
	encoded := strings.TrimPrefix(selector, prefix)
	if len(encoded) != encodedSHA256Bytes || strings.ToLower(encoded) != encoded {
		return false
	}
	decoded, err := hex.DecodeString(encoded)
	return err == nil && len(decoded) == encodedSHA256Bytes/2
}
