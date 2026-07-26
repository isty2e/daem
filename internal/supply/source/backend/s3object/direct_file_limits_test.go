package s3object

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"

	sourcepkg "github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/directfile"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
)

func TestResolveRejectsKnownOversizedS3DirectFileBeforeBodyRead(t *testing.T) {
	bodyReader := &readCountingReader{reader: bytes.NewReader([]byte("unused"))}
	contentLength := int64(128<<20 + 1)
	client := &fakeS3Client{
		body:          []byte("unused"),
		contentLength: &contentLength,
		readerFactory: func([]byte) io.Reader { return bodyReader },
	}
	resolver, err := newResolverWithClient(t.TempDir(), client)
	if err != nil {
		t.Fatal(err)
	}
	sourceSpec := sourcetest.S3(t, "s3://daem/direct-file", "v1", "", sourcepkg.S3ObjectFormatFile)

	_, err = resolver.Resolve(context.Background(), sourceSpec)
	requireDirectFileLimit(t, err)
	if bodyReader.reads != 0 {
		t.Fatalf("body reads = %d, want zero after known-size rejection", bodyReader.reads)
	}
	if closes := client.closeCount(); closes != 1 {
		t.Fatalf("Body closes = %d, want 1", closes)
	}
	assertNoS3TempEntries(t, resolver.artifactParent(mustS3SourceID(t, sourceSpec)))
	assertNoS3CompletionRecords(t, resolver.state.cacheRoot)
}

func TestResolveRejectsUnderreportedOversizedS3DirectFileWithoutPublication(t *testing.T) {
	underreported := int64(1)
	client := &fakeS3Client{
		contentLength: &underreported,
		readerFactory: func([]byte) io.Reader {
			return io.LimitReader(zeroReader{}, 128<<20+1)
		},
	}
	resolver, err := newResolverWithClient(t.TempDir(), client)
	if err != nil {
		t.Fatal(err)
	}
	sourceSpec := sourcetest.S3(t, "s3://daem/direct-file", "v1", "", sourcepkg.S3ObjectFormatFile)

	_, err = resolver.Resolve(context.Background(), sourceSpec)
	requireDirectFileLimit(t, err)
	if closes := client.closeCount(); closes != 1 {
		t.Fatalf("Body closes = %d, want 1", closes)
	}
	assertNoS3TempEntries(t, resolver.artifactParent(mustS3SourceID(t, sourceSpec)))
	assertNoS3CompletionRecords(t, resolver.state.cacheRoot)
}

func TestResolveRejectsOversizedImmutableCacheWithoutRemoteRetry(t *testing.T) {
	client := &fakeS3Client{body: []byte("valid")}
	resolver, err := newResolverWithClient(t.TempDir(), client)
	if err != nil {
		t.Fatal(err)
	}
	sourceSpec := sourcetest.S3(t, "s3://daem/direct-file", "v1", "", sourcepkg.S3ObjectFormatFile)
	resolved, err := resolver.Resolve(context.Background(), sourceSpec)
	if err != nil {
		t.Fatalf("initial Resolve: %v", err)
	}
	contentPath := s3ResolutionContentPath(resolver, resolved)
	if err := os.Truncate(contentPath, 128<<20+1); err != nil {
		t.Fatal(err)
	}

	_, err = resolver.Resolve(context.Background(), sourceSpec)
	requireDirectFileLimit(t, err)
	if calls := client.callCount(); calls != 1 {
		t.Fatalf("GetObject calls = %d, want no retry after oversized cache", calls)
	}
	if _, statErr := os.Stat(contentPath); statErr != nil {
		t.Fatalf("oversized cache was removed: %v", statErr)
	}
}

func requireDirectFileLimit(t *testing.T, err error) {
	t.Helper()
	var limitErr *directfile.LimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("error = %v, want directfile.LimitError", err)
	}
	if limitErr.Limit() != 128<<20 || limitErr.Observed() != 128<<20+1 {
		t.Fatalf(
			"limit error = limit %d observed %d",
			limitErr.Limit(),
			limitErr.Observed(),
		)
	}
}

type zeroReader struct{}

func (zeroReader) Read(payload []byte) (int, error) {
	clear(payload)
	return len(payload), nil
}
