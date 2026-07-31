package mutation

import (
	"fmt"
	"path/filepath"
	"strings"
)

// PersistedDirectoryEntryAuthority is one immutable observation used to
// compare durable keys with current directory-entry authority.
type PersistedDirectoryEntryAuthority struct {
	currentKey string
	legacyKey  string
}

// ObservePersistedDirectoryEntryAuthority captures current and v0.1.0 legacy
// key facts in one filesystem observation epoch.
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
	return PersistedDirectoryEntryAuthority{
		currentKey: identity.keyPath,
		legacyKey:  platformLegacyDirectoryEntryKey(selection, identity.keyPath),
	}, nil
}

// CurrentKey returns the current canonical directory-entry authority key.
func (authority PersistedDirectoryEntryAuthority) CurrentKey() string {
	return authority.currentKey
}

// ValidatePersistedKey verifies exact current authority and diagnoses an exact
// v0.1.0 Darwin-wide fold before returning an ordinary foreign-key mismatch.
func (authority PersistedDirectoryEntryAuthority) ValidatePersistedKey(persistedKey string) error {
	if err := authority.validate(); err != nil {
		return err
	}
	if err := validatePersistedPathKey("persisted", persistedKey); err != nil {
		return err
	}
	if authority.currentKey == persistedKey {
		return nil
	}
	return persistedDirectoryEntryKeyMismatch(
		authority.currentKey,
		persistedKey,
		authority.legacyKey != "" && authority.legacyKey == persistedKey,
	)
}

// RejectLegacyPersistedKey rejects persistedKey only when it is exactly the
// v0.1.0 Darwin-wide fold. Other authorities remain caller-owned conflicts.
func (authority PersistedDirectoryEntryAuthority) RejectLegacyPersistedKey(persistedKey string) error {
	if err := authority.validate(); err != nil {
		return err
	}
	if err := validatePersistedPathKey("persisted", persistedKey); err != nil {
		return err
	}
	if authority.currentKey == persistedKey || authority.legacyKey == "" ||
		authority.legacyKey != persistedKey {
		return nil
	}
	return persistedDirectoryEntryKeyMismatch(authority.currentKey, persistedKey, true)
}

func (authority PersistedDirectoryEntryAuthority) validate() error {
	if authority.currentKey == "" {
		return fmt.Errorf("current path authority key is required")
	}
	return nil
}

func persistedDirectoryEntryKeyMismatch(
	currentKey string,
	persistedKey string,
	legacy bool,
) error {
	if legacy {
		return fmt.Errorf(
			"persisted path authority %q uses the legacy Darwin-wide case fold instead of current filesystem semantics for %q; see docs/troubleshooting.md#legacy-darwin-path-authority and do not edit or delete daem state manually",
			persistedKey,
			currentKey,
		)
	}
	return fmt.Errorf(
		"persisted path authority %q does not match current filesystem authority %q",
		persistedKey,
		currentKey,
	)
}

func validatePersistedPathKey(label string, key string) error {
	if key == "" {
		return fmt.Errorf("%s path authority key is required", label)
	}
	if strings.ContainsRune(key, '\x00') {
		return fmt.Errorf("%s path authority key contains a NUL byte", label)
	}
	if !filepath.IsAbs(key) || filepath.Clean(key) != key {
		return fmt.Errorf("%s path authority key %q must be absolute and clean", label, key)
	}
	return nil
}
