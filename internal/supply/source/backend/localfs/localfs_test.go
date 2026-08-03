package localfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	artifactpkg "github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/supply/source/directfile"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
)

var noOperationOptions acquisition.OperationOptions

func TestResolveVendorLocalDirectory(t *testing.T) {
	root := t.TempDir()
	writeLocalTestFile(t, root, "skills/demo/SKILL.md", "---\nname: demo\n---\n")

	resolver, err := NewResolver(root)
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	sourceSpec := sourcetest.Local(t, "skills/demo", source.LocalSourceModeVendor)
	resolution, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	identity := resolution.Identity()
	if identity.Kind() != artifactpkg.ArtifactKindDirectory {
		t.Fatalf("Kind = %q, want directory", identity.Kind())
	}

	if identity.SourceID() != artifactpkg.SourceID("local:skills/demo?mode=vendor") {
		t.Fatalf("SourceID = %q", identity.SourceID())
	}

	if !strings.HasPrefix(string(identity.ContentHash()), "sha256:") {
		t.Fatalf("ContentHash = %q, want sha256 prefix", identity.ContentHash())
	}
	if err := resolution.View().Verify(context.Background(), identity); err != nil {
		t.Fatalf("View.Verify returned error: %v", err)
	}

	wantAuthorityPath := filepath.Join(root, "skills/demo")
	authorityPath, err := resolver.LocalInputAuthorityPath(sourceSpec)
	if err != nil {
		t.Fatalf("LocalInputAuthorityPath returned error: %v", err)
	}
	if authorityPath != wantAuthorityPath {
		t.Fatalf("LocalInputAuthorityPath = %q, want %q", authorityPath, wantAuthorityPath)
	}
}

func TestResolveEmitsHashEvent(t *testing.T) {
	root := t.TempDir()
	writeLocalTestFile(t, root, "instructions/project.md", "project instructions\n")

	resolver, err := NewResolver(root)
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	sourceSpec := sourcetest.Local(t, "instructions/project.md", source.LocalSourceModeVendor)
	events := make([]acquisition.Event, 0)

	request, err := acquisition.NewRequest("instructions:000000", 0, acquisition.OperationResolve, sourceSpec)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	options, err := acquisition.NewOperationOptions(request, func(event acquisition.Event) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatalf("NewOperationOptions returned error: %v", err)
	}
	_, err = resolver.Resolve(context.Background(), sourceSpec, options)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("events = %#v, want exactly one hash event", events)
	}
	if events[0].Kind() != acquisition.EventHash ||
		events[0].Request().ID() != "instructions:000000" ||
		events[0].SourceID() != artifactpkg.SourceID("local:instructions/project.md?mode=vendor") {
		t.Fatalf("event = %#v, want local hash event with request/source identity", events[0])
	}
}

func TestResolveLinkLocalFile(t *testing.T) {
	root := t.TempDir()
	writeLocalTestFile(t, root, "hooks/protect_env.py", "print('check')\n")

	resolver, err := NewResolver(root)
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	resolution, err := resolver.Resolve(context.Background(), sourcetest.Local(t, "hooks/protect_env.py", source.LocalSourceModeLink), noOperationOptions)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	if resolution.Identity().Kind() != artifactpkg.ArtifactKindFile {
		t.Fatalf("Kind = %q, want file", resolution.Identity().Kind())
	}

	if resolution.Identity().SourceID() != artifactpkg.SourceID("local:hooks/protect_env.py?mode=link") {
		t.Fatalf("SourceID = %q", resolution.Identity().SourceID())
	}
}

func TestResolveRejectsOversizedSparseLocalFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "oversized")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(128<<20 + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	resolver, err := NewResolver(root)
	if err != nil {
		t.Fatal(err)
	}

	_, err = resolver.Resolve(context.Background(), sourcetest.Local(t, "oversized", source.LocalSourceModeVendor), noOperationOptions)
	var limitErr *directfile.LimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("Resolve error = %v, want directfile.LimitError", err)
	}
	if limitErr.Limit() != 128<<20 || limitErr.Observed() != 128<<20+1 {
		t.Fatalf(
			"limit error = limit %d observed %d, want limit %d observed %d",
			limitErr.Limit(),
			limitErr.Observed(),
			int64(128<<20),
			int64(128<<20+1),
		)
	}
}

