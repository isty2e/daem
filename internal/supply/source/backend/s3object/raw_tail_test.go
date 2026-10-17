package s3object

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"testing/iotest"

	"github.com/isty2e/daem/internal/supply/artifact"
	sourcepkg "github.com/isty2e/daem/internal/supply/source"
	sourcearchive "github.com/isty2e/daem/internal/supply/source/archive"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
)

func TestResolveS3RawTarConsumesTrailingInput(t *testing.T) {
	content := rawTarPayload(t)
	var contentHash artifact.ContentHash
	for _, tail := range [][]byte{nil, []byte("nonzero trailing data")} {
		body := append(bytes.Clone(content), tail...)
		input := bytes.NewReader(body)
		client := &fakeS3Client{
			body:          body,
			versionID:     "v1",
			readerFactory: func([]byte) io.Reader { return input },
		}
		resolver, err := newResolverWithClient(t.TempDir(), client)
		if err != nil {
			t.Fatal(err)
		}
		sourceSpec := sourcetest.S3(t, "s3://daem/archive.tar", "v1", "", sourcepkg.S3ObjectFormatTar)

		resolved, err := resolver.Resolve(t.Context(), sourceSpec, noOperationOptions)
		if err != nil {
			t.Fatalf("resolve with %d trailing bytes: %v", len(tail), err)
		}
		if input.Len() != 0 {
			t.Fatalf("unread S3 body = %d bytes, want EOF", input.Len())
		}
		if client.closeCount() != 1 {
			t.Fatalf("S3 body closes = %d, want 1", client.closeCount())
		}
		payload, err := resolved.View().ReadFile(t.Context(), "payload.txt", 128)
		if err != nil || string(payload.Bytes()) != "payload" {
			t.Fatalf("resolved payload = %q, error = %v", payload.Bytes(), err)
		}
		if contentHash != "" && resolved.Identity().ContentHash() != contentHash {
			t.Fatal("raw tar tail changed the extracted content identity")
		}
		contentHash = resolved.Identity().ContentHash()
		assertCompletionRecordOutsideContent(t, resolver, resolved)
	}
}

func TestResolveS3RawTarTailFailuresDoNotPublish(t *testing.T) {
	content := rawTarPayload(t)
	readErr := errors.New("tail read failed")
	for _, testCase := range []struct {
		name string
		tail io.Reader
		want error
	}{
		{name: "read_error", tail: iotest.ErrReader(readErr), want: readErr},
		{name: "canceled", tail: iotest.ErrReader(context.Canceled), want: context.Canceled},
		{name: "input_limit", tail: io.LimitReader(zeroReader{}, 256<<20), want: sourcearchive.ErrLimitExceeded},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			client := &fakeS3Client{
				body:      content,
				versionID: "v1",
				readerFactory: func([]byte) io.Reader {
					return io.MultiReader(bytes.NewReader(content), testCase.tail)
				},
			}
			cacheRoot := t.TempDir()
			resolver, err := newResolverWithClient(cacheRoot, client)
			if err != nil {
				t.Fatal(err)
			}
			sourceSpec := sourcetest.S3(t, "s3://daem/archive.tar", "v1", "", sourcepkg.S3ObjectFormatTar)

			_, err = resolver.Resolve(t.Context(), sourceSpec, noOperationOptions)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("S3 tail error = %v, want identity %v", err, testCase.want)
			}
			if errors.Is(testCase.want, sourcearchive.ErrLimitExceeded) {
				var limitErr *sourcearchive.LimitError
				if !errors.As(err, &limitErr) || limitErr.Kind() != sourcearchive.LimitInputBytes {
					t.Fatalf("S3 tail error = %v, want input-byte limit", err)
				}
			}
			if client.closeCount() != 1 {
				t.Fatalf("S3 body closes = %d, want 1", client.closeCount())
			}
			assertNoS3TempEntries(t, resolver.artifactParent(mustS3SourceID(t, sourceSpec)))
			assertNoS3CompletionRecords(t, cacheRoot)
		})
	}
}

func rawTarPayload(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	if err := writer.WriteHeader(&tar.Header{Name: "payload.txt", Mode: 0o600, Size: 7}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
