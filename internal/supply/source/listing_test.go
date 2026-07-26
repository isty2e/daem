package source

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
)

func TestRootListingOwnsCanonicalOrderingAndDefensiveFacts(t *testing.T) {
	root := mustListingGitSource(t, "skills", "main")
	resolvedRef := artifact.ResolvedRef(strings.Repeat("a", 40))
	input := []string{"zeta", "alpha", "skill with space"}

	listing, err := NewRootListing(root, resolvedRef, artifact.ArtifactKindDirectory, input)
	if err != nil {
		t.Fatalf("NewRootListing returned error: %v", err)
	}
	input[0] = "mutated"

	want := []string{"alpha", "skill with space", "zeta"}
	got := listing.ChildNames()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("ChildNames() = %v, want %v", got, want)
	}
	got[0] = "mutated"
	if listing.ChildNames()[0] != "alpha" {
		t.Fatal("ChildNames returned mutable listing storage")
	}
	if listing.SourceID() == "" || listing.ResolvedRef() != resolvedRef || !listing.IsDirectory() {
		t.Fatalf("listing facts = (%q, %q, %q)", listing.SourceID(), listing.ResolvedRef(), listing.Kind())
	}
	if err := listing.ValidateFor(root); err != nil {
		t.Fatalf("ValidateFor returned error: %v", err)
	}
}

func TestRootListingRejectsMalformedOrDuplicateChildFacts(t *testing.T) {
	root := mustListingGitSource(t, "skills", "main")
	resolvedRef := artifact.ResolvedRef(strings.Repeat("a", 40))
	invalid := []string{
		"", ".", "..", "../escape", "child/name", `child\name`,
		" padded", "padded ", "control\x00name", "format\u200bname",
		"line\u2028separator", "paragraph\u2029separator", string([]byte{0xff}),
	}
	for _, childName := range invalid {
		t.Run(childName, func(t *testing.T) {
			if _, err := NewRootListing(root, resolvedRef, artifact.ArtifactKindDirectory, []string{childName}); err == nil {
				t.Fatalf("NewRootListing accepted child %q", childName)
			}
		})
	}

	if _, err := NewRootListing(root, resolvedRef, artifact.ArtifactKindDirectory, []string{"same", "same"}); err == nil {
		t.Fatal("NewRootListing accepted duplicate child facts")
	}
	if _, err := NewRootListing(root, resolvedRef, artifact.ArtifactKindFile, []string{"child"}); err == nil {
		t.Fatal("NewRootListing accepted children for file root")
	}
	if _, err := NewRootListing(root, resolvedRef, artifact.ArtifactKind("socket"), nil); err == nil {
		t.Fatal("NewRootListing accepted unsupported root kind")
	}
	if _, err := NewRootListing(Source{}, "", artifact.ArtifactKindDirectory, nil); err == nil {
		t.Fatal("NewRootListing accepted a zero source")
	}
}

func TestRootListingEnforcesSourceSpecificResolvedRefCorrelation(t *testing.T) {
	gitCommit := strings.Repeat("a", 40)
	immutableRoot := mustListingGitSource(t, "skills", gitCommit)
	if _, err := NewRootListing(immutableRoot, artifact.ResolvedRef(strings.Repeat("b", 40)), artifact.ArtifactKindDirectory, nil); err == nil {
		t.Fatal("NewRootListing accepted a different commit for immutable Git source")
	}

	localRoot := mustLocalSource(t, "skills", LocalSourceModeVendor)
	if _, err := NewRootListing(localRoot, artifact.ResolvedRef(gitCommit), artifact.ArtifactKindDirectory, nil); err == nil {
		t.Fatal("NewRootListing accepted a resolved ref for local source")
	}

	s3Root := mustS3Source(t, "s3://bucket/archive.tar", "version-a", "", S3ObjectFormatTar)
	if _, err := NewRootListing(s3Root, artifact.ResolvedRef("version-b"), artifact.ArtifactKindDirectory, nil); err == nil {
		t.Fatal("NewRootListing accepted a different explicit S3 version")
	}
}

func TestRootListingRejectsUseWithAnotherSource(t *testing.T) {
	root := mustListingGitSource(t, "skills", "main")
	listing, err := NewRootListing(root, artifact.ResolvedRef(strings.Repeat("a", 40)), artifact.ArtifactKindDirectory, []string{"review"})
	if err != nil {
		t.Fatalf("NewRootListing returned error: %v", err)
	}
	other := mustListingGitSource(t, "other", "main")
	if err := listing.ValidateFor(other); err == nil {
		t.Fatal("ValidateFor accepted a listing from another source")
	}
}

