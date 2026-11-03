package operationplan

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
)

func TestOwnershipRegistryUsesSemanticIdentityWithoutDroppingLeaseOrOtherRevisions(t *testing.T) {
	compile := func(identity string) Plan {
		t.Helper()
		builder := NewBuilder(RevisionsFirstEffect, nil, 0)
		if err := builder.AddOwnershipRegistry("/registry", mutation.AccessExclusive, identity); err != nil {
			t.Fatal(err)
		}
		if err := builder.AddLogical("/state", mutation.AccessExclusive, mutation.PathEffectDirectoryEntry); err != nil {
			t.Fatal(err)
		}
		return builder.Compile()
	}
	before, after := compile("before"), compile("after")
	if revisions := before.Revisions(); len(revisions) != 1 || revisions[0].Path != "/state" {
		t.Fatalf("scoped Registry changed unrelated revision policy: %#v", revisions)
	}
	if len(before.DomainSteps()) != 3 {
		t.Fatal("entry/referent Registry domains were lost")
	}
	for _, step := range before.DomainSteps()[:2] {
		path, _ := step.Path()
		logical, ok := path.Logical()
		if !ok || logical.Path != "/registry" || logical.Access != mutation.AccessExclusive {
			t.Fatal("Registry no longer has its metadata-store lease")
		}
	}
	left, err := ApplyAuthorityFingerprint(before, nil, "barrier")
	if err != nil {
		t.Fatal(err)
	}
	right, err := ApplyAuthorityFingerprint(after, nil, "barrier")
	if err != nil || left.Equal(right) {
		t.Fatal("changed scoped facts did not invalidate approval")
	}
	logical := NewBuilder(RevisionsFirstEffect, nil, 0)
	if err := logical.AddLogical("/registry", mutation.AccessShared, mutation.PathEffectDirectoryEntry); err != nil {
		t.Fatal(err)
	}
	if len(logical.Compile().Revisions()) != 1 {
		t.Fatal("Registry path spelling globally bypassed ordinary revisions")
	}
}

func TestDocumentWriteScopeDoesNotInferMutationFromExclusiveAccess(t *testing.T) {
	builder := NewBuilder(RevisionsFirstEffect, nil, 0)
	if err := builder.AddPhysicalPair("/observed", mutation.AccessExclusive, "codex", "project"); err != nil {
		t.Fatal(err)
	}
	if err := builder.AddPhysicalPair("/source", mutation.AccessShared, "codex", "project"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := builder.AddDocumentWritePair("/changed", "codex", "project"); err != nil {
			t.Fatal(err)
		}
	}
	requests := builder.Compile().DocumentWriteRequests()
	if len(requests) != 1 || requests[0].Path != "/changed" || requests[0].Target != "codex" || requests[0].Scope != "project" {
		t.Fatalf("exclusive observations became writes or write identity was lost: %#v", requests)
	}
}
