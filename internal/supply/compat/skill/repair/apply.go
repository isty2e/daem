package repair

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	skillcompat "github.com/isty2e/daem/internal/supply/compat/skill"
)

func applyOperation(
	ctx context.Context,
	root string,
	operation Operation,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := operation.Validate(); err != nil {
		return err
	}
	switch operation.kind {
	case OperationRename:
		return applyRename(ctx, root, *operation.rename)
	case OperationReplaceBytes:
		return applyReplaceBytes(ctx, root, *operation.replaceBytes)
	case OperationSetFrontmatterString:
		return applySetFrontmatterString(ctx, root, *operation.setFrontmatterString)
	default:
		return fmt.Errorf("unknown repair operation kind %q", operation.kind)
	}
}

func applyRename(ctx context.Context, root string, operation RenameOperation) error {
	state, err := skillDocumentState(ctx, root, operation.from)
	if err != nil {
		return err
	}
	if state.Hash != operation.fileHash {
		return fmt.Errorf("%s hash %q does not match expected %q", operation.from, state.Hash, operation.fileHash)
	}
	if state.Mode != operation.mode {
		return fmt.Errorf("%s mode %#o does not match expected %#o", operation.from, state.Mode, operation.mode)
	}
	toPath, err := artifactPath(root, operation.to)
	if err != nil {
		return err
	}
	fromPath, err := artifactPath(root, operation.from)
	if err != nil {
		return err
	}
	fromInfo, err := os.Lstat(fromPath)
	if err != nil {
		return fmt.Errorf("stat %s: %w", operation.from, err)
	}
	if toInfo, err := os.Lstat(toPath); err == nil {
		if !os.SameFile(fromInfo, toInfo) {
			return fmt.Errorf("%s already exists", operation.to)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect %s: %w", operation.to, err)
	}
	if err := os.Rename(fromPath, toPath); err != nil {
		return fmt.Errorf("rename %s to %s: %w", operation.from, operation.to, err)
	}
	return verifyRenamePostcondition(ctx, root, operation)
}

func verifyRenamePostcondition(
	ctx context.Context,
	root string,
	operation RenameOperation,
) error {
	view, err := access.OpenView(root)
	if err != nil {
		return fmt.Errorf("inspect renamed artifact: %w", err)
	}
	fromKind, fromPresent, err := exactDirectoryEntry(ctx, view, operation.from)
	if err != nil {
		return err
	}
	if fromPresent {
		return fmt.Errorf("rename source %s remains as exact %s entry", operation.from, fromKind)
	}
	toKind, toPresent, err := exactDirectoryEntry(ctx, view, operation.to)
	if err != nil {
		return err
	}
	if !toPresent || toKind != access.EntryKindFile {
		return fmt.Errorf("rename destination %s is not an exact regular-file entry", operation.to)
	}
	state, err := skillDocumentState(ctx, root, operation.to)
	if err != nil {
		return err
	}
	if state.Hash != operation.fileHash || state.Mode != operation.mode {
		return fmt.Errorf("rename destination %s does not preserve source bytes and mode", operation.to)
	}
	return nil
}

func exactDirectoryEntry(
	ctx context.Context,
	view access.View,
	relativePath string,
) (access.EntryKind, bool, error) {
	directory := path.Dir(relativePath)
	name := path.Base(relativePath)
	entries, err := view.ReadDirectory(ctx, directory)
	if err != nil {
		return "", false, fmt.Errorf("inspect repair directory %s: %w", directory, err)
	}
	for _, entry := range entries {
		if entry.Name() == name {
			return entry.Kind(), true, nil
		}
	}
	return "", false, nil
}

func applyReplaceBytes(ctx context.Context, root string, operation ReplaceBytesOperation) error {
	content, err := applyBytesPrecondition(
		ctx,
		root,
		operation.path,
		operation.offset,
		operation.old,
		operation.inputHash,
	)
	if err != nil {
		return err
	}
	if err := checkSkillDocumentReplacementSize(
		len(content),
		len(operation.old),
		len(operation.new),
	); err != nil {
		return err
	}
	repaired := replaceAt(content, operation.offset, operation.old, operation.new)
	if err := skillcompat.CheckSkillDocumentSize(int64(len(repaired))); err != nil {
		return err
	}
	if outputHash := artifact.HashFileContent(repaired); outputHash != operation.outputHash {
		return fmt.Errorf("%s output hash %q does not match expected %q", operation.path, outputHash, operation.outputHash)
	}
	path, err := artifactPath(root, operation.path)
	if err != nil {
		return err
	}
	return writeExistingFile(path, repaired)
}

func applySetFrontmatterString(
	ctx context.Context,
	root string,
	operation SetFrontmatterStringOperation,
) error {
	content, err := applyBytesPrecondition(
		ctx,
		root,
		operation.path,
		operation.offset,
		operation.old,
		operation.inputHash,
	)
	if err != nil {
		return err
	}
	frontmatter, err := skillcompat.ParseSkillFrontmatter(content)
	if err != nil {
		return err
	}
	if err := validateFrontmatterStringPrecondition(frontmatter, operation); err != nil {
		return err
	}
	if err := checkSkillDocumentReplacementSize(
		len(content),
		len(operation.old),
		len(operation.new),
	); err != nil {
		return err
	}
	repaired := replaceAt(content, operation.offset, operation.old, operation.new)
	if err := skillcompat.CheckSkillDocumentSize(int64(len(repaired))); err != nil {
		return err
	}
	if outputHash := artifact.HashFileContent(repaired); outputHash != operation.outputHash {
		return fmt.Errorf("%s output hash %q does not match expected %q", operation.path, outputHash, operation.outputHash)
	}
	repairedFrontmatter, err := skillcompat.ParseSkillFrontmatter(repaired)
	if err != nil {
		return err
	}
	if err := validateFrontmatterStringPostcondition(repairedFrontmatter, operation); err != nil {
		return err
	}
	path, err := artifactPath(root, operation.path)
	if err != nil {
		return err
	}
	if err := writeExistingFile(path, repaired); err != nil {
		return err
	}

	return nil
}

func validateFrontmatterStringPrecondition(
	frontmatter skillcompat.SkillFrontmatter,
	operation SetFrontmatterStringOperation,
) error {
	_, fieldPresent := frontmatter.Fields[operation.field]
	if operation.oldValue == nil {
		if fieldPresent {
			return fmt.Errorf(
				"SKILL.md frontmatter field %q is present but repair expects it absent",
				operation.field,
			)
		}
		return nil
	}
	if !fieldPresent {
		return fmt.Errorf("SKILL.md frontmatter field %q is absent", operation.field)
	}
	actual, isString := frontmatter.StringField(operation.field)
	if !isString {
		return fmt.Errorf("SKILL.md frontmatter field %q is not a string", operation.field)
	}
	if actual != *operation.oldValue {
		return fmt.Errorf(
			"SKILL.md frontmatter field %q value %q does not match expected %q",
			operation.field,
			actual,
			*operation.oldValue,
		)
	}
	return nil
}

func validateFrontmatterStringPostcondition(
	frontmatter skillcompat.SkillFrontmatter,
	operation SetFrontmatterStringOperation,
) error {
	actual, isString := frontmatter.StringField(operation.field)
	if !isString {
		return fmt.Errorf("SKILL.md frontmatter field %q is not a string after repair", operation.field)
	}
	if actual != operation.newValue {
		return fmt.Errorf(
			"SKILL.md frontmatter field %q value %q does not match expected %q after repair",
			operation.field,
			actual,
			operation.newValue,
		)
	}
	return nil
}

func applyBytesPrecondition(
	ctx context.Context,
	root string,
	path string,
	offset int,
	oldBytes []byte,
	inputHash artifact.ContentHash,
) ([]byte, error) {
	state, err := skillDocumentState(ctx, root, path)
	if err != nil {
		return nil, err
	}
	if state.Hash != inputHash {
		return nil, fmt.Errorf("%s hash %q does not match expected %q", path, state.Hash, inputHash)
	}
	if offset < 0 || offset > len(state.Content) || len(oldBytes) > len(state.Content)-offset {
		return nil, fmt.Errorf("%s byte range is outside the file", path)
	}
	if !bytes.Equal(state.Content[offset:offset+len(oldBytes)], oldBytes) {
		return nil, fmt.Errorf("%s bytes at offset %d do not match repair precondition", path, offset)
	}
	return state.Content, nil
}
