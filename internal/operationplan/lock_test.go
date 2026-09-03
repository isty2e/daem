package operationplan

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
)

func TestCompileLockPreservesDomainAndRevisionOrder(t *testing.T) {
	t.Parallel()

	program := CompileLock(LockInput{
		ManifestPath:            "/manifest",
		LockfilePath:            "/lockfile",
		MetadataTransactionPath: "/metadata",
		LocalPaths:              []string{"/source-b", "/source-a"},
		StateDirPath:            "/state",
		StateDirPresent:         true,
		DocumentMaximumBytes:    4096,
	})
	steps := program.DomainSteps()
	want := []struct {
		path   string
		access mutation.AccessMode
		effect mutation.PathEffect
	}{
		{path: "/manifest", access: mutation.AccessShared, effect: mutation.PathEffectDirectoryEntry},
		{path: "/manifest", access: mutation.AccessShared, effect: mutation.PathEffectReferent},
		{path: "/lockfile", access: mutation.AccessExclusive, effect: mutation.PathEffectDirectoryEntry},
		{path: "/lockfile", access: mutation.AccessShared, effect: mutation.PathEffectReferent},
		{path: "/metadata", access: mutation.AccessExclusive, effect: mutation.PathEffectDirectoryEntry},
		{path: "/source-b", access: mutation.AccessShared, effect: mutation.PathEffectReferent},
		{path: "/source-a", access: mutation.AccessShared, effect: mutation.PathEffectReferent},
		{path: "/state", access: mutation.AccessShared, effect: mutation.PathEffectDirectoryEntry},
		{path: "/state", access: mutation.AccessShared, effect: mutation.PathEffectReferent},
	}
	if len(steps) != len(want) {
		t.Fatalf("domain steps = %d, want %d", len(steps), len(want))
	}
	for index, expected := range want {
		assertLockLogicalDomainStep(
			t,
			steps[index],
			expected.path,
			expected.access,
			expected.effect,
		)
	}

	revisions, err := program.RevisionRequests()
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 7 {
		t.Fatalf("revision requests = %d, want 7", len(revisions))
	}
	index := 0
	for _, path := range []string{"/manifest", "/lockfile"} {
		for _, effect := range []mutation.PathEffect{
			mutation.PathEffectDirectoryEntry,
			mutation.PathEffectReferent,
		} {
			expected, requestErr := mutation.NewBoundedFileRevisionRequest(
				4096,
				path,
				effect,
			)
			if requestErr != nil {
				t.Fatal(requestErr)
			}
			if !revisions[index].Equal(expected) {
				t.Fatalf("revision[%d] = %#v, want %#v", index, revisions[index], expected)
			}
			index++
		}
	}
	for _, expected := range []mutation.RevisionRequest{
		mutation.NewBoundedContentRevisionRequest("/metadata", mutation.PathEffectDirectoryEntry),
		mutation.NewBoundedContentRevisionRequest("/source-b", mutation.PathEffectReferent),
		mutation.NewBoundedContentRevisionRequest("/source-a", mutation.PathEffectReferent),
	} {
		if !revisions[index].Equal(expected) {
			t.Fatalf("revision[%d] = %#v, want %#v", index, revisions[index], expected)
		}
		index++
	}
}

func TestCompileLockUsesExclusiveStateDirDomainsWhenAbsent(t *testing.T) {
	t.Parallel()

	program := CompileLock(LockInput{
		ManifestPath:            "/manifest",
		LockfilePath:            "/lockfile",
		MetadataTransactionPath: "/metadata",
		StateDirPath:            "/state",
		StateDirPresent:         false,
		DocumentMaximumBytes:    4096,
	})
	steps := program.DomainSteps()
	for _, index := range []int{len(steps) - 2, len(steps) - 1} {
		request, ok := steps[index].Path()
		if !ok {
			t.Fatalf("state dir step[%d] is not a path request", index)
		}
		logical, ok := request.Logical()
		if !ok || logical.Access != mutation.AccessExclusive {
			t.Fatalf("state dir step[%d] = %#v, want exclusive logical", index, request)
		}
	}
}

func TestLockProgramReturnsDefensiveCopiesAndValidatesRevisionLimitLazily(t *testing.T) {
	t.Parallel()

	inputPaths := []string{"/source"}
	program := CompileLock(LockInput{
		ManifestPath:            "/manifest",
		LockfilePath:            "/lockfile",
		MetadataTransactionPath: "/metadata",
		LocalPaths:              inputPaths,
		StateDirPath:            "/state",
		DocumentMaximumBytes:    0,
	})
	inputPaths[0] = "/changed"
	steps := program.DomainSteps()
	steps[0] = DomainStep{}
	assertLockLogicalDomainStep(
		t,
		program.DomainSteps()[0],
		"/manifest",
		mutation.AccessShared,
		mutation.PathEffectDirectoryEntry,
	)
	if _, err := program.RevisionRequests(); err == nil {
		t.Fatal("RevisionRequests accepted a non-positive document byte limit")
	}
}

func assertLockLogicalDomainStep(
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
	if !ok {
		t.Fatalf("domain request = %#v, want logical", request)
	}
	if logical.Path != path || logical.Access != access || logical.Effect != effect {
		t.Fatalf(
			"logical domain = %#v, want path=%q access=%d effect=%d",
			logical,
			path,
			access,
			effect,
		)
	}
}
