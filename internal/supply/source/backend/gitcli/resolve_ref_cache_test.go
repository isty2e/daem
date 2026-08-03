package gitcli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	artifactpkg "github.com/isty2e/daem/internal/supply/artifact"
)

func TestResolveRejectsUnownedStaleCacheDirectory(t *testing.T) {
	t.Parallel()
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\ndescription: demo\n---\n")
	commitAll(t, repoPath, "initial skill")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	staleRepoPath := resolver.repositoryPath(repoPath)
	if err := os.MkdirAll(staleRepoPath, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

	if err := os.WriteFile(filepath.Join(staleRepoPath, "partial"), []byte("interrupted clone\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	_, err = resolver.Resolve(context.Background(), mustGitSource(t, repoPath, "skills/demo", "main"), noOperationOptions)
	if err == nil || !strings.Contains(err.Error(), "cache authority record") {
		t.Fatalf("Resolve error = %v, want unowned cache rejection", err)
	}
	if _, err := os.Stat(filepath.Join(staleRepoPath, "partial")); err != nil {
		t.Fatalf("unowned cache entry was removed or changed: %v", err)
	}
}

func TestResolveAnnotatedTagRef(t *testing.T) {
	t.Parallel()
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\ndescription: demo\n---\n")
	commit := commitAll(t, repoPath, "initial skill")
	runGitTestCommand(t, repoPath, "tag", "-a", "v1.0.0", "-m", "v1.0.0")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	resolution, err := resolver.Resolve(context.Background(), mustGitSource(t, repoPath, "skills/demo", "v1.0.0"), noOperationOptions)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	if resolution.Identity().ResolvedRef() != artifactpkg.ResolvedRef(commit) {
		t.Fatalf("ResolvedRef = %q, want %q", resolution.Identity().ResolvedRef(), commit)
	}
}

func TestResolveSameCommitHasStableContentHash(t *testing.T) {
	t.Parallel()
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\ndescription: demo\n---\n")
	commitAll(t, repoPath, "initial skill")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	firstArtifact, err := resolver.Resolve(context.Background(), mustGitSource(t, repoPath, "skills/demo", "main"), noOperationOptions)
	if err != nil {
		t.Fatalf("first Resolve returned error: %v", err)
	}

	secondArtifact, err := resolver.Resolve(context.Background(), mustGitSource(t, repoPath, "skills/demo", "main"), noOperationOptions)
	if err != nil {
		t.Fatalf("second Resolve returned error: %v", err)
	}

	if firstArtifact.Identity().ContentHash() != secondArtifact.Identity().ContentHash() {
		t.Fatalf("ContentHash changed across identical resolves: %q != %q", firstArtifact.Identity().ContentHash(), secondArtifact.Identity().ContentHash())
	}
}

func TestResolvePinnedCommitDoesNotMoveWithBranch(t *testing.T) {
	t.Parallel()
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	skillPath := writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\ndescription: one\n---\n")
	firstCommit := commitAll(t, repoPath, "initial skill")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	firstArtifact, err := resolver.Resolve(context.Background(), mustGitSource(t, repoPath, "skills/demo", firstCommit), noOperationOptions)
	if err != nil {
		t.Fatalf("first Resolve returned error: %v", err)
	}

	if err := os.WriteFile(skillPath, []byte("---\nname: demo\ndescription: two\n---\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	commitAll(t, repoPath, "update skill")

	secondArtifact, err := resolver.Resolve(context.Background(), mustGitSource(t, repoPath, "skills/demo", firstCommit), noOperationOptions)
	if err != nil {
		t.Fatalf("second Resolve returned error: %v", err)
	}

	if secondArtifact.Identity().ResolvedRef() != artifactpkg.ResolvedRef(firstCommit) {
		t.Fatalf("ResolvedRef = %q, want pinned commit %q", secondArtifact.Identity().ResolvedRef(), firstCommit)
	}

	if secondArtifact.Identity().ContentHash() != firstArtifact.Identity().ContentHash() {
		t.Fatalf("ContentHash changed for pinned commit: %q != %q", secondArtifact.Identity().ContentHash(), firstArtifact.Identity().ContentHash())
	}
}

func TestResolveFetchesForcePushedBranch(t *testing.T) {
	t.Parallel()
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	skillPath := writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\ndescription: one\n---\n")
	firstCommit := commitAll(t, repoPath, "initial skill")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	if _, err := resolver.Resolve(context.Background(), mustGitSource(t, repoPath, "skills/demo", "main"), noOperationOptions); err != nil {
		t.Fatalf("first Resolve returned error: %v", err)
	}

	if err := os.WriteFile(skillPath, []byte("---\nname: demo\ndescription: two\n---\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	commitAll(t, repoPath, "update skill")
	runGitTestCommand(t, repoPath, "reset", "--hard", firstCommit)

	resolution, err := resolver.Resolve(context.Background(), mustGitSource(t, repoPath, "skills/demo", "main"), noOperationOptions)
	if err != nil {
		t.Fatalf("second Resolve returned error: %v", err)
	}

	if resolution.Identity().ResolvedRef() != artifactpkg.ResolvedRef(firstCommit) {
		t.Fatalf("ResolvedRef = %q, want force-pushed commit %q", resolution.Identity().ResolvedRef(), firstCommit)
	}
}

func TestResolveFetchesForceUpdatedTag(t *testing.T) {
	t.Parallel()
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	skillPath := writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\ndescription: one\n---\n")
	commitAll(t, repoPath, "initial skill")
	runGitTestCommand(t, repoPath, "tag", "-a", "v1.0.0", "-m", "v1.0.0")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	if _, err := resolver.Resolve(context.Background(), mustGitSource(t, repoPath, "skills/demo", "v1.0.0"), noOperationOptions); err != nil {
		t.Fatalf("first Resolve returned error: %v", err)
	}

	if err := os.WriteFile(skillPath, []byte("---\nname: demo\ndescription: two\n---\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	secondCommit := commitAll(t, repoPath, "update skill")
	runGitTestCommand(t, repoPath, "tag", "-f", "-a", "v1.0.0", "-m", "v1.0.0")

	resolution, err := resolver.Resolve(context.Background(), mustGitSource(t, repoPath, "skills/demo", "v1.0.0"), noOperationOptions)
	if err != nil {
		t.Fatalf("second Resolve returned error: %v", err)
	}

	if resolution.Identity().ResolvedRef() != artifactpkg.ResolvedRef(secondCommit) {
		t.Fatalf("ResolvedRef = %q, want force-updated tag commit %q", resolution.Identity().ResolvedRef(), secondCommit)
	}
}

func TestResolveFetchesUpdatedRef(t *testing.T) {
	t.Parallel()
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	skillPath := writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\ndescription: one\n---\n")
	firstCommit := commitAll(t, repoPath, "initial skill")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	firstArtifact, err := resolver.Resolve(context.Background(), mustGitSource(t, repoPath, "skills/demo", "main"), noOperationOptions)
	if err != nil {
		t.Fatalf("first Resolve returned error: %v", err)
	}

	if firstArtifact.Identity().ResolvedRef() != artifactpkg.ResolvedRef(firstCommit) {
		t.Fatalf("first ResolvedRef = %q, want %q", firstArtifact.Identity().ResolvedRef(), firstCommit)
	}

	if err := os.WriteFile(skillPath, []byte("---\nname: demo\ndescription: two\n---\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	secondCommit := commitAll(t, repoPath, "update skill")

	secondArtifact, err := resolver.Resolve(context.Background(), mustGitSource(t, repoPath, "skills/demo", "main"), noOperationOptions)
	if err != nil {
		t.Fatalf("second Resolve returned error: %v", err)
	}

	if secondArtifact.Identity().ResolvedRef() != artifactpkg.ResolvedRef(secondCommit) {
		t.Fatalf("second ResolvedRef = %q, want %q", secondArtifact.Identity().ResolvedRef(), secondCommit)
	}

	if secondArtifact.Identity().ContentHash() == firstArtifact.Identity().ContentHash() {
		t.Fatalf("ContentHash did not change after ref update: %q", secondArtifact.Identity().ContentHash())
	}
}
