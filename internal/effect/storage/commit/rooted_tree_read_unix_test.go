//go:build darwin || linux

package commit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

func TestSnapshotRootedDirectoryStreamsStableCanonicalTree(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	destination := filepath.Join(root, ".agents", "tree")
	if err := os.MkdirAll(filepath.Join(destination, "a"), 0o700); err != nil {
		t.Fatalf("create rooted tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(destination, "a", "run"), []byte("run\n"), 0o700); err != nil {
		t.Fatalf("write executable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(destination, "z"), []byte("plain\n"), 0o600); err != nil {
		t.Fatalf("write plain file: %v", err)
	}

	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, ".agents/tree")
	sink := newHashingRootedTreeSink()
	if _, err := SnapshotRootedDirectory(
		context.Background(),
		capability,
		defaultTreeTraversalLimits(),
		sink,
	); err != nil {
		t.Fatalf("SnapshotRootedDirectory returned error: %v", err)
	}
	if err := capability.Validate(); err != nil {
		t.Fatalf("SnapshotRootedDirectory consumed capability: %v", err)
	}
	if err := capability.Close(); err != nil {
		t.Fatalf("close capability: %v", err)
	}

	streamed, err := sink.builder.Sum()
	if err != nil {
		t.Fatalf("snapshot hash Sum returned error: %v", err)
	}
	fromPath, kind, err := access.HashPath(context.Background(), destination)
	if err != nil {
		t.Fatalf("HashPath returned error: %v", err)
	}
	if kind != artifact.ArtifactKindDirectory || streamed != fromPath {
		t.Fatalf("snapshot hash = %s (%s), path hash = %s (%s)", streamed, artifact.ArtifactKindDirectory, fromPath, kind)
	}
	wantEvents := []string{"root", "dir:a", "file:a/run:run\n", "file:z:plain\n"}
	if fmt.Sprint(sink.events) != fmt.Sprint(wantEvents) {
		t.Fatalf("events = %q, want %q", sink.events, wantEvents)
	}
}

func TestSnapshotRootedDirectoryRejectsFinalAndDescendantSymlinks(t *testing.T) {
	t.Run("final", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "project")
		outside := filepath.Join(t.TempDir(), "outside")
		if err := os.MkdirAll(filepath.Join(root, ".agents"), 0o700); err != nil {
			t.Fatalf("create project ancestry: %v", err)
		}
		if err := os.Mkdir(outside, 0o700); err != nil {
			t.Fatalf("create outside directory: %v", err)
		}
		if err := os.Symlink(outside, filepath.Join(root, ".agents", "tree")); err != nil {
			t.Fatalf("create final symlink: %v", err)
		}
		captured := captureRootForCommitTest(t, root)
		capability := rootedCapabilityForCommitTest(t, captured, ".agents/tree")
		defer capability.Close()
		_, err := SnapshotRootedDirectory(
			context.Background(),
			capability,
			defaultTreeTraversalLimits(),
			newHashingRootedTreeSink(),
		)
		if !hasRootedPathFailureKind(err, rootedpath.FailureFinalSymlink) {
			t.Fatalf("error = %v, want %s", err, rootedpath.FailureFinalSymlink)
		}
	})

	t.Run("descendant", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "project")
		destination := filepath.Join(root, ".agents", "tree")
		if err := os.MkdirAll(destination, 0o700); err != nil {
			t.Fatalf("create rooted tree: %v", err)
		}
		if err := os.Symlink("missing", filepath.Join(destination, "link")); err != nil {
			t.Fatalf("create descendant symlink: %v", err)
		}
		captured := captureRootForCommitTest(t, root)
		capability := rootedCapabilityForCommitTest(t, captured, ".agents/tree")
		defer capability.Close()
		_, err := SnapshotRootedDirectory(
			context.Background(),
			capability,
			defaultTreeTraversalLimits(),
			newHashingRootedTreeSink(),
		)
		if err == nil {
			t.Fatal("descendant symlink snapshot returned nil error")
		}
	})
}

