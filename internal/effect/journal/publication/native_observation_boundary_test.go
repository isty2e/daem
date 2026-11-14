package publication

import (
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	"github.com/isty2e/daem/internal/effect/journal/namespace/namespacetest"
	journalobservation "github.com/isty2e/daem/internal/effect/journal/observation"
	"github.com/isty2e/daem/internal/output"

	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/target"
)

func TestObservationSelectionKeepsBorrowedCustodyWithoutRequalification(t *testing.T) {
	fixture := nativeCaptureInput(t, false, target.ScopeProject)
	if err := (journalobservation.Batch{}).ValidateOriginals(t.Context(), fixture.batch.roots); err == nil {
		t.Fatal("zero evidence claimed unchanged originals")
	}
	before := fixture.batch.Evidence().BeforeState()
	namespaces := fixture.batch.Evidence().Namespaces()
	if len(namespaces) == 0 {
		t.Fatal("fixture has no selected namespace")
	}
	namespaces[0] = journalnamespace.DocumentNamespace{}
	if fixture.batch.Evidence().Namespaces()[0].Observation().Validate() != nil {
		t.Fatal("evidence exposed its namespace slice")
	}
	if err := namespacetest.LegacyRoot(t, fixture.batch.roots, output.RootProject).Root.Close(); err != nil {
		t.Fatal(err)
	}
	selected, members, err := fixture.batch.CaptureMembers(fixture.after, "read-only-selection")
	if err != nil || len(members) == 0 || selected.Evidence().Validate() != nil || !selected.Evidence().BeforeState().Equal(before) {
		t.Fatalf("pure selection performed root freshness work or lost evidence: %v", err)
	}
	if _, err := selected.manifest(t.Context()); err == nil {
		t.Fatal("selection replaced its closed borrowed root with new authority")
	}
	if value, members, err := (NativeDocumentObservationBatch{}).CaptureMembers(durable.EmptySnapshot(), "zero"); err == nil || value.Evidence().Validate() == nil || len(members) != 0 {
		t.Fatal("zero custody pair produced usable selection")
	}
}

func TestObservationCaptureInputClonesCandidateCollectionsAndClaims(t *testing.T) {
	fixture := wholeGroupFixture(t, target.TargetPi, target.ScopeGlobal, "replace", []byte("before"), []byte("after"))
	input := fixture.input.CaptureInput
	spec := journalobservation.CaptureInputSpec{
		Owner: input.Owner(), OperationID: input.OperationID(), Claims: input.Claims(),
		Provisional: input.Provisional(), Bindings: input.Bindings(), GroupIndex: input.GroupIndex(), Limits: input.Limits(), Budget: input.Budget(),
	}
	if spec.Claims == nil || len(spec.Bindings) == 0 {
		t.Fatal("fixture lacks exact ownership and binding facts")
	}
	before, prepared, after := spec.Claims.Before(), spec.Claims.Prepared(), spec.Claims.After()
	matches := func(value *ownershipmutation.DocumentClaimTransition) bool {
		return value != nil && slices.EqualFunc(value.Before(), before, ownership.Claim.Equal) && slices.EqualFunc(value.Prepared(), prepared, ownership.Claim.Equal) && slices.EqualFunc(value.After(), after, ownership.Claim.Equal)
	}
	candidate := journalobservation.NewCaptureInput(spec)
	*spec.Claims, spec.Bindings[0] = ownershipmutation.DocumentClaimTransition{}, ownership.DocumentBindingIntent{}
	if !matches(candidate.Claims()) {
		t.Fatal("candidate retained its caller's claim pointer")
	}
	if err := candidate.Bindings()[0].Validate(); err != nil {
		t.Fatal("candidate retained its caller's binding slice", err)
	}
	claims, bindings := candidate.Claims(), candidate.Bindings()
	*claims, bindings[0] = ownershipmutation.DocumentClaimTransition{}, ownership.DocumentBindingIntent{}
	if !matches(candidate.Claims()) || candidate.Bindings()[0].Validate() != nil {
		t.Fatal("candidate getters exposed mutable ownership facts")
	}
	if err := (journalobservation.CaptureInput{}).Validate(t.Context()); err == nil {
		t.Fatal("zero capture input supplied a valid candidate")
	}
}
