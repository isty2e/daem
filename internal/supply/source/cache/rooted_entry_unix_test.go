//go:build darwin || linux

package cache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/supply/artifact"
)

func TestVerifyDirectoryRootedRejectsCompletionRecordSubstitution(t *testing.T) {
	cacheRoot := t.TempDir()
	root := mustCaptureCacheRoot(t, cacheRoot)
	defer root.Close()
	leftSpec := rootedTestEntrySpec(t, "left")
	rightSpec := rootedTestEntrySpec(t, "right")
	publishRootedTestEntry(t, root, "artifacts/left", leftSpec, "same-content")
	publishRootedTestEntry(t, root, "artifacts/right", rightSpec, "same-content")

	rightRecord, err := os.ReadFile(filepath.Join(
		cacheRoot,
		"artifacts",
		"right",
		completionRecordName,
	))
	if err != nil {
		t.Fatalf("read right completion record: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(cacheRoot, "artifacts", "left", completionRecordName),
		rightRecord,
		0o600,
	); err != nil {
		t.Fatalf("replace left completion record: %v", err)
	}

	valid, err := VerifyDirectoryRooted(t.Context(), root, "artifacts/left", leftSpec)
	if valid || !errors.Is(err, ErrInvalidEntry) {
		t.Fatalf("VerifyDirectoryRooted = %t, %v, want invalid entry", valid, err)
	}
}

func TestVerifyDirectoryRootedRejectsMalformedCompletionRecords(t *testing.T) {
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
	} {
		t.Run(test.name, func(t *testing.T) {
			cacheRoot := t.TempDir()
			root := mustCaptureCacheRoot(t, cacheRoot)
			defer root.Close()
			spec := rootedTestEntrySpec(t, test.name)
			relativeRoot := "artifacts/entry"
			publishRootedTestEntry(t, root, relativeRoot, spec, "content")
			recordPath := filepath.Join(cacheRoot, filepath.FromSlash(relativeRoot), completionRecordName)
			content, err := os.ReadFile(recordPath)
			if err != nil {
				t.Fatal(err)
			}
			record, err := decodeCompletionRecord(content)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(recordPath, test.mutate(record), 0o600); err != nil {
				t.Fatal(err)
			}

			valid, err := VerifyDirectoryRooted(t.Context(), root, relativeRoot, spec)
			if valid || !errors.Is(err, ErrInvalidEntry) {
				t.Fatalf("VerifyDirectoryRooted = %t, %v, want invalid entry", valid, err)
			}
		})
	}
}

func TestVerifyDirectoryRootedRejectsCompletionRecordBoundsAndMode(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(string) error
	}{
		{
			name: "oversized",
			mutate: func(path string) error {
				return os.WriteFile(path, make([]byte, maximumCompletionBytes+1), 0o600)
			},
		},
		{
			name: "mode",
			mutate: func(path string) error {
				return os.Chmod(path, 0o644)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			cacheRoot := t.TempDir()
			root := mustCaptureCacheRoot(t, cacheRoot)
			defer root.Close()
			spec := rootedTestEntrySpec(t, test.name)
			relativeRoot := "artifacts/entry"
			publishRootedTestEntry(t, root, relativeRoot, spec, "content")
			if err := test.mutate(filepath.Join(
				cacheRoot,
				filepath.FromSlash(relativeRoot),
				completionRecordName,
			)); err != nil {
				t.Fatal(err)
			}

			valid, err := VerifyDirectoryRooted(t.Context(), root, relativeRoot, spec)
			if valid || !errors.Is(err, ErrInvalidEntry) {
				t.Fatalf("VerifyDirectoryRooted = %t, %v, want invalid entry", valid, err)
			}
		})
	}
}

