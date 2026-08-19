package gitcli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	artifactpkg "github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
)

func TestResolveRootPathKeepsCompletionRecordOutsideContent(t *testing.T) {
	t.Parallel()
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "SKILL.md", "---\nname: root\n---\n")
	commit := commitAll(t, repoPath, "initial skill")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	resolved, err := resolver.Resolve(context.Background(), mustGitSource(t, repoPath, ".", "main"), noOperationOptions)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	entryRoot := resolver.artifactEntryRoot(repoPath, commit, ".")
	if !cacheEntryExists(entryRoot) {
		t.Fatalf("artifact entry %q was not published", entryRoot)
	}
	contentPath := resolver.artifactRoot(repoPath, commit, ".")
	if _, err := os.Lstat(filepath.Join(contentPath, ".daem-complete")); !os.IsNotExist(err) {
		t.Fatalf("completion record is visible under ContentPath: %v", err)
	}

	contentHash, err := resolved.View().Hash(context.Background())
	if err != nil {
		t.Fatalf("View.Hash returned error: %v", err)
	}
	if contentHash != resolved.Identity().ContentHash() {
		t.Fatalf("ContentHash = %q, want %q", resolved.Identity().ContentHash(), contentHash)
	}
	if resolved.Identity().Kind() != artifactpkg.ArtifactKindDirectory {
		t.Fatalf("Kind = %q, want directory", resolved.Identity().Kind())
	}
}

func TestResolveConcurrentSameRepoDifferentPaths(t *testing.T) {
	t.Parallel()
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/alpha/SKILL.md", "---\nname: alpha\n---\n")
	writeGitTestFile(t, repoPath, "skills/beta/SKILL.md", "---\nname: beta\n---\n")
	commit := commitAll(t, repoPath, "add skills")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	sources := []source.Source{
		mustGitSource(t, repoPath, "skills/alpha", "main"),
		mustGitSource(t, repoPath, "skills/beta", "main"),
	}
	results := resolveConcurrently(t, resolver, sources)

	for index, result := range results {
		if result.err != nil {
			t.Fatalf("Resolve %d returned error: %v", index, result.err)
		}
		if result.resolution.Identity().ResolvedRef() != artifactpkg.ResolvedRef(commit) {
			t.Fatalf("Resolve %d ResolvedRef = %q, want %q", index, result.resolution.Identity().ResolvedRef(), commit)
		}
		if result.resolution.Identity().Kind() != artifactpkg.ArtifactKindDirectory {
			t.Fatalf("Resolve %d Kind = %q, want directory", index, result.resolution.Identity().Kind())
		}
		mustReadGitResolutionFile(t, result.resolution, "SKILL.md")
	}

	assertNoTemporaryArtifacts(t, filepath.Dir(resolver.artifactEntryRoot(repoPath, commit, "skills/alpha")))
	assertNoTemporaryArtifacts(t, filepath.Dir(resolver.artifactEntryRoot(repoPath, commit, "skills/beta")))
}

func TestResolveConcurrentSameRepoDifferentRefs(t *testing.T) {
	t.Parallel()
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\ndescription: base\n---\n")
	commitAll(t, repoPath, "base skill")

	runGitTestCommand(t, repoPath, "checkout", "-b", "feature")
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\ndescription: feature\n---\n")
	featureCommit := commitAll(t, repoPath, "feature skill")

	runGitTestCommand(t, repoPath, "checkout", "main")
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\ndescription: main\n---\n")
	mainCommit := commitAll(t, repoPath, "main skill")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	sources := []source.Source{
		mustGitSource(t, repoPath, "skills/demo", "main"),
		mustGitSource(t, repoPath, "skills/demo", "feature"),
	}
	results := resolveConcurrently(t, resolver, sources)

	wantRefs := []artifactpkg.ResolvedRef{
		artifactpkg.ResolvedRef(mainCommit),
		artifactpkg.ResolvedRef(featureCommit),
	}
	wantDescriptions := []string{"description: main", "description: feature"}
	for index, result := range results {
		if result.err != nil {
			t.Fatalf("Resolve %d returned error: %v", index, result.err)
		}
		if result.resolution.Identity().ResolvedRef() != wantRefs[index] {
			t.Fatalf("Resolve %d ResolvedRef = %q, want %q", index, result.resolution.Identity().ResolvedRef(), wantRefs[index])
		}
		content := mustReadGitResolutionFile(t, result.resolution, "SKILL.md")
		if !strings.Contains(string(content), wantDescriptions[index]) {
			t.Fatalf("Resolve %d content = %q, want %q", index, content, wantDescriptions[index])
		}
	}
}

