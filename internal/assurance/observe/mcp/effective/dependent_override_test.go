package effective

import (
	"path/filepath"
	"testing"
)

func TestDependentOverrideRequiresExactHigherNonDefiningEvidence(t *testing.T) {
	valid := SourceObservationInput{
		ID: "pi-native-project", Path: filepath.Join(t.TempDir(), "mcp.json"),
		Kind: SourceNormal, Precedence: PrecedenceHigher, State: SourceExact,
		DefinitionEquivalence:      DefinitionEquivalenceNotApplicable,
		DependsOnRemovedDefinition: true,
	}
	source, err := NewSourceObservation(valid)
	if err != nil || !source.DependsOnRemovedDefinition() || source.DefinesSelectedName() {
		t.Fatalf("dependent override = %#v, %v", source, err)
	}
	for _, change := range []func(*SourceObservationInput){
		func(input *SourceObservationInput) { input.State = SourceAbsent },
		func(input *SourceObservationInput) { input.State = SourceOpaque; input.Detail = "unreadable" },
		func(input *SourceObservationInput) { input.Kind = SourceImport },
		func(input *SourceObservationInput) { input.Precedence = PrecedenceLower },
		func(input *SourceObservationInput) { input.Precedence = PrecedenceSelected },
		func(input *SourceObservationInput) {
			input.DefinesSelectedName = true
			input.DefinitionEquivalence = DefinitionEquivalenceEquivalent
		},
	} {
		input := valid
		change(&input)
		if _, err := NewSourceObservation(input); err == nil {
			t.Fatalf("unsupported dependent evidence admitted: %#v", input)
		}
	}
}
