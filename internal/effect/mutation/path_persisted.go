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

// ObservePersistedDirectoryEntryAuthority captures the current canonical key
// and filesystem-semantics witness in one observation epoch.
func ObservePersistedDirectoryEntryAuthority(path string) (PersistedDirectoryEntryAuthority, error) {
	selection, err := selectPath(path, PathEffectDirectoryEntry)
	if err != nil {
		return PersistedDirectoryEntryAuthority{}, err
	}
	identity, err := canonicalPathIdentityFromSelection(
		path,
		selection,
		PathEffectDirectoryEntry,
	)
	if err != nil {
		return PersistedDirectoryEntryAuthority{}, err
	}
	exact, err := pathauthority.NewExact(identity.keyPath, string(identity.witness))
	if err != nil {
		return PersistedDirectoryEntryAuthority{}, fmt.Errorf("construct exact path authority: %w", err)
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
