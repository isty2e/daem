package gitcli

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveFileURLLocator(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\n---\n")
	commit := commitAll(t, repoPath, "initial skill")
	locator := (&url.URL{Scheme: "file", Path: repoPath}).String()

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	resolved, err := resolver.Resolve(context.Background(), mustGitSource(t, locator, "skills/demo", "main"))
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if string(resolved.Identity().ResolvedRef()) != commit {
		t.Fatalf("resolved ref = %q, want %q", resolved.Identity().ResolvedRef(), commit)
	}
}

func TestResolveRebuildsSubstitutedCompletedArtifact(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	want := "---\nname: trusted\n---\n"
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", want)
	commitAll(t, repoPath, "initial skill")
	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	sourceSpec := mustGitSource(t, repoPath, "skills/demo", "main")

	first, err := resolver.Resolve(context.Background(), sourceSpec)
	if err != nil {
		t.Fatalf("first Resolve returned error: %v", err)
	}
	cacheArtifactRoot := resolver.artifactRoot(repoPath, string(first.Identity().ResolvedRef()), "skills/demo")
	if err := os.WriteFile(filepath.Join(cacheArtifactRoot, "SKILL.md"), []byte("substituted\n"), 0o600); err != nil {
		t.Fatalf("substitute completed artifact: %v", err)
	}

	second, err := resolver.Resolve(context.Background(), sourceSpec)
	if err != nil {
		t.Fatalf("second Resolve returned error: %v", err)
	}
	content := mustReadGitResolutionFile(t, second, "SKILL.md")
	if string(content) != want {
		t.Fatalf("rebuilt content = %q, want %q", content, want)
	}
	if second.Identity().ContentHash() != first.Identity().ContentHash() {
		t.Fatalf("rebuilt hash = %q, want original %q", second.Identity().ContentHash(), first.Identity().ContentHash())
	}
}

func TestResolveRejectsUnqualifiedBranchTagCollision(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\n---\n")
	commitAll(t, repoPath, "initial skill")
	runGitTestCommand(t, repoPath, "branch", "collision")
	runGitTestCommand(t, repoPath, "tag", "collision")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	_, err = resolver.Resolve(context.Background(), mustGitSource(t, repoPath, "skills/demo", "collision"))
	if err == nil || !strings.Contains(err.Error(), "matches both a branch and a tag") {
		t.Fatalf("Resolve error = %v, want branch/tag ambiguity", err)
	}
}

func TestResolvePrunesDeletedTagFromRepositoryCache(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\n---\n")
	commitAll(t, repoPath, "initial skill")
	runGitTestCommand(t, repoPath, "tag", "temporary")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	sourceSpec := mustGitSource(t, repoPath, "skills/demo", "refs/tags/temporary")
	if _, err := resolver.Resolve(context.Background(), sourceSpec); err != nil {
		t.Fatalf("first Resolve returned error: %v", err)
	}

	runGitTestCommand(t, repoPath, "tag", "-d", "temporary")
	_, err = resolver.Resolve(context.Background(), sourceSpec)
	if err == nil || !strings.Contains(err.Error(), "no matching branch or tag") {
		t.Fatalf("second Resolve error = %v, want deleted tag rejection", err)
	}
}

func TestNativeLocalCloneDoesNotHardlinkSourceObjects(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\n---\n")
	commit := commitAll(t, repoPath, "initial skill")

	gitSource, ok := mustGitSource(t, repoPath, ".", commit).Git()
	if !ok {
		t.Fatal("source is not git")
	}
	clonePath := filepath.Join(tempDir, "clone")
	command := exec.Command(gitExecutable, cloneArgs(gitSource, clonePath)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git clone failed: %v\n%s", err, output)
	}

	sourceObject := filepath.Join(repoPath, ".git", "objects", commit[:2], commit[2:])
	cloneObject := filepath.Join(clonePath, ".git", "objects", commit[:2], commit[2:])
	sourceInfo, sourceErr := os.Stat(sourceObject)
	cloneInfo, cloneErr := os.Stat(cloneObject)
	if sourceErr == nil && cloneErr == nil && os.SameFile(sourceInfo, cloneInfo) {
		t.Fatal("native local clone hardlinked a source object")
	}

	if err := os.RemoveAll(repoPath); err != nil {
		t.Fatalf("RemoveAll source repository returned error: %v", err)
	}
	verify := exec.Command(gitExecutable, "cat-file", "-e", commit+"^{commit}")
	verify.Dir = clonePath
	if output, err := verify.CombinedOutput(); err != nil {
		t.Fatalf("cloned repository lost commit after source removal: %v\n%s", err, output)
	}
}

