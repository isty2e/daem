//go:build darwin || linux

package access

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
	"golang.org/x/sys/unix"
)

func TestCopiedViewsReverifyConcurrentOperations(t *testing.T) {
	root := t.TempDir()
	writeAccessTestFile(t, filepath.Join(root, "SKILL.md"), []byte("original\n"))
	identity := accessTestIdentity(t, root)
	view := accessTestView(t, root)
	copied := view

	var wait sync.WaitGroup
	errorsByWorker := make(chan error, 8)
	for range 8 {
		wait.Add(1)
		go func(candidate View) {
			defer wait.Done()
			for range 8 {
				if err := candidate.Verify(context.Background(), identity); err != nil {
					errorsByWorker <- err
					return
				}
			}
		}(copied)
	}
	wait.Wait()
	close(errorsByWorker)
	for err := range errorsByWorker {
		t.Fatalf("concurrent Verify returned error: %v", err)
	}

	writeAccessTestFile(t, filepath.Join(root, "SKILL.md"), []byte("mutated!\n"))
	if err := view.Verify(context.Background(), identity); err == nil {
		t.Fatal("original view accepted content changed after identity construction")
	}
	if err := copied.Verify(context.Background(), identity); err == nil {
		t.Fatal("copied view retained stale verification authority")
	}
}

func TestViewReportsRootDisappearanceAndKindReplacement(t *testing.T) {
	root := filepath.Join(t.TempDir(), "artifact")
	writeAccessTestFile(t, root, []byte("content\n"))
	view := accessTestView(t, root)

	if err := os.Remove(root); err != nil {
		t.Fatalf("remove root: %v", err)
	}
	if _, err := view.Hash(context.Background()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Hash after removal error = %v, want not-exist", err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("replace root with directory: %v", err)
	}
	if _, err := view.Hash(context.Background()); err == nil || !strings.Contains(err.Error(), "does not match expected kind") {
		t.Fatalf("Hash after kind replacement error = %v, want kind mismatch", err)
	}
}

func TestViewRejectsRootSymlinkToPreviouslyOpenedInode(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "artifact")
	saved := filepath.Join(parent, "saved")
	writeAccessTestFile(t, root, []byte("same inode\n"))
	view := accessTestView(t, root)

	if err := os.Rename(root, saved); err != nil {
		t.Fatalf("rename root: %v", err)
	}
	if err := os.Symlink(saved, root); err != nil {
		t.Fatalf("replace root with symlink: %v", err)
	}
	if _, err := view.Hash(context.Background()); err == nil || !strings.Contains(err.Error(), "symlinks are not supported") {
		t.Fatalf("Hash error = %v, want no-follow symlink rejection", err)
	}
}

func TestViewRejectsChildSymlinkToPreviouslyOpenedInode(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "artifact")
	child := filepath.Join(root, "child")
	saved := filepath.Join(parent, "saved")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create root: %v", err)
	}
	writeAccessTestFile(t, child, []byte("same inode\n"))
	view := accessTestView(t, root)

	if err := os.Rename(child, saved); err != nil {
		t.Fatalf("rename child: %v", err)
	}
	if err := os.Symlink(saved, child); err != nil {
		t.Fatalf("replace child with symlink: %v", err)
	}
	if _, err := view.Hash(context.Background()); err == nil || !strings.Contains(err.Error(), "symlinks are not supported") {
		t.Fatalf("Hash error = %v, want child no-follow rejection", err)
	}
}

func TestViewParentAliasRetargetDoesNotRedirectCapturedRoot(t *testing.T) {
	base := t.TempDir()
	firstParent := filepath.Join(base, "first")
	secondParent := filepath.Join(base, "second")
	for _, parent := range []string{firstParent, secondParent} {
		if err := os.Mkdir(parent, 0o700); err != nil {
			t.Fatalf("create parent %q: %v", parent, err)
		}
	}
	firstRoot := filepath.Join(firstParent, "artifact")
	secondRoot := filepath.Join(secondParent, "artifact")
	writeAccessTestFile(t, filepath.Join(firstRoot, "content"), []byte("first\n"))
	writeAccessTestFile(t, filepath.Join(secondRoot, "content"), []byte("second\n"))

	alias := filepath.Join(base, "alias")
	if err := os.Symlink(firstParent, alias); err != nil {
		t.Fatalf("create parent alias: %v", err)
	}
	view, err := OpenView(filepath.Join(alias, "artifact"))
	if err != nil {
		t.Fatalf("OpenView through parent alias returned error: %v", err)
	}
	identity := accessTestIdentity(t, firstRoot)

	if err := os.Remove(alias); err != nil {
		t.Fatalf("remove parent alias: %v", err)
	}
	if err := os.Symlink(secondParent, alias); err != nil {
		t.Fatalf("retarget parent alias: %v", err)
	}
	if err := view.Verify(context.Background(), identity); err != nil {
		t.Fatalf("captured View followed retargeted parent alias: %v", err)
	}
	content, err := view.ReadFile(context.Background(), "content", 64)
	if err != nil {
		t.Fatalf("ReadFile after parent alias retarget returned error: %v", err)
	}
	if got := string(content.Bytes()); got != "first\n" {
		t.Fatalf("captured View content = %q, want original root bytes", got)
	}
}

