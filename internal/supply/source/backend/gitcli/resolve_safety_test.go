package gitcli

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	sourcepkg "github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	sourcearchive "github.com/isty2e/daem/internal/supply/source/archive"
)

func TestResolveReportsMissingGitPath(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\n---\n")
	commitAll(t, repoPath, "initial skill")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	_, err = resolver.Resolve(context.Background(), mustGitSource(t, repoPath, "skills/missing", "main"), noOperationOptions)
	if err == nil {
		t.Fatal("Resolve returned nil error")
	}

	if !strings.Contains(err.Error(), "skills/missing") {
		t.Fatalf("error = %q, want missing path context", err)
	}
}

func TestExtractArchiveRejectsTraversalEntry(t *testing.T) {
	tempDir := t.TempDir()
	archive := gitTestArchive(t, tar.Header{
		Name:     "safe/../evil.txt",
		Typeflag: tar.TypeReg,
		Mode:     0o644,
		Size:     int64(len("evil\n")),
	}, "evil\n")

	err := sourcearchive.ExtractTar(context.Background(), bytes.NewReader(archive), tempDir)
	if err == nil {
		t.Fatal("extractArchive returned nil error")
	}

	if _, err := os.Stat(filepath.Join(tempDir, "evil.txt")); !os.IsNotExist(err) {
		t.Fatalf("escaped archive file exists or stat failed unexpectedly: %v", err)
	}
}

func TestExtractArchiveRejectsAbsoluteEntry(t *testing.T) {
	tempDir := t.TempDir()
	archive := gitTestArchive(t, tar.Header{
		Name:     "/tmp/evil.txt",
		Typeflag: tar.TypeReg,
		Mode:     0o644,
		Size:     int64(len("evil\n")),
	}, "evil\n")

	err := sourcearchive.ExtractTar(context.Background(), bytes.NewReader(archive), tempDir)
	if err == nil {
		t.Fatal("extractArchive returned nil error")
	}

	if !strings.Contains(err.Error(), "safe relative path") {
		t.Fatalf("error = %q, want archive safety diagnostic", err)
	}
}

