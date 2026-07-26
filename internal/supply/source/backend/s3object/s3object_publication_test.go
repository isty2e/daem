package s3object

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
	sourcepkg "github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
)

func TestResolvePreservesCompleteFinalEntry(t *testing.T) {
	client := &fakeS3Client{body: []byte("keep\n"), versionID: "v1"}
	resolver, err := newResolverWithClient(t.TempDir(), client)
	if err != nil {
		t.Fatalf("newResolverWithClient returned error: %v", err)
	}
	sourceSpec := sourcetest.S3(t, "s3://daem/instructions/project.md", "v1", "", sourcepkg.S3ObjectFormatFile)

	resolved, err := resolver.Resolve(context.Background(), sourceSpec)
	if err != nil {
		t.Fatalf("first Resolve returned error: %v", err)
	}
	resolvedIdentity := resolved.Identity()
	entryRoot := resolver.artifactEntryRoot(
		resolvedIdentity.SourceID(),
		resolvedIdentity.ResolvedRef(),
		resolvedIdentity.ContentHash(),
	)
	sentinelPath := filepath.Join(entryRoot, "sentinel.txt")
	if err := os.WriteFile(sentinelPath, []byte("do not remove\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	secondResolved, err := resolver.Resolve(context.Background(), sourceSpec)
	if err != nil {
		t.Fatalf("second Resolve returned error: %v", err)
	}
	if got := s3ResolutionContentPath(resolver, secondResolved); got != filepath.Join(entryRoot, "content") {
		t.Fatalf("content path = %q, want final content path", got)
	}
	if _, err := os.Stat(sentinelPath); err != nil {
		t.Fatalf("complete final entry was removed or rewritten: %v", err)
	}
}

func TestResolveRebuildsPoisonedSameKeyFinalEntry(t *testing.T) {
	body := []byte("trusted\n")
	client := &fakeS3Client{body: body, versionID: "v1"}
	resolver, err := newResolverWithClient(t.TempDir(), client)
	if err != nil {
		t.Fatalf("newResolverWithClient returned error: %v", err)
	}
	sourceSpec := sourcetest.S3(t, "s3://daem/instructions/project.md", "v1", "", sourcepkg.S3ObjectFormatFile)

	first, err := resolver.Resolve(context.Background(), sourceSpec)
	if err != nil {
		t.Fatalf("first Resolve returned error: %v", err)
	}
	if err := os.WriteFile(s3ResolutionContentPath(resolver, first), []byte("poisoned\n"), 0o600); err != nil {
		t.Fatalf("poison final entry: %v", err)
	}

	second, err := resolver.Resolve(context.Background(), sourceSpec)
	if err != nil {
		t.Fatalf("second Resolve returned error: %v", err)
	}
	content, err := os.ReadFile(s3ResolutionContentPath(resolver, second))
	if err != nil {
		t.Fatalf("read rebuilt content: %v", err)
	}
	if !bytes.Equal(content, body) {
		t.Fatalf("rebuilt content = %q, want %q", content, body)
	}
	if !second.Identity().Equal(first.Identity()) {
		t.Fatalf("rebuilt identity = %#v, want %#v", second.Identity(), first.Identity())
	}
	if client.callCount() != 2 {
		t.Fatalf("GetObject calls = %d, want 2 fresh expected identities", client.callCount())
	}
}

func TestResolveRecoversStalePartialFinalEntry(t *testing.T) {
	body := []byte("recover\n")
	client := &fakeS3Client{body: body, versionID: "v1"}
	resolver, err := newResolverWithClient(t.TempDir(), client)
	if err != nil {
		t.Fatalf("newResolverWithClient returned error: %v", err)
	}
	sourceSpec := sourcetest.S3(t, "s3://daem/instructions/project.md", "v1", "", sourcepkg.S3ObjectFormatFile)
	sourceID := mustS3SourceID(t, sourceSpec)
	contentHash := artifact.HashFileContent(body)
	entryRoot := resolver.artifactEntryRoot(sourceID, "v1", contentHash)
	if err := os.MkdirAll(entryRoot, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(entryRoot, "partial.txt"), []byte("partial\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	resolved, err := resolver.Resolve(context.Background(), sourceSpec)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if !s3EntryExists(entryRoot) {
		t.Fatalf("final entry %q was not published", entryRoot)
	}
	if _, err := os.Lstat(filepath.Join(entryRoot, "partial.txt")); !os.IsNotExist(err) {
		t.Fatalf("partial file exists or stat failed unexpectedly: %v", err)
	}
	if got := s3ResolutionContentPath(resolver, resolved); got != filepath.Join(entryRoot, "content") {
		t.Fatalf("content path = %q, want rebuilt final content", got)
	}
}

func TestResolveBodyReadErrorCleansTemp(t *testing.T) {
	wantErr := errors.New("network reset")
	client := &fakeS3Client{
		body: []byte("partial\n"),
		readerFactory: func(body []byte) io.Reader {
			return &errorAfterBytesReader{body: body, err: wantErr}
		},
	}
	resolver, err := newResolverWithClient(t.TempDir(), client)
	if err != nil {
		t.Fatalf("newResolverWithClient returned error: %v", err)
	}
	sourceSpec := sourcetest.S3(t, "s3://daem/instructions/project.md", "", "", sourcepkg.S3ObjectFormatFile)
	sourceID := mustS3SourceID(t, sourceSpec)

	_, err = resolver.Resolve(context.Background(), sourceSpec)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Resolve error = %v, want body read error", err)
	}
	assertNoS3TempEntries(t, resolver.artifactParent(sourceID))
	assertNoS3CompletionRecords(t, resolver.artifactParent(sourceID))
}

func TestResolveBodyReadCancellationCleansTemp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &fakeS3Client{
		readerFactory: func([]byte) io.Reader {
			return cancelingReader{cancel: cancel}
		},
	}
	resolver, err := newResolverWithClient(t.TempDir(), client)
	if err != nil {
		t.Fatalf("newResolverWithClient returned error: %v", err)
	}
	sourceSpec := sourcetest.S3(t, "s3://daem/instructions/project.md", "", "", sourcepkg.S3ObjectFormatFile)
	sourceID := mustS3SourceID(t, sourceSpec)

	_, err = resolver.Resolve(ctx, sourceSpec)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve error = %v, want context.Canceled", err)
	}
	assertNoS3TempEntries(t, resolver.artifactParent(sourceID))
	assertNoS3CompletionRecords(t, resolver.artifactParent(sourceID))
}

