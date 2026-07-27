package s3object

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"testing"

	sourcepkg "github.com/isty2e/daem/internal/supply/source"
	sourcearchive "github.com/isty2e/daem/internal/supply/source/archive"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
)

func TestResolveRejectsKnownOversizedS3ArchiveBeforeBodyRead(t *testing.T) {
	bodyReader := &readCountingReader{reader: bytes.NewReader([]byte("unused"))}
	contentLength := int64(math.MaxInt64)
	client := &fakeS3Client{
		body:          []byte("unused"),
		contentLength: &contentLength,
		readerFactory: func([]byte) io.Reader { return bodyReader },
	}
	resolver, err := newResolverWithClient(t.TempDir(), client)
	if err != nil {
		t.Fatalf("newResolverWithClient returned error: %v", err)
	}
	sourceSpec := sourcetest.S3(t, "s3://daem/archive.tar", "v1", "", sourcepkg.S3ObjectFormatTar)

	_, err = resolver.Resolve(context.Background(), sourceSpec, noOperationOptions)
	var limitErr *sourcearchive.LimitError
	if !errors.As(err, &limitErr) || limitErr.Kind() != sourcearchive.LimitInputBytes {
		t.Fatalf("Resolve error = %v, want input LimitError", err)
	}
	if bodyReader.reads != 0 {
		t.Fatalf("body reads = %d, want zero after known-size rejection", bodyReader.reads)
	}
	if closes := client.closeCount(); closes != 1 {
		t.Fatalf("Body closes = %d, want 1", closes)
	}
	assertNoS3TempEntries(t, resolver.artifactParent(mustS3SourceID(t, sourceSpec)))
}

func TestResolveRejectsOversizedS3ArchiveWhenLengthIsMissingOrUnderreported(t *testing.T) {
	archive := oversizedTarHeader(t, 128<<20+1)
	oneByte := int64(1)
	for _, test := range []struct {
		name          string
		contentLength *int64
	}{
		{name: "missing"},
		{name: "underreported", contentLength: &oneByte},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeS3Client{body: archive, contentLength: test.contentLength}
			resolver, err := newResolverWithClient(t.TempDir(), client)
			if err != nil {
				t.Fatalf("newResolverWithClient returned error: %v", err)
			}
			sourceSpec := sourcetest.S3(t, "s3://daem/archive.tar", "v1", "", sourcepkg.S3ObjectFormatTar)

			_, err = resolver.Resolve(context.Background(), sourceSpec, noOperationOptions)
			var limitErr *sourcearchive.LimitError
			if !errors.As(err, &limitErr) || limitErr.Kind() != sourcearchive.LimitEntryBytes {
				t.Fatalf("Resolve error = %v, want entry-byte LimitError", err)
			}
			if closes := client.closeCount(); closes != 1 {
				t.Fatalf("Body closes = %d, want 1", closes)
			}
			assertNoS3TempEntries(t, resolver.artifactParent(mustS3SourceID(t, sourceSpec)))
		})
	}
}

type readCountingReader struct {
	reader io.Reader
	reads  int
}

func (reader *readCountingReader) Read(buffer []byte) (int, error) {
	reader.reads++
	return reader.reader.Read(buffer)
}

func oversizedTarHeader(t *testing.T, size int64) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	if err := writer.WriteHeader(&tar.Header{Name: "partial", Typeflag: tar.TypeReg, Mode: 0o600, Size: 7}); err != nil {
		t.Fatalf("write partial tar header: %v", err)
	}
	if _, err := writer.Write([]byte("partial")); err != nil {
		t.Fatalf("write partial tar content: %v", err)
	}
	if err := writer.WriteHeader(&tar.Header{Name: "oversized", Typeflag: tar.TypeReg, Mode: 0o600, Size: size}); err != nil {
		t.Fatalf("write oversized tar header: %v", err)
	}
	return buffer.Bytes()
}
