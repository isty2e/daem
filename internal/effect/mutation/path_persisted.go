package mutation

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
)

// PersistedDirectoryEntryAuthority is one immutable observation used to
// compare durable keys with current directory-entry authority.
type PersistedDirectoryEntryAuthority struct {
	exact pathauthority.Exact
}

// DirectoryEntryAuthorityObservation is exactly one current path-authority
// state: exact authority or normalization-sensitive provisional intent.
type DirectoryEntryAuthorityObservation struct {
	exact       pathauthority.Exact
	provisional pathauthority.Provisional
}

// Validate rejects zero, ambiguous, or invalid directory-entry authority.
func (observation DirectoryEntryAuthorityObservation) Validate() error {
	hasExact := !observation.exact.IsZero()
	hasProvisional := !observation.provisional.IsZero()
	if hasExact == hasProvisional {
		return fmt.Errorf("directory-entry authority must contain exactly one exact or provisional observation")
	}
	if hasExact {
		return observation.exact.Validate()
	}
	return observation.provisional.Validate()
}

// ObserveDirectoryEntryAuthority captures exact or explicitly provisional
// authority without collapsing the two states.
func ObserveDirectoryEntryAuthority(path string) (DirectoryEntryAuthorityObservation, error) {
	selection, err := selectPath(path, PathEffectDirectoryEntry)
	if err != nil {
		return DirectoryEntryAuthorityObservation{}, err
	}
	identity, err := canonicalPathIdentityFromSelection(path, selection, PathEffectDirectoryEntry)
	if err != nil {
		return DirectoryEntryAuthorityObservation{}, err
	}
	if !identity.provisional.IsZero() {
		observation := DirectoryEntryAuthorityObservation{provisional: identity.provisional}
		if err := observation.Validate(); err != nil {
			return DirectoryEntryAuthorityObservation{}, err
		}
		return observation, nil
	}
	exact, err := pathauthority.NewExact(identity.keyPath, string(identity.witness))
	if err != nil {
		return DirectoryEntryAuthorityObservation{}, fmt.Errorf("construct exact path authority: %w", err)
	}
	observation := DirectoryEntryAuthorityObservation{exact: exact}
	if err := observation.Validate(); err != nil {
		return DirectoryEntryAuthorityObservation{}, err
	}
	return observation, nil
}

// Exact returns exact authority when the entry is normalization-stable.
func (observation DirectoryEntryAuthorityObservation) Exact() (pathauthority.Exact, bool) {
	return observation.exact, !observation.exact.IsZero()
}

// Provisional returns the future-path intent when exact authority is not yet observable.
func (observation DirectoryEntryAuthorityObservation) Provisional() (pathauthority.Provisional, bool) {
	return observation.provisional, !observation.provisional.IsZero()
}

// ObservePersistedDirectoryEntryAuthority captures the current canonical key
// and filesystem-semantics witness in one observation epoch.
func ObservePersistedDirectoryEntryAuthority(path string) (PersistedDirectoryEntryAuthority, error) {
	observation, err := ObserveDirectoryEntryAuthority(path)
	if err != nil {
		return PersistedDirectoryEntryAuthority{}, err
	}
	exact, ok := observation.Exact()
	if !ok {
		return PersistedDirectoryEntryAuthority{}, fmt.Errorf("path %q has provisional authority until its normalization-sensitive entry becomes visible", path)
	}
	return PersistedDirectoryEntryAuthority{
		exact: exact,
	}, nil
}

// Exact returns the canonical key and observed versioned semantics.
func (authority PersistedDirectoryEntryAuthority) Exact() pathauthority.Exact {
	return authority.exact
}

func (authority PersistedDirectoryEntryAuthority) validate() error {
	if err := authority.exact.Validate(); err != nil {
		return fmt.Errorf("current path authority: %w", err)
	}
	return nil
}
