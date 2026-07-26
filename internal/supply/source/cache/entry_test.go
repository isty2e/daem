package cache

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

func TestNewEntrySpecRejectsAmbiguousIdentityAndUnsafeContentPath(t *testing.T) {
	key := mustKey(t, "entry-spec", "value")
	for _, test := range []struct {
		name string
		path string
		hash artifact.ContentHash
		kind artifact.ArtifactKind
	}{
		{name: "empty path"},
		{name: "root path", path: "."},
		{name: "parent escape", path: "../content"},
		{name: "unclean path", path: "content/../file"},
		{name: "absolute path", path: "/content"},
		{name: "backslash", path: `content\file`},
		{name: "hash only", path: "content", hash: "sha256:hash"},
		{name: "kind only", path: "content", kind: artifact.ArtifactKindFile},
		{name: "unknown kind", path: "content", hash: "sha256:hash", kind: "archive"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewEntrySpec(key, test.path, test.hash, test.kind); err == nil {
				t.Fatal("NewEntrySpec returned nil error")
			}
		})
	}
}

func TestVerifyDirectoryRejectsContentSubstitution(t *testing.T) {
	root := filepath.Join(t.TempDir(), "entry")
	spec := testEntrySpec(t, "content-substitution")
	writeCompleteEntry(t, root, spec, "trusted")
	if err := os.WriteFile(filepath.Join(root, "content.txt"), []byte("substituted"), 0o600); err != nil {
		t.Fatalf("replace cached content: %v", err)
	}

	valid, err := VerifyDirectory(context.Background(), root, spec)
	if valid || !errors.Is(err, ErrInvalidEntry) {
		t.Fatalf("VerifyDirectory = %v, %v, want invalid entry", valid, err)
	}
}

func TestVerifyDirectoryRejectsCompletionRecordSwap(t *testing.T) {
	parent := t.TempDir()
	leftRoot := filepath.Join(parent, "left")
	rightRoot := filepath.Join(parent, "right")
	leftSpec := testEntrySpec(t, "left")
	rightSpec := testEntrySpec(t, "right")
	writeCompleteEntry(t, leftRoot, leftSpec, "same-content")
	writeCompleteEntry(t, rightRoot, rightSpec, "same-content")

	rightRecord, err := os.ReadFile(filepath.Join(rightRoot, completionRecordName))
	if err != nil {
		t.Fatalf("read right completion record: %v", err)
	}
	if err := os.WriteFile(filepath.Join(leftRoot, completionRecordName), rightRecord, 0o600); err != nil {
		t.Fatalf("swap completion record: %v", err)
	}

	valid, err := VerifyDirectory(context.Background(), leftRoot, leftSpec)
	if valid || !errors.Is(err, ErrInvalidEntry) {
		t.Fatalf("VerifyDirectory = %v, %v, want key-bound invalid entry", valid, err)
	}
}

func TestVerifyDirectoryRejectsMalformedCompletionRecords(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(completionRecord) []byte
	}{
		{name: "malformed", mutate: func(completionRecord) []byte { return []byte("not-json\n") }},
		{name: "unknown field", mutate: func(record completionRecord) []byte {
			return []byte(`{"version":1,"key":"` + record.Key + `","content_path":"content.txt","content_hash":"` + string(record.ContentHash) + `","kind":"file","extra":true}`)
		}},
		{name: "trailing value", mutate: func(record completionRecord) []byte {
			encoded, _ := json.Marshal(record)
			return append(encoded, []byte("\n{}\n")...)
		}},
		{name: "unknown version", mutate: func(record completionRecord) []byte {
			record.Version++
			encoded, _ := encodeCompletionRecord(record)
			return encoded
		}},
		{name: "wrong path", mutate: func(record completionRecord) []byte {
			record.ContentPath = "other.txt"
			encoded, _ := encodeCompletionRecord(record)
			return encoded
		}},
		{name: "wrong kind", mutate: func(record completionRecord) []byte {
			record.Kind = artifact.ArtifactKindDirectory
			encoded, _ := encodeCompletionRecord(record)
			return encoded
		}},
		{name: "wrong hash", mutate: func(record completionRecord) []byte {
			record.ContentHash = "sha256:wrong"
			encoded, _ := encodeCompletionRecord(record)
			return encoded
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "entry")
			spec := testEntrySpec(t, test.name)
			writeCompleteEntry(t, root, spec, "content")
			content, err := os.ReadFile(filepath.Join(root, completionRecordName))
			if err != nil {
				t.Fatal(err)
			}
			record, err := decodeCompletionRecord(content)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, completionRecordName), test.mutate(record), 0o600); err != nil {
				t.Fatal(err)
			}

			valid, err := VerifyDirectory(context.Background(), root, spec)
			if valid || !errors.Is(err, ErrInvalidEntry) {
				t.Fatalf("VerifyDirectory = %v, %v, want invalid entry", valid, err)
			}
		})
	}
}

