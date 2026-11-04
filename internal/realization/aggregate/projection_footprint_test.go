package aggregate

import (
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestProjectionContractOwnsNativeKeyIdentity(t *testing.T) {
	for _, segments := range [][]string{
		{""}, {"."}, {".."}, {"a/b"}, {"a~b"}, {"0"}, {"a.b"}, {"☃"}, {"line\nbreak"}, {"\x00"},
	} {
		key, err := output.NewKeyPath(segments...)
		if err != nil {
			t.Fatal(err)
		}
		scope, err := output.NewKeyFootprint(key)
		if err != nil {
			t.Fatal(err)
		}
		contract := testNativeProjectionContract(t, scope)
		if contract.Validate() != nil || contract.Address().Footprint() != scope {
			t.Fatalf("native scope changed for %q", segments)
		}
		value, err := NewManagedContribution(contract, `{}`)
		if err != nil || value.Footprint() != scope || !value.Contract().Equal(contract) {
			t.Fatalf("native contribution failed for %q: %v", segments, err)
		}
	}
}

func TestProjectionContractRejectsNonKeyAuthority(t *testing.T) {
	for _, scope := range []output.Footprint{{}, output.WholePathFootprint()} {
		input := testProjectionContractInput(t, scope)
		if _, err := NewProjectionContract(input); err == nil {
			t.Fatal("aggregate constructor admitted missing or whole-path authority")
		}
	}
}

func TestNativeProjectionCannotEscapeThroughLegacyAddress(t *testing.T) {
	for _, name := range []string{"", ".", "..", "a/b", "a~b", "a b", "☃", "\x00"} {
		key, err := output.NewKeyPath(name)
		if err != nil {
			t.Fatal(err)
		}
		scope, err := output.NewKeyFootprint(key)
		if err != nil {
			t.Fatal(err)
		}
		contract := testNativeProjectionContract(t, scope)
		if _, err := contract.Address().LegacyContentPath(); !errors.Is(err, output.ErrNotLegacyFootprint) {
			t.Fatalf("unrepresentable key %q escaped through legacy address: %v", name, err)
		}
	}

	legacy := mustManagedContribution(t, testSharedHookContributionInput(t, `{}`))
	if got := string(outputtest.LegacyContentPath(t, legacy.Address())); got != "/hooks" {
		t.Fatalf("legacy scope changed: %q", got)
	}
	key, err := output.NewKeyPath("hooks")
	if err != nil {
		t.Fatal(err)
	}
	scope, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Footprint() != scope {
		t.Fatal("legacy ingress did not preserve literal key identity")
	}
}

func TestNativeSelectionUsesExactFootprintNotFilesystemMeaning(t *testing.T) {
	keys := []string{"/a~1b", "/a/b", "/a.b", "/a", "/"}
	contracts := make([]ProjectionContract, 0, len(keys))
	for _, pointer := range keys {
		key, err := output.ParseKeyPath(pointer)
		if err != nil {
			t.Fatal(err)
		}
		scope, err := output.NewKeyFootprint(key)
		if err != nil {
			t.Fatal(err)
		}
		contracts = append(contracts, testNativeProjectionContract(t, scope))
	}
	selection, err := NewSelection(contracts)
	if err != nil || len(selection.Contracts()) != len(keys) {
		t.Fatalf("read selection conflated native keys or rejected ancestor coverage: %v", err)
	}
	input := testProjectionContractInput(t, contracts[0].Address().Footprint())
	input.PlacementID = "pi.other"
	duplicate, err := NewProjectionContract(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewSelection([]ProjectionContract{contracts[0], duplicate}); err == nil {
		t.Fatal("placement names bypassed exact physical key collision")
	}
}

func testNativeProjectionContract(t testing.TB, scope output.Footprint) ProjectionContract {
	t.Helper()
	contract, err := NewProjectionContract(testProjectionContractInput(t, scope))
	if err != nil {
		t.Fatal(err)
	}
	return contract
}

func testProjectionContractInput(t testing.TB, scope output.Footprint) ProjectionContractInput {
	t.Helper()
	return ProjectionContractInput{
		PlacementID: "pi.host-config", Target: target.TargetPi, Scope: target.ScopeProject,
		AggregateRoot: outputtest.Parse(t, ".pi/settings.json"), Footprint: scope,
		MergeUnit: "native-key", Cardinality: ContributionExclusive,
		SiblingRetention: PreserveUnmanagedSiblings, SiblingPreservation: PreserveSiblingsSemantic,
		Equivalence: EquivalenceCanonicalSemantic, CodecContractID: "native-test-v1",
		ComparedFields: []string{"value"},
	}
}

func TestProjectionContractOwnsItsStaticInput(t *testing.T) {
	key, err := output.NewKeyPath("a/b")
	if err != nil {
		t.Fatal(err)
	}
	scope, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	input := testProjectionContractInput(t, scope)
	input.ComparedFields = []string{"value", "kind", "value"}
	contract, err := NewProjectionContract(input)
	if err != nil {
		t.Fatal(err)
	}
	input.ComparedFields[0] = "mutated"
	fields := contract.ComparedFields()
	if len(fields) != 2 || fields[0] != "kind" || fields[1] != "value" {
		t.Fatalf("static compared fields changed: %#v", fields)
	}
	fields[0] = "mutated"
	if contract.ComparedFields()[0] != "kind" {
		t.Fatal("static contract exposed mutable input storage")
	}
}

func TestLegacyAggregateIngressDoesNotAdmitRFCSpelling(t *testing.T) {
	input := testSharedHookContributionInput(t, `{}`)
	input.ContentPath = "/a~1b"
	if _, err := NewManagedContributionFromLegacy(input); err == nil {
		t.Fatal("legacy token grammar was widened to admit RFC spelling")
	}
	key, err := output.NewKeyPath("a~1b")
	if err != nil {
		t.Fatal(err)
	}
	scope, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	contract := testNativeProjectionContract(t, scope)
	if contract.Address().KeyPath().String() != "/a~01b" {
		t.Fatal("literal native tilde was recursively interpreted")
	}
	if _, err := contract.Address().LegacyContentPath(); !errors.Is(err, output.ErrNotLegacyFootprint) {
		t.Fatal("legacy egress did not retain the frozen aggregate token grammar")
	}
}
