package journal

import (
	"bytes"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/target"
)

func TestDocumentGroupDraftFreezesInputsAndBranchesWithoutChangingEarlierRecords(t *testing.T) {
	fixture := wholeGroupFixture(t, target.TargetCodex, target.ScopeGlobal, "replace", []byte("before\n"), []byte("after\n"))
	input := fixture.input
	bindings, claims := input.Bindings(), input.Claims()
	if len(bindings) == 0 || claims == nil {
		t.Fatal("fixture requires actual binding and claim inputs")
	}
	draft, err := beginDocumentGroupRecord(documentGroupCaptureFacts{owner: input.Owner(), operationID: input.OperationID(), bindings: bindings, claims: claims})
	if err != nil {
		t.Fatal(err)
	}
	bindings[0], *claims = ownership.DocumentBindingIntent{}, ownershipmutation.DocumentClaimTransition{}

	grants, err := fixture.namespace.QualifyRoots(t.Context(), input.Budget())
	if err != nil {
		t.Fatal(err)
	}
	manifest, present := grants.Manifest()
	if !present {
		t.Fatal("fixture has no manifest grant")
	}
	var aliases []documentGroupAliasFacts
	for _, alias := range fixture.namespace.Aliases() {
		aliases = append(aliases, documentGroupAliasFacts{address: alias.Address(), root: alias.Root().Provenance(), observation: alias.Observation()})
	}
	pin := fixture.namespace.WriterPin()
	draft, err = draft.withNamespace(t.Context(), fixture.namespace.Observation(), pin.Provenance(), pin.Relative(), aliases, manifest.Provenance())
	if err != nil {
		t.Fatal("input mutation changed the draft", err)
	}
	comparison, err := PrepareWholeDocument(t.Context(), WholeDocumentPreparationInput{Before: fixture.original, BeforeMode: recovery.NewPermissionMode(0o644), Limits: input.Limits()}, input.mutation, input.payloads)
	if err != nil {
		t.Fatal(err)
	}
	first, err := draft.withComparison(comparison, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	before, _, err := first.seal(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded := nativeRecordJSON(t, before.record)

	second, err := draft.withComparison(comparison, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := second.seal(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	after, _, err := first.seal(t.Context(), nil)
	if err != nil || !bytes.Equal(encoded, nativeRecordJSON(t, after.record)) {
		t.Fatal("later value transition changed an earlier record", err)
	}
	if before.record.Original.Artifact.Path == other.record.Original.Artifact.Path || before.record.Expected.Artifact.Path == other.record.Expected.Artifact.Path {
		t.Fatal("branching lost its distinct group image references")
	}
	fixture.requireUnchangedHost(t)
}

func TestDocumentGroupDraftRefusesIncompleteTransitionsWithoutRecords(t *testing.T) {
	for _, initialized := range []bool{false, true} {
		draft := documentGroupRecordDraft{}
		if initialized {
			var err error
			draft, err = beginDocumentGroupRecord(documentGroupCaptureFacts{})
			if err != nil {
				t.Fatal(err)
			}
		}
		candidate, err := draft.withComparison(journaldocument.Comparison{}, 0, nil)
		if err == nil || candidate.Comparison().Validate() == nil {
			t.Fatal("incomplete metadata produced capture evidence")
		}
		if group, comparison, err := candidate.seal(t.Context(), nil); err == nil || group.initialized || comparison.Validate() == nil {
			t.Fatal("failed transition produced a usable record")
		}
		if next, _, err := draft.withPiOrder(pipackage.OrderObservation{}); err == nil || next.phase != 0 {
			t.Fatal("incomplete metadata accepted order evidence")
		}
	}
}
