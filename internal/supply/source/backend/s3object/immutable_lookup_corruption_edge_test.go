package s3object

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
	sourcepkg "github.com/isty2e/daem/internal/supply/source"
	sourcecache "github.com/isty2e/daem/internal/supply/source/cache"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
)

func TestResolveRejectsCompleteButInvalidImmutableLookupRows(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(immutableLookupRecord, []byte) []byte
	}{
		{name: "malformed", mutate: func(immutableLookupRecord, []byte) []byte { return []byte("not json\n") }},
		{name: "unknown field", mutate: func(_ immutableLookupRecord, valid []byte) []byte {
			withoutNewline := bytes.TrimSuffix(valid, []byte("\n"))
			return append(append(bytes.Clone(withoutNewline[:len(withoutNewline)-1]), []byte(",\"extra\":true}")...), '\n')
		}},
		{name: "trailing value", mutate: func(_ immutableLookupRecord, valid []byte) []byte {
			return append(bytes.Clone(valid), []byte("{}\n")...)
		}},
		{name: "noncanonical", mutate: func(record immutableLookupRecord, _ []byte) []byte {
			encoded, _ := json.MarshalIndent(record, "", "  ")
			return append(encoded, '\n')
		}},
		{name: "oversized", mutate: func(immutableLookupRecord, []byte) []byte {
			return bytes.Repeat([]byte("x"), maximumLookupRecordBytes+1)
		}},
		{name: "unsupported schema", mutate: func(record immutableLookupRecord, _ []byte) []byte {
			record.Version++
			return mustEncodeImmutableLookupRecord(t, record)
		}},
		{name: "wrong source", mutate: func(record immutableLookupRecord, _ []byte) []byte {
			record.SourceID = "s3:s3://other/object?version_id=v1"
			return mustEncodeImmutableLookupRecord(t, record)
		}},
		{name: "wrong requested version", mutate: func(record immutableLookupRecord, _ []byte) []byte {
			record.RequestedVersionID = "v2"
			return mustEncodeImmutableLookupRecord(t, record)
		}},
		{name: "empty resolved ref", mutate: func(record immutableLookupRecord, _ []byte) []byte {
			record.ResolvedRef = ""
			return mustEncodeImmutableLookupRecord(t, record)
		}},
		{name: "empty hash", mutate: func(record immutableLookupRecord, _ []byte) []byte {
			record.ContentHash = ""
			return mustEncodeImmutableLookupRecord(t, record)
		}},
		{name: "unknown kind", mutate: func(record immutableLookupRecord, _ []byte) []byte {
			record.Kind = "archive"
			return mustEncodeImmutableLookupRecord(t, record)
		}},
		{name: "stale artifact identity", mutate: func(record immutableLookupRecord, _ []byte) []byte {
			record.ResolvedRef = "stale-ref"
			record.ContentHash = artifact.HashFileContent([]byte("stale bytes\n"))
			return mustEncodeImmutableLookupRecord(t, record)
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newImmutableCorruptionFixture(t, t.TempDir(), "s3://daem/object", "v1", []byte("trusted\n"))
			valid := mustEncodeImmutableLookupRecord(t, fixture.record)
			replaceImmutableLookupRow(t, fixture.resolver.state.immutableIndex, fixture.identity, test.mutate(fixture.record, valid), 0o600)

			assertImmutableFallbackRepairs(t, fixture)
		})
	}
}

func TestResolveRepairsIncompleteImmutableLookupRows(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, immutableCorruptionFixture)
	}{
		{name: "missing record", mutate: func(t *testing.T, fixture immutableCorruptionFixture) {
			if err := os.Remove(filepath.Join(fixture.rowRoot, immutableLookupRecordName)); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "missing completion", mutate: func(t *testing.T, fixture immutableCorruptionFixture) {
			if err := os.Remove(filepath.Join(fixture.rowRoot, immutableTestCompletionRecordName)); err != nil {
				t.Fatal(err)
			}
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newImmutableCorruptionFixture(t, t.TempDir(), "s3://daem/object", "v1", []byte("trusted\n"))
			test.mutate(t, fixture)
			assertImmutableFallbackRepairs(t, fixture)
		})
	}
}

func TestResolveRepairsImmutableLookupSymlinksWithoutFollowingTargets(t *testing.T) {
	t.Run("record symlink", func(t *testing.T) {
		fixture := newImmutableCorruptionFixture(t, t.TempDir(), "s3://daem/object", "v1", []byte("trusted\n"))
		recordPath := filepath.Join(fixture.rowRoot, immutableLookupRecordName)
		targetPath := filepath.Join(filepath.Dir(fixture.rowRoot), "outside-record")
		if err := os.Rename(recordPath, targetPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(targetPath, recordPath); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}

		assertImmutableFallbackRepairs(t, fixture)
		assertFileContent(t, targetPath, mustEncodeImmutableLookupRecord(t, fixture.record))
	})

	t.Run("row root symlink", func(t *testing.T) {
		fixture := newImmutableCorruptionFixture(t, t.TempDir(), "s3://daem/object", "v1", []byte("trusted\n"))
		targetRoot := fixture.rowRoot + ".outside"
		if err := os.Rename(fixture.rowRoot, targetRoot); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(targetRoot, fixture.rowRoot); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}

		assertImmutableFallbackRepairs(t, fixture)
		if _, err := os.Stat(filepath.Join(targetRoot, immutableLookupRecordName)); err != nil {
			t.Fatalf("row symlink target was changed: %v", err)
		}
	})
}

