package extension

import "strings"

// GitCommitPinChange reports a ref-only change between full commit pins. The
// exact locator spelling is preserved; native repository identity is not
// authority to change transports, ports, credentials, or repository addresses.
func GitCommitPinChange(before, after string) bool {
	oldSource, oldOK := ParseGitSource(before)
	newSource, newOK := ParseGitSource(after)
	if !oldOK || !newOK || !oldSource.CredentialFree() || !newSource.CredentialFree() ||
		!fullCommitPin(oldSource.ref) || !fullCommitPin(newSource.ref) || oldSource.ref == newSource.ref {
		return false
	}
	oldLocator, oldSuffix := strings.CutSuffix(before, oldSource.ref)
	newLocator, newSuffix := strings.CutSuffix(after, newSource.ref)
	return oldSuffix && newSuffix && oldLocator == newLocator
}

func fullCommitPin(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, character := range value {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}