func TestResolveMissingSourcePreservesRequestedPath(t *testing.T) {
	root := t.TempDir()
	resolver, err := NewResolver(root)
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	_, err = resolver.Resolve(context.Background(), sourcetest.Local(t, "skills/missing-review", source.LocalSourceModeVendor), noOperationOptions)
	if err == nil {
		t.Fatal("Resolve returned nil error")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error = %v, want not-exist classification", err)
	}
	if !IsSourceUnavailable(err) {
		t.Fatalf("error = %v, want missing local source classification", err)
	}
	if !strings.Contains(err.Error(), filepath.Join("skills", "missing-review")) {
		t.Fatalf("error = %q, want requested source path", err)
	}
}

func TestIsSourceUnavailableRejectsUnownedNotExistErrors(t *testing.T) {
	err := errors.Join(errors.New("hash child"), os.ErrNotExist)
	if IsSourceUnavailable(err) {
		t.Fatalf("IsSourceUnavailable(%v) = true, want false", err)
	}
}

func TestListSourceRootListsDirectLocalDirectories(t *testing.T) {
	root := t.TempDir()
	writeLocalTestFile(t, root, "skills/beta/SKILL.md", "---\nname: beta\n---\n")
	writeLocalTestFile(t, root, "skills/alpha/SKILL.md", "---\nname: alpha\n---\n")
	writeLocalTestFile(t, root, "skills/README.md", "not a skill directory\n")

	resolver, err := NewResolver(root)
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	listing, err := resolver.ListSourceRoot(context.Background(), sourcetest.Local(t, "skills", source.LocalSourceModeVendor), noOperationOptions)
	if err != nil {
		t.Fatalf("ListSourceRoot returned error: %v", err)
	}
	if listing.Kind() != artifactpkg.ArtifactKindDirectory {
		t.Fatalf("Kind = %q, want directory", listing.Kind())
	}
	if strings.Join(listing.ChildNames(), ",") != "alpha,beta" {
		t.Fatalf("ChildNames = %#v, want alpha,beta", listing.ChildNames())
	}
	if listing.ResolvedRef() != "" {
		t.Fatalf("ResolvedRef = %q, want empty", listing.ResolvedRef())
	}
}

func TestListSourceRootAppliesBudgetToEveryDirectEntry(t *testing.T) {
	root := t.TempDir()
	writeLocalTestFile(t, root, "skills/alpha/SKILL.md", "---\nname: alpha\n---\n")
	writeLocalTestFile(t, root, "skills/beta/SKILL.md", "---\nname: beta\n---\n")
	writeLocalTestFile(t, root, "skills/README.md", "not a directory\n")
	if err := os.Symlink("alpha", filepath.Join(root, "skills", "alias")); err != nil {
		t.Fatalf("create direct-entry symlink: %v", err)
	}
	resolver, err := NewResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	sourceSpec := sourcetest.Local(t, "skills", source.LocalSourceModeVendor)

	exactBudget := source.NewRootListingBudget()
	prefillLocalRootListingEntries(t, exactBudget, 99_996)
	exactOptions, err := noOperationOptions.WithRootListingBudget(exactBudget)
	if err != nil {
		t.Fatal(err)
	}
	listing, err := resolver.ListSourceRoot(context.Background(), sourceSpec, exactOptions)
	if err != nil {
		t.Fatalf("exact-limit ListSourceRoot returned error: %v", err)
	}
	if got := strings.Join(listing.ChildNames(), ","); got != "alpha,beta" {
		t.Fatalf("exact-limit ChildNames = %q, want alpha,beta", got)
	}

	overflowBudget := source.NewRootListingBudget()
	prefillLocalRootListingEntries(t, overflowBudget, 99_997)
	overflowOptions, err := noOperationOptions.WithRootListingBudget(overflowBudget)
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolver.ListSourceRoot(context.Background(), sourceSpec, overflowOptions)
	var limitErr *source.RootListingLimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("overflow error = %v, want RootListingLimitError", err)
	}
}