func TestResolveRejectsBundleFileBeforeGitProcessLaunch(t *testing.T) {
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	markerPath := filepath.Join(tempDir, "git-invoked")
	bundlePath := filepath.Join(tempDir, "repo.bundle")
	if err := os.WriteFile(bundlePath, []byte("not needed: rejected by file kind\n"), 0o600); err != nil {
		t.Fatalf("WriteFile bundle returned error: %v", err)
	}
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("MkdirAll bin returned error: %v", err)
	}
	fakeGitPath := filepath.Join(binDir, gitExecutable)
	if err := os.WriteFile(fakeGitPath, []byte("#!/bin/sh\nprintf invoked > \"$DAEM_GIT_MARKER\"\nexit 99\n"), 0o700); err != nil {
		t.Fatalf("WriteFile fake git returned error: %v", err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("DAEM_GIT_MARKER", markerPath)

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	fileURL := (&url.URL{Scheme: "file", Path: bundlePath}).String()
	for _, locator := range []string{bundlePath, fileURL} {
		_, err := resolver.Resolve(context.Background(), mustGitSource(t, locator, ".", "main"))
		if err == nil || !strings.Contains(err.Error(), "bundle files are unsupported") {
			t.Fatalf("Resolve(%q) error = %v, want bundle rejection", locator, err)
		}
	}
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Fatalf("fake git was launched or marker stat failed: %v", err)
	}
}

func TestResolveRejectsInvalidGitCommitOutputBeforeArtifactExport(t *testing.T) {
	requestedCommit := strings.Repeat("a", 40)
	for _, test := range []struct {
		name       string
		gitOutput  string
		wantDetail string
	}{
		{name: "empty", gitOutput: "", wantDetail: "lowercase full immutable object id"},
		{name: "malformed", gitOutput: "not-an-object-id\n", wantDetail: "lowercase full immutable object id"},
		{name: "different commit", gitOutput: strings.Repeat("b", 40) + "\n", wantDetail: "does not match requested immutable object id"},
	} {
		t.Run(test.name, func(t *testing.T) {
			tempDir := t.TempDir()
			resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
			if err != nil {
				t.Fatalf("NewResolver returned error: %v", err)
			}
			locator := filepath.Join(tempDir, "origin")
			sourceSpec := mustGitSource(t, locator, ".", requestedCommit)
			if err := os.MkdirAll(filepath.Join(resolver.repositoryPath(locator), ".git"), 0o700); err != nil {
				t.Fatalf("create cached repository marker: %v", err)
			}

			binDir := filepath.Join(tempDir, "bin")
			if err := os.MkdirAll(binDir, 0o700); err != nil {
				t.Fatalf("create fake git directory: %v", err)
			}
			archiveMarker := filepath.Join(tempDir, "archive-invoked")
			fakeGit := `#!/bin/sh
case "$1" in
fetch)
  exit 0
  ;;
rev-parse)
  printf '%s' "$DAEM_FAKE_GIT_OUTPUT"
  exit 0
  ;;
archive)
  printf invoked > "$DAEM_GIT_ARCHIVE_MARKER"
  exit 97
  ;;
*)
  exit 98
  ;;
esac
`
			if err := os.WriteFile(filepath.Join(binDir, gitExecutable), []byte(fakeGit), 0o700); err != nil {
				t.Fatalf("write fake git: %v", err)
			}
			t.Setenv("PATH", binDir)
			t.Setenv("DAEM_FAKE_GIT_OUTPUT", test.gitOutput)
			t.Setenv("DAEM_GIT_ARCHIVE_MARKER", archiveMarker)

			_, err = resolver.Resolve(context.Background(), sourceSpec)
			if err == nil || !strings.Contains(err.Error(), test.wantDetail) {
				t.Fatalf("Resolve error = %v, want %q", err, test.wantDetail)
			}
			if _, err := os.Stat(archiveMarker); !os.IsNotExist(err) {
				t.Fatalf("artifact export ran before commit correlation succeeded: %v", err)
			}
		})
	}
}

func TestResolveTreatsShellMetacharactersInValidRefAsInertData(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\n---\n")
	commit := commitAll(t, repoPath, "initial skill")
	ref := "main;touch${IFS}owned-by-shell"
	runGitTestCommand(t, repoPath, "branch", ref)

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	resolved, err := resolver.Resolve(context.Background(), mustGitSource(t, repoPath, "skills/demo", ref))
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if string(resolved.Identity().ResolvedRef()) != commit {
		t.Fatalf("resolved ref = %q, want %q", resolved.Identity().ResolvedRef(), commit)
	}
	markerPath := filepath.Join(resolver.repositoryPath(repoPath), "owned-by-shell")
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Fatalf("shell metacharacter ref executed or marker stat failed: %v", err)
	}
}