func TestSnapshotRootedDirectoryRejectsPartialConsumerAndInPlaceMutation(t *testing.T) {
	t.Run("partial consumer", func(t *testing.T) {
		_, capability := rootedTreeReadFixture(t, "payload")
		defer capability.Close()
		_, err := SnapshotRootedDirectory(
			context.Background(),
			capability,
			defaultTreeTraversalLimits(),
			partialRootedTreeSink{},
		)
		if err == nil || !bytes.Contains([]byte(err.Error()), []byte("consumed 0 of 7 bytes")) {
			t.Fatalf("error = %v, want incomplete-consumption failure", err)
		}
	})

	t.Run("in-place mutation", func(t *testing.T) {
		root, capability := rootedTreeReadFixture(t, "payload")
		defer capability.Close()
		sink := &mutatingRootedTreeSink{path: filepath.Join(root, ".agents", "tree", "entry")}
		_, err := SnapshotRootedDirectory(
			context.Background(),
			capability,
			defaultTreeTraversalLimits(),
			sink,
		)
		if err == nil || !bytes.Contains([]byte(err.Error()), []byte("identity changed")) {
			t.Fatalf("error = %v, want identity-change failure", err)
		}
	})
}

func TestSnapshotRootedDirectoryDetectsAncestorReplacement(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	destination := filepath.Join(root, ".agents", "tree")
	if err := os.MkdirAll(destination, 0o700); err != nil {
		t.Fatalf("create rooted tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(destination, "entry"), []byte("payload"), 0o600); err != nil {
		t.Fatalf("write project entry: %v", err)
	}
	outside := filepath.Join(parent, "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatalf("create outside directory: %v", err)
	}

	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, ".agents/tree")
	defer capability.Close()
	sink := newHashingRootedTreeSink()
	sink.visitRoot = func() error {
		if err := os.Rename(filepath.Join(root, ".agents"), filepath.Join(outside, "moved-agents")); err != nil {
			return err
		}
		return os.Mkdir(filepath.Join(root, ".agents"), 0o700)
	}
	_, err := SnapshotRootedDirectory(
		context.Background(),
		capability,
		defaultTreeTraversalLimits(),
		sink,
	)
	if !hasRootedPathFailureKind(err, rootedpath.FailureAncestorChanged) {
		t.Fatalf("error = %v, want %s", err, rootedpath.FailureAncestorChanged)
	}
	if _, statErr := os.Lstat(filepath.Join(root, ".agents", "tree")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("replacement ancestor unexpectedly contains tree: %v", statErr)
	}
}

func TestSnapshotRootedDirectoryEnforcesTraversalLimits(t *testing.T) {
	t.Run("entries", func(t *testing.T) {
		root, capability := rootedTreeReadFixture(t, "payload")
		defer capability.Close()
		writeTestFile(
			t,
			filepath.Join(root, ".agents", "tree", "second"),
			"x",
			0o600,
		)
		limits := mustTreeTraversalLimits(t, 1, 1, 64)
		if _, err := SnapshotRootedDirectory(
			t.Context(),
			capability,
			limits,
			newHashingRootedTreeSink(),
		); err == nil {
			t.Fatal("entry limit was not enforced")
		}
	})

	t.Run("depth", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "project")
		destination := filepath.Join(root, ".agents", "tree")
		if err := os.MkdirAll(filepath.Join(destination, "one", "two"), 0o700); err != nil {
			t.Fatal(err)
		}
		captured := captureRootForCommitTest(t, root)
		capability := rootedCapabilityForCommitTest(t, captured, ".agents/tree")
		defer capability.Close()
		limits := mustTreeTraversalLimits(t, 3, 1, 64)
		if _, err := SnapshotRootedDirectory(
			t.Context(),
			capability,
			limits,
			newHashingRootedTreeSink(),
		); err == nil {
			t.Fatal("depth limit was not enforced")
		}
	})

	t.Run("bytes exact and exceeded", func(t *testing.T) {
		_, capability := rootedTreeReadFixture(t, "payload")
		defer capability.Close()
		exact := mustTreeTraversalLimits(t, 1, 0, 7)
		if _, err := SnapshotRootedDirectory(
			t.Context(),
			capability,
			exact,
			newHashingRootedTreeSink(),
		); err != nil {
			t.Fatalf("exact byte limit failed: %v", err)
		}
		exceeded := mustTreeTraversalLimits(t, 1, 0, 6)
		if _, err := SnapshotRootedDirectory(
			t.Context(),
			capability,
			exceeded,
			newHashingRootedTreeSink(),
		); err == nil {
			t.Fatal("byte limit was not enforced")
		}
	})

	t.Run("concurrent growth remains bounded", func(t *testing.T) {
		root, capability := rootedTreeReadFixture(t, "payload")
		defer capability.Close()
		sink := &growingRootedTreeSink{
			path: filepath.Join(root, ".agents", "tree", "entry"),
		}
		limits := mustTreeTraversalLimits(t, 1, 0, 7)
		if _, err := SnapshotRootedDirectory(
			t.Context(),
			capability,
			limits,
			sink,
		); err == nil {
			t.Fatal("concurrent growth was not rejected")
		}
		if sink.bytesRead != 7 {
			t.Fatalf("sink read %d bytes, want observed 7-byte bound", sink.bytesRead)
		}
	})
}

