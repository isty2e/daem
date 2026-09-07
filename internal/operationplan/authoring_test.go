package operationplan

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
)

func TestCompileMetadataDomainsPreservesTargetMarkerLocalAndTrailingOrder(t *testing.T) {
	t.Parallel()

	trailing, err := mutation.NewHostRouteDomain(mutation.HostRouteRequest{
		Target: "codex", Scope: "project", Family: "barrier-test",
		Containment: mutation.RouteContainmentUnknown,
	})
	if err != nil {
		t.Fatal(err)
	}
	steps := CompileMetadataDomains(MetadataDomainInput{
		TargetPaths:     []string{"/target-b", "/target-a"},
		MarkerPath:      "/marker",
		LocalPaths:      []string{"/source-b", "/source-a"},
		TrailingDomains: []mutation.Domain{trailing},
	})
	want := []struct {
		path   string
		access mutation.AccessMode
		effect mutation.PathEffect
	}{
		{path: "/target-b", access: mutation.AccessExclusive, effect: mutation.PathEffectDirectoryEntry},
		{path: "/target-b", access: mutation.AccessShared, effect: mutation.PathEffectReferent},
		{path: "/target-a", access: mutation.AccessExclusive, effect: mutation.PathEffectDirectoryEntry},
		{path: "/target-a", access: mutation.AccessShared, effect: mutation.PathEffectReferent},
		{path: "/marker", access: mutation.AccessExclusive, effect: mutation.PathEffectDirectoryEntry},
		{path: "/source-b", access: mutation.AccessShared, effect: mutation.PathEffectReferent},
		{path: "/source-a", access: mutation.AccessShared, effect: mutation.PathEffectReferent},
	}
	if len(steps) != len(want)+1 {
		t.Fatalf("domain steps = %d, want %d", len(steps), len(want)+1)
	}
	for index, expected := range want {
		assertAuthoringLogicalDomainStep(
			t,
			steps[index],
			expected.path,
			expected.access,
			expected.effect,
		)
	}
	if _, ok := steps[len(steps)-1].Compiled(); !ok {
		t.Fatal("trailing owner domain is not the final compiled step")
	}
}

func TestCompileAuthoringPreservesRevisionOrder(t *testing.T) {
	t.Parallel()

	barrier := mutation.NewBoundedContentRevisionRequest(
		"/barrier",
		mutation.PathEffectReferent,
	)
	program := CompileAuthoring(AuthoringInput{
		ManifestPath:         "/manifest",
		LockfilePath:         "/lockfile",
		MarkerPath:           "/marker",
		LocalPaths:           []string{"/source-b", "/source-a"},
		BarrierRevisions:     []mutation.RevisionRequest{barrier},
		DocumentMaximumBytes: 4096,
	})
	revisions, err := program.RevisionRequests()
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 8 {
		t.Fatalf("revision requests = %d, want 8", len(revisions))
	}
	index := assertDocumentRevisionPairs(t, revisions, 0, 4096, []string{"/manifest", "/lockfile"})
	for _, expected := range []mutation.RevisionRequest{
		mutation.NewBoundedContentRevisionRequest("/marker", mutation.PathEffectDirectoryEntry),
		mutation.NewBoundedContentRevisionRequest("/source-b", mutation.PathEffectReferent),
		mutation.NewBoundedContentRevisionRequest("/source-a", mutation.PathEffectReferent),
		barrier,
	} {
		if !revisions[index].Equal(expected) {
			t.Fatalf("revision[%d] = %#v, want %#v", index, revisions[index], expected)
		}
		index++
	}
}

