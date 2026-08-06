// Package residue owns the canonical storage-neutral name of a logical
// removal residue selected before a journaled effect.
package residue

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const logicalRemovalResiduePrefix = ".daem-tombstone-"

// LogicalRemovalResidueName is an opaque, same-parent name selected before a
// journaled logical removal. It carries only one canonical path component;
// recovery owns which operation is allowed to use it.
type LogicalRemovalResidueName struct {
	value string
}

// NewLogicalRemovalResidueName validates one canonical reserved sibling name.
func NewLogicalRemovalResidueName(value string) (LogicalRemovalResidueName, error) {
	if strings.TrimSpace(value) != value || value == "" || value == "." || value == ".." {
		return LogicalRemovalResidueName{}, fmt.Errorf("logical removal residue name must be a trimmed non-empty component")
	}
	if !utf8.ValidString(value) || strings.ContainsAny(value, "/\\") || strings.ContainsRune(value, '\x00') {
		return LogicalRemovalResidueName{}, fmt.Errorf("logical removal residue name must be one valid path component")
	}
	if !strings.HasPrefix(value, logicalRemovalResiduePrefix) || len(value) == len(logicalRemovalResiduePrefix) {
		return LogicalRemovalResidueName{}, fmt.Errorf("logical removal residue name must use the reserved tombstone prefix")
	}
	return LogicalRemovalResidueName{value: value}, nil
}

// String returns the exact persisted sibling name.
func (name LogicalRemovalResidueName) String() string { return name.value }

// Valid reports whether the value was constructed by the canonical boundary.
func (name LogicalRemovalResidueName) Valid() bool {
	_, err := NewLogicalRemovalResidueName(name.value)
	return err == nil
}
