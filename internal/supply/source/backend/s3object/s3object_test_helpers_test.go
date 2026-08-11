package s3object

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/supply/artifact"
	sourcepkg "github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
)

func newResolverWithClient(cacheRoot string, fixedClient client) (Resolver, error) {
	return newResolverWithClientFactory(cacheRoot, func(context.Context, clientConfiguration) (client, error) {
		return fixedClient, nil
	})
}

type fakeS3Client struct {
	mu            sync.Mutex
	input         *awss3.GetObjectInput
	body          []byte
	bodies        [][]byte
	versionID     string
	versionIDs    []string
	nilBody       bool
	err           error
	readerFactory func([]byte) io.Reader
	contentLength *int64
	started       chan<- struct{}
	release       <-chan struct{}
	calls         int
	closes        int
}

func (client *fakeS3Client) GetObject(
	ctx context.Context,
	input *awss3.GetObjectInput,
	_ ...func(*awss3.Options),
) (*awss3.GetObjectOutput, error) {
	client.mu.Lock()
	client.input = input
	callIndex := client.calls
	client.calls++
	body := client.bodyForCall(callIndex)
	versionID := client.versionIDForCall(callIndex)
	nilBody := client.nilBody
	err := client.err
	readerFactory := client.readerFactory
	contentLength := client.contentLength
	started := client.started
	release := client.release
	client.mu.Unlock()

	if started != nil {
		started <- struct{}{}
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if err != nil {
		return nil, err
	}
	if nilBody {
		return &awss3.GetObjectOutput{VersionId: aws.String(versionID)}, nil
	}

	var reader io.Reader = bytes.NewReader(body)
	if readerFactory != nil {
		reader = readerFactory(body)
	}

	return &awss3.GetObjectOutput{
		Body: trackingReadCloser{
			Reader: reader,
			close: func() {
				client.mu.Lock()
				defer client.mu.Unlock()
				client.closes++
			},
		},
		VersionId:     aws.String(versionID),
		ContentLength: contentLength,
	}, nil
}

func (client *fakeS3Client) bodyForCall(callIndex int) []byte {
	if len(client.bodies) == 0 {
		return client.body
	}
	if callIndex >= len(client.bodies) {
		return client.bodies[len(client.bodies)-1]
	}

	return client.bodies[callIndex]
}

func (client *fakeS3Client) versionIDForCall(callIndex int) string {
	if len(client.versionIDs) == 0 {
		return client.versionID
	}
	if callIndex >= len(client.versionIDs) {
		return client.versionIDs[len(client.versionIDs)-1]
	}

	return client.versionIDs[callIndex]
}

func (client *fakeS3Client) callCount() int {
	client.mu.Lock()
	defer client.mu.Unlock()

	return client.calls
}

func (client *fakeS3Client) closeCount() int {
	client.mu.Lock()
	defer client.mu.Unlock()

	return client.closes
}

type trackingReadCloser struct {
	io.Reader
	close func()
}

func (reader trackingReadCloser) Close() error {
	reader.close()
	return nil
}

type s3ResolveResult struct {
	artifact acquisition.Resolution
	err      error
}

func waitForS3TestSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	select {
	case <-signal:
	case <-timer.C:
		t.Fatalf("timed out waiting for %s", name)
	}
}

func mustS3SourceID(t *testing.T, sourceSpec sourcepkg.Source) artifact.SourceID {
	t.Helper()

	sourceID, err := sourcepkg.SourceIDFor(sourceSpec)
	if err != nil {
		t.Fatalf("SourceIDFor returned error: %v", err)
	}

	return sourceID
}

func mustCaptureS3CacheRoot(t testing.TB, resolver Resolver) *rootedpath.CapturedRoot {
	t.Helper()
	root, err := resolver.captureCacheRoot(context.Background())
	if err != nil {
		t.Fatalf("captureCacheRoot returned error: %v", err)
	}
	return root
}

func holdImmutableLookupLock(
	t *testing.T,
	resolver Resolver,
	identity immutableLookupIdentity,
) func() {
	t.Helper()
	root := mustCaptureS3CacheRoot(t, resolver)
	acquired := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		lockErr := resolver.state.immutableIndex.doRooted(
			context.Background(),
			root,
			identity,
			func() error {
				close(acquired)
				<-release
				return nil
			},
		)
		done <- errors.Join(lockErr, root.Close())
	}()
	waitForS3TestSignal(t, acquired, "immutable lookup lock acquisition")
	return func() {
		close(release)
		if err := <-done; err != nil {
			t.Errorf("release immutable lookup lock: %v", err)
		}
	}
}

func assertNoS3TempEntries(t *testing.T, artifactParent string) {
	t.Helper()

	entries, err := os.ReadDir(artifactParent)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatalf("ReadDir returned error: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp") {
			t.Fatalf("temporary S3 artifact entry was left behind: %s", entry.Name())
		}
	}
}

func assertNoS3CompletionRecords(t *testing.T, root string) {
	t.Helper()

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Name() == ".daem-complete" {
			return fmt.Errorf("completion record was left behind at %s", path)
		}

		return nil
	})
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
}

func s3EntryExists(root string) bool {
	info, err := os.Lstat(root)
	return err == nil && info.IsDir()
}

func s3ResolutionContentPath(resolver Resolver, resolved acquisition.Resolution) string {
	identity := resolved.Identity()
	return filepath.Join(
		resolver.artifactEntryRoot(identity.SourceID(), identity.ResolvedRef(), identity.ContentHash()),
		"content",
	)
}

type tarTestEntry struct {
	name    string
	content string
}

func tarGzipContent(t *testing.T, entries []tarTestEntry) []byte {
	t.Helper()

	return tarGzipRaw(t, func(writer *tar.Writer) {
		for _, entry := range entries {
			header := tar.Header{
				Name:     entry.name,
				Typeflag: tar.TypeReg,
				Mode:     0o644,
				Size:     int64(len(entry.content)),
			}
			if err := writer.WriteHeader(&header); err != nil {
				t.Fatalf("WriteHeader returned error: %v", err)
			}
			if _, err := writer.Write([]byte(entry.content)); err != nil {
				t.Fatalf("Write returned error: %v", err)
			}
		}
	})
}

func tarGzipRaw(t *testing.T, write func(*tar.Writer)) []byte {
	t.Helper()

	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	write(tarWriter)
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("tar Close returned error: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("gzip Close returned error: %v", err)
	}

	return buffer.Bytes()
}
