package directfile

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

func TestPolicyAcceptsExactBoundaryAndRejectsOneByteOver(t *testing.T) {
	value := policy{maxBytes: 4}

	if err := value.checkKnownSize(4); err != nil {
		t.Fatalf("check exact known size: %v", err)
	}
	err := value.checkKnownSize(5)
	requireLimitError(t, err, 4, 5)

	var exact bytes.Buffer
	if err := value.copy(context.Background(), &exact, strings.NewReader("1234")); err != nil {
		t.Fatalf("copy exact boundary: %v", err)
	}
	if got := exact.String(); got != "1234" {
		t.Fatalf("copied content = %q, want %q", got, "1234")
	}

	var over bytes.Buffer
	err = value.copy(context.Background(), &over, strings.NewReader("12345"))
	requireLimitError(t, err, 4, 5)
	if over.Len() != 0 {
		t.Fatalf("over-limit copy published %d bytes, want none from rejected chunk", over.Len())
	}
}

func TestStandardPolicyUses128MiBBoundary(t *testing.T) {
	if err := CheckKnownSize(128 << 20); err != nil {
		t.Fatalf("check standard exact boundary: %v", err)
	}
	requireLimitError(t, CheckKnownSize(128<<20+1), 128<<20, 128<<20+1)
}

func TestPolicyRejectsInvalidKnownSizeAndUninitializedBudget(t *testing.T) {
	if err := (policy{maxBytes: 4}).checkKnownSize(-1); err == nil {
		t.Fatal("negative known size accepted")
	}
	if err := (policy{}).checkKnownSize(0); err == nil {
		t.Fatal("uninitialized budget accepted")
	}
}

func TestPolicyHashAndReadExactShareTheSameBudget(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source")
	if err := os.WriteFile(path, []byte("1234"), 0o755); err != nil {
		t.Fatal(err)
	}
	view, err := access.OpenView(path)
	if err != nil {
		t.Fatal(err)
	}
	value := policy{maxBytes: 4}
	contentHash, err := value.hash(context.Background(), view)
	if err != nil {
		t.Fatalf("hash exact boundary: %v", err)
	}
	identity, err := artifact.NewExactIdentity("test:direct-file", "", artifact.ArtifactKindFile, contentHash)
	if err != nil {
		t.Fatal(err)
	}
	content, err := value.readExact(context.Background(), view, identity)
	if err != nil {
		t.Fatalf("read exact boundary: %v", err)
	}
	if got := string(content.Bytes()); got != "1234" {
		t.Fatalf("read content = %q, want %q", got, "1234")
	}

	if err := os.WriteFile(path, []byte("12345"), 0o755); err != nil {
		t.Fatal(err)
	}
	view, err = access.OpenView(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = value.hash(context.Background(), view)
	requireLimitError(t, err, 4, 5)
	_, err = value.readExact(context.Background(), view, identity)
	requireLimitError(t, err, 4, 5)
}

func TestPolicyCopyObservesCancellationBeforePublishingReadBytes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	source := cancelingReader{cancel: cancel}
	var destination bytes.Buffer

	err := (policy{maxBytes: 4}).copy(ctx, &destination, source)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("copy error = %v, want context cancellation", err)
	}
	if destination.Len() != 0 {
		t.Fatalf("canceled copy published %d bytes", destination.Len())
	}
}

func TestPolicyCopyRejectsStalledAndShortWriters(t *testing.T) {
	err := (policy{maxBytes: 4}).copy(context.Background(), io.Discard, stalledReader{})
	if !errors.Is(err, io.ErrNoProgress) {
		t.Fatalf("stalled copy error = %v, want io.ErrNoProgress", err)
	}

	err = (policy{maxBytes: 4}).copy(context.Background(), shortWriter{}, strings.NewReader("1"))
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short copy error = %v, want io.ErrShortWrite", err)
	}
}

func TestPolicyRejectsDirectoryViews(t *testing.T) {
	view, err := access.OpenView(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (policy{maxBytes: 4}).hash(context.Background(), view); err == nil {
		t.Fatal("directory admitted as direct file")
	}
}

func requireLimitError(t *testing.T, err error, limit int64, observed int64) {
	t.Helper()
	var limitErr *LimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("error = %v, want LimitError", err)
	}
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("error = %v, want ErrLimitExceeded", err)
	}
	if limitErr.Limit() != limit || limitErr.Observed() != observed {
		t.Fatalf(
			"limit error = limit %d observed %d, want limit %d observed %d",
			limitErr.Limit(),
			limitErr.Observed(),
			limit,
			observed,
		)
	}
}

type cancelingReader struct {
	cancel context.CancelFunc
}

func (reader cancelingReader) Read(payload []byte) (int, error) {
	reader.cancel()
	payload[0] = 'x'
	return 1, nil
}

type stalledReader struct{}

func (stalledReader) Read([]byte) (int, error) {
	return 0, nil
}

type shortWriter struct{}

func (shortWriter) Write([]byte) (int, error) {
	return 0, nil
}