func TestResolveRepairsSwappedLookupRowsWithoutTouchingUnrelatedRow(t *testing.T) {
	cacheRoot := t.TempDir()
	client := &fakeS3Client{
		bodies:     [][]byte{[]byte("left\n"), []byte("right\n"), []byte("left\n"), []byte("right\n")},
		versionIDs: []string{"v1", "v1", "v1", "v1"},
	}
	resolver, err := newResolverWithClient(cacheRoot, client)
	if err != nil {
		t.Fatal(err)
	}
	leftSource := sourcetest.S3(t, "s3://daem/left", "v1", "", sourcepkg.S3ObjectFormatFile)
	rightSource := sourcetest.S3(t, "s3://daem/right", "v1", "", sourcepkg.S3ObjectFormatFile)
	left := mustResolveS3(t, resolver, leftSource)
	right := mustResolveS3(t, resolver, rightSource)
	leftIdentity := mustImmutableLookupIdentity(t, left.Identity().SourceID(), "v1")
	rightIdentity := mustImmutableLookupIdentity(t, right.Identity().SourceID(), "v1")
	leftRoot := mustImmutableLookupRoot(t, resolver.state.immutableIndex, leftIdentity)
	rightRoot := mustImmutableLookupRoot(t, resolver.state.immutableIndex, rightIdentity)
	temporaryRoot := leftRoot + ".swap"
	if err := os.Rename(leftRoot, temporaryRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(rightRoot, leftRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temporaryRoot, rightRoot); err != nil {
		t.Fatal(err)
	}
	rightBefore := mustReadFile(t, filepath.Join(rightRoot, immutableLookupRecordName))

	if resolved := mustResolveS3(t, resolver, leftSource); resolved != left {
		t.Fatalf("repaired left artifact = %#v, want %#v", resolved, left)
	}
	assertFileContent(t, filepath.Join(rightRoot, immutableLookupRecordName), rightBefore)
	if resolved := mustResolveS3(t, resolver, rightSource); resolved != right {
		t.Fatalf("repaired right artifact = %#v, want %#v", resolved, right)
	}
	if mustResolveS3(t, resolver, leftSource) != left || mustResolveS3(t, resolver, rightSource) != right {
		t.Fatal("post-repair lookup did not stabilize")
	}
	if calls := client.callCount(); calls != 4 {
		t.Fatalf("GetObject calls = %d, want two initial fills plus two exact-row repairs", calls)
	}
}

func replaceImmutableLookupRow(
	t *testing.T,
	index immutableLookupIndex,
	identity immutableLookupIdentity,
	content []byte,
	mode os.FileMode,
) {
	t.Helper()
	lock, err := index.acquire(t.Context(), identity)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := lock.Release(); err != nil {
			t.Errorf("release immutable lookup lock: %v", err)
		}
	}()
	if err := index.retire(t.Context(), identity); err != nil {
		t.Fatal(err)
	}
	key, err := identity.key()
	if err != nil {
		t.Fatal(err)
	}
	hash := artifact.HashFileContent(content)
	spec, err := sourcecache.NewEntrySpec(key, immutableLookupRecordName, hash, artifact.ArtifactKindFile)
	if err != nil {
		t.Fatal(err)
	}
	entryRoot := mustImmutableLookupRoot(t, index, identity)
	published, err := sourcecache.PublishDirectoryOnce(t.Context(), entryRoot, spec, func(tempRoot string) (artifact.ContentHash, artifact.ArtifactKind, error) {
		recordPath := filepath.Join(tempRoot, immutableLookupRecordName)
		if err := os.WriteFile(recordPath, content, mode); err != nil {
			return "", "", err
		}
		if err := os.Chmod(recordPath, mode); err != nil {
			return "", "", err
		}
		return hash, artifact.ArtifactKindFile, nil
	})
	if err != nil || !published {
		t.Fatalf("publish forged immutable row = %t, %v", published, err)
	}
}

func mustEncodeImmutableLookupRecord(t *testing.T, record immutableLookupRecord) []byte {
	t.Helper()
	content, err := encodeImmutableLookupRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func mustImmutableLookupRoot(
	t *testing.T,
	index immutableLookupIndex,
	identity immutableLookupIdentity,
) string {
	t.Helper()
	root, err := index.entryRoot(identity)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestResolveCorruptLookupDoesNotMaskRemoteFailure(t *testing.T) {
	cacheRoot := t.TempDir()
	fixture := newImmutableCorruptionFixture(t, cacheRoot, "s3://daem/object", "v1", []byte("trusted\n"))
	replaceImmutableLookupRow(t, fixture.resolver.state.immutableIndex, fixture.identity, []byte("not json\n"), 0o600)
	remoteErr := errors.New("remote unavailable")
	failingClient := &fakeS3Client{err: remoteErr}
	failingResolver, err := newResolverWithClient(cacheRoot, failingClient)
	if err != nil {
		t.Fatal(err)
	}

	_, err = failingResolver.Resolve(t.Context(), fixture.sourceSpec)
	if !errors.Is(err, remoteErr) {
		t.Fatalf("Resolve error = %v, want remote failure", err)
	}
	if calls := failingClient.callCount(); calls != 1 {
		t.Fatalf("GetObject calls = %d, want one failed fallback", calls)
	}
}