func TestResolveHashCancellationCleansTemp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &fakeS3Client{body: []byte("hash me\n")}
	resolver, err := newResolverWithClient(t.TempDir(), client)
	if err != nil {
		t.Fatalf("newResolverWithClient returned error: %v", err)
	}
	resolver.state.testBeforeHash = cancel
	sourceSpec := sourcetest.S3(t, "s3://daem/instructions/project.md", "", "", sourcepkg.S3ObjectFormatFile)
	sourceID := mustS3SourceID(t, sourceSpec)

	_, err = resolver.Resolve(ctx, sourceSpec)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve error = %v, want context.Canceled", err)
	}
	assertNoS3TempEntries(t, resolver.artifactParent(sourceID))
	assertNoS3CompletionRecords(t, resolver.artifactParent(sourceID))
}

type errorAfterBytesReader struct {
	body []byte
	err  error
	done bool
}

func (reader *errorAfterBytesReader) Read(buffer []byte) (int, error) {
	if reader.done {
		return 0, reader.err
	}

	copied := copy(buffer, reader.body)
	reader.done = true
	return copied, nil
}

type cancelingReader struct {
	cancel func()
}

func (reader cancelingReader) Read([]byte) (int, error) {
	reader.cancel()
	return 0, context.Canceled
}
