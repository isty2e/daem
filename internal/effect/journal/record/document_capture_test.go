package record

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
)

func TestDocumentGroupDraftFreezesInputsAndBranchesWithoutChangingEarlierRecords(t *testing.T) {
	fixture := observedNativeRecordFixture(t)
	claim := documentActiveClaim(t, fixture.claims, output.WholePathFootprint())
	transition, err := ownershipmutation.NewDocumentClaimTransition([]ownership.Claim{claim}, []ownership.Claim{claim}, fixture.record.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	value := membershipBindingValue(t, fixture.claims)
	bindings := []ownership.DocumentBindingIntent{membershipIntent(t, fixture.claims, value, value)}
	claims := &transition
	draft, err := BeginDocumentGroup(fixture.claims.owner, fixture.record.OperationID, bindings, claims, nil)
	if err != nil {
		t.Fatal(err)
	}
	bindings[0], *claims = ownership.DocumentBindingIntent{}, ownershipmutation.DocumentClaimTransition{}
	aliases := []DocumentGroupAlias{NewDocumentGroupAlias(fixture.claims.alias, fixture.provenance, fixture.observation)}
	coordinate, err := configuration.NewDirectoryChild(fixture.relative.Path())
	if err != nil {
		t.Fatal(err)
	}
	if rejected, err := draft.WithNamespace(t.Context(), fixture.observation, fixture.provenance, configuration.PointSelf(), aliases, fixture.provenance); err == nil || rejected.phase != 0 {
		t.Fatal("native16 serialized a point-self writer coordinate", err)
	}
	draft, err = draft.WithNamespace(t.Context(), fixture.observation, fixture.provenance, coordinate, aliases, fixture.provenance)
	if err != nil {
		t.Fatal("input mutation changed the draft", err)
	}
	mode := recovery.NewPermissionMode(0o600)
	comparison, err := journaldocument.NewWholeComparison(t.Context(), journaldocument.ComparisonInput{Before: fixture.original, Expected: fixture.expected, BeforeMode: mode, ExpectedMode: mode, Limits: fixture.limits})
	if err != nil {
		t.Fatal(err)
	}
	first, err := draft.WithComparison(comparison, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	before, _, err := first.Seal(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.record.Aliases) != 1 || !before.record.Aliases[0].SelectedObservation.equal(before.record.CapturedTarget) || !before.record.Aliases[0].ObservedBefore.equal(before.record.CapturedTarget) {
		t.Fatal("equal captured and selected observations changed during record construction")
	}
	encoded := nativeRecordJSON(t, before.record)
	second, err := draft.WithComparison(comparison, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := second.Seal(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	after, _, err := first.Seal(t.Context(), nil)
	if err != nil || !bytes.Equal(encoded, nativeRecordJSON(t, after.record)) {
		t.Fatal("later value transition changed an earlier record", err)
	}
	if before.record.Original.Artifact.Path == other.record.Original.Artifact.Path || before.record.Expected.Artifact.Path == other.record.Expected.Artifact.Path {
		t.Fatal("branching lost its distinct group image references")
	}
	host, err := os.ReadFile(filepath.Join(fixture.provenance.PhysicalRoot(), fixture.relative.Path()))
	if err != nil || !bytes.Equal(host, fixture.original.Content()) {
		t.Fatal("record draft changed the observed host document", err)
	}
}

func TestDocumentGroupDraftRefusesIncompleteTransitionsWithoutRecords(t *testing.T) {
	for _, initialized := range []bool{false, true} {
		draft := DocumentGroupDraft{}
		if initialized {
			var err error
			draft, err = BeginDocumentGroup(stateauthority.Authority{}, "", nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
		}
		candidate, err := draft.WithComparison(journaldocument.Comparison{}, 0, nil)
		if err == nil || candidate.Comparison().Validate() == nil {
			t.Fatal("incomplete metadata produced capture evidence")
		}
		if group, comparison, err := candidate.Seal(t.Context(), nil); err == nil || group.initialized || comparison.Validate() == nil {
			t.Fatal("failed transition produced a usable record")
		}
		if next, _, err := draft.WithPiOrder(pipackage.OrderObservation{}); err == nil || next.phase != 0 {
			t.Fatal("incomplete metadata accepted order evidence")
		}
	}
}