func TestRootListingDerivesPinnedGitChildAndDeclarationRelativeChild(t *testing.T) {
	root := mustListingGitSource(t, "skills", "main")
	resolvedRef := artifact.ResolvedRef(strings.Repeat("a", 40))
	listing, err := NewRootListing(root, resolvedRef, artifact.ArtifactKindDirectory, []string{"review"})
	if err != nil {
		t.Fatalf("NewRootListing returned error: %v", err)
	}

	resolvedChild, err := root.ResolvedChild("review", listing.ResolvedRef())
	if err != nil {
		t.Fatalf("ResolvedChild returned error: %v", err)
	}
	gitResolvedChild, ok := resolvedChild.Git()
	if !ok || gitResolvedChild.RepositoryPath().String() != "skills/review" || gitResolvedChild.Ref().String() != string(resolvedRef) {
		t.Fatalf("resolved child = %#v", resolvedChild)
	}

	declaredChild, err := root.Child("review")
	if err != nil {
		t.Fatalf("Child returned error: %v", err)
	}
	gitDeclaredChild, ok := declaredChild.Git()
	if !ok || gitDeclaredChild.Ref().String() != "main" {
		t.Fatalf("declaration-relative child = %#v", declaredChild)
	}
	if err := root.ValidateChild("review", resolvedChild); err != nil {
		t.Fatalf("ValidateChild rejected resolved child: %v", err)
	}
	if err := root.ValidateChild("review", declaredChild); err != nil {
		t.Fatalf("ValidateChild rejected declaration-relative child: %v", err)
	}
}

func TestSourceValidateChildRejectsCrossRootAndMutableGitSubstitution(t *testing.T) {
	root := mustListingGitSource(t, "skills", "main")
	otherRoot := mustListingGitSource(t, "other", "main")
	crossRootChild, err := otherRoot.Child("review")
	if err != nil {
		t.Fatalf("otherRoot.Child returned error: %v", err)
	}
	if err := root.ValidateChild("review", crossRootChild); err == nil {
		t.Fatal("ValidateChild accepted a child from another root")
	}

	mutableChild := mustListingGitSource(t, "skills/review", "other-branch")
	if err := root.ValidateChild("review", mutableChild); err == nil {
		t.Fatal("ValidateChild accepted a different mutable Git ref")
	}
	otherLocator, err := NewGitSource(
		"https://github.com/other/skills.git",
		"skills/review",
		strings.Repeat("a", 40),
	)
	if err != nil {
		t.Fatalf("NewGitSource returned error: %v", err)
	}
	if err := root.ValidateChild("review", otherLocator); err == nil {
		t.Fatal("ValidateChild accepted an immutable child from another locator")
	}

	localRoot := mustLocalSource(t, "skills", LocalSourceModeVendor)
	wrongMode := mustLocalSource(t, "skills/review", LocalSourceModeLink)
	if err := localRoot.ValidateChild("review", wrongMode); err == nil {
		t.Fatal("ValidateChild accepted a local child with a different source mode")
	}
}

func TestRootListingKeepsDistinctUnicodeComponentsByteExact(t *testing.T) {
	root := mustLocalSource(t, "skills", LocalSourceModeVendor)
	listing, err := NewRootListing(
		root,
		"",
		artifact.ArtifactKindDirectory,
		[]string{"\u00e9", "e\u0301"},
	)
	if err != nil {
		t.Fatalf("NewRootListing returned error: %v", err)
	}
	if len(listing.ChildNames()) != 2 {
		t.Fatalf("ChildNames = %#v, want two byte-distinct source facts", listing.ChildNames())
	}
}

func TestRootListingAllowsFileFactWithoutChildren(t *testing.T) {
	root := mustLocalSource(t, "skill.md", LocalSourceModeVendor)
	listing, err := NewRootListing(root, "", artifact.ArtifactKindFile, nil)
	if err != nil {
		t.Fatalf("NewRootListing returned error: %v", err)
	}
	if listing.IsDirectory() || listing.Kind() != artifact.ArtifactKindFile || len(listing.ChildNames()) != 0 {
		t.Fatalf("file listing = %#v", listing)
	}
}

func mustListingGitSource(t *testing.T, repositoryPath string, ref string) Source {
	t.Helper()
	value, err := NewGitSource("https://github.com/example/skills.git", repositoryPath, ref)
	if err != nil {
		t.Fatalf("NewGitSource returned error: %v", err)
	}
	return value
}
