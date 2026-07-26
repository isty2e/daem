package gitcli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	artifactpkg "github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
)

func TestResolveGitSkillDirectory(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\ndescription: demo\n---\n")
	commit := commitAll(t, repoPath, "initial skill")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	sourceSpec := mustGitSource(t, repoPath, "skills/demo", "main")
	resolution, err := resolver.Resolve(context.Background(), sourceSpec)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	wantSourceID, err := source.SourceIDFor(sourceSpec)
	if err != nil {
		t.Fatalf("SourceIDFor returned error: %v", err)
	}
	assertGitResolutionIdentity(t, resolution, wantSourceID, artifactpkg.ResolvedRef(commit), artifactpkg.ArtifactKindDirectory)
	content := mustReadGitResolutionFile(t, resolution, "SKILL.md")

	if !strings.Contains(string(content), "name: demo") {
		t.Fatalf("exported skill content = %q", content)
	}
}

func TestResolveWithOptionsEmitsGitBackendEvents(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\ndescription: demo\n---\n")
	commit := commitAll(t, repoPath, "initial skill")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	sourceSpec := mustGitSource(t, repoPath, "skills/demo", "main")
	events := make([]acquisition.Event, 0)

	request := mustGitAcquisitionRequest(t, "skill:000000", 0, acquisition.OperationResolve, sourceSpec)
	options := mustGitOperationOptions(t, request, func(event acquisition.Event) {
		events = append(events, event)
	})
	resolution, err := resolver.ResolveWithOptions(context.Background(), sourceSpec, options)
	if err != nil {
		t.Fatalf("ResolveWithOptions returned error: %v", err)
	}
	if resolution.Identity().ResolvedRef() != artifactpkg.ResolvedRef(commit) {
		t.Fatalf("ResolvedRef = %q, want %q", resolution.Identity().ResolvedRef(), commit)
	}

	for _, want := range []acquisition.EventKind{
		acquisition.EventCacheWait,
		acquisition.EventFetch,
		acquisition.EventExport,
		acquisition.EventHash,
		acquisition.EventPublished,
	} {
		if !hasGitEventKind(events, want) {
			t.Fatalf("events = %#v, want %s", events, want)
		}
	}
	for _, event := range events {
		if event.Request().ID() != "skill:000000" ||
			event.Request().Operation() != acquisition.OperationResolve ||
			event.Request().Source().Kind() != source.SourceKindGit {
			t.Fatalf("event = %#v, want request/source operation identity", event)
		}
	}
}

func TestResolveWithOptionsEmitsGitCacheHitOnlyForReusedCache(t *testing.T) {
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
	request := mustGitAcquisitionRequest(t, "skill:000000", 0, acquisition.OperationResolve, sourceSpec)
	firstEvents := make([]acquisition.Event, 0)
	firstOptions := mustGitOperationOptions(t, request, func(event acquisition.Event) {
		firstEvents = append(firstEvents, event)
	})
	if _, err := resolver.ResolveWithOptions(context.Background(), sourceSpec, firstOptions); err != nil {
		t.Fatalf("first ResolveWithOptions returned error: %v", err)
	}
	if got := countGitEventKind(firstEvents, acquisition.EventCacheHit); got != 0 {
		t.Fatalf("first events cache_hit count = %d, want 0; events=%#v", got, firstEvents)
	}

	secondEvents := make([]acquisition.Event, 0)
	secondOptions := mustGitOperationOptions(t, request, func(event acquisition.Event) {
		secondEvents = append(secondEvents, event)
	})
	if _, err := resolver.ResolveWithOptions(context.Background(), sourceSpec, secondOptions); err != nil {
		t.Fatalf("second ResolveWithOptions returned error: %v", err)
	}
	if got := countGitEventKind(secondEvents, acquisition.EventCacheHit); got < 2 {
		t.Fatalf("second events cache_hit count = %d, want repo and artifact cache hits; events=%#v", got, secondEvents)
	}
	if hasGitEventKind(secondEvents, acquisition.EventExport) || hasGitEventKind(secondEvents, acquisition.EventPublished) {
		t.Fatalf("second events = %#v, want reused cache without export/published", secondEvents)
	}
}

func TestResolveRejectsLocalSource(t *testing.T) {
	resolver, err := NewResolver(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	_, err = resolver.Resolve(context.Background(), sourcetest.Local(t, "skills/demo", source.LocalSourceModeVendor))
	if err == nil {
		t.Fatal("Resolve returned nil error")
	}

	if !strings.Contains(err.Error(), "only supports git sources") {
		t.Fatalf("error = %q, want git-only diagnostic", err)
	}
}

func TestResolveGitFilePreservesExecutableBit(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	hookPath := writeGitTestFile(t, repoPath, "hooks/protect.sh", "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(hookPath, 0o700); err != nil {
		t.Fatalf("Chmod returned error: %v", err)
	}
	commitAll(t, repoPath, "add hook")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	resolution, err := resolver.Resolve(context.Background(), mustGitSource(t, repoPath, "hooks/protect.sh", "main"))
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	if resolution.Identity().Kind() != artifactpkg.ArtifactKindFile {
		t.Fatalf("Kind = %q, want file", resolution.Identity().Kind())
	}
	content, err := resolution.View().ReadFile(context.Background(), ".", 1<<20)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if content.Mode().Perm()&0o111 == 0 {
		t.Fatalf("exported hook mode = %s, want executable bit", content.Mode().Perm())
	}
}
