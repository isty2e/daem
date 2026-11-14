package mutation

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

// PersistedDirectoryEntryAuthority is one immutable observation used to
// compare durable keys with current directory-entry authority.
type PersistedDirectoryEntryAuthority struct {
	reference pathauthority.Reference
}

// DirectoryEntryAuthorityObservation is exactly one current path-authority
// state: exact authority or normalization-sensitive provisional intent.
type DirectoryEntryAuthorityObservation struct {
	exact       pathauthority.Exact
	provisional pathauthority.Provisional
	locator     string
}

// Validate rejects zero, ambiguous, or invalid directory-entry authority.
func (observation DirectoryEntryAuthorityObservation) Validate() error {
	hasExact := !observation.exact.IsZero()
	hasProvisional := !observation.provisional.IsZero()
	if hasExact == hasProvisional {
		return fmt.Errorf("directory-entry authority must contain exactly one exact or provisional observation")
	}
	if hasExact {
		if err := observation.exact.Validate(); err != nil {
			return err
		}
		if observation.locator != "" {
			_, err := pathauthority.NewReference(observation.exact, observation.locator)
			return err
		}
		return nil
	}
	if observation.locator != "" {
		return fmt.Errorf("provisional directory-entry authority cannot supply exact lookup spelling")
	}
	return observation.provisional.Validate()
}

// ObserveDirectoryEntryAuthority captures exact or explicitly provisional
// authority without collapsing the two states.
func ObserveDirectoryEntryAuthority(path string) (DirectoryEntryAuthorityObservation, error) {
	identity, err := canonicalPathIdentity(path, PathEffectDirectoryEntry)
	if err != nil {
		return DirectoryEntryAuthorityObservation{}, err
	}
	return directoryEntryAuthorityObservation(identity)
}

// ObserveDirectoryEntryAuthorityBounded captures directory-entry authority
// while charging every physical path observation to one operation budget.
func ObserveDirectoryEntryAuthorityBounded(
	path string,
	maximumPhysicalDepth int,
	budget rootedpath.PhysicalTraversalBudget,
) (DirectoryEntryAuthorityObservation, error) {
	identity, err := canonicalDirectoryEntryPathIdentityBounded(path, maximumPhysicalDepth, budget)
	if err != nil {
		return DirectoryEntryAuthorityObservation{}, err
	}
	return directoryEntryAuthorityObservation(identity)
}

func directoryEntryAuthorityObservation(
	identity canonicalPath,
) (DirectoryEntryAuthorityObservation, error) {
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
	observation := DirectoryEntryAuthorityObservation{exact: exact, locator: identity.accessPath}
	if err := observation.Validate(); err != nil {
		return DirectoryEntryAuthorityObservation{}, err
	}
	return observation, nil
}

// Exact returns exact authority when the entry is normalization-stable.
func (observation DirectoryEntryAuthorityObservation) Exact() (pathauthority.Exact, bool) {
	return observation.exact, !observation.exact.IsZero()
}

// Reference returns lookup evidence only when the exact observation retained it.
// Provisional and comparison-only projections do not supply a locator.
func (observation DirectoryEntryAuthorityObservation) Reference() (pathauthority.Reference, bool) {
	if observation.exact.IsZero() || !observation.provisional.IsZero() || observation.locator == "" {
		return pathauthority.Reference{}, false
	}
	reference, err := pathauthority.NewReference(observation.exact, observation.locator)
	return reference, err == nil
}

func (observation DirectoryEntryAuthorityObservation) NamespaceKey() string {
	if exact, present := observation.Exact(); present {
		return exact.Key()
	}
	provisional, _ := observation.Provisional()
	return provisional.CandidateKey()
}

// MatchesRetainedExact accepts the same exact entry or the corresponding
// currently missing namespace; it does not supply fresh mutation authority.
func (observation DirectoryEntryAuthorityObservation) MatchesRetainedExact(retained pathauthority.Exact) bool {
	if observation.Validate() != nil || retained.Validate() != nil {
		return false
	}
	if exact, present := observation.Exact(); present {
		return exact.Equal(retained)
	}
	missing, present := observation.Provisional()
	return present && missing.MatchesMissingExact(retained)
}

