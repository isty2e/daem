package repair

import (
	"bytes"
	"context"
	"math"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

func testArtifact(t *testing.T, root string) (artifact.ExactIdentity, access.View) {
	t.Helper()
	view, err := access.OpenView(root)
	if err != nil {
		t.Fatalf("OpenView(%q) error: %v", root, err)
	}
	contentHash, err := view.Hash(context.Background())
	if err != nil {
		t.Fatalf("Hash(%q) error: %v", root, err)
	}
	identity, err := artifact.NewExactIdentity(
		artifact.SourceID("local:"+filepath.ToSlash(root)),
		"",
		artifact.ArtifactKindDirectory,
		contentHash,
	)
	if err != nil {
		t.Fatalf("NewExactIdentity(%q) error: %v", root, err)
	}
	return identity, view
}

func writeTestFile(t *testing.T, root string, relativePath string, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll(%q) error: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error: %v", path, err)
	}
}

func readTestFile(t *testing.T, root string, relativePath string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relativePath)))
	if err != nil {
		t.Fatalf("ReadFile(%q) error: %v", relativePath, err)
	}
	return string(content)
}

func readTestViewFile(t *testing.T, view access.View, relativePath string) string {
	t.Helper()
	content, err := view.ReadFile(context.Background(), relativePath, math.MaxInt64)
	if err != nil {
		t.Fatalf("ReadFile(%q) error: %v", relativePath, err)
	}
	return string(content.Bytes())
}

func resultView(t *testing.T, result Result) access.View {
	t.Helper()
	view, err := result.View()
	if err != nil {
		t.Fatalf("Result.View() error: %v", err)
	}
	return view
}

func releaseResult(t *testing.T, result Result) {
	t.Helper()
	if err := result.Release(); err != nil {
		t.Fatalf("Result.Release() error: %v", err)
	}
}

func directoryHasEntry(t *testing.T, root string, name string) bool {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir(%q) error: %v", root, err)
	}
	for _, entry := range entries {
		if entry.Name() == name {
			return true
		}
	}
	return false
}

func chmodTestDirectoryTree(root string, mode os.FileMode) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.Chmod(path, mode)
		}
		return nil
	})
}

func assertTestViewsEqual(t *testing.T, expected access.View, actual access.View) {
	t.Helper()
	assertTestViewDirectoryEqual(t, expected, actual, ".")
}

func assertTestViewDirectoryEqual(
	t *testing.T,
	expected access.View,
	actual access.View,
	relativePath string,
) {
	t.Helper()
	expectedEntries, err := expected.ReadDirectory(context.Background(), relativePath)
	if err != nil {
		t.Fatalf("expected ReadDirectory(%q) error: %v", relativePath, err)
	}
	actualEntries, err := actual.ReadDirectory(context.Background(), relativePath)
	if err != nil {
		t.Fatalf("actual ReadDirectory(%q) error: %v", relativePath, err)
	}
	if len(actualEntries) != len(expectedEntries) {
		t.Fatalf("ReadDirectory(%q) entry count = %d, want %d", relativePath, len(actualEntries), len(expectedEntries))
	}
	for index, expectedEntry := range expectedEntries {
		actualEntry := actualEntries[index]
		if actualEntry.Name() != expectedEntry.Name() ||
			actualEntry.Kind() != expectedEntry.Kind() ||
			actualEntry.Mode().Perm() != expectedEntry.Mode().Perm() {
			t.Fatalf(
				"ReadDirectory(%q) entry[%d] = %q/%s/%#o, want %q/%s/%#o",
				relativePath,
				index,
				actualEntry.Name(),
				actualEntry.Kind(),
				actualEntry.Mode().Perm(),
				expectedEntry.Name(),
				expectedEntry.Kind(),
				expectedEntry.Mode().Perm(),
			)
		}
		childPath := path.Join(relativePath, expectedEntry.Name())
		switch expectedEntry.Kind() {
		case access.EntryKindDirectory:
			assertTestViewDirectoryEqual(t, expected, actual, childPath)
		case access.EntryKindFile:
			expectedContent, err := expected.ReadFile(context.Background(), childPath, math.MaxInt64)
			if err != nil {
				t.Fatalf("expected ReadFile(%q) error: %v", childPath, err)
			}
			actualContent, err := actual.ReadFile(context.Background(), childPath, math.MaxInt64)
			if err != nil {
				t.Fatalf("actual ReadFile(%q) error: %v", childPath, err)
			}
			if !bytes.Equal(actualContent.Bytes(), expectedContent.Bytes()) ||
				actualContent.Mode().Perm() != expectedContent.Mode().Perm() {
				t.Fatalf("ReadFile(%q) content or mode differs", childPath)
			}
		default:
			t.Fatalf("expected tree contains unsupported entry %q of kind %s", childPath, expectedEntry.Kind())
		}
	}
}
