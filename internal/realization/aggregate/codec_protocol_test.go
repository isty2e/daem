package aggregate

import (
	"bytes"
	"strings"
	"testing"
)

func TestDocumentDistinguishesAbsentFromExistingEmptyAndClonesBytes(t *testing.T) {
	absent := AbsentDocument()
	existing := ExistingDocument(nil)
	if absent.Exists() || !existing.Exists() || len(existing.Content()) != 0 {
		t.Fatalf("document presence = absent:%t existing:%t", absent.Exists(), existing.Exists())
	}

	content := []byte("secret-bearing-host-config")
	document := ExistingDocument(content)
	content[0] = 'X'
	got := document.Content()
	got[0] = 'Y'
	if string(document.Content()) != "secret-bearing-host-config" {
		t.Fatal("document content was not defensively copied")
	}

	forged := Document{content: []byte("impossible")}
	if err := forged.Validate(); err == nil || !strings.Contains(err.Error(), "absent") {
		t.Fatalf("forged absent document error = %v", err)
	}
}

func TestSelectionCanonicalizesOneDocumentAndRejectsPhysicalAlias(t *testing.T) {
	leftInput := testSharedHookContributionInput(`{"command":"left"}`)
	leftInput.ContentPath = "/hooks/left"
	rightInput := testSharedHookContributionInput(`{"command":"right"}`)
	rightInput.ContentPath = "/hooks/right"
	left := mustManagedContribution(t, leftInput).Contract()
	right := mustManagedContribution(t, rightInput).Contract()

	selection, err := NewSelection([]ProjectionContract{right, left})
	if err != nil {
		t.Fatalf("NewSelection returned error: %v", err)
	}
	contracts := selection.Contracts()
	if len(contracts) != 2 || contracts[0].Address().ContentPath() != "/hooks/left" || contracts[1].Address().ContentPath() != "/hooks/right" {
		t.Fatalf("canonical selection = %#v", contracts)
	}

	aliasInput := leftInput
	aliasInput.PlacementID = "codex.project.alias-hooks"
	alias := mustManagedContribution(t, aliasInput).Contract()
	if _, err := NewSelection([]ProjectionContract{left, alias}); err == nil || !strings.Contains(err.Error(), "physical projection collision") {
		t.Fatalf("physical alias error = %v", err)
	}

	otherDocumentInput := rightInput
	otherDocumentInput.AggregateRoot = "other-settings.json"
	otherDocument := mustManagedContribution(t, otherDocumentInput).Contract()
	if _, err := NewSelection([]ProjectionContract{left, otherDocument}); err == nil || !strings.Contains(err.Error(), "mixes documents") {
		t.Fatalf("mixed document error = %v", err)
	}

	otherCodecInput := rightInput
	otherCodecInput.CodecContractID = "codex-project-hooks-v2"
	otherCodec := mustManagedContribution(t, otherCodecInput).Contract()
	if _, err := NewSelection([]ProjectionContract{left, otherCodec}); err == nil || !strings.Contains(err.Error(), "mixes codec") {
		t.Fatalf("mixed codec error = %v", err)
	}
}

func TestProtocolValuesRejectDuplicateAndContradictoryCoverage(t *testing.T) {
	leftInput := testSharedHookContributionInput(`{"command":"left"}`)
	leftInput.ContentPath = "/hooks/left"
	rightInput := testSharedHookContributionInput(`{"command":"right"}`)
	rightInput.ContentPath = "/hooks/right"
	left := mustManagedContribution(t, leftInput).Contract()
	right := mustManagedContribution(t, rightInput).Contract()

	if _, err := NewSelection(nil); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("empty selection error = %v", err)
	}
	if _, err := NewSelection([]ProjectionContract{left, left}); err == nil || !strings.Contains(err.Error(), "repeats projection") {
		t.Fatalf("duplicate selection error = %v", err)
	}
	selection, err := NewSelection([]ProjectionContract{left, right})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := NewProjectionState(left, false, true, `{"command":"left"}`); err == nil || !strings.Contains(err.Error(), "requires its parent") {
		t.Fatalf("orphan projection error = %v", err)
	}
	if _, err := NewProjectionState(left, true, false, `{"command":"left"}`); err == nil || !strings.Contains(err.Error(), "absent") {
		t.Fatalf("absent projection content error = %v", err)
	}
	if _, err := NewProjectionState(left, true, true, ""); err == nil || !strings.Contains(err.Error(), "canonical UTF-8") {
		t.Fatalf("empty present projection error = %v", err)
	}

	leftAbsent, _ := NewProjectionState(left, true, false, "")
	rightAbsent, _ := NewProjectionState(right, true, false, "")
	if _, err := NewSnapshot(true, selection, []ProjectionState{leftAbsent}); err == nil || !strings.Contains(err.Error(), "state count") {
		t.Fatalf("partial snapshot error = %v", err)
	}
	if _, err := NewSnapshot(true, selection, []ProjectionState{leftAbsent, leftAbsent}); err == nil || !strings.Contains(err.Error(), "repeats projection") {
		t.Fatalf("duplicate snapshot error = %v", err)
	}
	before, err := NewSnapshot(true, selection, []ProjectionState{rightAbsent, leftAbsent})
	if err != nil {
		t.Fatal(err)
	}
	leftDesired, _ := NewContributionSet([]SubjectContribution{
		mustSubjectContribution(t, "left", leftInput),
	})
	leftIntent, _ := NewProjectionIntent(leftAbsent, &leftDesired)
	if _, err := NewPlan(before, []ProjectionIntent{leftIntent}); err == nil || !strings.Contains(err.Error(), "cover every selected projection") {
		t.Fatalf("partial plan error = %v", err)
	}
	if _, err := NewPlan(before, []ProjectionIntent{leftIntent, leftIntent}); err == nil || !strings.Contains(err.Error(), "repeats projection") {
		t.Fatalf("duplicate plan error = %v", err)
	}
}