func TestCompileUnmanagePreservesDeclarationPersistenceAndRevisionOrder(t *testing.T) {
	t.Parallel()

	barrier := mutation.NewBoundedContentRevisionRequest(
		"/barrier",
		mutation.PathEffectDirectoryEntry,
	)
	program := CompileUnmanage(UnmanageInput{
		DeclarationPaths:     []string{"/manifest", "/lockfile"},
		PersistencePaths:     []string{"/state", "/registry"},
		MarkerPath:           "/marker",
		LocalPaths:           []string{"/source"},
		BarrierRevisions:     []mutation.RevisionRequest{barrier},
		DocumentMaximumBytes: 4096,
	})
	steps := program.DomainSteps()
	wantPaths := []string{
		"/manifest", "/manifest",
		"/lockfile", "/lockfile",
		"/state", "/state",
		"/registry", "/registry",
		"/marker", "/source",
	}
	if len(steps) != len(wantPaths) {
		t.Fatalf("domain steps = %d, want %d", len(steps), len(wantPaths))
	}
	for index, path := range wantPaths {
		request, ok := steps[index].Path()
		if !ok {
			t.Fatalf("domain step[%d] is not a path request", index)
		}
		logical, ok := request.Logical()
		if !ok || logical.Path != path {
			t.Fatalf("domain step[%d] = %#v, want path %q", index, request, path)
		}
	}

	revisions, err := program.RevisionRequests()
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 11 {
		t.Fatalf("revision requests = %d, want 11", len(revisions))
	}
	index := assertDocumentRevisionPairs(t, revisions, 0, 4096, []string{"/manifest", "/lockfile"})
	for _, path := range []string{"/state", "/registry"} {
		for _, effect := range []mutation.PathEffect{
			mutation.PathEffectDirectoryEntry,
			mutation.PathEffectReferent,
		} {
			expected := mutation.NewBoundedContentRevisionRequest(path, effect)
			if !revisions[index].Equal(expected) {
				t.Fatalf("revision[%d] = %#v, want %#v", index, revisions[index], expected)
			}
			index++
		}
	}
	for _, expected := range []mutation.RevisionRequest{
		mutation.NewBoundedContentRevisionRequest("/marker", mutation.PathEffectDirectoryEntry),
		mutation.NewBoundedContentRevisionRequest("/source", mutation.PathEffectReferent),
		barrier,
	} {
		if !revisions[index].Equal(expected) {
			t.Fatalf("revision[%d] = %#v, want %#v", index, revisions[index], expected)
		}
		index++
	}
}

func TestAuthoringAndUnmanageProgramsOwnInputSlices(t *testing.T) {
	t.Parallel()

	localPaths := []string{"/source"}
	authoring := CompileAuthoring(AuthoringInput{
		ManifestPath:         "/manifest",
		LockfilePath:         "/lockfile",
		MarkerPath:           "/marker",
		LocalPaths:           localPaths,
		DocumentMaximumBytes: 4096,
	})
	declarations := []string{"/manifest", "/lockfile"}
	persistence := []string{"/state", "/registry"}
	unmanage := CompileUnmanage(UnmanageInput{
		DeclarationPaths:     declarations,
		PersistencePaths:     persistence,
		MarkerPath:           "/marker",
		LocalPaths:           localPaths,
		DocumentMaximumBytes: 4096,
	})
	localPaths[0] = "/changed"
	declarations[0] = "/changed"
	persistence[0] = "/changed"

	authoringRevisions, err := authoring.RevisionRequests()
	if err != nil {
		t.Fatal(err)
	}
	unmanageRevisions, err := unmanage.RevisionRequests()
	if err != nil {
		t.Fatal(err)
	}
	if !containsExactAdoptRevision(
		authoringRevisions,
		mutation.NewBoundedContentRevisionRequest("/source", mutation.PathEffectReferent),
	) {
		t.Fatalf("authoring local path changed through input alias: %#v", authoringRevisions)
	}
	if !containsExactAdoptRevision(
		unmanageRevisions,
		mutation.NewBoundedContentRevisionRequest("/state", mutation.PathEffectDirectoryEntry),
	) {
		t.Fatalf("unmanage persistence path changed through input alias: %#v", unmanageRevisions)
	}
}

func assertAuthoringLogicalDomainStep(
	t *testing.T,
	step DomainStep,
	path string,
	access mutation.AccessMode,
	effect mutation.PathEffect,
) {
	t.Helper()
	request, ok := step.Path()
	if !ok {
		t.Fatalf("domain step = %#v, want path request", step)
	}
	logical, ok := request.Logical()
	if !ok || logical.Path != path || logical.Access != access || logical.Effect != effect {
		t.Fatalf(
			"logical domain = %#v, want path=%q access=%d effect=%d",
			logical,
			path,
			access,
			effect,
		)
	}
}

func assertDocumentRevisionPairs(
	t *testing.T,
	revisions []mutation.RevisionRequest,
	start int,
	maximumBytes int64,
	paths []string,
) int {
	t.Helper()
	index := start
	for _, path := range paths {
		for _, effect := range []mutation.PathEffect{
			mutation.PathEffectDirectoryEntry,
			mutation.PathEffectReferent,
		} {
			expected, err := mutation.NewBoundedFileRevisionRequest(maximumBytes, path, effect)
			if err != nil {
				t.Fatal(err)
			}
			if !revisions[index].Equal(expected) {
				t.Fatalf("revision[%d] = %#v, want %#v", index, revisions[index], expected)
			}
			index++
		}
	}
	return index
}