func TestResolveConcurrentSameArtifactAcrossResolverInstances(t *testing.T) {
	t.Parallel()
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\n---\n")
	commit := commitAll(t, repoPath, "initial skill")

	cacheRoot := filepath.Join(tempDir, "cache")
	firstResolver, err := NewResolver(cacheRoot)
	if err != nil {
		t.Fatalf("first NewResolver returned error: %v", err)
	}
	secondResolver, err := NewResolver(cacheRoot)
	if err != nil {
		t.Fatalf("second NewResolver returned error: %v", err)
	}

	sourceSpec := mustGitSource(t, repoPath, "skills/demo", "main")
	const workers = 8
	results := make(chan resolveResult, workers)
	start := make(chan struct{})
	var waitGroup sync.WaitGroup
	for worker := range workers {
		waitGroup.Add(1)
		go func(worker int) {
			defer waitGroup.Done()
			<-start
			resolver := firstResolver
			if worker%2 == 1 {
				resolver = secondResolver
			}
			resolved, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions)
			results <- resolveResult{resolution: resolved, err: err}
		}(worker)
	}
	close(start)
	waitGroup.Wait()
	close(results)

	var first artifactpkg.ExactIdentity
	for result := range results {
		if result.err != nil {
			t.Fatalf("Resolve returned error: %v", result.err)
		}
		identity := result.resolution.Identity()
		if first.SourceID() == "" {
			first = identity
			continue
		}
		if !identity.Equal(first) {
			t.Fatalf("artifact identity mismatch: %#v != %#v", identity, first)
		}
	}

	entryRoot := firstResolver.artifactEntryRoot(repoPath, commit, "skills/demo")
	if !cacheEntryExists(entryRoot) {
		t.Fatalf("artifact entry %q was not published", entryRoot)
	}
	assertNoTemporaryArtifacts(t, filepath.Dir(entryRoot))
}

func TestConcurrentResolveAndListSourceRootDoesNotPublishListingArtifact(t *testing.T) {
	t.Parallel()
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/alpha/SKILL.md", "---\nname: alpha\n---\n")
	writeGitTestFile(t, repoPath, "skills/beta/SKILL.md", "---\nname: beta\n---\n")
	commit := commitAll(t, repoPath, "add skills")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	resolveDone := make(chan resolveResult, 1)
	listDone := make(chan listResult, 1)
	go func() {
		resolved, err := resolver.Resolve(context.Background(), mustGitSource(t, repoPath, "skills/alpha", "main"), noOperationOptions)
		resolveDone <- resolveResult{resolution: resolved, err: err}
	}()
	go func() {
		listing, err := resolver.ListSourceRoot(context.Background(), mustGitSource(t, repoPath, "skills", "main"), noOperationOptions)
		listDone <- listResult{listing: listing, err: err}
	}()

	resolved := <-resolveDone
	if resolved.err != nil {
		t.Fatalf("Resolve returned error: %v", resolved.err)
	}
	listed := <-listDone
	if listed.err != nil {
		t.Fatalf("ListSourceRoot returned error: %v", listed.err)
	}

	if strings.Join(listed.listing.ChildNames(), ",") != "alpha,beta" {
		t.Fatalf("ChildNames = %#v, want alpha,beta", listed.listing.ChildNames())
	}
	if listed.listing.ResolvedRef() != artifactpkg.ResolvedRef(commit) {
		t.Fatalf("ResolvedRef = %q, want %q", listed.listing.ResolvedRef(), commit)
	}
	if _, err := os.Lstat(resolver.artifactEntryRoot(repoPath, commit, "skills")); !os.IsNotExist(err) {
		t.Fatalf("ListSourceRoot created an artifact entry or stat failed unexpectedly: %v", err)
	}
}

