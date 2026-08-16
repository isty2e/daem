package gitcli

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
)

func TestResolveGitSHA256CommitAndBranch(t *testing.T) {
	requireSHA256Git(t)
	tempDir := t.TempDir()
	repoPath := initSHA256GitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\ndescription: demo\n---\n")
	commit := commitAll(t, repoPath, "initial skill")
	if len(commit) != 64 {
		t.Fatalf("SHA-256 commit length = %d, want 64", len(commit))
	}

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	for _, ref := range []string{commit, "main"} {
		t.Run(ref, func(t *testing.T) {
			sourceSpec := mustGitSource(t, repoPath, "skills/demo", ref)
			resolution, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions)
			if err != nil {
				t.Fatalf("Resolve(%q) returned error: %v", ref, err)
			}
			wantSourceID, err := source.SourceIDFor(sourceSpec)
			if err != nil {
				t.Fatalf("SourceIDFor returned error: %v", err)
			}
			assertGitResolutionIdentity(
				t,
				resolution,
				wantSourceID,
				artifact.ResolvedRef(commit),
				artifact.ArtifactKindDirectory,
			)
			if _, err := os.Stat(resolver.repositoryCachePath(repoPath, gitObjectFormatSHA256)); err != nil {
				t.Fatalf("SHA-256 cache path: %v", err)
			}
			if _, err := os.Stat(resolver.repositoryPath(repoPath)); !os.IsNotExist(err) {
				t.Fatalf("SHA-1 locator-only cache path exists for SHA-256 origin: %v", err)
			}
		})
	}
}

func TestResolveRejectsEmptyNetworkSymbolicWithoutCreatingCache(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	realGit, err := exec.LookPath(gitExecutable)
	if err != nil {
		t.Fatalf("LookPath returned error: %v", err)
	}
	fakeGit := "#!/bin/sh\n" + detectGitSubcommandShell + `
if [ "$git_subcommand" = "ls-remote" ]; then
  exit 0
fi
exec ` + shellQuoteForTest(realGit) + ` "$@"
`
	if err := os.WriteFile(filepath.Join(binDir, gitExecutable), []byte(fakeGit), 0o700); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	locator := "https://example.invalid/acme/skills.git"
	_, err = resolver.Resolve(context.Background(), mustGitSource(t, locator, ".", "main"), noOperationOptions)
	if err == nil || !strings.Contains(err.Error(), "unobservable") {
		t.Fatalf("Resolve error = %v, want unobservable object format", err)
	}
	assertNoRepositoryCaches(t, filepath.Join(tempDir, "cache", "repos"))
}

func TestResolveDoesNotGuessSHA1WhenGITDefaultHashIsSHA256(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\ndescription: demo\n---\n")
	commitAll(t, repoPath, "initial skill")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	t.Setenv("GIT_DEFAULT_HASH", "sha256")
	if _, err := resolver.Resolve(
		context.Background(),
		mustGitSource(t, repoPath, "skills/demo", "main"),
		noOperationOptions,
	); err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	cachePath := resolver.repositoryPath(repoPath)
	format := strings.TrimSpace(runGitTestCommand(t, cachePath, "rev-parse", "--show-object-format"))
	if format != string(gitObjectFormatSHA1) {
		t.Fatalf("cache object format = %q, want sha1 despite GIT_DEFAULT_HASH", format)
	}
}

func TestResolveRejectsCommitWidthDisagreeingWithLocalFormat(t *testing.T) {
	requireSHA256Git(t)
	tempDir := t.TempDir()
	repoPath := initSHA256GitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\ndescription: demo\n---\n")
	commitAll(t, repoPath, "initial skill")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	_, err = resolver.Resolve(
		context.Background(),
		mustGitSource(t, repoPath, "skills/demo", strings.Repeat("a", 40)),
		noOperationOptions,
	)
	if err == nil || !strings.Contains(err.Error(), "does not match the declared commit id") {
		t.Fatalf("Resolve error = %v, want commit/object-format disagreement", err)
	}
	assertNoRepositoryCaches(t, filepath.Join(tempDir, "cache", "repos"))
}

func TestResolveReusesLocatorOnlySHA1Cache(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\ndescription: demo\n---\n")
	commitAll(t, repoPath, "initial skill")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	sourceSpec := mustGitSource(t, repoPath, "skills/demo", "main")
	if _, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions); err != nil {
		t.Fatalf("first Resolve returned error: %v", err)
	}
	cachePath := resolver.repositoryPath(repoPath)
	first, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("Stat returned error: %v", err)
	}
	if _, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions); err != nil {
		t.Fatalf("second Resolve returned error: %v", err)
	}
	second, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("Stat returned error: %v", err)
	}
	if !os.SameFile(first, second) {
		t.Fatal("SHA-1 cache path was recreated instead of reused")
	}
}

func requireSHA256Git(t *testing.T) {
	t.Helper()
	requireGit(t)

	command := exec.Command(gitExecutable, "init", "--bare", "--quiet", "--object-format=sha256")
	command.Dir = t.TempDir()
	if output, err := command.CombinedOutput(); err != nil {
		t.Skipf("git SHA-256 repositories are unavailable: %v\n%s", err, output)
	}
}

func initSHA256GitRepository(t *testing.T, root string) string {
	t.Helper()

	repoPath := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoPath, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	runGitTestCommand(t, repoPath, "init", "--object-format=sha256")
	runGitTestCommand(t, repoPath, "checkout", "-b", "main")
	runGitTestCommand(t, repoPath, "config", "user.email", "daem@example.invalid")
	runGitTestCommand(t, repoPath, "config", "user.name", "Agent Env Test")
	return repoPath
}

func assertNoRepositoryCaches(t *testing.T, reposRoot string) {
	t.Helper()

	entries, err := os.ReadDir(reposRoot)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatalf("ReadDir returned error: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("repository caches = %v, want none", names)
	}
}

func TestFileURLLocalSHA256UsesLocalFormatAuthority(t *testing.T) {
	requireSHA256Git(t)
	tempDir := t.TempDir()
	repoPath := initSHA256GitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\ndescription: demo\n---\n")
	commit := commitAll(t, repoPath, "initial skill")
	fileLocator := (&url.URL{Scheme: "file", Path: filepath.ToSlash(repoPath)}).String()

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	resolution, err := resolver.Resolve(
		context.Background(),
		mustGitSource(t, fileLocator, "skills/demo", "main"),
		noOperationOptions,
	)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if resolution.Identity().ResolvedRef() != artifact.ResolvedRef(commit) {
		t.Fatalf("ResolvedRef = %q, want %q", resolution.Identity().ResolvedRef(), commit)
	}
}
