package retirement

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
)

const (
	currentVersion = 1

	visibleRetirementPrefix = "retirement-"
	controlPrefix           = "retirement-v1-"
	residuePrefix           = ".daem-journal-residue-v1-"
	gcPrefix                = ".daem-journal-gc-v1-"

	hiddenResidueFamilyPrefix = ".daem-journal-residue-"
	hiddenGCFamilyPrefix      = ".daem-journal-gc-"
	legacyTombstonePrefix     = ".daem-tombstone-"
)

// Identity is the immutable correlation shared by one active journal and its
// retirement artifacts. Its digest is not an authenticity claim.
type Identity struct {
	operationID                 string
	journalAuthorityFingerprint string
	digest                      string
}

// NewIdentity constructs one canonical journal-retirement identity.
func NewIdentity(operationID string, journalAuthorityFingerprint string) (Identity, error) {
	if err := ValidateOperationID(operationID); err != nil {
		return Identity{}, err
	}
	if err := validateJournalAuthorityFingerprint(journalAuthorityFingerprint); err != nil {
		return Identity{}, err
	}
	return Identity{
		operationID:                 operationID,
		journalAuthorityFingerprint: journalAuthorityFingerprint,
		digest: retirementDigest(
			operationID,
			journalAuthorityFingerprint,
		),
	}, nil
}

// ValidateOperationID rejects invalid path components and the retirement
// namespace reserved beside active recovery journals.
func ValidateOperationID(value string) error {
	if value == "" || value == "." || value == ".." {
		return fmt.Errorf("operation id must be a non-empty safe path component")
	}
	if strings.HasPrefix(value, ".") || isReservedName(value) {
		return fmt.Errorf("operation id %q uses a reserved recovery name", value)
	}
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z':
		case char >= 'A' && char <= 'Z':
		case char >= '0' && char <= '9':
		case char == '-', char == '_', char == '.':
		default:
			return fmt.Errorf("operation id %q must be an ASCII safe path component", value)
		}
	}
	return nil
}

func validateJournalAuthorityFingerprint(value string) error {
	const prefix = "sha256:"
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+sha256.Size*2 {
		return fmt.Errorf("journal authority fingerprint must be sha256 followed by 64 lowercase hex digits")
	}
	if !isLowerHex(value[len(prefix):], sha256.Size*2) {
		return fmt.Errorf("journal authority fingerprint must be sha256 followed by 64 lowercase hex digits")
	}
	return nil
}

func retirementDigest(operationID string, journalAuthorityFingerprint string) string {
	canonical := lengthDelimited(
		strconv.Itoa(currentVersion),
		operationID,
		journalAuthorityFingerprint,
	)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(canonical)))
}

func lengthDelimited(values ...string) string {
	var canonical strings.Builder
	for _, value := range values {
		canonical.WriteString(strconv.Itoa(len(value)))
		canonical.WriteByte(':')
		canonical.WriteString(value)
	}
	return canonical.String()
}

// OperationID returns the correlated active journal operation id.
func (identity Identity) OperationID() string {
	return identity.operationID
}

// JournalAuthorityFingerprint returns the complete journal authority digest.
func (identity Identity) JournalAuthorityFingerprint() string {
	return identity.journalAuthorityFingerprint
}

func (identity Identity) equal(other Identity) bool {
	return identity == other && identity.valid()
}

func (identity Identity) valid() bool {
	if err := ValidateOperationID(identity.operationID); err != nil {
		return false
	}
	if err := validateJournalAuthorityFingerprint(identity.journalAuthorityFingerprint); err != nil {
		return false
	}
	return identity.digest == retirementDigest(
		identity.operationID,
		identity.journalAuthorityFingerprint,
	)
}

// ControlName returns the non-hidden downgrade-fence directory name.
func (identity Identity) ControlName() string {
	if !identity.valid() {
		return ""
	}
	return controlPrefix + identity.digest
}

// ResidueName returns the hidden active-journal residue directory name.
func (identity Identity) ResidueName() string {
	if !identity.valid() {
		return ""
	}
	return residuePrefix + identity.digest
}

// GCName returns the hidden post-finalization garbage-collection name.
func (identity Identity) GCName() string {
	if !identity.valid() {
		return ""
	}
	return gcPrefix + identity.digest
}

// NameKind classifies one recovery-root entry name without granting authority.
type NameKind string

const (
	NameUnrelated       NameKind = "unrelated"
	NameControl         NameKind = "control"
	NameResidue         NameKind = "residue"
	NameGC              NameKind = "gc"
	NameLegacyTombstone NameKind = "legacy_tombstone"
	nameMalformed       NameKind = "malformed_reserved"
)

// Name is one normalized recovery-root entry-name observation.
type Name struct {
	value  string
	kind   NameKind
	digest string
}

// InspectName classifies valid, malformed, legacy, and unrelated entry names.
func InspectName(value string) Name {
	switch {
	case strings.HasPrefix(value, controlPrefix):
		return inspectDigestName(value, controlPrefix, NameControl)
	case strings.HasPrefix(value, residuePrefix):
		return inspectDigestName(value, residuePrefix, NameResidue)
	case strings.HasPrefix(value, gcPrefix):
		return inspectDigestName(value, gcPrefix, NameGC)
	case strings.HasPrefix(value, legacyTombstonePrefix):
		suffix := strings.TrimPrefix(value, legacyTombstonePrefix)
		if isLowerHex(suffix, 32) {
			return Name{value: value, kind: NameLegacyTombstone}
		}
		return Name{value: value, kind: nameMalformed}
	case isReservedName(value):
		return Name{value: value, kind: nameMalformed}
	default:
		return Name{value: value, kind: NameUnrelated}
	}
}

func inspectDigestName(value string, prefix string, kind NameKind) Name {
	digest := strings.TrimPrefix(value, prefix)
	if !isLowerHex(digest, sha256.Size*2) {
		return Name{value: value, kind: nameMalformed}
	}
	return Name{value: value, kind: kind, digest: digest}
}

func isReservedName(value string) bool {
	return strings.HasPrefix(value, visibleRetirementPrefix) ||
		strings.HasPrefix(value, hiddenResidueFamilyPrefix) ||
		strings.HasPrefix(value, hiddenGCFamilyPrefix) ||
		strings.HasPrefix(value, legacyTombstonePrefix)
}

// Kind returns the normalized entry-name classification.
func (name Name) Kind() NameKind {
	return name.kind
}

func (name Name) digestValue() (string, bool) {
	if !name.valid() {
		return "", false
	}
	switch name.kind {
	case NameControl, NameResidue, NameGC:
		return name.digest, true
	default:
		return "", false
	}
}

func (name Name) valid() bool {
	return InspectName(name.value) == name
}

// BelongsTo reports whether a valid retirement artifact name carries the
// supplied identity digest.
func (name Name) BelongsTo(identity Identity) bool {
	digest, ok := name.digestValue()
	return ok && identity.valid() && digest == identity.digest
}

func isLowerHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