func TestOpenNoFollowViewRejectsParentSymlink(t *testing.T) {
	base := resolvedAccessTestRoot(t)
	parent := filepath.Join(base, "parent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("create parent: %v", err)
	}
	root := filepath.Join(parent, "artifact")
	writeAccessTestFile(t, root, []byte("content\n"))
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(parent, alias); err != nil {
		t.Fatalf("create parent alias: %v", err)
	}

	if _, err := OpenNoFollowView(filepath.Join(alias, "artifact")); err == nil ||
		!strings.Contains(err.Error(), "symlinks are not supported") {
		t.Fatalf("OpenNoFollowView error = %v, want parent symlink rejection", err)
	}
}

func TestHashWithLimitRejectsEntryAndByteOverflow(t *testing.T) {
	root := resolvedAccessTestRoot(t)
	writeAccessTestFile(t, filepath.Join(root, "one"), []byte("1234"))
	writeAccessTestFile(t, filepath.Join(root, "two"), []byte("56"))
	view, err := OpenNoFollowView(root)
	if err != nil {
		t.Fatalf("OpenNoFollowView returned error: %v", err)
	}

	entryLimit, err := NewTraversalLimit(2, 64)
	if err != nil {
		t.Fatalf("NewTraversalLimit returned error: %v", err)
	}
	if _, err := view.HashWithLimit(context.Background(), entryLimit); err == nil ||
		!strings.Contains(err.Error(), "entry limit 2") {
		t.Fatalf("HashWithLimit entry error = %v, want bounded rejection", err)
	}

	byteLimit, err := NewTraversalLimit(8, 5)
	if err != nil {
		t.Fatalf("NewTraversalLimit returned error: %v", err)
	}
	if _, err := view.HashWithLimit(context.Background(), byteLimit); err == nil ||
		!strings.Contains(err.Error(), "byte limit 5") {
		t.Fatalf("HashWithLimit byte error = %v, want bounded rejection", err)
	}
}

func TestHashWithLimitMatchesUnboundedHashWithinBudget(t *testing.T) {
	root := resolvedAccessTestRoot(t)
	writeAccessTestFile(t, filepath.Join(root, "nested", "one"), []byte("1234"))
	view, err := OpenNoFollowView(root)
	if err != nil {
		t.Fatalf("OpenNoFollowView returned error: %v", err)
	}
	limit, err := NewTraversalLimit(8, 64)
	if err != nil {
		t.Fatalf("NewTraversalLimit returned error: %v", err)
	}

	bounded, err := view.HashWithLimit(context.Background(), limit)
	if err != nil {
		t.Fatalf("HashWithLimit returned error: %v", err)
	}
	unbounded, err := view.Hash(context.Background())
	if err != nil {
		t.Fatalf("Hash returned error: %v", err)
	}
	if bounded != unbounded {
		t.Fatalf("bounded hash = %q, unbounded hash = %q", bounded, unbounded)
	}
}

func TestHashDirectoryRequiringRootFileBindsEligibilityToTreeIdentity(t *testing.T) {
	root := resolvedAccessTestRoot(t)
	writeAccessTestFile(t, filepath.Join(root, "SKILL.md"), []byte("---\nname: review\n---\n"))
	writeAccessTestFile(t, filepath.Join(root, "scripts", "review.sh"), []byte("#!/bin/sh\n"))
	view, err := OpenNoFollowView(root)
	if err != nil {
		t.Fatal(err)
	}

	got, err := view.HashDirectoryRequiringRootFile(
		context.Background(),
		"SKILL.md",
		accessTreeStructureLimitForTest(t, 8, 4),
	)
	if err != nil {
		t.Fatalf("HashDirectoryRequiringRootFile returned error: %v", err)
	}
	want, err := view.Hash(context.Background())
	if err != nil {
		t.Fatalf("Hash returned error: %v", err)
	}
	if got != want {
		t.Fatalf("required-file hash = %q, ordinary hash = %q", got, want)
	}
}

