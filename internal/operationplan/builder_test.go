package operationplan

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
)

func TestBuilderPreservesDomainAdmissionOrderAndSortsFingerprintFacts(t *testing.T) {
	t.Parallel()

	builder := NewBuilder(RevisionsFirstEffect, []string{"/manifest.toml", "/lock.toml"}, 0)
	if err := builder.AddLogicalPair("/manifest.toml", mutation.AccessShared, mutation.AccessShared); err != nil {
		t.Fatal(err)
	}
	if err := builder.AddLogical("/state.json", mutation.AccessExclusive, mutation.PathEffectDirectoryEntry); err != nil {
		t.Fatal(err)
	}
	if err := builder.AddFingerprintOnly(
		FactRecoveryBarrier,
		"/recovery",
		mutation.AccessExclusive,
		mutation.PathEffectDirectoryEntry,
		"",
	); err != nil {
		t.Fatal(err)
	}
	if err := builder.AddRoute("claude-code", "project", "write", mutation.RouteContainmentUnknown); err != nil {
		t.Fatal(err)
	}
	if err := builder.AddRoute("claude-code", "project", "write", mutation.RouteContainmentUnknown); err != nil {
		t.Fatal(err)
	}

	plan := builder.Compile()
	steps := plan.DomainSteps()
	if got := len(steps); got != 4 {
		t.Fatalf("domains = %d, want 4 (3 path + 1 route)", got)
	}
	first, ok := steps[0].Path()
	if !ok {
		t.Fatal("first domain step is not a path request")
	}
	logical, ok := first.Logical()
	if !ok || logical.Path != "/manifest.toml" || logical.Effect != mutation.PathEffectDirectoryEntry {
		t.Fatalf("first domain request = %#v", first)
	}
	if _, ok := steps[3].Compiled(); !ok {
		t.Fatal("host route domain is not the final compiled step")
	}
	if got := len(plan.Facts()); got != 5 {
		t.Fatalf("facts = %d, want 5", got)
	}
	if plan.Facts()[0].Kind() != FactLogical || plan.Facts()[0].Path() != "/manifest.toml" {
		t.Fatalf("admission-order first fact = %#v", plan.Facts()[0])
	}
	revisions := plan.Revisions()
	if len(revisions) != 1 || revisions[0].Path != "/state.json" {
		t.Fatalf("first-effect revisions = %#v, want only /state.json", revisions)
	}

	fingerprint, err := ApplyAuthorityFingerprint(plan, nil, "barrier")
	if err != nil {
		t.Fatal(err)
	}
	again, err := ApplyAuthorityFingerprint(plan, nil, "barrier")
	if err != nil {
		t.Fatal(err)
	}
	if !fingerprint.Equal(again) {
		t.Fatal("apply authority fingerprint is not stable")
	}
}

func TestRefreshPersistenceRevisionsSelectsOnlyNamedRolePaths(t *testing.T) {
	t.Parallel()

	builder := NewBuilder(
		RevisionsRefreshFull,
		[]string{"/manifest", "/lockfile"},
		4096,
	)
	for _, path := range []string{"/manifest", "/lockfile", "/state", "/other"} {
		if err := builder.AddLogicalPair(path, mutation.AccessShared, mutation.AccessShared); err != nil {
			t.Fatal(err)
		}
	}
	selected := RefreshPersistenceRevisions(
		builder.Compile(),
		"/manifest",
		"/lockfile",
		"/state",
	)
	if len(selected) != 6 {
		t.Fatalf("persistence revisions = %d, want 6", len(selected))
	}
	for _, request := range selected {
		if request.Path == "/other" {
			t.Fatalf("non-persistence revision selected: %#v", request)
		}
	}
}

func TestRecoverFingerprintOmitsFamilyAndContainment(t *testing.T) {
	t.Parallel()

	builder := NewBuilder(RevisionsOff, nil, 0)
	if err := builder.AddLogical("/recovery", mutation.AccessExclusive, mutation.PathEffectDirectoryEntry); err != nil {
		t.Fatal(err)
	}
	if err := builder.AddFingerprintOnly(
		FactStateDirIdentity,
		"/statedir",
		mutation.AccessShared,
		mutation.PathEffectDirectoryEntry,
		"identity-token",
	); err != nil {
		t.Fatal(err)
	}
	plan := builder.Compile()
	left, err := RecoverAuthorityFingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}
	right, err := RecoverAuthorityFingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}
	if !left.Equal(right) {
		t.Fatal("recover authority fingerprint is not stable")
	}
	if _, err := ApplyAuthorityFingerprint(plan, nil, ""); err == nil {
		t.Fatal("apply projection must reject recover-only fact kinds")
	}
}

func TestFactsCoverMatchesRequiredSubset(t *testing.T) {
	t.Parallel()

	builder := NewBuilder(RevisionsOff, nil, 0)
	if err := builder.AddLogical("/a", mutation.AccessShared, mutation.PathEffectDirectoryEntry); err != nil {
		t.Fatal(err)
	}
	if err := builder.AddLogical("/b", mutation.AccessExclusive, mutation.PathEffectReferent); err != nil {
		t.Fatal(err)
	}
	all := builder.Compile().Facts()
	required := []Fact{all[1]}
	if !FactsCover(all, required) {
		t.Fatal("expected required subset to be covered")
	}
	missing := NewBuilder(RevisionsOff, nil, 0)
	if err := missing.AddLogical("/c", mutation.AccessShared, mutation.PathEffectDirectoryEntry); err != nil {
		t.Fatal(err)
	}
	if FactsCover(all, missing.Compile().Facts()) {
		t.Fatal("missing path must not be covered")
	}
}