func TestResolveRejectsUnownedPartialArtifactUnderLock(t *testing.T) {
	t.Parallel()
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\n---\n")
	commit := commitAll(t, repoPath, "initial skill")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	entryRoot := resolver.artifactEntryRoot(repoPath, commit, "skills/demo")
	if err := os.MkdirAll(entryRoot, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(entryRoot, "partial.txt"), []byte("partial\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	_, err = resolver.Resolve(context.Background(), mustGitSource(t, repoPath, "skills/demo", "main"), noOperationOptions)
	if err == nil || !strings.Contains(err.Error(), "completion record is missing") {
		t.Fatalf("Resolve error = %v, want unowned partial-entry rejection", err)
	}

	if !cacheEntryExists(entryRoot) {
		t.Fatalf("unowned artifact entry %q was removed", entryRoot)
	}
	content, err := os.ReadFile(filepath.Join(entryRoot, "partial.txt"))
	if err != nil || string(content) != "partial\n" {
		t.Fatalf("unowned partial content = %q, %v, want preserved", content, err)
	}
}

func TestResolveDoesNotRemoveCompleteArtifactOnReResolve(t *testing.T) {
	t.Parallel()
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\n---\n")
	commit := commitAll(t, repoPath, "initial skill")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	if _, err := resolver.Resolve(context.Background(), mustGitSource(t, repoPath, "skills/demo", "main"), noOperationOptions); err != nil {
		t.Fatalf("first Resolve returned error: %v", err)
	}

	entryRoot := resolver.artifactEntryRoot(repoPath, commit, "skills/demo")
	sentinelPath := filepath.Join(entryRoot, "sentinel.txt")
	if err := os.WriteFile(sentinelPath, []byte("keep\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	if _, err := resolver.Resolve(context.Background(), mustGitSource(t, repoPath, "skills/demo", "main"), noOperationOptions); err != nil {
		t.Fatalf("second Resolve returned error: %v", err)
	}
	if _, err := os.Stat(sentinelPath); err != nil {
		t.Fatalf("complete artifact entry was removed or rewritten: %v", err)
	}
}

func TestResolveCancellationDuringArtifactPublishLeavesNoCompletionRecord(t *testing.T) {
	t.Parallel()
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\n---\n")
	commit := commitAll(t, repoPath, "initial skill")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	resolver.state.testAfterArchiveExtract = cancel
	_, err = resolver.Resolve(ctx, mustGitSource(t, repoPath, "skills/demo", "main"), noOperationOptions)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve error = %v, want context.Canceled", err)
	}

	entryRoot := resolver.artifactEntryRoot(repoPath, commit, "skills/demo")
	if cacheEntryExists(entryRoot) {
		t.Fatalf("artifact entry %q was published after cancellation", entryRoot)
	}
	if _, err := os.Lstat(entryRoot); !os.IsNotExist(err) {
		t.Fatalf("artifact entry exists after cancellation or stat failed unexpectedly: %v", err)
	}
	assertNoTemporaryArtifacts(t, filepath.Dir(entryRoot))
}

func TestResolveRepoLockWaiterCancellationReportsPathContext(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("rooted cache locks are unsupported on this platform")
	}
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\n---\n")
	commitAll(t, repoPath, "initial skill")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	key, err := cacheKeyForGitRepo(repoPath)
	if err != nil {
		t.Fatalf("cacheKeyForGitRepo returned error: %v", err)
	}
	ownerRoot, err := resolver.captureCacheRoot(context.Background())
	if err != nil {
		t.Fatalf("captureCacheRoot returned error: %v", err)
	}
	defer ownerRoot.Close()

	held := make(chan struct{})
	release := make(chan struct{})
	ownerErr := make(chan error, 1)
	var releaseOnce sync.Once
	releaseOwner := func() {
		releaseOnce.Do(func() { close(release) })
	}
	ownerCtx, ownerCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer ownerCancel()
	go func() {
		ownerErr <- resolver.state.repoLocker.DoRooted(
			ownerCtx,
			ownerRoot,
			key,
			func() error {
				close(held)
				<-release
				return nil
			},
		)
	}()
	ownerDone := false
	defer func() {
		releaseOwner()
		if ownerDone {
			return
		}
		select {
		case err := <-ownerErr:
			if err != nil {
				t.Errorf("owner DoRooted returned error: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("owner DoRooted did not finish")
		}
	}()

	select {
	case <-held:
	case err := <-ownerErr:
		ownerDone = true
		t.Fatalf("owner DoRooted failed before hold: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for owner to hold rooted repo lock")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waitEntered := make(chan struct{})
	resolver.state.testAfterRepoLockWaitBlocked = func() {
		close(waitEntered)
		cancel()
	}
	waiterErr := make(chan error, 1)
	go func() {
		_, err := resolver.Resolve(ctx, mustGitSource(t, repoPath, "skills/demo", "main"), noOperationOptions)
		waiterErr <- err
	}()

	select {
	case <-waitEntered:
		select {
		case err = <-waiterErr:
		case <-time.After(5 * time.Second):
			cancel()
			releaseOwner()
			t.Fatal("timed out waiting for Resolve cancellation")
		}
	case err = <-waiterErr:
		select {
		case <-waitEntered:
		default:
			t.Fatalf("Resolve returned before wait-entered: %v", err)
		}
	case <-time.After(5 * time.Second):
		cancel()
		releaseOwner()
		t.Fatal("timed out waiting for Resolve to enter rooted repo lock wait")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve error = %v, want context.Canceled", err)
	}
	if !strings.Contains(err.Error(), "wait for rooted cache lock") ||
		!strings.Contains(err.Error(), key.PathComponent()) ||
		!strings.Contains(err.Error(), filepath.Join("locks", "git-repo")) {
		t.Fatalf("Resolve error = %q, want rooted repo lock wait diagnostic", err)
	}
}

type resolveResult struct {
	resolution acquisition.Resolution
	err        error
}

type listResult struct {
	listing source.RootListing
	err     error
}

func resolveConcurrently(t *testing.T, resolver Resolver, sources []source.Source) []resolveResult {
	t.Helper()

	start := make(chan struct{})
	results := make([]resolveResult, len(sources))
	var waitGroup sync.WaitGroup
	for index, sourceSpec := range sources {
		waitGroup.Add(1)
		go func(index int, sourceSpec source.Source) {
			defer waitGroup.Done()
			<-start
			resolved, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions)
			results[index] = resolveResult{resolution: resolved, err: err}
		}(index, sourceSpec)
	}
	close(start)
	waitGroup.Wait()

	return results
}
