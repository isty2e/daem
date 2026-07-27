package gitcli

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/supply/source/acquisition"
)

func TestRepositorySnapshotSessionCompletedCallWinsOverCanceledWait(t *testing.T) {
	key := repositorySnapshotKey{locator: "https://example.test/repository.git", canonicalRef: "name:main"}
	want := repositoryResolution{repoPath: "/cache/repository", commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	for iteration := range 100 {
		done := make(chan struct{})
		call := &repositorySnapshotCall{done: done, resolution: want}
		session := newRepositorySnapshotSession()
		session.calls[key] = call
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		close(done)

		got, err := session.resolve(ctx, key, func() (repositoryResolution, error) {
			t.Fatal("resolve function ran despite completed in-flight call")
			return repositoryResolution{}, nil
		})
		if err != nil || got != want {
			t.Fatalf("iteration %d: resolve = %#v, %v, want %#v, nil", iteration, got, err, want)
		}
	}
}

func TestRepositorySnapshotSessionFailureIsNotMemoized(t *testing.T) {
	session := newRepositorySnapshotSession()
	key := repositorySnapshotKey{locator: "https://example.test/repository.git", canonicalRef: "name:main"}
	wantErr := errors.New("temporary repository failure")
	calls := 0

	_, err := session.resolve(context.Background(), key, func() (repositoryResolution, error) {
		calls++
		return repositoryResolution{}, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("first resolve error = %v, want temporary failure", err)
	}
	want := repositoryResolution{repoPath: "/cache/repository", commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	got, err := session.resolve(context.Background(), key, func() (repositoryResolution, error) {
		calls++
		return want, nil
	})
	if err != nil || got != want || calls != 2 {
		t.Fatalf("retry resolve = %#v, %v, calls %d, want %#v, nil, 2", got, err, calls, want)
	}
}

func TestRepositorySnapshotSessionRegistersImmutableCommitAlias(t *testing.T) {
	session := newRepositorySnapshotSession()
	locator := "https://example.test/repository.git"
	commit := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	selectorKey := repositorySnapshotKey{locator: locator, canonicalRef: "branch:main"}
	want := repositoryResolution{repoPath: "/cache/repository", commit: commit}
	if _, err := session.resolve(context.Background(), selectorKey, func() (repositoryResolution, error) {
		return want, nil
	}); err != nil {
		t.Fatalf("selector resolve returned error: %v", err)
	}

	got, err := session.resolve(context.Background(), immutableRepositoryKey(locator, commit), func() (repositoryResolution, error) {
		t.Fatal("immutable alias did not reuse selector snapshot")
		return repositoryResolution{}, nil
	})
	if err != nil || got != want {
		t.Fatalf("alias resolve = %#v, %v, want %#v, nil", got, err, want)
	}
}

func TestRepositorySnapshotSessionKeepsPathFailuresIsolated(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	repositoryPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repositoryPath, "skills/valid/SKILL.md", "---\nname: valid\ndescription: valid\n---\n")
	commitAll(t, repositoryPath, "add valid skill")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	resolver = resolver.WithRepositorySnapshotSession()
	events := make([]acquisition.Event, 0)
	eventSink := func(event acquisition.Event) {
		events = append(events, event)
	}
	missingSource := mustGitSource(t, repositoryPath, "skills/missing", "main")
	missingOptions := mustGitOperationOptions(
		t,
		mustGitAcquisitionRequest(t, "missing", 0, acquisition.OperationResolve, missingSource),
		eventSink,
	)

	if _, err := resolver.Resolve(
		context.Background(),
		missingSource,
		missingOptions,
	); err == nil {
		t.Fatal("missing path Resolve returned nil error")
	}
	validSource := mustGitSource(t, repositoryPath, "skills/valid", "main")
	validOptions := mustGitOperationOptions(
		t,
		mustGitAcquisitionRequest(t, "valid", 1, acquisition.OperationResolve, validSource),
		eventSink,
	)
	valid, err := resolver.Resolve(
		context.Background(),
		validSource,
		validOptions,
	)
	if err != nil {
		t.Fatalf("valid Resolve returned error after sibling failure: %v", err)
	}
	if valid.Identity().SourceID() == "" || valid.Identity().ContentHash() == "" {
		t.Fatalf("valid artifact is incomplete: %#v", valid)
	}
	if countGitEvents(events, acquisition.EventFetch) != 1 {
		t.Fatalf("fetch event count = %d, want 1: %#v", countGitEvents(events, acquisition.EventFetch), events)
	}
}

func countGitEvents(events []acquisition.Event, kind acquisition.EventKind) int {
	count := 0
	for _, event := range events {
		if event.Kind() == kind {
			count++
		}
	}
	return count
}
