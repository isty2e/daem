package operationplan

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
)

func TestCompileInitPreservesDomainAndRevisionOrder(t *testing.T) {
	t.Parallel()

	barrierDomain, err := mutation.NewHostRouteDomain(mutation.HostRouteRequest{
		Target: "codex", Scope: "project", Family: "barrier-test",
		Containment: mutation.RouteContainmentUnknown,
	})
	if err != nil {
		t.Fatal(err)
	}
	barrierRevision := mutation.NewBoundedContentRevisionRequest(
		"/barrier",
		mutation.PathEffectReferent,
	)
	program := CompileInit(InitInput{
		ManifestPath:            "/manifest",
		ManifestMaximumBytes:    4096,
		MetadataTransactionPath: "/metadata",
		BarrierDomains:          []mutation.Domain{barrierDomain},
		BarrierRevisions:        []mutation.RevisionRequest{barrierRevision},
	})

	steps := program.DomainSteps()
	if len(steps) != 4 {
		t.Fatalf("domain steps = %d, want 4", len(steps))
	}
	assertInitLogicalDomainStep(
		t,
		steps[0],
		"/manifest",
		mutation.AccessExclusive,
		mutation.PathEffectDirectoryEntry,
	)
	assertInitLogicalDomainStep(
		t,
		steps[1],
		"/manifest",
		mutation.AccessShared,
		mutation.PathEffectReferent,
	)
	assertInitLogicalDomainStep(
		t,
		steps[2],
		"/metadata",
		mutation.AccessExclusive,
		mutation.PathEffectDirectoryEntry,
	)
	if _, ok := steps[3].Compiled(); !ok {
		t.Fatal("barrier domain is not the final owner-compiled domain step")
	}

	revisions, err := program.RevisionRequests()
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 4 {
		t.Fatalf("revision requests = %d, want 4", len(revisions))
	}
	for index, effect := range []mutation.PathEffect{
		mutation.PathEffectDirectoryEntry,
		mutation.PathEffectReferent,
	} {
		expected, requestErr := mutation.NewBoundedFileRevisionRequest(
			4096,
			"/manifest",
			effect,
		)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		if !revisions[index].Equal(expected) {
			t.Fatalf("revision[%d] = %#v, want %#v", index, revisions[index], expected)
		}
	}
	expectedMetadata := mutation.NewBoundedContentRevisionRequest(
		"/metadata",
		mutation.PathEffectDirectoryEntry,
	)
	if !revisions[2].Equal(expectedMetadata) {
		t.Fatalf("metadata revision = %#v, want %#v", revisions[2], expectedMetadata)
	}
	if !revisions[3].Equal(barrierRevision) {
		t.Fatalf("barrier revision = %#v, want %#v", revisions[3], barrierRevision)
	}
}

func TestInitProgramReturnsDefensiveCopiesAndValidatesRevisionLimitLazily(t *testing.T) {
	t.Parallel()

	program := CompileInit(InitInput{
		ManifestPath:            "/manifest",
		ManifestMaximumBytes:    0,
		MetadataTransactionPath: "/metadata",
	})
	steps := program.DomainSteps()
	steps[0] = DomainStep{}
	if path, ok := program.DomainSteps()[0].Path(); !ok {
		t.Fatal("domain step mutation escaped program ownership")
	} else if logical, logicalPath := path.Logical(); !logicalPath || logical.Path != "/manifest" {
		t.Fatalf("first domain path = %#v", path)
	}
	if _, err := program.RevisionRequests(); err == nil {
		t.Fatal("RevisionRequests accepted a non-positive manifest byte limit")
	}
}

func assertInitLogicalDomainStep(
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
