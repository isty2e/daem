package gitcli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	artifactpkg "github.com/isty2e/daem/internal/supply/artifact"
)

func TestListSourceRootRejectsNilAndCanceledContexts(t *testing.T) {
	sourceSpec := mustGitSource(t, "https://example.test/repository.git", "skills", "main")

	if _, err := (Resolver{}).ListSourceRoot(nil, sourceSpec, noOperationOptions); err == nil ||
		!strings.Contains(err.Error(), "context is required") {
		t.Fatalf("nil context error = %v, want explicit rejection", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Resolver{}).ListSourceRoot(ctx, sourceSpec, noOperationOptions); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context error = %v, want context.Canceled", err)
	}
}

func TestResolveGitPathWithSpaces(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/my skill/SKILL.md", "---\nname: my-skill\ndescription: demo\n---\n")
	commitAll(t, repoPath, "initial skill")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	resolution, err := resolver.Resolve(context.Background(), mustGitSource(t, repoPath, "skills/my skill", "main"), noOperationOptions)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	mustReadGitResolutionFile(t, resolution, "SKILL.md")
}

func TestResolveReportsMissingGitRef(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\n---\n")
	commitAll(t, repoPath, "initial skill")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	_, err = resolver.Resolve(context.Background(), mustGitSource(t, repoPath, "skills/demo", "missing-ref"), noOperationOptions)
	if err == nil {
		t.Fatal("Resolve returned nil error")
	}

	if !strings.Contains(err.Error(), "resolve git ref name:missing-ref") {
		t.Fatalf("error = %q, want missing ref context", err)
	}
}

func TestResolveLeadingDashGitPath(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "-skill/SKILL.md", "---\nname: dash\ndescription: demo\n---\n")
	commitAll(t, repoPath, "initial skill")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	resolution, err := resolver.Resolve(context.Background(), mustGitSource(t, repoPath, "-skill", "main"), noOperationOptions)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	if resolution.Identity().Kind() != artifactpkg.ArtifactKindDirectory {
		t.Fatalf("Kind = %q, want directory", resolution.Identity().Kind())
	}
}

func TestResolveHonorsCanceledContext(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\n---\n")
	commitAll(t, repoPath, "initial skill")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = resolver.Resolve(ctx, mustGitSource(t, repoPath, "skills/demo", "main"), noOperationOptions)
	if err == nil {
		t.Fatal("Resolve returned nil error")
	}

	if err != context.Canceled {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestResolveRepositoryPathWithSpaces(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	repoParent := filepath.Join(tempDir, "parent with spaces")
	repoPath := initGitRepository(t, repoParent)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\ndescription: demo\n---\n")
	commitAll(t, repoPath, "initial skill")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache with spaces"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	resolution, err := resolver.Resolve(context.Background(), mustGitSource(t, repoPath, "skills/demo", "main"), noOperationOptions)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	mustReadGitResolutionFile(t, resolution, "SKILL.md")
}

func TestResolveLinkFailureCleansTemporaryArtifact(t *testing.T) {
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
	commit := commitAll(t, repoPath, "add symlink")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	_, err = resolver.Resolve(context.Background(), mustGitSource(t, repoPath, "links/target", "main"), noOperationOptions)
	if err == nil {
		t.Fatal("Resolve returned nil error")
	}

	entryRoot := resolver.artifactEntryRoot(repoPath, commit, "links/target")
	if cacheEntryExists(entryRoot) {
		t.Fatalf("symlink artifact entry %q was published", entryRoot)
	}
	if _, err := os.Lstat(entryRoot); !os.IsNotExist(err) {
		t.Fatalf("symlink artifact entry exists or stat failed unexpectedly: %v", err)
	}
	assertNoTemporaryArtifacts(t, filepath.Dir(entryRoot))
}

func TestResolveMissingPathDoesNotPublishArtifact(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\n---\n")
	commit := commitAll(t, repoPath, "initial skill")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	_, err = resolver.Resolve(context.Background(), mustGitSource(t, repoPath, "skills/missing", "main"), noOperationOptions)
	if err == nil {
		t.Fatal("Resolve returned nil error")
	}

	entryRoot := resolver.artifactEntryRoot(repoPath, commit, "skills/missing")
	if cacheEntryExists(entryRoot) {
		t.Fatalf("missing path artifact entry %q was published", entryRoot)
	}
	if _, err := os.Stat(entryRoot); !os.IsNotExist(err) {
		t.Fatalf("missing path artifact exists or stat failed unexpectedly: %v", err)
	}
	assertNoTemporaryArtifacts(t, filepath.Dir(entryRoot))
}

func TestResolveAllowsRootGitPath(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "SKILL.md", "---\nname: root\n---\n")
	commit := commitAll(t, repoPath, "initial skill")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	resolution, err := resolver.Resolve(context.Background(), mustGitSource(t, repoPath, ".", "main"), noOperationOptions)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if err := resolution.View().Verify(context.Background(), resolution.Identity()); err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(resolver.artifactRoot(repoPath, commit, "."), "SKILL.md")); err != nil {
		t.Fatalf("root cache artifact is unavailable: %v", err)
	}
}

func TestListSourceRootListsGitDirectoriesWithoutArtifactExport(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/beta/SKILL.md", "---\nname: beta\n---\n")
	writeGitTestFile(t, repoPath, "skills/alpha/SKILL.md", "---\nname: alpha\n---\n")
	writeGitTestFile(t, repoPath, "docs/README.md", "docs\n")
	writeGitTestFile(t, repoPath, "README.md", "root file\n")
	commit := commitAll(t, repoPath, "initial tree")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	rootListing, err := resolver.ListSourceRoot(context.Background(), mustGitSource(t, repoPath, ".", "main"), noOperationOptions)
	if err != nil {
		t.Fatalf("ListSourceRoot root returned error: %v", err)
	}
	if rootListing.Kind() != artifactpkg.ArtifactKindDirectory {
		t.Fatalf("root Kind = %q, want directory", rootListing.Kind())
	}
	if strings.Join(rootListing.ChildNames(), ",") != "docs,skills" {
		t.Fatalf("root ChildNames = %#v, want docs,skills", rootListing.ChildNames())
	}
	if rootListing.ResolvedRef() != artifactpkg.ResolvedRef(commit) {
		t.Fatalf("root ResolvedRef = %q, want %q", rootListing.ResolvedRef(), commit)
	}

	skillsListing, err := resolver.ListSourceRoot(context.Background(), mustGitSource(t, repoPath, "skills", "main"), noOperationOptions)
	if err != nil {
		t.Fatalf("ListSourceRoot skills returned error: %v", err)
	}
	if strings.Join(skillsListing.ChildNames(), ",") != "alpha,beta" {
		t.Fatalf("skills ChildNames = %#v, want alpha,beta", skillsListing.ChildNames())
	}
	if _, err := os.Stat(resolver.artifactRoot(repoPath, commit, ".")); !os.IsNotExist(err) {
		t.Fatalf("root artifact exists or stat failed unexpectedly: %v", err)
	}
	if _, err := os.Stat(resolver.artifactRoot(repoPath, commit, "skills")); !os.IsNotExist(err) {
		t.Fatalf("skills artifact exists or stat failed unexpectedly: %v", err)
	}
}
