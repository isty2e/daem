package artifact_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

func TestDirectoryHashBuilderMatchesHashPath(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "a"), 0o750); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", "run"), []byte("run\n"), 0o700); err != nil {
		t.Fatalf("write executable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "z"), []byte("plain\n"), 0o600); err != nil {
		t.Fatalf("write plain file: %v", err)
	}

	builder := artifact.NewDirectoryHashBuilder()
	if err := builder.AddDirectory("a"); err != nil {
		t.Fatalf("AddDirectory returned error: %v", err)
	}
	if err := builder.AddFile(context.Background(), "a/run", true, 4, bytes.NewBufferString("run\n")); err != nil {
		t.Fatalf("AddFile executable returned error: %v", err)
	}
	if err := builder.AddFile(context.Background(), "z", false, 6, bytes.NewBufferString("plain\n")); err != nil {
		t.Fatalf("AddFile plain returned error: %v", err)
	}
	streamed, err := builder.Sum()
	if err != nil {
		t.Fatalf("Sum returned error: %v", err)
	}
	fromPath, kind, err := access.HashPath(context.Background(), root)
	if err != nil {
		t.Fatalf("HashPath returned error: %v", err)
	}
	if kind != artifact.ArtifactKindDirectory {
		t.Fatalf("HashPath kind = %s, want %s", kind, artifact.ArtifactKindDirectory)
	}
	if streamed != fromPath {
		t.Fatalf("streamed hash = %s, path hash = %s", streamed, fromPath)
	}
	const wantCompatibilityHash = artifact.ContentHash("sha256:98540bf7efe9e111b6ec5f557f8c31947bc0c4a1f6af14396bb175e812e2611d")
	if streamed != wantCompatibilityHash {
		t.Fatalf("streamed hash = %s, want compatibility hash %s", streamed, wantCompatibilityHash)
	}
	second, err := builder.Sum()
	if err != nil || second != streamed {
		t.Fatalf("second Sum = %s, %v; want %s, nil", second, err, streamed)
	}
}

func TestDirectoryHashBuilderMatchesEmptyDirectory(t *testing.T) {
	builder := artifact.NewDirectoryHashBuilder()
	streamed, err := builder.Sum()
	if err != nil {
		t.Fatalf("Sum returned error: %v", err)
	}
	fromPath, _, err := access.HashPath(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("HashPath returned error: %v", err)
	}
	if streamed != fromPath {
		t.Fatalf("streamed hash = %s, path hash = %s", streamed, fromPath)
	}
}

func TestDirectoryHashBuilderAcceptsComponentOrderedPrefixSiblings(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "a"), 0o750); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", "child"), []byte("nested\n"), 0o600); err != nil {
		t.Fatalf("write nested file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a-file"), []byte("sibling\n"), 0o600); err != nil {
		t.Fatalf("write prefix sibling: %v", err)
	}

	builder := artifact.NewDirectoryHashBuilder()
	if err := builder.AddDirectory("a"); err != nil {
		t.Fatalf("AddDirectory returned error: %v", err)
	}
	if err := builder.AddFile(context.Background(), "a/child", false, 7, bytes.NewBufferString("nested\n")); err != nil {
		t.Fatalf("AddFile nested returned error: %v", err)
	}
	if err := builder.AddFile(context.Background(), "a-file", false, 8, bytes.NewBufferString("sibling\n")); err != nil {
		t.Fatalf("AddFile prefix sibling returned error: %v", err)
	}
	streamed, err := builder.Sum()
	if err != nil {
		t.Fatalf("Sum returned error: %v", err)
	}
	fromPath, _, err := access.HashPath(context.Background(), root)
	if err != nil {
		t.Fatalf("HashPath returned error: %v", err)
	}
	if streamed != fromPath {
		t.Fatalf("streamed hash = %s, path hash = %s", streamed, fromPath)
	}
	const wantHash = artifact.ContentHash("sha256:d9ad1458e5212cc4afc07473e769702a0b4748502200cddbea1bac5e05d45e80")
	if streamed != wantHash {
		t.Fatalf("streamed hash = %s, want %s", streamed, wantHash)
	}
}