func TestReadVerifiedFileRootedBindsIdentityAndEnforcesLimit(t *testing.T) {
	cacheRoot := t.TempDir()
	root := mustCaptureCacheRoot(t, cacheRoot)
	defer root.Close()
	spec := rootedTestEntrySpec(t, "verified-file")
	relativeRoot := "artifacts/verified"
	publishRootedTestEntry(t, root, relativeRoot, spec, "trusted")

	file, found, err := ReadVerifiedFileRooted(t.Context(), root, relativeRoot, spec, 64)
	if err != nil || !found {
		t.Fatalf("ReadVerifiedFileRooted = found %t, error %v", found, err)
	}
	if got := string(file.Content()); got != "trusted" {
		t.Fatalf("Content = %q, want trusted", got)
	}
	if file.Mode().Perm() != 0o600 {
		t.Fatalf("Mode = %04o, want 0600", file.Mode().Perm())
	}

	if _, found, err := ReadVerifiedFileRooted(t.Context(), root, relativeRoot, spec, 3); found {
		t.Fatal("ReadVerifiedFileRooted found oversized payload")
	} else {
		var limitErr *VerifiedFileLimitError
		if !errors.As(err, &limitErr) || limitErr.Observed() != int64(len("trusted")) {
			t.Fatalf("ReadVerifiedFileRooted error = %v, want observed-size limit error", err)
		}
	}

	if err := os.WriteFile(
		filepath.Join(cacheRoot, filepath.FromSlash(relativeRoot), "content.txt"),
		[]byte("substituted"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if _, found, err := ReadVerifiedFileRooted(t.Context(), root, relativeRoot, spec, 64); found || !errors.Is(err, ErrInvalidEntry) {
		t.Fatalf("ReadVerifiedFileRooted after substitution = found %t, error %v", found, err)
	}
}

func TestRetireDirectoryRootedRemovesOnlySelectedEntry(t *testing.T) {
	cacheRoot := t.TempDir()
	root := mustCaptureCacheRoot(t, cacheRoot)
	defer root.Close()
	retiredSpec := rootedTestEntrySpec(t, "retired")
	keptSpec := rootedTestEntrySpec(t, "kept")
	publishRootedTestEntry(t, root, "artifacts/retired", retiredSpec, "retire")
	publishRootedTestEntry(t, root, "artifacts/kept", keptSpec, "keep")

	if err := RetireDirectoryRooted(t.Context(), root, "artifacts/retired"); err != nil {
		t.Fatalf("RetireDirectoryRooted returned error: %v", err)
	}
	if err := RetireDirectoryRooted(t.Context(), root, "artifacts/retired"); err != nil {
		t.Fatalf("missing RetireDirectoryRooted returned error: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(cacheRoot, "artifacts", "retired")); !os.IsNotExist(err) {
		t.Fatalf("retired entry exists or stat failed: %v", err)
	}
	if valid, err := VerifyDirectoryRooted(t.Context(), root, "artifacts/kept", keptSpec); err != nil || !valid {
		t.Fatalf("unrelated entry validity = %t, error %v", valid, err)
	}
}

func TestPrepareDirectoryRejectsDeclaredIdentityAndReservedRecord(t *testing.T) {
	t.Run("declared identity", func(t *testing.T) {
		prepared, err := PrepareDirectory(
			t.Context(),
			"content.txt",
			func(tempRoot string) (artifact.ContentHash, artifact.ArtifactKind, error) {
				_, _, err := writeRootedTestContent(tempRoot, "actual")
				return artifact.HashFileContent([]byte("different")), artifact.ArtifactKindFile, err
			},
		)
		if prepared != nil || err == nil {
			t.Fatalf("PrepareDirectory = %v, %v, want mismatch failure", prepared, err)
		}
	})

	t.Run("reserved completion record", func(t *testing.T) {
		cacheRoot := t.TempDir()
		root := mustCaptureCacheRoot(t, cacheRoot)
		defer root.Close()
		prepared, err := PrepareDirectory(
			t.Context(),
			"content.txt",
			func(tempRoot string) (artifact.ContentHash, artifact.ArtifactKind, error) {
				hash, kind, err := writeRootedTestContent(tempRoot, "content")
				if err != nil {
					return "", "", err
				}
				if err := os.WriteFile(
					filepath.Join(tempRoot, completionRecordName),
					[]byte("reserved"),
					0o600,
				); err != nil {
					return "", "", err
				}
				return hash, kind, nil
			},
		)
		if err != nil {
			t.Fatalf("PrepareDirectory returned error before publication: %v", err)
		}
		defer prepared.Close(t.Context())
		hash, kind, err := prepared.ContentIdentity()
		if err != nil {
			t.Fatal(err)
		}
		key := mustKey(t, "reserved-record", "entry")
		spec, err := NewEntrySpec(key, "content.txt", hash, kind)
		if err != nil {
			t.Fatal(err)
		}
		published, err := prepared.PublishRooted(t.Context(), root, "artifacts/reserved", spec)
		if published || err == nil {
			t.Fatalf("PublishRooted = %t, %v, want reserved-record failure", published, err)
		}
		if _, statErr := os.Lstat(filepath.Join(cacheRoot, "artifacts", "reserved")); !os.IsNotExist(statErr) {
			t.Fatalf("reserved-record stage was published: %v", statErr)
		}
	})
}

func publishRootedTestEntry(
	t *testing.T,
	root *rootedpath.CapturedRoot,
	relativeRoot string,
	spec EntrySpec,
	content string,
) {
	t.Helper()
	_, _, published, err := PublishDirectoryOnceRooted(
		t.Context(),
		root,
		relativeRoot,
		spec,
		func(tempRoot string) (artifact.ContentHash, artifact.ArtifactKind, error) {
			return writeRootedTestContent(tempRoot, content)
		},
	)
	if err != nil || !published {
		t.Fatalf("PublishDirectoryOnceRooted = %t, %v, want published", published, err)
	}
}
