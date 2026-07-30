//go:build darwin || linux

package cache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/supply/artifact"
)

func TestPublishDirectoryOnceRootedPublishesAndReusesValidEntry(t *testing.T) {
	cacheRoot := t.TempDir()
	root := mustCaptureCacheRoot(t, cacheRoot)
	defer root.Close()
	spec := rootedTestEntrySpec(t, "valid")
	buildCalls := 0

	for attempt := range 2 {
		contentHash, contentKind, published, err := PublishDirectoryOnceRooted(
			t.Context(),
			root,
			"artifacts/valid",
			spec,
			func(tempRoot string) (artifact.ContentHash, artifact.ArtifactKind, error) {
				buildCalls++
				return writeRootedTestContent(tempRoot, "valid")
			},
		)
		if err != nil {
			t.Fatalf("attempt %d PublishDirectoryOnceRooted returned error: %v", attempt, err)
		}
		if published != (attempt == 0) {
			t.Fatalf("attempt %d published = %t", attempt, published)
		}
		if contentHash != artifact.HashFileContent([]byte("valid")) ||
			contentKind != artifact.ArtifactKindFile {
			t.Fatalf(
				"attempt %d content identity = %q/%q, want valid file identity",
				attempt,
				contentHash,
				contentKind,
			)
		}
	}
	if buildCalls != 1 {
		t.Fatalf("build calls = %d, want 1", buildCalls)
	}
	assertFileContent(t, filepath.Join(cacheRoot, "artifacts", "valid", "content.txt"), "valid")
}

