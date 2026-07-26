package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBoundedReaderExactBoundary(t *testing.T) {
	for _, kind := range []LimitKind{LimitInputBytes, LimitTarStreamBytes} {
		t.Run(string(kind), func(t *testing.T) {
			content := []byte("1234")
			accepted, err := io.ReadAll(newBoundedReader(context.Background(), bytes.NewReader(content), kind, 4))
			if err != nil || !bytes.Equal(accepted, content) {
				t.Fatalf("exact boundary = %q, %v", accepted, err)
			}

			_, err = io.ReadAll(newBoundedReader(context.Background(), bytes.NewReader(content), kind, 3))
			requireLimitKind(t, err, kind)
		})
	}
}

func TestBoundedReaderRejectsRepeatedNoProgress(t *testing.T) {
	_, err := io.ReadAll(newBoundedReader(context.Background(), zeroProgressReader{}, LimitInputBytes, 1024))
	if !errors.Is(err, io.ErrNoProgress) {
		t.Fatalf("bounded reader error = %v, want io.ErrNoProgress", err)
	}
}

func TestBudgetCheckInputSizeExactBoundary(t *testing.T) {
	budget := testBudget()
	if err := budget.checkInputSize(budget.inputBytes); err != nil {
		t.Fatalf("CheckInputSize exact boundary: %v", err)
	}
	requireLimitKind(t, budget.checkInputSize(budget.inputBytes+1), LimitInputBytes)
}

func TestDefaultBudgetContract(t *testing.T) {
	budget := defaultBudget()
	got := [...]int64{
		budget.inputBytes,
		budget.tarStreamBytes,
		budget.expandedBytes,
		budget.entryBytes,
		budget.entryCount,
		budget.pathBytes,
		budget.pathDepth,
	}
	want := [...]int64{256 << 20, 768 << 20, 512 << 20, 128 << 20, 100_000, 4_096, 64}
	if got != want {
		t.Fatalf("default budget = %v, want %v", got, want)
	}
	if err := CheckInputSize(want[0]); err != nil {
		t.Fatalf("CheckInputSize exact default: %v", err)
	}
	requireLimitKind(t, CheckInputSize(want[0]+1), LimitInputBytes)
}

func TestExtractTarEnforcesHeaderBudgetsBeforeCreatingEntry(t *testing.T) {
	for _, test := range []struct {
		name       string
		budget     budget
		entries    []tarTestEntry
		wantKind   LimitKind
		absentPath string
	}{
		{
			name:       "entry bytes",
			budget:     withBudget(testBudget(), func(budget *budget) { budget.entryBytes = 3 }),
			entries:    []tarTestEntry{{header: tar.Header{Name: "large", Typeflag: tar.TypeReg}, content: "1234"}},
			wantKind:   LimitEntryBytes,
			absentPath: "large",
		},
		{
			name: "expanded bytes",
			budget: withBudget(testBudget(), func(budget *budget) {
				budget.expandedBytes = 5
				budget.entryBytes = 5
			}),
			entries: []tarTestEntry{
				{header: tar.Header{Name: "first", Typeflag: tar.TypeReg}, content: "123"},
				{header: tar.Header{Name: "second", Typeflag: tar.TypeReg}, content: "456"},
			},
			wantKind:   LimitExpandedBytes,
			absentPath: "second",
		},
		{
			name:   "entry count",
			budget: withBudget(testBudget(), func(budget *budget) { budget.entryCount = 1 }),
			entries: []tarTestEntry{
				{header: tar.Header{Name: "first", Typeflag: tar.TypeDir}},
				{header: tar.Header{Name: "second", Typeflag: tar.TypeDir}},
			},
			wantKind:   LimitEntryCount,
			absentPath: "second",
		},
		{
			name:       "path bytes",
			budget:     withBudget(testBudget(), func(budget *budget) { budget.pathBytes = 4 }),
			entries:    []tarTestEntry{{header: tar.Header{Name: "12345", Typeflag: tar.TypeDir}}},
			wantKind:   LimitPathBytes,
			absentPath: "12345",
		},
		{
			name:       "path depth",
			budget:     withBudget(testBudget(), func(budget *budget) { budget.pathDepth = 2 }),
			entries:    []tarTestEntry{{header: tar.Header{Name: "a/b/c", Typeflag: tar.TypeDir}}},
			wantKind:   LimitPathDepth,
			absentPath: "a/b/c",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			err := extractTarWithBudget(
				context.Background(),
				bytes.NewReader(tarEntries(t, test.entries)),
				root,
				test.budget,
			)
			requireLimitKind(t, err, test.wantKind)
			if _, statErr := os.Lstat(filepath.Join(root, filepath.FromSlash(test.absentPath))); !os.IsNotExist(statErr) {
				t.Fatalf("rejected entry exists or stat failed: %v", statErr)
			}
		})
	}
}

