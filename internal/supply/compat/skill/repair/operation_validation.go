package repair

import (
	"bytes"
	"fmt"
	"io/fs"
	"strings"
	"unicode"

	skillcompat "github.com/isty2e/daem/internal/supply/compat/skill"
)

// Validate rejects unknown, multi-body, malformed, and no-op variants.
func (operation Operation) Validate() error {
	bodyCount := 0
	if operation.rename != nil {
		bodyCount++
	}
	if operation.replaceBytes != nil {
		bodyCount++
	}
	if operation.setFrontmatterString != nil {
		bodyCount++
	}
	if bodyCount != 1 {
		return fmt.Errorf("repair operation %q must contain exactly one body", operation.kind)
	}
	switch operation.kind {
	case OperationRename:
		if operation.rename == nil {
			return fmt.Errorf("rename operation body is required")
		}
		return operation.rename.validate()
	case OperationReplaceBytes:
		if operation.replaceBytes == nil {
			return fmt.Errorf("replace_bytes operation body is required")
		}
		return operation.replaceBytes.validate()
	case OperationSetFrontmatterString:
		if operation.setFrontmatterString == nil {
			return fmt.Errorf("set_frontmatter_string operation body is required")
		}
		return operation.setFrontmatterString.validate()
	default:
		return fmt.Errorf("unknown repair operation kind %q", operation.kind)
	}
}

func (operation RenameOperation) validate() error {
	if err := validateRecipePath(operation.from); err != nil {
		return fmt.Errorf("rename from: %w", err)
	}
	if err := validateRecipePath(operation.to); err != nil {
		return fmt.Errorf("rename to: %w", err)
	}
	if !isSkillDocumentCasingPair(operation.from, operation.to) {
		return fmt.Errorf("rename must change skill.md casing to or from SKILL.md")
	}
	if err := operation.fileHash.Validate(); err != nil {
		return fmt.Errorf("rename file hash: %w", err)
	}
	if operation.mode&^uint32(0o777) != 0 {
		return fmt.Errorf("rename mode %#o contains unsupported bits", operation.mode)
	}
	return nil
}

func (operation ReplaceBytesOperation) validate() error {
	if err := validateRecipePath(operation.path); err != nil {
		return fmt.Errorf("replace_bytes path: %w", err)
	}
	if operation.path != "SKILL.md" {
		return fmt.Errorf("replace_bytes path must be SKILL.md")
	}
	if operation.offset < 0 {
		return fmt.Errorf("replace_bytes offset must be non-negative")
	}
	if err := validateReplacementOperandSizes(operation.old, operation.new); err != nil {
		return err
	}
	if bytes.Equal(operation.old, operation.new) {
		return fmt.Errorf("replace_bytes old and new bytes must differ")
	}
	if err := operation.inputHash.Validate(); err != nil {
		return fmt.Errorf("replace_bytes input hash: %w", err)
	}
	if err := operation.outputHash.Validate(); err != nil {
		return fmt.Errorf("replace_bytes output hash: %w", err)
	}
	if operation.inputHash == operation.outputHash {
		return fmt.Errorf("replace_bytes input and output hashes must differ")
	}
	return nil
}

func validateReplacementOperandSizes(oldBytes []byte, newBytes []byte) error {
	if err := skillcompat.CheckSkillDocumentSize(int64(len(oldBytes))); err != nil {
		return fmt.Errorf("repair old bytes: %w", err)
	}
	if err := skillcompat.CheckSkillDocumentSize(int64(len(newBytes))); err != nil {
		return fmt.Errorf("repair new bytes: %w", err)
	}
	return nil
}

func isSkillDocumentCasingPair(from string, to string) bool {
	return from == "skill.md" && to == "SKILL.md" ||
		from == "SKILL.md" && to == "skill.md"
}

func (operation SetFrontmatterStringOperation) validate() error {
	if operation.path != "SKILL.md" {
		return fmt.Errorf("set_frontmatter_string path must be SKILL.md")
	}
	if err := validateFrontmatterField(operation.field); err != nil {
		return err
	}
	return ReplaceBytesOperation{
		path:       operation.path,
		offset:     operation.offset,
		old:        operation.old,
		new:        operation.new,
		inputHash:  operation.inputHash,
		outputHash: operation.outputHash,
	}.validate()
}

func validateRecipePath(value string) error {
	if value == "." || !fs.ValidPath(value) || strings.ContainsRune(value, '\\') {
		return fmt.Errorf("repair path %q is not a canonical relative file path", value)
	}
	if strings.IndexFunc(value, func(character rune) bool {
		return unicode.IsControl(character) || unicode.Is(unicode.Bidi_Control, character)
	}) >= 0 {
		return fmt.Errorf("repair path %q contains an unsafe control character", value)
	}
	return nil
}

func validateFrontmatterField(value string) error {
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("frontmatter field is required and must be trimmed")
	}
	if strings.ContainsAny(value, ":\r\n\x00") {
		return fmt.Errorf("frontmatter field %q contains an unsafe character", value)
	}
	return nil
}
