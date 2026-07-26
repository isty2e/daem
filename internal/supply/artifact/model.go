package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ArtifactKind describes the structural form produced by source resolution.
type ArtifactKind string

const (
	ArtifactKindFile      ArtifactKind = "file"
	ArtifactKindDirectory ArtifactKind = "directory"
)

// SourceID is the stable source identity stored in reproducible artifacts and lockfiles.
type SourceID string

// ContentHash is an algorithm-qualified digest such as "sha256:<hex>".
type ContentHash string

// Validate rejects a malformed or unsupported content digest.
func (contentHash ContentHash) Validate() error {
	return validateContentHash(contentHash)
}

// ResolvedRef is the immutable revision resolved from a mutable source ref.
type ResolvedRef string

// Validate rejects a malformed or empty artifact kind.
func (kind ArtifactKind) Validate() error {
	switch kind {
	case ArtifactKindFile, ArtifactKindDirectory:
		return nil
	default:
		return fmt.Errorf("artifact kind %q is unsupported", kind)
	}
}

// Validate rejects a malformed source identity token.
func (sourceID SourceID) Validate() error {
	return validateIdentityText("source id", string(sourceID), false)
}

// Validate rejects a malformed optional resolved revision token.
func (resolvedRef ResolvedRef) Validate() error {
	return validateIdentityText("resolved ref", string(resolvedRef), true)
}

// ExactIdentity is the canonical identity of one resolved artifact.
// It deliberately carries no materialization path, locked desired authority, or host fact.
type ExactIdentity struct {
	sourceID    SourceID
	resolvedRef ResolvedRef
	kind        ArtifactKind
	contentHash ContentHash
}

// NewExactIdentity constructs a validated exact artifact identity.
func NewExactIdentity(
	sourceID SourceID,
	resolvedRef ResolvedRef,
	kind ArtifactKind,
	contentHash ContentHash,
) (ExactIdentity, error) {
	identity := ExactIdentity{
		sourceID:    sourceID,
		resolvedRef: resolvedRef,
		kind:        kind,
		contentHash: contentHash,
	}
	if err := identity.Validate(); err != nil {
		return ExactIdentity{}, err
	}
	return identity, nil
}

// Validate rejects a zero or malformed exact artifact identity.
func (identity ExactIdentity) Validate() error {
	if err := identity.sourceID.Validate(); err != nil {
		return fmt.Errorf("exact artifact %w", err)
	}
	if err := identity.resolvedRef.Validate(); err != nil {
		return fmt.Errorf("exact artifact %w", err)
	}
	if err := identity.kind.Validate(); err != nil {
		return fmt.Errorf("exact %w", err)
	}
	if err := identity.contentHash.Validate(); err != nil {
		return fmt.Errorf("exact artifact %w", err)
	}
	return nil
}

// SourceID returns the stable source identity component.
func (identity ExactIdentity) SourceID() SourceID { return identity.sourceID }

// ResolvedRef returns the optional immutable source revision component.
func (identity ExactIdentity) ResolvedRef() ResolvedRef { return identity.resolvedRef }

// Kind returns the artifact structural kind component.
func (identity ExactIdentity) Kind() ArtifactKind { return identity.kind }

// ContentHash returns the algorithm-qualified content digest component.
func (identity ExactIdentity) ContentHash() ContentHash { return identity.contentHash }

// Equal reports whether every exact identity component is equal.
func (identity ExactIdentity) Equal(other ExactIdentity) bool {
	return identity == other
}

func validateIdentityText(label string, value string, optional bool) error {
	if value == "" {
		if optional {
			return nil
		}
		return fmt.Errorf("%s is required", label)
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s must be valid UTF-8", label)
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must be trimmed", label)
	}
	if strings.IndexFunc(value, func(character rune) bool {
		return unicode.IsControl(character) || unicode.Is(unicode.Bidi_Control, character)
	}) >= 0 {
		return fmt.Errorf("%s contains an unsafe control character", label)
	}
	return nil
}

func validateContentHash(contentHash ContentHash) error {
	value := string(contentHash)
	if err := validateIdentityText("content hash", value, false); err != nil {
		return err
	}
	algorithm, digest, ok := strings.Cut(value, ":")
	if !ok || algorithm == "" || digest == "" {
		return fmt.Errorf("content hash %q must be algorithm-qualified", contentHash)
	}
	if algorithm != hashAlgorithm {
		return fmt.Errorf("content hash algorithm %q is unsupported", algorithm)
	}
	if len(digest) != sha256.Size*2 || strings.ToLower(digest) != digest {
		return fmt.Errorf("content hash %q must contain a lowercase SHA-256 digest", contentHash)
	}
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("content hash %q must contain a lowercase SHA-256 digest", contentHash)
	}
	return nil
}
