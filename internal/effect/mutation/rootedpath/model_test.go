package rootedpath

import (
	"errors"
	"path/filepath"
	"testing"
)

func hasFailureKind(err error, kind FailureKind) bool {
	var failure *Failure
	return errors.As(err, &failure) && failure.Kind() == kind
}

func TestAuthorityAdapterIngressRejectsInvalidOrContradictoryFacts(t *testing.T) {
	validObject := testIdentityToken(1)
	validMount := testIdentityToken(2)
	root := filepath.Join(t.TempDir(), "project")
	tests := []struct {
		name   string
		root   string
		object identityToken
		mount  identityToken
	}{
		{name: "missing root", root: "", object: validObject, mount: validMount},
		{name: "relative root", root: "project", object: validObject, mount: validMount},
		{name: "unclean root", root: root + string(filepath.Separator) + ".." + string(filepath.Separator) + "project", object: validObject, mount: validMount},
		{name: "filesystem root", root: string(filepath.Separator), object: validObject, mount: validMount},
		{name: "NUL character", root: root + "\x00other", object: validObject, mount: validMount},
		{name: "missing object identity", root: root, mount: validMount},
		{name: "missing mount identity", root: root, object: validObject},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := newCapturedAuthority(
				test.root,
				test.object,
				newMountIdentities(test.mount, availableRecoveryMountEvidence(test.mount)),
			)
			if !hasFailureKind(err, FailureInvalidRoot) {
				t.Fatalf("newCapturedAuthority error = %v, want %s", err, FailureInvalidRoot)
			}
		})
	}
}

func TestAuthorityIdentityIncludesPhysicalRootObjectAndMount(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project with spaces")
	object := testIdentityToken(1)
	mount := testIdentityToken(2)
	authority := mustAuthority(t, root, object, mount)

	if authority.PhysicalRoot() != root {
		t.Fatalf("PhysicalRoot = %q, want %q", authority.PhysicalRoot(), root)
	}
	if !authority.Equal(mustAuthority(t, root, object, mount)) {
		t.Fatal("identical authority facts did not compare equal")
	}
	if authority.Equal(mustAuthority(t, filepath.Join(filepath.Dir(root), "other"), object, mount)) {
		t.Fatal("different physical roots compared equal")
	}
	if authority.Equal(mustAuthority(t, root, testIdentityToken(3), mount)) {
		t.Fatal("different root objects compared equal")
	}
	if authority.Equal(mustAuthority(t, root, object, testIdentityToken(4))) {
		t.Fatal("different mount identities compared equal")
	}
	if authority.Equal(Authority{}) {
		t.Fatal("valid authority compared equal to zero authority")
	}
}

func TestAuthorityIdentityIgnoresRecoveryEvidenceAvailability(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	object := testIdentityToken(1)
	operationMount := testIdentityToken(2)
	withRecovery := mustAuthorityWithMountIdentities(
		t,
		root,
		object,
		operationMount,
		testIdentityToken(3),
	)
	withoutRecovery := mustAuthorityWithMountIdentities(
		t,
		root,
		object,
		operationMount,
		identityToken{},
	)
	if !withRecovery.Equal(withoutRecovery) {
		t.Fatal("recovery evidence availability changed operation-local authority identity")
	}
}

func TestNewRelativeDestinationNormalizesOnlyContainedPaths(t *testing.T) {
	valid := []struct {
		input string
		want  string
	}{
		{input: ".agents/skills/review", want: ".agents/skills/review"},
		{input: ".agents//skills/./review/", want: ".agents/skills/review"},
		{input: "directory with spaces/skill", want: "directory with spaces/skill"},
	}
	for _, test := range valid {
		relative, err := NewRelativeDestination(test.input)
		if err != nil {
			t.Fatalf("NewRelativeDestination(%q) returned error: %v", test.input, err)
		}
		if relative.Path() != test.want {
			t.Fatalf("NewRelativeDestination(%q) = %q, want %q", test.input, relative.Path(), test.want)
		}
	}

	invalid := []string{
		"",
		"   ",
		".",
		"..",
		"../outside",
		"inside/../outside",
		"/absolute",
		"C:/absolute",
		"z:drive-relative",
		"~",
		"~/global",
		`windows\separator`,
		"line\nbreak",
		"nul\x00byte",
	}
	for _, input := range invalid {
		_, err := NewRelativeDestination(input)
		if !hasFailureKind(err, FailureInvalidDestination) {
			t.Fatalf("NewRelativeDestination(%q) error = %v, want %s", input, err, FailureInvalidDestination)
		}
	}
}