func prefillLocalRootListingEntries(t *testing.T, budget *source.RootListingBudget, count int) {
	t.Helper()
	for range count {
		if err := budget.AdmitEntryName(1); err != nil {
			t.Fatalf("prefill root listing budget: %v", err)
		}
	}
}

func TestListSourceRootUsesNoFollowRootAndChildSemantics(t *testing.T) {
	root := t.TempDir()
	writeLocalTestFile(t, root, "skills/real/SKILL.md", "---\nname: real\n---\n")
	if err := os.Symlink(filepath.Join(root, "skills", "real"), filepath.Join(root, "skills", "alias")); err != nil {
		t.Fatalf("create child symlink: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "skills"), filepath.Join(root, "linked-skills")); err != nil {
		t.Fatalf("create root symlink: %v", err)
	}
	resolver, err := NewResolver(root)
	if err != nil {
		t.Fatal(err)
	}

	listing, err := resolver.ListSourceRoot(context.Background(), sourcetest.Local(t, "skills", source.LocalSourceModeVendor), noOperationOptions)
	if err != nil {
		t.Fatalf("ListSourceRoot returned error: %v", err)
	}
	if got := strings.Join(listing.ChildNames(), ","); got != "real" {
		t.Fatalf("ChildNames = %q, want only no-follow directory real", got)
	}

	_, err = resolver.ListSourceRoot(context.Background(), sourcetest.Local(t, "linked-skills", source.LocalSourceModeVendor), noOperationOptions)
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("root symlink error = %v, want no-follow rejection", err)
	}
}

func TestListSourceRootClassifiesFileRootWithoutReadingChildren(t *testing.T) {
	root := t.TempDir()
	writeLocalTestFile(t, root, "SKILL.md", "---\nname: root\n---\n")
	resolver, err := NewResolver(root)
	if err != nil {
		t.Fatal(err)
	}

	listing, err := resolver.ListSourceRoot(context.Background(), sourcetest.Local(t, "SKILL.md", source.LocalSourceModeVendor), noOperationOptions)
	if err != nil {
		t.Fatalf("ListSourceRoot returned error: %v", err)
	}
	if listing.Kind() != artifactpkg.ArtifactKindFile || len(listing.ChildNames()) != 0 {
		t.Fatalf("listing = %#v, want childless file root", listing)
	}
}

func TestListSourceRootRejectsNilAndCanceledContexts(t *testing.T) {
	root := t.TempDir()
	writeLocalTestFile(t, root, "skills/demo/SKILL.md", "---\nname: demo\n---\n")
	resolver, err := NewResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	sourceSpec := sourcetest.Local(t, "skills", source.LocalSourceModeVendor)

	if _, err := resolver.ListSourceRoot(nil, sourceSpec, noOperationOptions); err == nil || !strings.Contains(err.Error(), "context is required") {
		t.Fatalf("nil context error = %v, want explicit rejection", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := resolver.ListSourceRoot(ctx, sourceSpec, noOperationOptions); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context error = %v, want context.Canceled", err)
	}
}

func TestResolveRejectsGitSource(t *testing.T) {
	resolver, err := NewResolver(t.TempDir())
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	gitSource, err := source.NewGitSource("https://example.com/repo.git", "skills/demo", "main")
	if err != nil {
		t.Fatalf("NewGitSource returned error: %v", err)
	}

	_, err = resolver.Resolve(context.Background(), gitSource, noOperationOptions)
	if err == nil {
		t.Fatal("Resolve returned nil error")
	}

	if !strings.Contains(err.Error(), "only supports local sources") {
		t.Fatalf("error = %q, want local-only diagnostic", err)
	}
}

func TestResolveHonorsContextCancellation(t *testing.T) {
	resolver, err := NewResolver(t.TempDir())
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = resolver.Resolve(ctx, sourcetest.Local(t, "skills/demo", source.LocalSourceModeVendor), noOperationOptions)
	if err == nil {
		t.Fatal("Resolve returned nil error")
	}

	if err != context.Canceled {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func writeLocalTestFile(t *testing.T, root string, relativePath string, content string) {
	t.Helper()

	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}
