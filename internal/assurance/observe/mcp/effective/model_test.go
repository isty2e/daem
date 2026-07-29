package effective

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/topology"
)

func TestSourceObservationRequiresEquivalenceOnlyForExactSameNameDefinitions(t *testing.T) {
	subject, err := topology.NewSubjectID(
		topology.SubjectProjection,
		"pi.project.mcp-server",
		"context7",
	)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "mcp.json")
	tests := []struct {
		name  string
		input SourceObservationInput
	}{
		{
			name: "same name without equivalence",
			input: SourceObservationInput{
				ID: "lower", Path: path, Kind: SourceNormal,
				Precedence: PrecedenceLower, State: SourceExact,
				DefinesSelectedName: true,
			},
		},
		{
			name: "absent name with equivalence",
			input: SourceObservationInput{
				ID: "lower", Path: path, Kind: SourceNormal,
				Precedence: PrecedenceLower, State: SourceExact,
				DefinitionEquivalence: DefinitionEquivalenceEquivalent,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewSourceObservation(test.input); err == nil {
				t.Fatal("NewSourceObservation returned nil error")
			}
		})
	}

	source, err := NewSourceObservation(SourceObservationInput{
		ID: "lower", Path: path, Kind: SourceNormal,
		Precedence: PrecedenceLower, State: SourceExact,
		DefinesSelectedName:   true,
		DefinitionEquivalence: DefinitionEquivalenceDifferent,
	})
	if err != nil {
		t.Fatalf("NewSourceObservation returned error: %v", err)
	}
	selected, err := NewSourceObservation(SourceObservationInput{
		ID: "selected", Path: path, Kind: SourceNormal,
		Precedence: PrecedenceSelected, State: SourceExact,
		DefinesSelectedName:   true,
		DefinitionEquivalence: DefinitionEquivalenceEquivalent,
	})
	if err != nil {
		t.Fatalf("NewSourceObservation(selected) returned error: %v", err)
	}
	observation, err := NewObservation(ObservationInput{
		Subject: subject, ServerName: "context7", SelectedPath: path,
		Sources: []SourceObservation{source, selected},
	})
	if err != nil {
		t.Fatalf("NewObservation returned error: %v", err)
	}
	equivalence, present := observation.LowerFallbackEquivalence()
	if !present || equivalence != DefinitionEquivalenceDifferent {
		t.Fatalf(
			"lower fallback equivalence = %q/%t, want different/true",
			equivalence,
			present,
		)
	}
}
