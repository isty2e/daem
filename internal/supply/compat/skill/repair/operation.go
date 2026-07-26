package repair

import (
	"fmt"

	"github.com/isty2e/daem/internal/supply/artifact"
)

// OperationKind identifies one replayable mechanical repair operation.
type OperationKind string

const (
	OperationRename               OperationKind = "rename"
	OperationReplaceBytes         OperationKind = "replace_bytes"
	OperationSetFrontmatterString OperationKind = "set_frontmatter_string"
)

// Operation is one closed repair operation with exact pre/postconditions.
type Operation struct {
	kind                 OperationKind
	rename               *RenameOperation
	replaceBytes         *ReplaceBytesOperation
	setFrontmatterString *SetFrontmatterStringOperation
}

// RenameOperation changes only the admitted skill document casing.
type RenameOperation struct {
	from     string
	to       string
	fileHash artifact.ContentHash
	mode     uint32
}

// ReplaceBytesOperation replaces exact SKILL.md bytes at a recorded offset.
type ReplaceBytesOperation struct {
	path       string
	offset     int
	old        []byte
	new        []byte
	inputHash  artifact.ContentHash
	outputHash artifact.ContentHash
}

// SetFrontmatterStringOperation records one exact simple YAML scalar edit.
type SetFrontmatterStringOperation struct {
	path       string
	field      string
	oldValue   *string
	newValue   string
	offset     int
	old        []byte
	new        []byte
	inputHash  artifact.ContentHash
	outputHash artifact.ContentHash
}

// NewRenameOperation constructs a lossless skill document casing change.
func NewRenameOperation(
	from string,
	to string,
	fileHash artifact.ContentHash,
	mode uint32,
) (Operation, error) {
	operation := Operation{
		kind: OperationRename,
		rename: &RenameOperation{
			from:     from,
			to:       to,
			fileHash: fileHash,
			mode:     mode,
		},
	}
	if err := operation.Validate(); err != nil {
		return Operation{}, err
	}
	return operation, nil
}

// NewReplaceBytesOperation constructs one exact SKILL.md byte replacement.
func NewReplaceBytesOperation(
	path string,
	offset int,
	oldBytes []byte,
	newBytes []byte,
	inputHash artifact.ContentHash,
	outputHash artifact.ContentHash,
) (Operation, error) {
	if err := validateReplacementOperandSizes(oldBytes, newBytes); err != nil {
		return Operation{}, err
	}
	operation := Operation{
		kind: OperationReplaceBytes,
		replaceBytes: &ReplaceBytesOperation{
			path:       path,
			offset:     offset,
			old:        append([]byte(nil), oldBytes...),
			new:        append([]byte(nil), newBytes...),
			inputHash:  inputHash,
			outputHash: outputHash,
		},
	}
	if err := operation.Validate(); err != nil {
		return Operation{}, err
	}
	return operation, nil
}

// NewSetFrontmatterStringOperation constructs one exact frontmatter string edit.
func NewSetFrontmatterStringOperation(
	path string,
	field string,
	oldValue *string,
	newValue string,
	offset int,
	oldBytes []byte,
	newBytes []byte,
	inputHash artifact.ContentHash,
	outputHash artifact.ContentHash,
) (Operation, error) {
	if err := validateReplacementOperandSizes(oldBytes, newBytes); err != nil {
		return Operation{}, err
	}
	operation := Operation{
		kind: OperationSetFrontmatterString,
		setFrontmatterString: &SetFrontmatterStringOperation{
			path:       path,
			field:      field,
			oldValue:   cloneStringPointer(oldValue),
			newValue:   newValue,
			offset:     offset,
			old:        append([]byte(nil), oldBytes...),
			new:        append([]byte(nil), newBytes...),
			inputHash:  inputHash,
			outputHash: outputHash,
		},
	}
	if err := operation.Validate(); err != nil {
		return Operation{}, err
	}
	return operation, nil
}

// Kind returns the operation variant.
func (operation Operation) Kind() OperationKind { return operation.kind }

// Rename returns the rename body when this is a rename operation.
func (operation Operation) Rename() (RenameOperation, bool) {
	if operation.kind != OperationRename || operation.rename == nil {
		return RenameOperation{}, false
	}
	return *operation.rename, true
}

// ReplaceBytes returns the byte-replacement body when present.
func (operation Operation) ReplaceBytes() (ReplaceBytesOperation, bool) {
	if operation.kind != OperationReplaceBytes || operation.replaceBytes == nil {
		return ReplaceBytesOperation{}, false
	}
	return cloneReplaceBytes(*operation.replaceBytes), true
}

// SetFrontmatterString returns the semantic frontmatter edit when present.
func (operation Operation) SetFrontmatterString() (SetFrontmatterStringOperation, bool) {
	if operation.kind != OperationSetFrontmatterString || operation.setFrontmatterString == nil {
		return SetFrontmatterStringOperation{}, false
	}
	return cloneSetFrontmatterString(*operation.setFrontmatterString), true
}