// Provisional returns the future-path intent when exact authority is not yet observable.
func (observation DirectoryEntryAuthorityObservation) Provisional() (pathauthority.Provisional, bool) {
	return observation.provisional, !observation.provisional.IsZero()
}

// Equal compares exact and provisional states without promoting either.
func (observation DirectoryEntryAuthorityObservation) Equal(other DirectoryEntryAuthorityObservation) bool {
	if observation.Validate() != nil || other.Validate() != nil {
		return false
	}
	if exact, known := observation.Exact(); known {
		candidate, known := other.Exact()
		return known && exact.Equal(candidate)
	}
	provisional, _ := observation.Provisional()
	candidate, known := other.Provisional()
	return known && provisional.Equal(candidate)
}

// MayOverlap projects conservative exclusion without granting lookup or writes.
func (observation DirectoryEntryAuthorityObservation) MayOverlap(other DirectoryEntryAuthorityObservation) bool {
	if observation.Validate() != nil || other.Validate() != nil {
		return true
	}
	if left, exact := observation.Exact(); exact {
		if right, exact := other.Exact(); exact {
			return left.MayOverlap(right)
		}
		right, _ := other.Provisional()
		return right.CouldOverlap(left)
	}
	left, _ := observation.Provisional()
	if right, exact := other.Exact(); exact {
		return left.CouldOverlap(right)
	}
	right, _ := other.Provisional()
	return left.CouldOverlapProvisional(right)
}

// MayOverlapExact compares current exclusion evidence with retained authority.
func (observation DirectoryEntryAuthorityObservation) MayOverlapExact(other pathauthority.Exact) bool {
	if observation.Validate() != nil || other.Validate() != nil {
		return true
	}
	if exact, present := observation.Exact(); present {
		return exact.MayOverlap(other)
	}
	missing, _ := observation.Provisional()
	return missing.CouldOverlap(other)
}

// ObservePersistedDirectoryEntryAuthority captures the current canonical key
// and filesystem-semantics witness in one observation epoch.
func ObservePersistedDirectoryEntryAuthority(path string) (PersistedDirectoryEntryAuthority, error) {
	identity, err := canonicalPathIdentity(path, PathEffectDirectoryEntry)
	if err != nil {
		return PersistedDirectoryEntryAuthority{}, err
	}
	return persistedDirectoryEntryAuthority(path, identity)
}

// ObservePersistedDirectoryEntryAuthorityBounded captures exact persisted
// authority under one bounded physical traversal budget.
func ObservePersistedDirectoryEntryAuthorityBounded(
	path string,
	maximumPhysicalDepth int,
	budget rootedpath.PhysicalTraversalBudget,
) (PersistedDirectoryEntryAuthority, error) {
	identity, err := canonicalDirectoryEntryPathIdentityBounded(path, maximumPhysicalDepth, budget)
	if err != nil {
		return PersistedDirectoryEntryAuthority{}, err
	}
	return persistedDirectoryEntryAuthority(path, identity)
}

func persistedDirectoryEntryAuthority(path string, identity canonicalPath) (PersistedDirectoryEntryAuthority, error) {
	if !identity.provisional.IsZero() {
		return PersistedDirectoryEntryAuthority{}, fmt.Errorf(
			"path %q has provisional authority until its normalization-sensitive entry becomes visible",
			path,
		)
	}
	observation, err := directoryEntryAuthorityObservation(identity)
	if err != nil {
		return PersistedDirectoryEntryAuthority{}, err
	}
	reference, present := observation.Reference()
	if !present {
		return PersistedDirectoryEntryAuthority{}, fmt.Errorf("path %q has no observed access spelling", path)
	}
	return PersistedDirectoryEntryAuthority{reference: reference}, nil
}

// Exact returns the canonical key and observed versioned semantics.
func (authority PersistedDirectoryEntryAuthority) Exact() pathauthority.Exact {
	return authority.reference.Identity()
}

// Reference retains the selected access spelling from the same observation epoch.
func (authority PersistedDirectoryEntryAuthority) Reference() pathauthority.Reference {
	return authority.reference
}

func (authority PersistedDirectoryEntryAuthority) validate() error {
	if err := authority.reference.Validate(); err != nil {
		return fmt.Errorf("current path authority: %w", err)
	}
	return nil
}