func TestDestinationBindsRelativePathToExactlyOneRootAuthority(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	authority := mustAuthority(t, root, testIdentityToken(1), testIdentityToken(2))
	relative, err := NewRelativeDestination(".agents/skills/review")
	if err != nil {
		t.Fatalf("NewRelativeDestination returned error: %v", err)
	}
	destination, err := authority.Bind(relative)
	if err != nil {
		t.Fatalf("Bind returned error: %v", err)
	}

	wantPath := filepath.Join(root, ".agents", "skills", "review")
	gotPath, err := destination.LexicalPath()
	if err != nil {
		t.Fatalf("LexicalPath returned error: %v", err)
	}
	if gotPath != wantPath {
		t.Fatalf("LexicalPath = %q, want %q", gotPath, wantPath)
	}
	if !destination.Root().Equal(authority) || !destination.Relative().Equal(relative) {
		t.Fatalf("destination facts = %#v, want bound authority and relative path", destination)
	}
	rootDepth, err := absolutePathDepth(authority.PhysicalRoot())
	if err != nil {
		t.Fatal(err)
	}
	wantValidationWork := 2*rootDepth + 2
	if work, err := destination.ParentChainValidationWork(); err != nil || work != wantValidationWork {
		t.Fatalf("parent-chain validation work = %d, err=%v, want %d", work, err, wantValidationWork)
	}
	if !destination.Equal(destination) {
		t.Fatal("destination did not compare equal to itself")
	}
	if destination.Equal(Destination{}) {
		t.Fatal("valid destination compared equal to zero destination")
	}
	if _, err := (Authority{}).Bind(relative); !hasFailureKind(err, FailureInvalidRoot) {
		t.Fatalf("zero authority Bind error = %v, want %s", err, FailureInvalidRoot)
	}
	if _, err := authority.Bind(RelativeDestination{}); !hasFailureKind(err, FailureInvalidDestination) {
		t.Fatalf("zero relative Bind error = %v, want %s", err, FailureInvalidDestination)
	}
	if _, err := (Destination{root: authority}).LexicalPath(); !hasFailureKind(err, FailureInvalidDestination) {
		t.Fatalf("contradictory destination error = %v, want %s", err, FailureInvalidDestination)
	}
	if _, err := (Destination{root: authority}).ParentChainValidationWork(); !hasFailureKind(err, FailureInvalidDestination) {
		t.Fatalf("contradictory parent-chain work error = %v, want %s", err, FailureInvalidDestination)
	}
}

func TestFailureRetainsKindPathAndCause(t *testing.T) {
	cause := errors.New("native failure")
	err := newFailure(FailureAncestorChanged, "/project/.agents", "changed while opening", cause)

	if err.Kind() != FailureAncestorChanged || err.Path() != "/project/.agents" {
		t.Fatalf("failure = kind %q path %q", err.Kind(), err.Path())
	}
	if !errors.Is(err, cause) || !hasFailureKind(err, FailureAncestorChanged) {
		t.Fatalf("failure did not retain typed cause: %v", err)
	}
	if hasFailureKind(err, FailureRootReplaced) {
		t.Fatal("failure matched an unrelated kind")
	}
}

func TestAuthorityFailureKindsAreStableAndDistinct(t *testing.T) {
	kinds := []FailureKind{
		FailureInvalidRoot,
		FailureInvalidDestination,
		FailureRootUnavailable,
		FailureRootReplaced,
		FailureMountChanged,
		FailureAncestorSymlink,
		FailureDanglingAncestorSymlink,
		FailureFinalSymlink,
		FailureAncestorNotDirectory,
		FailureAncestorChanged,
		FailureIndeterminateBinding,
		FailureUnsupportedPlatform,
	}
	seen := make(map[FailureKind]struct{}, len(kinds))
	for _, kind := range kinds {
		if kind == "" {
			t.Fatal("authority failure kind is empty")
		}
		if _, duplicate := seen[kind]; duplicate {
			t.Fatalf("duplicate authority failure kind %q", kind)
		}
		seen[kind] = struct{}{}
	}
}

func testIdentityToken(value byte) identityToken {
	var token identityToken
	token[0] = value
	return token
}
