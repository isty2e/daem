package repair

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
)

func TestApplyRenameRequiresExactDestinationEntry(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "skill.md", "content")
	fromPath := filepath.Join(root, "skill.md")
	if err := os.Chmod(fromPath, 0o751); err != nil {
		t.Fatalf("Chmod() error: %v", err)
	}
	state, err := skillDocumentState(context.Background(), root, "skill.md")
	if err != nil {
		t.Fatalf("skillDocumentState() error: %v", err)
	}

	err = applyRename(context.Background(), root, RenameOperation{
		from:     "skill.md",
		to:       "SKILL.md",
		fileHash: state.Hash,
		mode:     state.Mode,
	})
	if err != nil {
		t.Fatalf("applyRename() error: %v", err)
	}
	if directoryHasEntry(t, root, "skill.md") {
		t.Fatal("exact source entry remains after case-only rename")
	}
	if !directoryHasEntry(t, root, "SKILL.md") {
		t.Fatal("exact destination entry is missing after case-only rename")
	}
	info, err := os.Lstat(filepath.Join(root, "SKILL.md"))
	if err != nil {
		t.Fatalf("Lstat(destination) error: %v", err)
	}
	if info.Mode().Perm() != 0o751 {
		t.Fatalf("destination mode = %#o, want %#o", info.Mode().Perm(), os.FileMode(0o751))
	}
}

func TestApplyRenameRejectsCaseOnlyHardlinkNoop(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "skill.md", "content")
	if err := os.Link(filepath.Join(root, "skill.md"), filepath.Join(root, "SKILL.md")); err != nil {
		if os.IsExist(err) {
			t.Skip("filesystem does not permit distinct case-only entries")
		}
		t.Skipf("hard links unavailable: %v", err)
	}
	state, err := skillDocumentState(context.Background(), root, "skill.md")
	if err != nil {
		t.Fatalf("skillDocumentState() error: %v", err)
	}

	err = applyRename(context.Background(), root, RenameOperation{
		from:     "skill.md",
		to:       "SKILL.md",
		fileHash: state.Hash,
		mode:     state.Mode,
	})
	if err == nil {
		t.Fatal("applyRename() accepted a same-inode rename that left the source entry")
	}
	if !strings.Contains(err.Error(), "remains as exact") {
		t.Fatalf("error = %q, want exact source-entry rejection", err)
	}
}

func TestApplySetFrontmatterStringRejectsMismatchedOldScalarBeforeWrite(t *testing.T) {
	root := t.TempDir()
	original := []byte("---\nname: before\ndescription: Demo\n---\n")
	repaired := []byte("---\nname: after\ndescription: Demo\n---\n")
	writeTestFile(t, root, "SKILL.md", string(original))
	wrongOldValue := "other"
	operation := SetFrontmatterStringOperation{
		path:       "SKILL.md",
		field:      "name",
		oldValue:   &wrongOldValue,
		newValue:   "after",
		offset:     strings.Index(string(original), "before"),
		old:        []byte("before"),
		new:        []byte("after"),
		inputHash:  artifact.HashFileContent(original),
		outputHash: artifact.HashFileContent(repaired),
	}

	err := applySetFrontmatterString(context.Background(), root, operation)
	if err == nil || !strings.Contains(err.Error(), `value "before" does not match expected "other"`) {
		t.Fatalf("applySetFrontmatterString error = %v, want old-value mismatch", err)
	}
	if content := readTestFile(t, root, "SKILL.md"); content != string(original) {
		t.Fatalf("failed semantic precondition changed SKILL.md to %q", content)
	}
}

func TestApplySetFrontmatterStringRejectsMismatchedGenericNewScalarBeforeWrite(t *testing.T) {
	root := t.TempDir()
	original := []byte("---\nname: demo\ndescription: Demo\nlicense: MIT\n---\n")
	tampered := []byte("---\nname: demo\ndescription: Demo\nlicense: GPL\n---\n")
	writeTestFile(t, root, "SKILL.md", string(original))
	oldValue := "MIT"
	operation := SetFrontmatterStringOperation{
		path:       "SKILL.md",
		field:      "license",
		oldValue:   &oldValue,
		newValue:   "Apache-2.0",
		offset:     strings.Index(string(original), "MIT"),
		old:        []byte("MIT"),
		new:        []byte("GPL"),
		inputHash:  artifact.HashFileContent(original),
		outputHash: artifact.HashFileContent(tampered),
	}

	err := applySetFrontmatterString(context.Background(), root, operation)
	if err == nil || !strings.Contains(err.Error(), `value "GPL" does not match expected "Apache-2.0" after repair`) {
		t.Fatalf("applySetFrontmatterString error = %v, want new-value mismatch", err)
	}
	if content := readTestFile(t, root, "SKILL.md"); content != string(original) {
		t.Fatalf("failed semantic postcondition changed SKILL.md to %q", content)
	}
}

func TestApplySetFrontmatterStringDoesNotTreatNullFieldAsAbsent(t *testing.T) {
	root := t.TempDir()
	original := []byte("---\nname: null\ndescription: Demo\n---\n")
	repaired := []byte("---\nname: review\ndescription: Demo\n---\n")
	writeTestFile(t, root, "SKILL.md", string(original))
	operation := SetFrontmatterStringOperation{
		path:       "SKILL.md",
		field:      "name",
		oldValue:   nil,
		newValue:   "review",
		offset:     strings.Index(string(original), "name: null"),
		old:        []byte("name: null"),
		new:        []byte("name: review"),
		inputHash:  artifact.HashFileContent(original),
		outputHash: artifact.HashFileContent(repaired),
	}

	err := applySetFrontmatterString(context.Background(), root, operation)
	if err == nil || !strings.Contains(err.Error(), "is present but repair expects it absent") {
		t.Fatalf("applySetFrontmatterString error = %v, want present-null rejection", err)
	}
}

func TestApplyReplaceBytesPreservesExactPermissionBits(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "SKILL.md", "before")
	targetPath := filepath.Join(root, "SKILL.md")
	if err := os.Chmod(targetPath, 0o751); err != nil {
		t.Fatalf("Chmod() error: %v", err)
	}
	operation, err := NewReplaceBytesOperation(
		"SKILL.md",
		0,
		[]byte("before"),
		[]byte("after"),
		artifact.HashFileContent([]byte("before")),
		artifact.HashFileContent([]byte("after")),
	)
	if err != nil {
		t.Fatalf("NewReplaceBytesOperation() error: %v", err)
	}
	body, ok := operation.ReplaceBytes()
	if !ok {
		t.Fatal("ReplaceBytes() reports wrong variant")
	}

	if err := applyReplaceBytes(context.Background(), root, body); err != nil {
		t.Fatalf("applyReplaceBytes() error: %v", err)
	}
	if content := readTestFile(t, root, "SKILL.md"); content != "after" {
		t.Fatalf("content = %q, want after", content)
	}
	info, err := os.Lstat(targetPath)
	if err != nil {
		t.Fatalf("Lstat() error: %v", err)
	}
	if info.Mode().Perm() != 0o751 {
		t.Fatalf("mode = %#o, want %#o", info.Mode().Perm(), os.FileMode(0o751))
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir() error: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".daem-skill-repair-") {
			t.Fatalf("successful replacement left temporary file %q", entry.Name())
		}
	}
}