func TestExtractTarAcceptsExactHeaderBudgetBoundaries(t *testing.T) {
	entries := []tarTestEntry{
		{header: tar.Header{Name: "a", Typeflag: tar.TypeReg}, content: "12"},
		{header: tar.Header{Name: "b/c", Typeflag: tar.TypeReg}, content: "345"},
	}
	archive := tarEntries(t, entries)
	budget := testBudget()
	budget.inputBytes = int64(len(archive))
	budget.entryBytes = 3
	budget.expandedBytes = 5
	budget.entryCount = 2
	budget.pathBytes = 3
	budget.pathDepth = 2
	if err := extractTarWithBudget(context.Background(), bytes.NewReader(archive), t.TempDir(), budget); err != nil {
		t.Fatalf("exact header boundaries returned error: %v", err)
	}

	budget.inputBytes--
	requireLimitKind(
		t,
		extractTarWithBudget(context.Background(), bytes.NewReader(archive), t.TempDir(), budget),
		LimitInputBytes,
	)
}

func TestExtractTarGzipBoundsDecompressedMetadataStream(t *testing.T) {
	archive := tarEntries(t, []tarTestEntry{{
		header: tar.Header{
			Name:       "small",
			Typeflag:   tar.TypeReg,
			PAXRecords: map[string]string{"comment": strings.Repeat("x", 16*1024)},
		},
		content: "x",
	}})
	compressed := gzipContent(t, archive)
	budget := testBudget()
	budget.inputBytes = int64(len(compressed))
	budget.tarStreamBytes = 1024

	err := extractTarGzipWithBudget(context.Background(), bytes.NewReader(compressed), t.TempDir(), budget)
	requireLimitKind(t, err, LimitTarStreamBytes)
}

func TestExtractTarGzipEnforcesCompressedInputBudget(t *testing.T) {
	archive := tarEntries(t, []tarTestEntry{{
		header:  tar.Header{Name: "entry", Typeflag: tar.TypeReg},
		content: "content",
	}})
	compressed := gzipContent(t, archive)
	budget := testBudget()
	budget.inputBytes = int64(len(compressed) - 1)

	err := extractTarGzipWithBudget(context.Background(), bytes.NewReader(compressed), t.TempDir(), budget)
	requireLimitKind(t, err, LimitInputBytes)
}

func TestArchiveAccountingRejectsExpandedSizeOverflow(t *testing.T) {
	accounting := archiveAccounting{budget: budget{
		inputBytes:     1,
		tarStreamBytes: 1,
		expandedBytes:  math.MaxInt64,
		entryBytes:     math.MaxInt64,
		entryCount:     1,
		pathBytes:      1,
		pathDepth:      1,
	}, expandedSize: 1}
	requireLimitKind(t, accounting.admitFile(math.MaxInt64, "x"), LimitExpandedBytes)
}

func TestWriteArchiveFileObservesCancellationDuringEntry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader := &cancelAfterRead{cancel: cancel}
	err := writeArchiveFile(ctx, reader, filepath.Join(t.TempDir(), "entry"), 0o600)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("writeArchiveFile error = %v, want context cancellation", err)
	}
}