func TestVerifyDirectoryRejectsOversizedCompletionRecord(t *testing.T) {
	root := filepath.Join(t.TempDir(), "entry")
	spec := testEntrySpec(t, "oversized-completion")
	writeCompleteEntry(t, root, spec, "content")
	if err := os.WriteFile(
		filepath.Join(root, completionRecordName),
		make([]byte, maximumCompletionBytes+1),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	valid, err := VerifyDirectory(context.Background(), root, spec)
	if valid || !errors.Is(err, ErrInvalidEntry) {
		t.Fatalf("VerifyDirectory = %v, %v, want oversized invalid entry", valid, err)
	}
}

func TestVerifyDirectoryRejectsCompletionModeAndSymlinkContent(t *testing.T) {
	t.Run("record mode", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "entry")
		spec := testEntrySpec(t, "record-mode")
		writeCompleteEntry(t, root, spec, "content")
		if err := os.Chmod(filepath.Join(root, completionRecordName), 0o644); err != nil {
			t.Fatal(err)
		}
		valid, err := VerifyDirectory(context.Background(), root, spec)
		if valid || !errors.Is(err, ErrInvalidEntry) {
			t.Fatalf("VerifyDirectory = %v, %v, want invalid entry", valid, err)
		}
	})

	t.Run("symlink content", func(t *testing.T) {
		parent := t.TempDir()
		root := filepath.Join(parent, "entry")
		spec := testEntrySpec(t, "symlink-content")
		writeCompleteEntry(t, root, spec, "content")
		target := filepath.Join(parent, "target")
		if err := os.WriteFile(target, []byte("content"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(root, "content.txt")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(root, "content.txt")); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		valid, err := VerifyDirectory(context.Background(), root, spec)
		if valid || !errors.Is(err, ErrInvalidEntry) {
			t.Fatalf("VerifyDirectory = %v, %v, want invalid entry", valid, err)
		}
	})

	t.Run("symlink content ancestor", func(t *testing.T) {
		parent := t.TempDir()
		root := filepath.Join(parent, "entry")
		targetRoot := filepath.Join(parent, "target")
		if err := os.MkdirAll(targetRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(targetRoot, "content.txt"), []byte("content"), 0o600); err != nil {
			t.Fatal(err)
		}
		key := mustKey(t, "cache-entry", "symlink-content-ancestor")
		spec, err := NewEntrySpec(key, "nested/content.txt", "", "")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(targetRoot, filepath.Join(root, "nested")); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		hash, kind, err := access.HashPath(context.Background(), filepath.Join(targetRoot, "content.txt"))
		if err != nil {
			t.Fatal(err)
		}
		record, err := newCompletionRecord(spec, hash, kind)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := encodeCompletionRecord(record)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, completionRecordName), encoded, 0o600); err != nil {
			t.Fatal(err)
		}

		valid, err := VerifyDirectory(context.Background(), root, spec)
		if valid || !errors.Is(err, ErrInvalidEntry) {
			t.Fatalf("VerifyDirectory = %v, %v, want symlink-ancestor rejection", valid, err)
		}
	})
}

func TestVerifyDirectoryEnforcesCallerExpectedIdentity(t *testing.T) {
	root := filepath.Join(t.TempDir(), "entry")
	looseSpec := testEntrySpec(t, "expected-identity")
	writeCompleteEntry(t, root, looseSpec, "content")
	wrongHash := artifact.HashFileContent([]byte("other"))
	strictSpec, err := NewEntrySpec(looseSpec.key, "content.txt", wrongHash, artifact.ArtifactKindFile)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := VerifyDirectory(context.Background(), root, strictSpec)
	if valid || !errors.Is(err, ErrInvalidEntry) {
		t.Fatalf("VerifyDirectory = %v, %v, want expected-identity mismatch", valid, err)
	}
}