func TestHashDirectoryRequiringRootFileRejectsMissingOrNonregularEntry(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*testing.T, string)
	}{
		{name: "missing"},
		{
			name: "directory",
			setup: func(t *testing.T, root string) {
				t.Helper()
				if err := os.Mkdir(filepath.Join(root, "SKILL.md"), 0o700); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := resolvedAccessTestRoot(t)
			writeAccessTestFile(t, filepath.Join(root, "other"), []byte("content"))
			if test.setup != nil {
				test.setup(t, root)
			}
			view, err := OpenNoFollowView(root)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := view.HashDirectoryRequiringRootFile(
				context.Background(),
				"SKILL.md",
				accessTreeStructureLimitForTest(t, 8, 4),
			); !errors.Is(err, ErrRequiredRootRegularFile) {
				t.Fatalf("HashDirectoryRequiringRootFile error = %v, want required-file rejection", err)
			}
		})
	}
}

func TestHashDirectoryRequiringRootFileEnforcesTreeStructureLimit(t *testing.T) {
	for _, test := range []struct {
		name    string
		limit   TreeStructureLimit
		setup   func(*testing.T, string)
		wantErr string
	}{
		{
			name:  "exact boundary",
			limit: accessTreeStructureLimitForTest(t, 3, 1),
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeAccessTestFile(t, filepath.Join(root, "SKILL.md"), []byte("skill"))
				writeAccessTestFile(t, filepath.Join(root, "scripts", "run.sh"), []byte("run"))
			},
		},
		{
			name:  "entry count exceeded",
			limit: accessTreeStructureLimitForTest(t, 2, 1),
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeAccessTestFile(t, filepath.Join(root, "SKILL.md"), []byte("skill"))
				writeAccessTestFile(t, filepath.Join(root, "scripts", "run.sh"), []byte("run"))
			},
			wantErr: "artifact tree exceeds 2 entries",
		},
		{
			name:  "directory depth exceeded",
			limit: accessTreeStructureLimitForTest(t, 4, 1),
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeAccessTestFile(t, filepath.Join(root, "SKILL.md"), []byte("skill"))
				writeAccessTestFile(t, filepath.Join(root, "one", "two", "run.sh"), []byte("run"))
			},
			wantErr: "artifact tree exceeds maximum depth 1",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := resolvedAccessTestRoot(t)
			test.setup(t, root)
			view, err := OpenNoFollowView(root)
			if err != nil {
				t.Fatal(err)
			}
			_, err = view.HashDirectoryRequiringRootFile(
				context.Background(),
				"SKILL.md",
				test.limit,
			)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("HashDirectoryRequiringRootFile returned error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("HashDirectoryRequiringRootFile error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func accessTreeStructureLimitForTest(
	t *testing.T,
	maximumEntries int,
	maximumDepth int,
) TreeStructureLimit {
	t.Helper()
	limit, err := NewTreeStructureLimit(maximumEntries, maximumDepth)
	if err != nil {
		t.Fatal(err)
	}
	return limit
}

func resolvedAccessTestRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve test root: %v", err)
	}
	return root
}

func TestHashPathRejectsSpecialFileWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pipe")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("create FIFO: %v", err)
	}
	if _, _, err := HashPath(context.Background(), path); err == nil || !strings.Contains(err.Error(), "unsupported kind") {
		t.Fatalf("HashPath error = %v, want unsupported-kind rejection", err)
	}
}

func TestHashPathRejectsNilContext(t *testing.T) {
	if _, _, err := HashPath(nil, t.TempDir()); err == nil || !strings.Contains(err.Error(), "context is required") {
		t.Fatalf("HashPath error = %v, want nil-context rejection", err)
	}
}

func TestCopyVerifiedRejectsNilSinkWriter(t *testing.T) {
	root := filepath.Join(t.TempDir(), "artifact")
	writeAccessTestFile(t, root, []byte("content\n"))
	identity := accessTestIdentity(t, root)
	view := accessTestView(t, root)
	sink := accessTestSink{openFile: func(string, fs.FileMode, int64) (io.WriteCloser, error) {
		return nil, nil
	}}

	if err := view.CopyVerified(context.Background(), identity, sink); err == nil || !strings.Contains(err.Error(), "returned no writer") {
		t.Fatalf("CopyVerified error = %v, want nil-writer rejection", err)
	}
}

func TestCopyVerifiedDetectsMutationWhileCopying(t *testing.T) {
	root := filepath.Join(t.TempDir(), "artifact")
	original := bytes.Repeat([]byte("a"), 128*1024)
	mutated := bytes.Repeat([]byte("b"), len(original))
	writeAccessTestFile(t, root, original)
	identity := accessTestIdentity(t, root)
	view := accessTestView(t, root)
	writer := &accessTestWriter{}
	sink := accessTestSink{openFile: func(string, fs.FileMode, int64) (io.WriteCloser, error) {
		if err := os.WriteFile(root, mutated, 0o600); err != nil {
			return nil, fmt.Errorf("mutate source: %w", err)
		}
		return writer, nil
	}}

	if err := view.CopyVerified(context.Background(), identity, sink); err == nil {
		t.Fatal("CopyVerified accepted bytes mutated after source open")
	}
	if !writer.closed {
		t.Fatal("CopyVerified did not close sink writer after mutation failure")
	}
}

