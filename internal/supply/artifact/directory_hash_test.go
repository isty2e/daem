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