func TestReadVerifiedFileBindsSnapshotToCompletionRecord(t *testing.T) {
	root := filepath.Join(t.TempDir(), "entry")
	spec := testEntrySpec(t, "verified-file")
	writeCompleteEntry(t, root, spec, "trusted")

	file, found, err := ReadVerifiedFile(context.Background(), root, spec, 64)
	if err != nil || !found {
		t.Fatalf("ReadVerifiedFile = found %t, error %v", found, err)
	}
	if got := string(file.Content()); got != "trusted" {
		t.Fatalf("Content = %q, want trusted", got)
	}
	if file.Mode().Perm() != 0o600 {
		t.Fatalf("Mode = %04o, want 0600", file.Mode().Perm())
	}

	if err := os.WriteFile(filepath.Join(root, "content.txt"), []byte("substituted"), 0o600); err != nil {
		t.Fatalf("replace cache file: %v", err)
	}
	if _, found, err := ReadVerifiedFile(context.Background(), root, spec, 64); found || !errors.Is(err, ErrInvalidEntry) {
		t.Fatalf("ReadVerifiedFile after substitution = found %t, error %v, want invalid entry", found, err)
	}
}

func TestReadVerifiedFileRejectsOversizedPayload(t *testing.T) {
	root := filepath.Join(t.TempDir(), "entry")
	spec := testEntrySpec(t, "verified-file-limit")
	writeCompleteEntry(t, root, spec, "too large")

	if _, found, err := ReadVerifiedFile(context.Background(), root, spec, 3); found || !errors.Is(err, ErrInvalidEntry) {
		t.Fatalf("ReadVerifiedFile = found %t, error %v, want bounded invalid entry", found, err)
	}
}

func TestRetireDirectoryRemovesOnlyExactCacheEntry(t *testing.T) {
	parent := t.TempDir()
	retiredRoot := filepath.Join(parent, "retired")
	retiredSpec := testEntrySpec(t, "retired")
	writeCompleteEntry(t, retiredRoot, retiredSpec, "retire")
	keptRoot := filepath.Join(parent, "kept")
	keptSpec := testEntrySpec(t, "kept")
	writeCompleteEntry(t, keptRoot, keptSpec, "keep")

	locker := NewLocker(filepath.Join(parent, "locks"))
	lock, err := locker.Acquire(context.Background(), retiredSpec.key)
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	defer lock.Release()
	if err := RetireDirectory(context.Background(), retiredRoot); err != nil {
		t.Fatalf("RetireDirectory returned error: %v", err)
	}
	if err := RetireDirectory(context.Background(), retiredRoot); err != nil {
		t.Fatalf("missing RetireDirectory returned error: %v", err)
	}
	if _, err := os.Lstat(retiredRoot); !os.IsNotExist(err) {
		t.Fatalf("retired root exists or stat failed: %v", err)
	}
	if valid, err := VerifyDirectory(context.Background(), keptRoot, keptSpec); err != nil || !valid {
		t.Fatalf("unrelated entry validity = %t, error %v", valid, err)
	}
}

func TestPublishDirectoryOnceRejectsDeclaredIdentityMismatch(t *testing.T) {
	finalRoot := filepath.Join(t.TempDir(), "entry")
	spec := testEntrySpec(t, "declared-mismatch")
	published, err := PublishDirectoryOnce(context.Background(), finalRoot, spec, func(tempRoot string) (artifact.ContentHash, artifact.ArtifactKind, error) {
		_, _, err := writeTestContent(tempRoot, "actual")
		return artifact.HashFileContent([]byte("different")), artifact.ArtifactKindFile, err
	})
	if published || err == nil {
		t.Fatalf("PublishDirectoryOnce = %v, %v, want mismatch failure", published, err)
	}
	if _, err := os.Lstat(finalRoot); !os.IsNotExist(err) {
		t.Fatalf("mismatched prepared tree was published: %v", err)
	}
}

func TestPublishDirectoryOnceRejectsPrecreatedCompletionRecordSymlink(t *testing.T) {
	parent := t.TempDir()
	finalRoot := filepath.Join(parent, "entry")
	spec := testEntrySpec(t, "record-symlink")
	target := filepath.Join(parent, "record-target")
	if err := os.WriteFile(target, []byte("sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	published, err := PublishDirectoryOnce(context.Background(), finalRoot, spec, func(tempRoot string) (artifact.ContentHash, artifact.ArtifactKind, error) {
		hash, kind, err := writeTestContent(tempRoot, "content")
		if err != nil {
			return "", "", err
		}
		if err := os.Symlink(target, filepath.Join(tempRoot, completionRecordName)); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		return hash, kind, nil
	})
	if published || err == nil {
		t.Fatalf("PublishDirectoryOnce = %v, %v, want reserved-record failure", published, err)
	}
	content, readErr := os.ReadFile(target)
	if readErr != nil || string(content) != "sentinel" {
		t.Fatalf("completion record symlink target = %q, %v, want unchanged", content, readErr)
	}
}