func mustTreeTraversalLimits(
	t *testing.T,
	maximumEntries int,
	maximumDepth int,
	maximumBytes int64,
) mutationfs.TreeTraversalLimits {
	t.Helper()
	limits, err := mutationfs.NewTreeTraversalLimits(
		maximumEntries,
		maximumDepth,
		maximumBytes,
	)
	if err != nil {
		t.Fatal(err)
	}
	return limits
}

type hashingRootedTreeSink struct {
	builder   *artifact.DirectoryHashBuilder
	events    []string
	visitRoot func() error
}

func newHashingRootedTreeSink() *hashingRootedTreeSink {
	return &hashingRootedTreeSink{builder: artifact.NewDirectoryHashBuilder()}
}

func (sink *hashingRootedTreeSink) VisitRoot(_ os.FileMode) error {
	sink.events = append(sink.events, "root")
	if sink.visitRoot != nil {
		return sink.visitRoot()
	}
	return nil
}

func (sink *hashingRootedTreeSink) VisitDirectory(path mutationfs.TreeRelativePath, _ os.FileMode) error {
	sink.events = append(sink.events, "dir:"+path.Path())
	return sink.builder.AddDirectory(path.Path())
}

func (sink *hashingRootedTreeSink) VisitRegularFile(
	path mutationfs.TreeRelativePath,
	mode os.FileMode,
	size int64,
	content io.Reader,
) error {
	var captured bytes.Buffer
	err := sink.builder.AddFile(
		context.Background(),
		path.Path(),
		mode.Perm()&0o111 != 0,
		size,
		io.TeeReader(content, &captured),
	)
	sink.events = append(sink.events, "file:"+path.Path()+":"+captured.String())
	return err
}

type partialRootedTreeSink struct{}

func (partialRootedTreeSink) VisitRoot(os.FileMode) error { return nil }

func (partialRootedTreeSink) VisitDirectory(mutationfs.TreeRelativePath, os.FileMode) error {
	return nil
}

func (partialRootedTreeSink) VisitRegularFile(
	mutationfs.TreeRelativePath,
	os.FileMode,
	int64,
	io.Reader,
) error {
	return nil
}

type mutatingRootedTreeSink struct {
	path string
}

func (*mutatingRootedTreeSink) VisitRoot(os.FileMode) error { return nil }

func (*mutatingRootedTreeSink) VisitDirectory(mutationfs.TreeRelativePath, os.FileMode) error {
	return nil
}

func (sink *mutatingRootedTreeSink) VisitRegularFile(
	_ mutationfs.TreeRelativePath,
	_ os.FileMode,
	_ int64,
	content io.Reader,
) error {
	if _, err := io.Copy(io.Discard, content); err != nil {
		return err
	}
	return os.WriteFile(sink.path, []byte("changed"), 0o600)
}

type growingRootedTreeSink struct {
	path      string
	bytesRead int
}

func (*growingRootedTreeSink) VisitRoot(os.FileMode) error { return nil }

func (*growingRootedTreeSink) VisitDirectory(mutationfs.TreeRelativePath, os.FileMode) error {
	return nil
}

func (sink *growingRootedTreeSink) VisitRegularFile(
	_ mutationfs.TreeRelativePath,
	_ os.FileMode,
	_ int64,
	content io.Reader,
) error {
	file, err := os.OpenFile(sink.path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(bytes.Repeat([]byte("x"), 1<<20))
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return errors.Join(writeErr, closeErr)
	}
	value, err := io.ReadAll(content)
	sink.bytesRead = len(value)
	return err
}

func rootedTreeReadFixture(t *testing.T, content string) (string, rootedpath.CommitCapability) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "project")
	destination := filepath.Join(root, ".agents", "tree")
	if err := os.MkdirAll(destination, 0o700); err != nil {
		t.Fatalf("create rooted tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(destination, "entry"), []byte(content), 0o600); err != nil {
		t.Fatalf("write project entry: %v", err)
	}
	captured := captureRootForCommitTest(t, root)
	return root, rootedCapabilityForCommitTest(t, captured, ".agents/tree")
}
