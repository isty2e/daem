package mcp

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestMCPFailureIndexCorrelatesCanonicalDiagnosticLocation(t *testing.T) {
	placement, admitted := aggregate.MCPPlacementForID(aggregate.MCPPlacementClaudeProject)
	if !admitted {
		t.Fatal("Claude project MCP placement was not admitted")
	}
	first, err := placement.ProjectionContract("first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := placement.ProjectionContract("second")
	if err != nil {
		t.Fatal(err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{first, second})
	if err != nil {
		t.Fatal(err)
	}
	emptyProperty, err := output.NewKeyPath("")
	if err != nil {
		t.Fatal(err)
	}
	literalSlash, err := output.NewKeyPath("mcpServers/first")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		location output.KeyPath
		first    aggregate.CodecFailureReason
		second   aggregate.CodecFailureReason
	}{
		{output.KeyPath{}, aggregate.CodecFailureDuplicateKey, aggregate.CodecFailureDuplicateKey},
		{first.Address().KeyPath(), aggregate.CodecFailureDuplicateKey, aggregate.CodecFailureEquivalenceUndefined},
		{second.Address().KeyPath(), aggregate.CodecFailureEquivalenceUndefined, aggregate.CodecFailureDuplicateKey},
		{emptyProperty, aggregate.CodecFailureEquivalenceUndefined, aggregate.CodecFailureEquivalenceUndefined},
		{literalSlash, aggregate.CodecFailureEquivalenceUndefined, aggregate.CodecFailureEquivalenceUndefined},
	} {
		failure, err := aggregate.NewCodecFailure(aggregate.CodecFailureDuplicateKey, test.location)
		if err != nil {
			t.Fatal(err)
		}
		observed, err := observe.NewAggregateObservationFailure(aggregate.ExistingDocument([]byte(`{}`)), selection, 0o600, failure)
		if err != nil {
			t.Fatal(err)
		}
		index, err := newAggregateObservationIndex(nil, []observe.AggregateObservationFailure{observed}, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, expected := range []struct {
			contract aggregate.ProjectionContract
			reason   aggregate.CodecFailureReason
		}{{first, test.first}, {second, test.second}} {
			projection, err := index.projection(expected.contract)
			if err != nil || projection.failureReason != expected.reason || projection.observed {
				t.Fatalf("diagnostic correlated with a different selected value: %#v, %v", projection, err)
			}
		}
	}
}