func TestPublishPreparedDirectoryRetiresIncompleteFinalEntry(t *testing.T) {
	root := t.TempDir()
	tempRoot := filepath.Join(root, "temp")
	finalRoot := filepath.Join(root, "final")
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		t.Fatalf("MkdirAll temp returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempRoot, "content"), []byte("ready\n"), 0o600); err != nil {
		t.Fatalf("WriteFile temp returned error: %v", err)
	}
	if err := os.MkdirAll(finalRoot, 0o700); err != nil {
		t.Fatalf("MkdirAll final returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(finalRoot, "partial"), []byte("partial\n"), 0o600); err != nil {
		t.Fatalf("WriteFile final returned error: %v", err)
	}

	hash, kind, err := access.HashPath(context.Background(), filepath.Join(tempRoot, "content"))
	if err != nil {
		t.Fatalf("HashPath returned error: %v", err)
	}
	key, err := NewKey("s3-artifact", "prepared-test")
	if err != nil {
		t.Fatalf("NewKey returned error: %v", err)
	}
	spec, err := NewEntrySpec(key, "content", hash, kind)
	if err != nil {
		t.Fatalf("NewEntrySpec returned error: %v", err)
	}
	published, err := PublishPreparedDirectory(context.Background(), tempRoot, finalRoot, spec, hash, kind)
	if err != nil || !published {
		t.Fatalf("PublishPreparedDirectory = %v, %v, want rebuilt publication", published, err)
	}
	if _, err := os.Lstat(filepath.Join(finalRoot, "partial")); !os.IsNotExist(err) {
		t.Fatalf("incomplete final entry remains: %v", err)
	}
	if content, err := os.ReadFile(filepath.Join(finalRoot, "content")); err != nil || string(content) != "ready\n" {
		t.Fatalf("published content = %q, %v", content, err)
	}
}

func TestPublishPreparedDirectoryRejectsSymlinkRootBeforeWritingRecord(t *testing.T) {
	parent := t.TempDir()
	targetRoot := filepath.Join(parent, "target")
	if err := os.MkdirAll(targetRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	hash, kind, err := writeTestContent(targetRoot, "content")
	if err != nil {
		t.Fatal(err)
	}
	tempRoot := filepath.Join(parent, "prepared-link")
	if err := os.Symlink(targetRoot, tempRoot); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	spec := testEntrySpec(t, "prepared-link")
	published, err := PublishPreparedDirectory(
		context.Background(),
		tempRoot,
		filepath.Join(parent, "final"),
		spec,
		hash,
		kind,
	)
	if published || err == nil {
		t.Fatalf("PublishPreparedDirectory = %v, %v, want symlink-root failure", published, err)
	}
	if _, err := os.Lstat(filepath.Join(targetRoot, completionRecordName)); !os.IsNotExist(err) {
		t.Fatalf("completion record was written through prepared symlink: %v", err)
	}
}

func TestPublishPreparedDirectoryRejectsSymlinkContentAncestor(t *testing.T) {
	parent := t.TempDir()
	targetRoot := filepath.Join(parent, "target")
	if err := os.MkdirAll(targetRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	targetPath := filepath.Join(targetRoot, "content.txt")
	if err := os.WriteFile(targetPath, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	tempRoot := filepath.Join(parent, "prepared")
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(targetRoot, filepath.Join(tempRoot, "nested")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	hash, kind, err := access.HashPath(context.Background(), targetPath)
	if err != nil {
		t.Fatal(err)
	}
	key := mustKey(t, "cache-entry", "prepared-symlink-ancestor")
	spec, err := NewEntrySpec(key, "nested/content.txt", hash, kind)
	if err != nil {
		t.Fatal(err)
	}

	published, err := PublishPreparedDirectory(
		context.Background(),
		tempRoot,
		filepath.Join(parent, "final"),
		spec,
		hash,
		kind,
	)
	if published || err == nil {
		t.Fatalf("PublishPreparedDirectory = %v, %v, want symlink-ancestor failure", published, err)
	}
	if _, err := os.Lstat(filepath.Join(tempRoot, completionRecordName)); !os.IsNotExist(err) {
		t.Fatalf("completion record was written through a symlinked content ancestor: %v", err)
	}
}