func TestExtractArchiveIgnoresPaxMetadata(t *testing.T) {
	tempDir := t.TempDir()
	archive := gitTestArchiveWithHeaders(t, []gitTestTarEntry{
		{
			header: tar.Header{
				Typeflag:   tar.TypeXGlobalHeader,
				PAXRecords: map[string]string{"comment": "ignored"},
			},
		},
		{
			header: tar.Header{
				Name:     "skills/demo/SKILL.md",
				Typeflag: tar.TypeReg,
				Mode:     0o644,
				Size:     int64(len("---\nname: demo\n---\n")),
			},
			content: "---\nname: demo\n---\n",
		},
	})

	if err := sourcearchive.ExtractTar(context.Background(), bytes.NewReader(archive), tempDir); err != nil {
		t.Fatalf("ExtractTar returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tempDir, "skills/demo/SKILL.md")); err != nil {
		t.Fatalf("Stat returned error: %v", err)
	}
}

func TestExtractGitArchiveRejectsOversizedEntryWithoutPublication(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake git archive executable uses a POSIX shell")
	}
	requireGit(t)
	tempDir := t.TempDir()
	repositoryRoot := filepath.Join(tempDir, "origin")
	if err := os.MkdirAll(repositoryRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	origin := initGitRepository(t, repositoryRoot)
	writeGitTestFile(t, origin, "SKILL.md", "---\nname: demo\n---\n")
	commit := commitAll(t, origin, "initial skill")
	sourceSpec := mustGitSource(t, origin, ".", commit)
	sourceID, err := sourcepkg.SourceIDFor(sourceSpec)
	if err != nil {
		t.Fatal(err)
	}
	gitSource, ok := sourceSpec.Git()
	if !ok {
		t.Fatal("source fixture is not Git")
	}
	cacheRoot := filepath.Join(tempDir, "cache")
	resolver, err := NewResolver(cacheRoot)
	if err != nil {
		t.Fatal(err)
	}
	capturedCacheRoot, err := resolver.captureCacheRoot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer capturedCacheRoot.Close()
	repository, err := resolver.ensureRepository(
		context.Background(),
		capturedCacheRoot,
		gitSource,
		sourceSpec,
		sourceID,
		acquisition.OperationOptions{},
	)
	if err != nil {
		t.Fatalf("prepare repository cache: %v", err)
	}

	realGit, err := exec.LookPath(gitExecutable)
	if err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	if err := writer.WriteHeader(&tar.Header{Name: "partial", Typeflag: tar.TypeReg, Mode: 0o600, Size: 7}); err != nil {
		t.Fatalf("write partial tar header: %v", err)
	}
	if _, err := writer.Write([]byte("partial")); err != nil {
		t.Fatalf("write partial tar content: %v", err)
	}
	if err := writer.WriteHeader(&tar.Header{
		Name:     "oversized",
		Typeflag: tar.TypeReg,
		Mode:     0o600,
		Size:     128<<20 + 1,
	}); err != nil {
		t.Fatalf("write oversized tar header: %v", err)
	}
	fixturePath := filepath.Join(tempDir, "archive.tar")
	if err := os.WriteFile(fixturePath, archive.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeGitPath := filepath.Join(binDir, gitExecutable)
	fakeGit := "#!/bin/sh\n" + detectGitSubcommandShell + `
if [ "$git_subcommand" = "archive" ]; then
  /bin/cat "$DAEM_ARCHIVE_FIXTURE"
  exit 0
fi
exec ` + shellQuoteForTest(realGit) + ` "$@"
`
	if err := os.WriteFile(fakeGitPath, []byte(fakeGit), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("DAEM_ARCHIVE_FIXTURE", fixturePath)
	_, _, _, err = resolver.ensureArtifact(
		context.Background(),
		repository,
		commit,
		".",
		sourceSpec,
		sourceID,
		acquisition.OperationOptions{},
	)
	var limitErr *sourcearchive.LimitError
	if !errors.As(err, &limitErr) || limitErr.Kind() != sourcearchive.LimitEntryBytes {
		t.Fatalf("ensureArtifact error = %v, want entry-byte LimitError", err)
	}
	entryRoot := resolver.artifactEntryRoot(origin, commit, ".")
	if _, statErr := os.Lstat(entryRoot); !os.IsNotExist(statErr) {
		t.Fatalf("limit-failed Git entry was published: %v", statErr)
	}
	entries, readErr := os.ReadDir(filepath.Dir(entryRoot))
	if os.IsNotExist(readErr) {
		return
	}
	if readErr != nil {
		t.Fatalf("read Git artifact parent: %v", readErr)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp") {
			t.Fatalf("limit-failed Git archive left temporary entry %q", entry.Name())
		}
	}
}

func TestResolveBadRepositoryReportsCloneContext(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	missingRepo := filepath.Join(tempDir, "missing-repo")
	_, err = resolver.Resolve(context.Background(), mustGitSource(t, missingRepo, "skills/demo", "main"), noOperationOptions)
	if err == nil {
		t.Fatal("Resolve returned nil error")
	}

	if !strings.Contains(err.Error(), "fetch git source") {
		t.Fatalf("error = %q, want fetch context", err)
	}
}

func TestResolveRejectsGitSymlink(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "target.txt", "target\n")
	linkPath := filepath.Join(repoPath, "links", "target")
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

	if err := os.Symlink("../target.txt", linkPath); err != nil {
		t.Skipf("Symlink is unavailable: %v", err)
	}
	commitAll(t, repoPath, "add symlink")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	_, err = resolver.Resolve(context.Background(), mustGitSource(t, repoPath, "links/target", "main"), noOperationOptions)
	if err == nil {
		t.Fatal("Resolve returned nil error")
	}

	if !strings.Contains(err.Error(), "links are not supported") {
		t.Fatalf("error = %q, want link diagnostic", err)
	}
}