func TestCopyVerifiedCancellationClosesSinkWriter(t *testing.T) {
	root := filepath.Join(t.TempDir(), "artifact")
	writeAccessTestFile(t, root, bytes.Repeat([]byte("a"), 256*1024))
	identity := accessTestIdentity(t, root)
	view := accessTestView(t, root)
	ctx, cancel := context.WithCancel(context.Background())
	writer := &accessTestWriter{afterWrite: cancel}
	sink := accessTestSink{openFile: func(string, fs.FileMode, int64) (io.WriteCloser, error) {
		return writer, nil
	}}

	err := view.CopyVerified(ctx, identity, sink)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("CopyVerified error = %v, want context.Canceled", err)
	}
	if !writer.closed {
		t.Fatal("CopyVerified did not close sink writer after cancellation")
	}
}

func TestCopyVerifiedSurfacesSinkCloseFailure(t *testing.T) {
	root := filepath.Join(t.TempDir(), "artifact")
	writeAccessTestFile(t, root, []byte("content\n"))
	identity := accessTestIdentity(t, root)
	view := accessTestView(t, root)
	closeFailure := errors.New("close staging writer")
	writer := &accessTestWriter{closeErr: closeFailure}
	sink := accessTestSink{openFile: func(string, fs.FileMode, int64) (io.WriteCloser, error) {
		return writer, nil
	}}

	if err := view.CopyVerified(context.Background(), identity, sink); !errors.Is(err, closeFailure) {
		t.Fatalf("CopyVerified error = %v, want sink close failure", err)
	}
}

func TestCopyVerifiedRejectsShortSinkWrite(t *testing.T) {
	root := filepath.Join(t.TempDir(), "artifact")
	writeAccessTestFile(t, root, []byte("content\n"))
	identity := accessTestIdentity(t, root)
	view := accessTestView(t, root)
	writer := &shortAccessTestWriter{}
	sink := accessTestSink{openFile: func(string, fs.FileMode, int64) (io.WriteCloser, error) {
		return writer, nil
	}}

	err := view.CopyVerified(context.Background(), identity, sink)
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("CopyVerified error = %v, want io.ErrShortWrite", err)
	}
	if !writer.closed {
		t.Fatal("CopyVerified did not close short-writing sink")
	}
}

func accessTestIdentity(t *testing.T, root string) artifact.ExactIdentity {
	t.Helper()
	contentHash, kind, err := HashPath(context.Background(), root)
	if err != nil {
		t.Fatalf("HashPath returned error: %v", err)
	}
	identity, err := artifact.NewExactIdentity("test:source", "", kind, contentHash)
	if err != nil {
		t.Fatalf("NewExactIdentity returned error: %v", err)
	}
	return identity
}

func accessTestView(t *testing.T, root string) View {
	t.Helper()
	view, err := OpenView(root)
	if err != nil {
		t.Fatalf("OpenView returned error: %v", err)
	}
	return view
}

func writeAccessTestFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create file parent: %v", err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

type accessTestSink struct {
	openFile func(string, fs.FileMode, int64) (io.WriteCloser, error)
}

func (accessTestSink) BeginDirectory(string, fs.FileMode) error { return nil }

func (sink accessTestSink) OpenFile(path string, mode fs.FileMode, size int64) (io.WriteCloser, error) {
	if sink.openFile == nil {
		return nil, fmt.Errorf("unexpected OpenFile for %q", path)
	}
	return sink.openFile(path, mode, size)
}

func (accessTestSink) EndDirectory(string, fs.FileMode) error { return nil }

type accessTestWriter struct {
	bytes.Buffer
	afterWrite func()
	closeErr   error
	closed     bool
}

type shortAccessTestWriter struct {
	closed bool
}

func (writer *shortAccessTestWriter) Write(content []byte) (int, error) {
	if len(content) == 0 {
		return 0, nil
	}
	return len(content) - 1, nil
}

func (writer *shortAccessTestWriter) Close() error {
	writer.closed = true
	return nil
}

func (writer *accessTestWriter) Write(content []byte) (int, error) {
	count, err := writer.Buffer.Write(content)
	if writer.afterWrite != nil {
		afterWrite := writer.afterWrite
		writer.afterWrite = nil
		afterWrite()
	}
	return count, err
}

func (writer *accessTestWriter) Close() error {
	writer.closed = true
	return writer.closeErr
}