func TestExtractTarObservesCancellationWhileEntryBodyIsBlocked(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	reader := &blockingTarEntryReader{
		ctx:     ctx,
		header:  bytes.NewReader(tarHeaderOnly(t, 1<<20)),
		started: started,
	}
	root := t.TempDir()
	done := make(chan error, 1)
	go func() {
		done <- ExtractTar(ctx, reader, root)
	}()

	timer := time.NewTimer(5 * time.Second)
	select {
	case <-started:
		timer.Stop()
	case <-timer.C:
		t.Fatal("ExtractTar did not enter the blocked entry body")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("ExtractTar error = %v, want context cancellation", err)
	}
}

func TestExtractTarRejectsTruncatedHeaderWithoutLimitMisclassification(t *testing.T) {
	err := ExtractTar(context.Background(), bytes.NewReader(bytes.Repeat([]byte{'x'}, 100)), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "read archive") {
		t.Fatalf("ExtractTar error = %v, want malformed archive", err)
	}
	var limitErr *LimitError
	if errors.As(err, &limitErr) {
		t.Fatalf("malformed archive was mislabeled as %s", limitErr.Kind())
	}
}

func TestArchiveAccountingRejectsNegativeEntrySizeAsMalformed(t *testing.T) {
	err := (&archiveAccounting{budget: testBudget()}).admitFile(-1, "negative")
	if err == nil || !strings.Contains(err.Error(), "negative size") {
		t.Fatalf("admitFile error = %v, want negative-size validation", err)
	}
	var limitErr *LimitError
	if errors.As(err, &limitErr) {
		t.Fatalf("negative size was mislabeled as %s", limitErr.Kind())
	}
}

func TestLimitErrorBoundsUntrustedEntryDiagnostic(t *testing.T) {
	err := newLimitError(LimitPathBytes, 4, 1024, strings.Repeat("x", 1024))
	if len(err.Entry()) > maximumDiagnosticBytes {
		t.Fatalf("entry diagnostic length = %d", len(err.Entry()))
	}
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("LimitError does not unwrap to ErrLimitExceeded: %v", err)
	}
}

func testBudget() budget {
	return budget{
		inputBytes:     1 << 20,
		tarStreamBytes: 1 << 20,
		expandedBytes:  1 << 20,
		entryBytes:     1 << 20,
		entryCount:     100,
		pathBytes:      1024,
		pathDepth:      16,
	}
}

func withBudget(budget budget, mutate func(*budget)) budget {
	mutate(&budget)
	return budget
}

func requireLimitKind(t *testing.T, err error, want LimitKind) *LimitError {
	t.Helper()
	var limitErr *LimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("error = %v, want LimitError", err)
	}
	if limitErr.Kind() != want {
		t.Fatalf("limit kind = %q, want %q", limitErr.Kind(), want)
	}
	if limitErr.Observed() <= limitErr.Limit() && want != LimitExpandedBytes {
		t.Fatalf("observed=%d limit=%d, want exceeded boundary", limitErr.Observed(), limitErr.Limit())
	}
	return limitErr
}

func gzipContent(t *testing.T, content []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(content); err != nil {
		t.Fatalf("write gzip content: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close gzip content: %v", err)
	}
	return buffer.Bytes()
}

type cancelAfterRead struct {
	cancel func()
	done   bool
}

type zeroProgressReader struct{}

func (zeroProgressReader) Read([]byte) (int, error) {
	return 0, nil
}

type blockingTarEntryReader struct {
	ctx     context.Context
	header  *bytes.Reader
	started chan struct{}
	once    sync.Once
}

func (reader *blockingTarEntryReader) Read(buffer []byte) (int, error) {
	if reader.header.Len() != 0 {
		return reader.header.Read(buffer)
	}
	reader.once.Do(func() { close(reader.started) })
	<-reader.ctx.Done()
	return 0, reader.ctx.Err()
}

func tarHeaderOnly(t *testing.T, size int64) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	if err := writer.WriteHeader(&tar.Header{Name: "blocked", Typeflag: tar.TypeReg, Mode: 0o600, Size: size}); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	return buffer.Bytes()
}

func (reader *cancelAfterRead) Read(buffer []byte) (int, error) {
	if reader.done {
		return 0, io.EOF
	}
	reader.done = true
	buffer[0] = 'x'
	reader.cancel()
	return 1, nil
}
