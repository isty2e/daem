package rootedpath

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
)

const (
	provenanceFingerprintPrefix    = "sha256:"
	operationFingerprintHashDomain = "daem-rooted-path-operation-v1"
	recoveryProvenanceHashDomain   = "daem-rooted-path-recovery-provenance-v2"
)

// AuthorityProvenance is durable evidence identifying one captured physical root
// incarnation. It contains no native handle and grants no mutation authority.
type AuthorityProvenance struct {
	physicalRoot      string
	objectFingerprint string
	mountFingerprint  string
}

// NewAuthorityProvenance reconstructs canonical persisted authority evidence.
// It validates, but cannot make the evidence current or authorize an effect.
func NewAuthorityProvenance(
	physicalRoot string,
	objectFingerprint string,
	mountFingerprint string,
) (AuthorityProvenance, error) {
	provenance := AuthorityProvenance{
		physicalRoot:      physicalRoot,
		objectFingerprint: objectFingerprint,
		mountFingerprint:  mountFingerprint,
	}
	if err := provenance.Validate(); err != nil {
		return AuthorityProvenance{}, err
	}
	return provenance, nil
}

// Provenance derives durable, non-authorizing evidence from canonical authority.
func (authority Authority) Provenance() (AuthorityProvenance, error) {
	if err := authority.Validate(); err != nil {
		return AuthorityProvenance{}, err
	}
	if authority.mount.recovery == (identityToken{}) {
		return AuthorityProvenance{}, newFailure(
			FailureUnsupportedPlatform,
			authority.physicalRoot,
			"durable recovery mount identity is unavailable; Linux recovery requires STATX_MNT_ID_UNIQUE",
			nil,
		)
	}
	return NewAuthorityProvenance(
		authority.physicalRoot,
		recoveryIdentityFingerprint("object", authority.object),
		recoveryIdentityFingerprint("mount", authority.mount.recovery),
	)
}

// OperationFingerprint identifies one root incarnation for an in-process plan.
// It is not durable recovery evidence and must never be persisted as such.
func (authority Authority) OperationFingerprint() (string, error) {
	if err := authority.Validate(); err != nil {
		return "", err
	}
	hasher := sha256.New()
	hasher.Write([]byte(operationFingerprintHashDomain))
	hasher.Write([]byte{0})
	hasher.Write([]byte(authority.physicalRoot))
	hasher.Write([]byte{0})
	hasher.Write(authority.object[:])
	hasher.Write(authority.mount.operation[:])
	return provenanceFingerprintPrefix + hex.EncodeToString(hasher.Sum(nil)), nil
}

// Validate checks the canonical durable representation without touching the filesystem.
func (provenance AuthorityProvenance) Validate() error {
	if err := validatePhysicalRoot(provenance.physicalRoot); err != nil {
		return err
	}
	if !validProvenanceFingerprint(provenance.objectFingerprint) {
		return newFailure(
			FailureInvalidRoot,
			provenance.physicalRoot,
			"captured root object fingerprint is invalid",
			nil,
		)
	}
	if !validProvenanceFingerprint(provenance.mountFingerprint) {
		return newFailure(
			FailureInvalidRoot,
			provenance.physicalRoot,
			"captured root mount fingerprint is invalid",
			nil,
		)
	}
	if provenance.objectFingerprint == provenance.mountFingerprint {
		return newFailure(
			FailureInvalidRoot,
			provenance.physicalRoot,
			"captured root object and mount fingerprints must be domain-distinct",
			nil,
		)
	}
	return nil
}

// PhysicalRoot returns the canonical physical spelling captured in the evidence.
func (provenance AuthorityProvenance) PhysicalRoot() string {
	return provenance.physicalRoot
}

// ObjectFingerprint returns the opaque root-object identity fingerprint.
func (provenance AuthorityProvenance) ObjectFingerprint() string {
	return provenance.objectFingerprint
}

// MountFingerprint returns the opaque durable-recovery mount fingerprint.
func (provenance AuthorityProvenance) MountFingerprint() string {
	return provenance.mountFingerprint
}

// Match verifies that current authority identifies the same physical root
// incarnation as this evidence. The caller must still retain a current witness
// and acquire a fresh destination capability before any effect.
func (provenance AuthorityProvenance) Match(authority Authority) error {
	if err := provenance.Validate(); err != nil {
		return err
	}
	current, err := authority.Provenance()
	if err != nil {
		return err
	}
	if provenance.physicalRoot != current.physicalRoot {
		return newFailure(
			FailureRootReplaced,
			current.physicalRoot,
			"selected root resolves to a different physical path than recovery evidence",
			nil,
		)
	}
	if provenance.mountFingerprint != current.mountFingerprint {
		return newFailure(
			FailureMountChanged,
			current.physicalRoot,
			"selected root mount identity differs from recovery evidence",
			nil,
		)
	}
	if provenance.objectFingerprint != current.objectFingerprint {
		return newFailure(
			FailureRootReplaced,
			current.physicalRoot,
			"selected root object identity differs from recovery evidence",
			nil,
		)
	}
	return nil
}

// MatchDescendant verifies that authority identifies either this exact root
// incarnation or a physical descendant on the same mount. It does not prove
// that this ancestor remains current; callers retaining only descendant
// authority must also recapture and Match the ancestor before effects.
func (provenance AuthorityProvenance) MatchDescendant(authority Authority) error {
	if err := provenance.Validate(); err != nil {
		return err
	}
	if err := authority.Validate(); err != nil {
		return err
	}
	if provenance.physicalRoot == authority.physicalRoot {
		return provenance.Match(authority)
	}
	relative, err := filepath.Rel(provenance.physicalRoot, authority.physicalRoot)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return newFailure(
			FailureRootReplaced,
			authority.physicalRoot,
			"current root is outside the recovery authority root",
			err,
		)
	}
	if authority.mount.recovery == (identityToken{}) {
		return newFailure(
			FailureUnsupportedPlatform,
			authority.physicalRoot,
			"durable recovery mount identity is unavailable; Linux recovery requires STATX_MNT_ID_UNIQUE",
			nil,
		)
	}
	if provenance.mountFingerprint != recoveryIdentityFingerprint("mount", authority.mount.recovery) {
		return newFailure(
			FailureMountChanged,
			authority.physicalRoot,
			"descendant root mount identity differs from recovery evidence",
			nil,
		)
	}
	return nil
}

func validProvenanceFingerprint(value string) bool {
	if !strings.HasPrefix(value, provenanceFingerprintPrefix) {
		return false
	}
	digest := strings.TrimPrefix(value, provenanceFingerprintPrefix)
	if len(digest) != sha256.Size*2 || strings.ToLower(digest) != digest {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil
}

func recoveryIdentityFingerprint(kind string, token identityToken) string {
	hasher := sha256.New()
	hasher.Write([]byte(recoveryProvenanceHashDomain))
	hasher.Write([]byte{0})
	hasher.Write([]byte(kind))
	hasher.Write([]byte{0})
	hasher.Write(token[:])
	return provenanceFingerprintPrefix + hex.EncodeToString(hasher.Sum(nil))
}
