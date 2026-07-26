package archive

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractTarRejectsUnsafeEntries(t *testing.T) {
	tests := []struct {
		name   string
		header tar.Header
	}{
		{
			name: "traversal",
			header: tar.Header{
				Name:     "safe/../evil.txt",
				Typeflag: tar.TypeReg,
				Mode:     0o644,
				Size:     int64(len("evil\n")),
			},
		},
		{
			name: "backslash",
			header: tar.Header{
				Name:     `safe\evil.txt`,
				Typeflag: tar.TypeReg,
				Mode:     0o644,
				Size:     int64(len("evil\n")),
			},
		},
		{
			name: "symlink",
			header: tar.Header{
				Name:     "link",
				Typeflag: tar.TypeSymlink,
				Linkname: "target",
			},
		},
		{
			name: "hardlink",
			header: tar.Header{
				Name:     "hardlink",
				Typeflag: tar.TypeLink,
				Linkname: "target",
			},
		},
		{
			name: "special",
			header: tar.Header{
				Name:     "fifo",
				Typeflag: tar.TypeFifo,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tempDir := t.TempDir()
			content := ""
			if test.header.Typeflag == tar.TypeReg || test.header.Typeflag == tar.TypeRegA {
				content = "evil\n"
			}
			err := ExtractTar(context.Background(), bytes.NewReader(tarContent(t, test.header, content)), tempDir)
			if err == nil {
				t.Fatal("ExtractTar returned nil error")
			}
			if _, statErr := os.Lstat(filepath.Join(tempDir, "evil.txt")); !os.IsNotExist(statErr) {
				t.Fatalf("unsafe file exists or stat failed unexpectedly: %v", statErr)
			}
		})
	}
}

func TestExtractTarWritesOnlyRegularFilesAndDirectories(t *testing.T) {
	tempDir := t.TempDir()
	archive := tarEntries(t, []tarTestEntry{
		{header: tar.Header{Name: "skill", Typeflag: tar.TypeDir, Mode: 0o755}},
		{
			header:  tar.Header{Name: "skill/SKILL.md", Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len("---\nname: skill\n---\n"))},
			content: "---\nname: skill\n---\n",
		},
	})

	if err := ExtractTar(context.Background(), bytes.NewReader(archive), tempDir); err != nil {
		t.Fatalf("ExtractTar returned error: %v", err)
	}

	info, err := os.Stat(filepath.Join(tempDir, "skill/SKILL.md"))
	if err != nil {
		t.Fatalf("Stat returned error: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("mode = %s, want executable-safe 0700", info.Mode().Perm())
	}
}

func TestExtractTarReportsInvalidGzip(t *testing.T) {
	err := ExtractTarGzip(context.Background(), strings.NewReader("not gzip"), t.TempDir())
	if err == nil {
		t.Fatal("ExtractTarGzip returned nil error")
	}
	if !strings.Contains(err.Error(), "gzip") {
		t.Fatalf("error = %q, want gzip diagnostic", err)
	}
}

type tarTestEntry struct {
	header  tar.Header
	content string
}

func tarContent(t *testing.T, header tar.Header, content string) []byte {
	t.Helper()

	return tarEntries(t, []tarTestEntry{{header: header, content: content}})
}

func tarEntries(t *testing.T, entries []tarTestEntry) []byte {
	t.Helper()

	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	for _, entry := range entries {
		header := entry.header
		if header.Typeflag == tar.TypeReg || header.Typeflag == tar.TypeRegA {
			header.Size = int64(len(entry.content))
		}
		if err := writer.WriteHeader(&header); err != nil {
			t.Fatalf("WriteHeader returned error: %v", err)
		}
		if entry.content != "" {
			if _, err := writer.Write([]byte(entry.content)); err != nil {
				t.Fatalf("Write returned error: %v", err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	return buffer.Bytes()
}