// Inverse reconstructs the exact reverse operation.
func (operation Operation) Inverse() (Operation, error) {
	if err := operation.Validate(); err != nil {
		return Operation{}, err
	}
	switch operation.kind {
	case OperationRename:
		body := *operation.rename
		return NewRenameOperation(body.to, body.from, body.fileHash, body.mode)
	case OperationReplaceBytes:
		body := operation.replaceBytes
		return NewReplaceBytesOperation(
			body.path,
			body.offset,
			body.new,
			body.old,
			body.outputHash,
			body.inputHash,
		)
	case OperationSetFrontmatterString:
		body := operation.setFrontmatterString
		if body.oldValue == nil {
			return NewReplaceBytesOperation(
				body.path,
				body.offset,
				body.new,
				body.old,
				body.outputHash,
				body.inputHash,
			)
		}
		newOldValue := body.newValue
		return NewSetFrontmatterStringOperation(
			body.path,
			body.field,
			&newOldValue,
			*body.oldValue,
			body.offset,
			body.new,
			body.old,
			body.outputHash,
			body.inputHash,
		)
	default:
		return Operation{}, fmt.Errorf("unknown repair operation kind %q", operation.kind)
	}
}

// Summary returns a stable human-readable description.
func (operation Operation) Summary() string {
	switch operation.kind {
	case OperationRename:
		if body := operation.rename; body != nil {
			return fmt.Sprintf("rename file: %s -> %s", body.from, body.to)
		}
	case OperationReplaceBytes:
		if body := operation.replaceBytes; body != nil && body.path == "SKILL.md" && body.offset == 0 {
			return "normalize frontmatter delimiter: line 1 -> \"---\""
		}
	case OperationSetFrontmatterString:
		if body := operation.setFrontmatterString; body != nil {
			oldValue := ""
			if body.oldValue != nil {
				oldValue = *body.oldValue
			}
			return fmt.Sprintf("set frontmatter %s: %q -> %q", body.field, oldValue, body.newValue)
		}
	}
	return string(operation.kind)
}

func (operation Operation) clone() Operation {
	cloned := Operation{kind: operation.kind}
	if operation.rename != nil {
		body := *operation.rename
		cloned.rename = &body
	}
	if operation.replaceBytes != nil {
		body := cloneReplaceBytes(*operation.replaceBytes)
		cloned.replaceBytes = &body
	}
	if operation.setFrontmatterString != nil {
		body := cloneSetFrontmatterString(*operation.setFrontmatterString)
		cloned.setFrontmatterString = &body
	}
	return cloned
}

func (operation RenameOperation) From() string                   { return operation.from }
func (operation RenameOperation) To() string                     { return operation.to }
func (operation RenameOperation) FileHash() artifact.ContentHash { return operation.fileHash }
func (operation RenameOperation) Mode() uint32                   { return operation.mode }
func (operation ReplaceBytesOperation) Path() string             { return operation.path }
func (operation ReplaceBytesOperation) Offset() int              { return operation.offset }
func (operation ReplaceBytesOperation) Old() []byte              { return append([]byte(nil), operation.old...) }

func (operation ReplaceBytesOperation) New() []byte { return append([]byte(nil), operation.new...) }

func (operation ReplaceBytesOperation) InputHash() artifact.ContentHash { return operation.inputHash }

func (operation ReplaceBytesOperation) OutputHash() artifact.ContentHash { return operation.outputHash }
func (operation SetFrontmatterStringOperation) Path() string             { return operation.path }
func (operation SetFrontmatterStringOperation) Field() string            { return operation.field }
func (operation SetFrontmatterStringOperation) OldValue() (string, bool) {
	if operation.oldValue == nil {
		return "", false
	}
	return *operation.oldValue, true
}
func (operation SetFrontmatterStringOperation) NewValue() string { return operation.newValue }
func (operation SetFrontmatterStringOperation) Offset() int      { return operation.offset }
func (operation SetFrontmatterStringOperation) Old() []byte {
	return append([]byte(nil), operation.old...)
}

func (operation SetFrontmatterStringOperation) New() []byte {
	return append([]byte(nil), operation.new...)
}

func (operation SetFrontmatterStringOperation) InputHash() artifact.ContentHash {
	return operation.inputHash
}

func (operation SetFrontmatterStringOperation) OutputHash() artifact.ContentHash {
	return operation.outputHash
}

func cloneReplaceBytes(operation ReplaceBytesOperation) ReplaceBytesOperation {
	operation.old = append([]byte(nil), operation.old...)
	operation.new = append([]byte(nil), operation.new...)
	return operation
}

func cloneSetFrontmatterString(operation SetFrontmatterStringOperation) SetFrontmatterStringOperation {
	operation.oldValue = cloneStringPointer(operation.oldValue)
	operation.old = append([]byte(nil), operation.old...)
	operation.new = append([]byte(nil), operation.new...)
	return operation
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