func TestPublishDirectoryOnceRootedRejectsSymlinkedAncestorWithoutBuilding(t *testing.T) {
	cacheRoot := t.TempDir()
	root := mustCaptureCacheRoot(t, cacheRoot)
	defer root.Close()
	external := t.TempDir()
	if err := os.Symlink(external, filepath.Join(cacheRoot, "artifacts")); err != nil {
		t.Fatalf("create artifact-ancestor symlink: %v", err)
	}
	buildCalled := false

	_, _, _, err := PublishDirectoryOnceRooted(
		t.Context(),
		root,
		"artifacts/redirected",
		rootedTestEntrySpec(t, "redirected"),
		func(string) (artifact.ContentHash, artifact.ArtifactKind, error) {
			buildCalled = true
			return "", "", nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("PublishDirectoryOnceRooted error = %v, want symlink rejection", err)
	}
	if buildCalled {
		t.Fatal("build ran after rooted artifact ancestor rejection")
	}
	entries, err := os.ReadDir(external)
	if err != nil {
		t.Fatalf("read external directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("external directory entries = %v, want none", entries)
	}
}

func TestPublishDirectoryOnceRootedRejectsUnownedEntryWithoutRemoval(t *testing.T) {
	cacheRoot := t.TempDir()
	root := mustCaptureCacheRoot(t, cacheRoot)
	defer root.Close()
	entryRoot := filepath.Join(cacheRoot, "artifacts", "unowned")
	if err := os.MkdirAll(entryRoot, 0o700); err != nil {
		t.Fatalf("create unowned entry: %v", err)
	}
	sentinel := filepath.Join(entryRoot, "sentinel")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
		t.Fatalf("write unowned sentinel: %v", err)
	}

	_, _, _, err := PublishDirectoryOnceRooted(
		t.Context(),
		root,
		"artifacts/unowned",
		rootedTestEntrySpec(t, "replacement"),
		func(tempRoot string) (artifact.ContentHash, artifact.ArtifactKind, error) {
			return writeRootedTestContent(tempRoot, "replacement")
		},
	)
	if err == nil || !strings.Contains(err.Error(), "completion record is missing") {
		t.Fatalf("PublishDirectoryOnceRooted error = %v, want unowned-entry rejection", err)
	}
	assertFileContent(t, sentinel, "keep")
}

func TestPublishDirectoryOnceRootedRejectsSymlinkedCompletionRecordWithoutRemoval(t *testing.T) {
	cacheRoot := t.TempDir()
	root := mustCaptureCacheRoot(t, cacheRoot)
	defer root.Close()
	entryRoot := filepath.Join(cacheRoot, "artifacts", "symlinked-record")
	if err := os.MkdirAll(entryRoot, 0o700); err != nil {
		t.Fatalf("create unowned entry: %v", err)
	}
	assertedContent := filepath.Join(entryRoot, "content.txt")
	if err := os.WriteFile(assertedContent, []byte("keep-entry"), 0o600); err != nil {
		t.Fatalf("write unowned entry content: %v", err)
	}
	externalRecord := filepath.Join(t.TempDir(), "external-record")
	if err := os.WriteFile(externalRecord, []byte("keep-external"), 0o600); err != nil {
		t.Fatalf("write external completion record: %v", err)
	}
	if err := os.Symlink(externalRecord, filepath.Join(entryRoot, completionRecordName)); err != nil {
		t.Fatalf("create completion-record symlink: %v", err)
	}
	buildCalled := false

	_, _, _, err := PublishDirectoryOnceRooted(
		t.Context(),
		root,
		"artifacts/symlinked-record",
		rootedTestEntrySpec(t, "replacement"),
		func(string) (artifact.ContentHash, artifact.ArtifactKind, error) {
			buildCalled = true
			return "", "", nil
		},
	)
	if err == nil ||
		!strings.Contains(err.Error(), "unsupported entry") ||
		!strings.Contains(err.Error(), completionRecordName) {
		t.Fatalf("PublishDirectoryOnceRooted error = %v, want completion-record symlink rejection", err)
	}
	if buildCalled {
		t.Fatal("build ran after completion-record symlink rejection")
	}
	assertFileContent(t, assertedContent, "keep-entry")
	assertFileContent(t, externalRecord, "keep-external")
}

func TestPublishDirectoryOnceRootedRebuildsOwnedCorruptEntry(t *testing.T) {
	cacheRoot := t.TempDir()
	root := mustCaptureCacheRoot(t, cacheRoot)
	defer root.Close()
	spec := rootedTestEntrySpec(t, "first")
	relativeRoot := "artifacts/corrupt"
	if _, _, _, err := PublishDirectoryOnceRooted(
		t.Context(),
		root,
		relativeRoot,
		spec,
		func(tempRoot string) (artifact.ContentHash, artifact.ArtifactKind, error) {
			return writeRootedTestContent(tempRoot, "first")
		},
	); err != nil {
		t.Fatalf("prime PublishDirectoryOnceRooted returned error: %v", err)
	}
	contentPath := filepath.Join(cacheRoot, filepath.FromSlash(relativeRoot), "content.txt")
	if err := os.WriteFile(contentPath, []byte("corrupt"), 0o600); err != nil {
		t.Fatalf("corrupt rooted cache entry: %v", err)
	}

	_, _, published, err := PublishDirectoryOnceRooted(
		t.Context(),
		root,
		relativeRoot,
		spec,
		func(tempRoot string) (artifact.ContentHash, artifact.ArtifactKind, error) {
			return writeRootedTestContent(tempRoot, "first")
		},
	)
	if err != nil || !published {
		t.Fatalf("repair PublishDirectoryOnceRooted = %t, %v, want published", published, err)
	}
	assertFileContent(t, contentPath, "first")
}

func TestPublishDirectoryOnceRootedRejectsReplacedBuildStage(t *testing.T) {
	cacheRoot := t.TempDir()
	root := mustCaptureCacheRoot(t, cacheRoot)
	defer root.Close()
	external := t.TempDir()
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
		t.Fatalf("write external sentinel: %v", err)
	}
	var movedStage string

	_, _, _, err := PublishDirectoryOnceRooted(
		t.Context(),
		root,
		"artifacts/raced",
		rootedTestEntrySpec(t, "raced"),
		func(tempRoot string) (artifact.ContentHash, artifact.ArtifactKind, error) {
			hash, kind, err := writeRootedTestContent(tempRoot, "raced")
			if err != nil {
				return "", "", err
			}
			movedStage = tempRoot + ".moved"
			if err := os.Rename(tempRoot, movedStage); err != nil {
				return "", "", err
			}
			if err := os.Symlink(external, tempRoot); err != nil {
				return "", "", err
			}
			return hash, kind, nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("PublishDirectoryOnceRooted error = %v, want stage replacement rejection", err)
	}
	assertFileContent(t, sentinel, "keep")
	if _, statErr := os.Lstat(filepath.Join(cacheRoot, "artifacts", "raced")); !os.IsNotExist(statErr) {
		t.Fatalf("raced cache entry stat error = %v, want missing", statErr)
	}
	if movedStage != "" {
		t.Cleanup(func() { _ = os.RemoveAll(movedStage) })
	}
}

func rootedTestEntrySpec(t *testing.T, content string) EntrySpec {
	t.Helper()
	key, err := NewKey("rooted-test", content)
	if err != nil {
		t.Fatalf("NewKey returned error: %v", err)
	}
	spec, err := NewEntrySpec(key, "content.txt", "", "")
	if err != nil {
		t.Fatalf("NewEntrySpec returned error: %v", err)
	}
	return spec
}

func mustCaptureCacheRoot(t *testing.T, cacheRoot string) *rootedpath.CapturedRoot {
	t.Helper()
	physicalRoot, err := filepath.EvalSymlinks(cacheRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks returned error: %v", err)
	}
	root, err := rootedpath.CaptureRootNoFollow(physicalRoot)
	if err != nil {
		t.Fatalf("CaptureRootNoFollow returned error: %v", err)
	}
	return root
}

func writeRootedTestContent(
	tempRoot string,
	content string,
) (artifact.ContentHash, artifact.ArtifactKind, error) {
	if err := os.WriteFile(
		filepath.Join(tempRoot, "content.txt"),
		[]byte(content),
		0o600,
	); err != nil {
		return "", "", err
	}
	return artifact.HashFileContent([]byte(content)), artifact.ArtifactKindFile, nil
}
