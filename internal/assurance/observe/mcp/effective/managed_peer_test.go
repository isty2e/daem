package effective

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	mcptopology "github.com/isty2e/daem/internal/topology/mcp"
)

func TestManagedPeerEvidencePreservesEqualityAndRetirement(t *testing.T) {
	subject, _ := mcptopology.ProjectionSubject(target.TargetPi, target.ScopeGlobal, "context7")
	peer, _ := mcptopology.ProjectionSubject(target.TargetPi, target.ScopeProject, "context7")
	root := t.TempDir()
	selectedPath := filepath.Join(root, "global.json")
	selected, err := NewSourceObservation(SourceObservationInput{
		ID: "global", Path: selectedPath, Kind: SourceNormal, State: SourceExact,
		Precedence: PrecedenceSelected, DefinitionEquivalence: DefinitionEquivalenceNotApplicable,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, policy := range []EvaluationPolicy{PolicyReplaceLower, PolicyExclusive} {
		for _, retiring := range []bool{false, true} {
			source, err := NewSourceObservation(SourceObservationInput{
				ID: "project", Path: filepath.Join(root, "project.json"), Kind: SourceNormal,
				State: SourceExact, Precedence: PrecedenceHigher, DefinesSelectedName: true,
				DefinitionEquivalence: DefinitionEquivalenceDifferent, PeerSubject: peer, PeerRetiring: retiring,
			})
			if err != nil {
				t.Fatal(err)
			}
			observed, err := NewObservation(ObservationInput{
				Subject: subject, ServerName: "context7", SelectedPath: selectedPath,
				Sources: []SourceObservation{source, selected}, Policy: policy,
			})
			if err != nil {
				t.Fatal(err)
			}
			conflicting := policy == PolicyExclusive && !retiring
			blockingCount := 0
			if conflicting {
				blockingCount = 1
			}
			if (observed.State() == StateConflicting) != conflicting || len(observed.BlockingSources()) != blockingCount {
				t.Fatalf("peer evaluation for %s/retiring=%t: %s", policy, retiring, observed.State())
			}
			if observed.HigherConflictPresent() != !retiring || source.DefinitionEquivalence() != DefinitionEquivalenceDifferent || source.PeerSubject() != peer {
				t.Fatal("peer coordination discarded equality, identity or survivor evidence")
			}
		}
	}
}

func TestManagedPeerEvidenceRejectsUnsupportedConstruction(t *testing.T) {
	peer, _ := mcptopology.ProjectionSubject(target.TargetPi, target.ScopeProject, "context7")
	valid := SourceObservationInput{
		ID: "project", Path: filepath.Join(t.TempDir(), "mcp.json"), Kind: SourceNormal,
		State: SourceExact, Precedence: PrecedenceHigher, DefinesSelectedName: true,
		DefinitionEquivalence: DefinitionEquivalenceDifferent, PeerSubject: peer,
	}
	for _, mutate := range []func(*SourceObservationInput){
		func(input *SourceObservationInput) { input.State = SourceAbsent },
		func(input *SourceObservationInput) { input.Kind = SourceImport },
		func(input *SourceObservationInput) { input.Precedence = PrecedenceSelected },
		func(input *SourceObservationInput) { input.DefinesSelectedName = false },
		func(input *SourceObservationInput) { input.DefinitionEquivalence = DefinitionEquivalenceUnknown },
		func(input *SourceObservationInput) {
			input.PeerSubject = topology.SubjectID{}
			input.PeerRetiring = true
		},
	} {
		input := valid
		mutate(&input)
		if _, err := NewSourceObservation(input); err == nil {
			t.Fatalf("unsupported peer evidence admitted: %#v", input)
		}
	}
}
