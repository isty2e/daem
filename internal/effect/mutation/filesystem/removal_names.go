package filesystem

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	logicalRemovalResiduePrefix = ".daem-tombstone-"
	logicalRemovalCleanupPrefix = ".daem-cleanup-"
	logicalRemovalTokenLength   = 32
)

// LogicalRemovalNames identifies the two exact same-parent namespace slots in
// a journal-authorized removal. Residue is valid only before cleanup starts;
// cleanup is the durable progress marker after the residue has been validated.
type LogicalRemovalNames struct {
	residue string
	cleanup string
}

// NewLogicalRemovalNames validates one exact residue and cleanup-stage pair.
func NewLogicalRemovalNames(residue string, cleanup string) (LogicalRemovalNames, error) {
	residueToken, err := validateLogicalRemovalName(residue, logicalRemovalResiduePrefix, "residue")
	if err != nil {
		return LogicalRemovalNames{}, err
	}
	cleanupToken, err := validateLogicalRemovalName(cleanup, logicalRemovalCleanupPrefix, "cleanup")
	if err != nil {
		return LogicalRemovalNames{}, err
	}
	if residueToken != cleanupToken {
		return LogicalRemovalNames{}, fmt.Errorf("logical removal names must carry the same opaque token")
	}
	return LogicalRemovalNames{residue: residue, cleanup: cleanup}, nil
}

func validateLogicalRemovalName(value string, prefix string, role string) (string, error) {
	if strings.TrimSpace(value) != value || value == "" || value == "." || value == ".." {
		return "", fmt.Errorf("logical removal %s name must be a trimmed non-empty component", role)
	}
	if !utf8.ValidString(value) || strings.ContainsAny(value, "/\\") || strings.ContainsRune(value, '\x00') {
		return "", fmt.Errorf("logical removal %s name must be one valid path component", role)
	}
	if !strings.HasPrefix(value, prefix) {
		return "", fmt.Errorf("logical removal %s name must use its reserved prefix", role)
	}
	token := strings.TrimPrefix(value, prefix)
	if len(token) != logicalRemovalTokenLength || !isLowercaseHex(token) {
		return "", fmt.Errorf("logical removal %s name must carry a 128-bit lowercase hexadecimal token", role)
	}
	return token, nil
}

func isLowercaseHex(value string) bool {
	for index := range len(value) {
		character := value[index]
		if !('0' <= character && character <= '9') && !('a' <= character && character <= 'f') {
			return false
		}
	}
	return true
}

// Residue returns the exact pre-cleanup sibling name.
func (names LogicalRemovalNames) Residue() string { return names.residue }

// Cleanup returns the exact post-validation cleanup-stage sibling name.
func (names LogicalRemovalNames) Cleanup() string { return names.cleanup }

// Equal reports whether both role-specific namespace slots are identical.
func (names LogicalRemovalNames) Equal(other LogicalRemovalNames) bool {
	return names.residue == other.residue && names.cleanup == other.cleanup
}

// Valid reports whether both role-specific names satisfy the canonical pair.
func (names LogicalRemovalNames) Valid() bool {
	_, err := NewLogicalRemovalNames(names.residue, names.cleanup)
	return err == nil
}