func TestSnapshotAndPlanRequireExactAddressCorrelatedCoverage(t *testing.T) {
	contribution := mustManagedContribution(t, testSharedHookContributionInput(`{"command":"review"}`))
	contract := contribution.Contract()
	selection, err := NewSelection([]ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	absent, err := NewProjectionState(contract, true, false, "")
	if err != nil {
		t.Fatal(err)
	}
	before, err := NewSnapshot(true, selection, []ProjectionState{absent})
	if err != nil {
		t.Fatal(err)
	}
	selected, covered := before.State(contract)
	if !covered || !selected.Contract().Equal(contract) || selected.Present() {
		t.Fatalf("snapshot selected state = %#v, %t", selected, covered)
	}
	driftedContractInput := testSharedHookContributionInput(`{"command":"review"}`)
	driftedContractInput.CodecContractID = "codex-project-hooks-v2"
	driftedContract := mustManagedContribution(t, driftedContractInput).Contract()
	if _, covered := before.State(driftedContract); covered {
		t.Fatal("snapshot matched an address-equal but contract-different projection")
	}
	desired, err := NewContributionSet([]SubjectContribution{mustSubjectContribution(t, "review", testSharedHookContributionInput(`{"command":"review"}`))})
	if err != nil {
		t.Fatal(err)
	}
	intent, err := NewProjectionIntent(absent, &desired)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan(before, []ProjectionIntent{intent})
	if err != nil {
		t.Fatal(err)
	}

	present, err := NewProjectionState(contract, true, true, `[{"command":"review"}]`)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := NewSnapshot(true, selection, []ProjectionState{present})
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := NewRenderedDocument(ExistingDocument([]byte(`{"hooks":[]}`)), plan, expected)
	if err != nil {
		t.Fatalf("NewRenderedDocument returned error: %v", err)
	}
	if !rendered.Expected().States()[0].Present() {
		t.Fatal("rendered expected state lost desired projection")
	}

	omitted, err := NewSnapshot(true, selection, []ProjectionState{absent})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewRenderedDocument(ExistingDocument(nil), plan, omitted); err == nil || !strings.Contains(err.Error(), "omits a desired projection") {
		t.Fatalf("omitted desired projection error = %v", err)
	}

	if _, err := NewSnapshot(false, selection, []ProjectionState{absent}); err == nil || !strings.Contains(err.Error(), "absent aggregate document") {
		t.Fatalf("absent document contradiction error = %v", err)
	}
	if _, err := NewProjectionIntent(absent, nil); err == nil || !strings.Contains(err.Error(), "requires a present") {
		t.Fatalf("remove absent projection error = %v", err)
	}

	driftedInput := testSharedHookContributionInput(`{"command":"other"}`)
	driftedInput.CodecContractID = "codex-project-hooks-v2"
	forgedDesired := ContributionSet{items: []SubjectContribution{
		mustSubjectContribution(t, "review", testSharedHookContributionInput(`{"command":"review"}`)),
		mustSubjectContribution(t, "other", driftedInput),
	}}
	forgedIntent := ProjectionIntent{before: absent, desired: &forgedDesired}
	if _, err := NewPlan(before, []ProjectionIntent{forgedIntent}); err == nil || !strings.Contains(err.Error(), "mixes codec") {
		t.Fatalf("forged desired set error = %v", err)
	}
}

func TestRenderedRemovalAndRecoveryRemainNonAuthoritativeCandidates(t *testing.T) {
	contribution := mustManagedContribution(t, testSharedHookContributionInput(`{"command":"review"}`))
	contract := contribution.Contract()
	selection, _ := NewSelection([]ProjectionContract{contract})
	present, _ := NewProjectionState(contract, true, true, `[{"command":"review"}]`)
	before, _ := NewSnapshot(true, selection, []ProjectionState{present})
	remove, err := NewProjectionIntent(present, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan(before, []ProjectionIntent{remove})
	if err != nil {
		t.Fatal(err)
	}
	absent, _ := NewProjectionState(contract, false, false, "")
	expected, _ := NewSnapshot(false, selection, []ProjectionState{absent})
	if _, err := NewRenderedDocument(AbsentDocument(), plan, expected); err != nil {
		t.Fatalf("remove-last rendered candidate error = %v", err)
	}
	if _, err := NewRenderedDocument(ExistingDocument(nil), plan, before); err == nil || !strings.Contains(err.Error(), "retains a projection") {
		t.Fatalf("retained removed projection error = %v", err)
	}

	restored, err := NewRestoredDocument(ExistingDocument([]byte(`{"hooks":[]}`)), before)
	if err != nil {
		t.Fatalf("NewRestoredDocument returned error: %v", err)
	}
	bytesCopy := restored.Document().Content()
	bytesCopy[0] = 'X'
	if bytes.Equal(bytesCopy, restored.Document().Content()) {
		t.Fatal("restored document exposed mutable candidate bytes")
	}
	if _, err := NewRestoredDocument(AbsentDocument(), before); err == nil || !strings.Contains(err.Error(), "requires its parent") {
		t.Fatalf("recovery presence error = %v", err)
	}
	absentBefore, _ := NewSnapshot(false, selection, []ProjectionState{absent})
	withNewSibling, err := NewRestoredDocument(ExistingDocument([]byte(`{"unmanaged":true}`)), absentBefore)
	if err != nil {
		t.Fatalf("restore absent baseline with new sibling: %v", err)
	}
	if !withNewSibling.Expected().DocumentExisted() || withNewSibling.Expected().States()[0].Present() {
		t.Fatalf("restored expected state = %#v, want existing document with absent selection", withNewSibling.Expected())
	}
	forgedBaseline := Snapshot{documentExisted: false, states: []ProjectionState{{
		contract: contract, parentPresent: true,
	}}}
	if _, err := NewRestoredDocument(AbsentDocument(), forgedBaseline); err == nil || !strings.Contains(err.Error(), "absent aggregate document") {
		t.Fatalf("forged recovery baseline error = %v", err)
	}
}

func TestCodecFailureCarriesOnlyClosedReasonAndCanonicalPath(t *testing.T) {
	reasons := []CodecFailureReason{
		CodecFailureDocumentMalformed,
		CodecFailureDuplicateKey,
		CodecFailureSelectedShapeUnsupported,
		CodecFailureEquivalenceUndefined,
		CodecFailurePreservationUndefined,
		CodecFailureCanonicalInvalid,
	}
	for _, reason := range reasons {
		failure, err := NewCodecFailure(reason, "/hooks")
		if err != nil {
			t.Fatalf("NewCodecFailure(%q) returned error: %v", reason, err)
		}
		if failure.Reason() != reason || strings.Contains(failure.Error(), "SECRET_CANARY") {
			t.Fatalf("failure = %q", failure.Error())
		}
	}
	if _, err := NewCodecFailure("parser_failed", "/hooks"); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unknown reason error = %v", err)
	}
	if _, err := NewCodecFailure(CodecFailureDuplicateKey, "hooks"); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("malformed path error = %v", err)
	}
}

type protocolOnlyCodec struct{}

func (protocolOnlyCodec) ContractID() CodecContractID { return "protocol-only-v1" }
func (protocolOnlyCodec) ValidateContribution(ManagedContribution) error {
	return nil
}

func (protocolOnlyCodec) Read(Document, Selection) (Snapshot, *CodecFailure) {
	return Snapshot{}, nil
}

func (protocolOnlyCodec) Render(Document, Plan) (RenderedDocument, *CodecFailure) {
	return RenderedDocument{}, nil
}

func (protocolOnlyCodec) Restore(Document, Snapshot) (RenderedDocument, *CodecFailure) {
	return RenderedDocument{}, nil
}

var _ Codec = protocolOnlyCodec{}