func TestDirectoryHashBuilderAcceptsNestedPrefixSiblings(t *testing.T) {
	builder := artifact.NewDirectoryHashBuilder()
	if err := builder.AddDirectory("a"); err != nil {
		t.Fatalf("AddDirectory a returned error: %v", err)
	}
	if err := builder.AddDirectory("a/b"); err != nil {
		t.Fatalf("AddDirectory a/b returned error: %v", err)
	}
	if err := builder.AddFile(context.Background(), "a/b/child", false, 0, bytes.NewReader(nil)); err != nil {
		t.Fatalf("AddFile nested child returned error: %v", err)
	}
	if err := builder.AddFile(context.Background(), "a/b-file", false, 0, bytes.NewReader(nil)); err != nil {
		t.Fatalf("AddFile nested prefix sibling returned error: %v", err)
	}
	if err := builder.AddFile(context.Background(), "a-file", false, 0, bytes.NewReader(nil)); err != nil {
		t.Fatalf("AddFile root prefix sibling returned error: %v", err)
	}
	if _, err := builder.Sum(); err != nil {
		t.Fatalf("Sum returned error: %v", err)
	}
}

func TestDirectoryHashBuilderRejectsMalformedStreams(t *testing.T) {
	tests := []struct {
		name string
		run  func(*artifact.DirectoryHashBuilder) error
	}{
		{name: "missing parent", run: func(builder *artifact.DirectoryHashBuilder) error { return builder.AddDirectory("a/b") }},
		{name: "out of order", run: func(builder *artifact.DirectoryHashBuilder) error {
			if err := builder.AddFile(context.Background(), "z", false, 0, bytes.NewReader(nil)); err != nil {
				return err
			}
			return builder.AddDirectory("a")
		}},
		{name: "subtree reentry after prefix sibling", run: func(builder *artifact.DirectoryHashBuilder) error {
			if err := builder.AddDirectory("a"); err != nil {
				return err
			}
			if err := builder.AddFile(context.Background(), "a-file", false, 0, bytes.NewReader(nil)); err != nil {
				return err
			}
			return builder.AddFile(context.Background(), "a/child", false, 0, bytes.NewReader(nil))
		}},
		{name: "nested subtree reentry after prefix sibling", run: func(builder *artifact.DirectoryHashBuilder) error {
			if err := builder.AddDirectory("a"); err != nil {
				return err
			}
			if err := builder.AddDirectory("a/b"); err != nil {
				return err
			}
			if err := builder.AddFile(context.Background(), "a/b-file", false, 0, bytes.NewReader(nil)); err != nil {
				return err
			}
			return builder.AddFile(context.Background(), "a/b/child", false, 0, bytes.NewReader(nil))
		}},
		{name: "short content", run: func(builder *artifact.DirectoryHashBuilder) error {
			return builder.AddFile(context.Background(), "a", false, 2, bytes.NewBufferString("x"))
		}},
		{name: "long content", run: func(builder *artifact.DirectoryHashBuilder) error {
			return builder.AddFile(context.Background(), "a", false, 1, bytes.NewBufferString("xx"))
		}},
		{name: "nil context", run: func(builder *artifact.DirectoryHashBuilder) error {
			return builder.AddFile(nil, "a", false, 0, bytes.NewReader(nil))
		}},
		{name: "after sum", run: func(builder *artifact.DirectoryHashBuilder) error {
			if _, err := builder.Sum(); err != nil {
				return err
			}
			return builder.AddDirectory("a")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			builder := artifact.NewDirectoryHashBuilder()
			if err := test.run(builder); err == nil {
				t.Fatal("malformed stream returned nil error")
			}
			if test.name == "short content" || test.name == "long content" {
				if _, err := builder.Sum(); err == nil {
					t.Fatal("Sum after partial file hash returned nil error")
				}
			}
		})
	}
}
