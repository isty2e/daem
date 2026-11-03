package nativecodec_test

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
	"github.com/isty2e/daem/internal/target"
)

func TestNativeCodecRefusesInvalidContractsContributionsAndBaselines(t *testing.T) {
	codec := fixtureCodec(t, configdocument.JSON)
	set := fixtureSet(t, codec, key(t, "a"), fixtureValue(t, configdocument.JSON, "true"))
	contract := set.Contract()
	bad, err := aggregate.NewProjectionContract(aggregate.ProjectionContractInput{
		PlacementID: "other-placement", Target: target.TargetPi, Scope: target.ScopeGlobal,
		AggregateRoot: contract.Address().Document().AggregateRoot(), Footprint: contract.Address().Footprint(),
		MergeUnit: "native-key", Cardinality: aggregate.ContributionExclusive,
		SiblingRetention: aggregate.PreserveUnmanagedSiblings, SiblingPreservation: aggregate.PreserveSiblingsNativeValuesComments,
		Equivalence: aggregate.EquivalenceCanonicalSemantic, CodecContractID: codec.ContractID(), ComparedFields: []string{"value"},
	})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{bad})
	if err != nil {
		t.Fatal(err)
	}
	document := aggregate.ExistingDocument([]byte(`{"a":true}`))
	if actual, failure := codec.Read(document, selection); failure == nil || failure.Reason() != aggregate.CodecFailureCanonicalInvalid || len(actual.States()) != 0 {
		t.Fatal("a well-formed but forged static contract gained native evidence")
	}
	other := fixtureSet(t, codec, key(t, "other"), fixtureValue(t, configdocument.JSON, "true"))
	for _, test := range []struct {
		canonical string
		foreign   bool
	}{
		{"SECRET_CANARY", false},
		{set.Contributions()[0].Contribution().CanonicalContribution(), true},
	} {
		body, err := aggregate.NewManagedContribution(contract, test.canonical)
		if err != nil {
			t.Fatal(err)
		}
		subject := set.Contributions()[0].SubjectID()
		if test.foreign {
			subject = other.Contributions()[0].SubjectID()
		}
		item, err := aggregate.NewSubjectContribution(subject, body)
		if err != nil {
			t.Fatal(err)
		}
		invalid, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{item})
		if err != nil {
			t.Fatal(err)
		}
		if err := codec.ValidateContributions(invalid); err == nil || strings.Contains(err.Error(), "SECRET_CANARY") {
			t.Fatal("invalid desired bytes/subject bypassed native contribution admission")
		}
	}
	state, err := aggregate.NewProjectionState(contract, true, true, "SECRET_CANARY")
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := aggregate.NewSnapshot(true, selectSets(t, set), []aggregate.ProjectionState{state})
	if err != nil {
		t.Fatal(err)
	}
	restored, failure := codec.Restore(document, baseline)
	if failure == nil || failure.Reason() != aggregate.CodecFailureCanonicalInvalid || restored.Document().Exists() || len(restored.Expected().States()) != 0 || strings.Contains(failure.Error(), "SECRET_CANARY") {
		t.Fatal("invalid baseline produced a recovery candidate or disclosed its contents")
	}
}

func TestNativeCodecLateSelectedFailureDiscardsEarlierEvidence(t *testing.T) {
	codec := fixtureCodec(t, configdocument.JSON)
	first := fixtureSet(t, codec, key(t, "a"), fixtureValue(t, configdocument.JSON, "true"))
	last := fixtureSet(t, codec, key(t, "z"), fixtureValue(t, configdocument.JSON, `""`))
	bounded, err := nativecodec.New(t.Context(), codec.ContractID(), fixtureLimits, configdocument.Limits{MaximumBytes: 32, MaximumDepth: 4})
	if err != nil {
		t.Fatal(err)
	}
	document := aggregate.ExistingDocument([]byte(`{"a":true,"z":"` + strings.Repeat("z", 80) + `"}`))
	snapshot, failure := bounded.Read(document, selectSets(t, first, last))
	if failure == nil || failure.Reason() != aggregate.CodecFailureResourceLimitExceeded || failure.Location() != last.Address().KeyPath() || len(snapshot.States()) != 0 {
		t.Fatal("late canonical resource refusal returned an earlier selected success")
	}
}

func TestNativeRecoveryRetainsFreshCommentsInFormerlyAbsentDocument(t *testing.T) {
	codec := fixtureCodec(t, configdocument.JSONC)
	set := fixtureSet(t, codec, key(t, "a"), fixtureValue(t, configdocument.JSONC, "true"))
	selection := selectSets(t, set)
	before := read(t, codec, aggregate.AbsentDocument(), selection)
	current := aggregate.ExistingDocument([]byte("// fresh unmanaged comment\n{\"a\":true}"))
	restored, failure := codec.Restore(current, before)
	if failure != nil || !restored.Document().Exists() {
		t.Fatalf("recovery deleted a fresh unmanaged comment: %v", failure)
	}
	assertComments(t, configdocument.JSONC, current, restored.Document())
	for _, state := range read(t, codec, restored.Document(), selection).States() {
		if !state.Absent() {
			t.Fatal("comment preservation also retained the managed value")
		}
	}
}

func TestNativeCodecNeverErasesAnUnselectedBlockingAncestor(t *testing.T) {
	codec := fixtureCodec(t, configdocument.JSON)
	path := key(t, "a", "b")
	set := fixtureSet(t, codec, path, fixtureValue(t, configdocument.JSON, "true"))
	selection := selectSets(t, set)
	before := read(t, codec, aggregate.ExistingDocument([]byte(`{"a":{}}`)), selection)
	stale := aggregate.ExistingDocument([]byte(`{"a":"outside selected scope"}`))
	rendered, failure := codec.Render(stale, plan(t, before, map[output.KeyPath]*aggregate.ContributionSet{path: &set}))
	if failure == nil || rendered.Document().Exists() || len(rendered.Expected().States()) != 0 ||
		!stale.Equal(aggregate.ExistingDocument([]byte(`{"a":"outside selected scope"}`))) {
		t.Fatal("writing a child erased an undeclared ancestor value")
	}
}
