package mcpcodec

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestMCPCodecClassifiesOccupancyByCanonicalProjectionEquality(t *testing.T) {
	operations := mustMCPCodecOperations(t, aggregate.MCPPlacementClaudeProject)
	placement := operations.Placement()
	codec, ok := For(placement.CodecContractID())
	if !ok {
		t.Fatal("MCP codec is missing")
	}

	desired := ClaudeProjectMCPServerProjection{
		ServerID:        "context7",
		Command:         "npx",
		Args:            []string{"-y", "@upstash/context7-mcp"},
		Env:             map[string]string{"TOKEN": "${TOKEN}"},
		AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
	}
	equal := desired
	commandMismatch := desired
	commandMismatch.Command = "node"
	argsMismatch := desired
	argsMismatch.Args = []string{"-y", "old-package"}
	envMismatch := desired
	envMismatch.Env = map[string]string{"OTHER": "${OTHER}"}
	sibling := ClaudeProjectMCPServerProjection{
		ServerID:        "sibling",
		Command:         "node",
		Args:            []string{"manual.js"},
		Env:             map[string]string{},
		AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
	}

	tests := []struct {
		name     string
		observed *ClaudeProjectMCPServerProjection
		sibling  bool
		want     aggregate.ContributionOccupancyState
	}{
		{name: "equal payload", observed: &equal, want: aggregate.ContributionPresent},
		{name: "command mismatch", observed: &commandMismatch, want: aggregate.ContributionAbsent},
		{name: "args mismatch", observed: &argsMismatch, want: aggregate.ContributionAbsent},
		{name: "env mismatch", observed: &envMismatch, want: aggregate.ContributionAbsent},
		{name: "missing id", want: aggregate.ContributionAbsent},
		{
			name:     "foreign sibling is outside occupancy",
			observed: &commandMismatch,
			sibling:  true,
			want:     aggregate.ContributionAbsent,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			existing := []byte(`{"unmanaged":true}`)
			var err error
			if test.observed != nil {
				canonical := mustCanonicalClaudeProjectMCPServerEntry(t, *test.observed)
				existing, err = operations.mergeCanonicalEntry(existing, test.observed.ServerID, canonical)
				if err != nil {
					t.Fatalf("MergeCanonicalEntry(observed): %v", err)
				}
			}
			if test.sibling {
				canonical := mustCanonicalClaudeProjectMCPServerEntry(t, sibling)
				existing, err = operations.mergeCanonicalEntry(existing, sibling.ServerID, canonical)
				if err != nil {
					t.Fatalf("MergeCanonicalEntry(sibling): %v", err)
				}
			}

			desiredCanonical := mustCanonicalClaudeProjectMCPServerEntry(t, desired)
			contribution := mcpCodecContribution(t, placement, desired.ServerID, desiredCanonical)
			desiredSet := mcpCodecExclusiveSet(t, contribution)
			selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contribution.Contract()})
			if err != nil {
				t.Fatal(err)
			}
			snapshot, failure := codec.Read(aggregate.ExistingDocument(existing), selection)
			if failure != nil {
				t.Fatal(failure)
			}
			occupancy, err := codec.ClassifyContributionOccupancy(snapshot.States()[0], desiredSet)
			if err != nil {
				t.Fatalf("ClassifyContributionOccupancy: %v", err)
			}
			subject := desiredSet.Contributions()[0].SubjectID()
			state, covered := occupancy.State(subject)
			if !covered || state != test.want {
				t.Fatalf("occupancy = %q covered=%t, want %q", state, covered, test.want)
			}
			if state == aggregate.ContributionAmbiguous {
				t.Fatal("MCP occupancy classified Ambiguous")
			}
			if test.sibling {
				present, err := operations.entryPresent(existing, sibling.ServerID)
				if err != nil || !present {
					t.Fatalf("sibling present = %t, error = %v", present, err)
				}
			}
		})
	}
}

func TestMCPCodecClassifiesAdapterCanonicalMismatchAbsent(t *testing.T) {
	operations := mustMCPCodecOperations(t, aggregate.MCPPlacementClaudeProject)
	placement := operations.Placement()
	codec, ok := For(placement.CodecContractID())
	if !ok {
		t.Fatal("MCP codec is missing")
	}

	desired := ClaudeProjectMCPServerProjection{
		ServerID:        "context7",
		Command:         "npx",
		Args:            []string{},
		Env:             map[string]string{},
		AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
	}
	desiredCanonical := mustCanonicalClaudeProjectMCPServerEntry(t, desired)
	observedCanonical := []byte(strings.Replace(
		string(desiredCanonical),
		`"type": "stdio"`,
		`"type": "sse"`,
		1,
	))
	if string(observedCanonical) == string(desiredCanonical) {
		t.Fatal("adapter canonical mismatch fixture did not change type")
	}

	contribution := mcpCodecContribution(t, placement, desired.ServerID, desiredCanonical)
	desiredSet := mcpCodecExclusiveSet(t, contribution)
	state, err := aggregate.NewProjectionState(contribution.Contract(), true, true, string(observedCanonical))
	if err != nil {
		t.Fatal(err)
	}
	occupancy, err := codec.ClassifyContributionOccupancy(state, desiredSet)
	if err != nil {
		t.Fatalf("ClassifyContributionOccupancy: %v", err)
	}
	subject := desiredSet.Contributions()[0].SubjectID()
	got, covered := occupancy.State(subject)
	if !covered || got != aggregate.ContributionAbsent {
		t.Fatalf("occupancy = %q covered=%t, want absent", got, covered)
	}
}
