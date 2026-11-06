package preparation_test

import (
	"testing"

	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/test/testkit/aggregatefixture"
)

func TestAggregateComparisonPreservesSelectedTransition(t *testing.T) {
	planned := mcpSameMutationEffect(t, aggregate.AbsentDocument(), "node")
	for _, document := range []string{`{}`, `{"foreign":true}`} {
		fresh := mcpSameMutationEffect(t, aggregate.ExistingDocument([]byte(document)), "node")
		if !effectpreparation.AggregateEffect.SameMutation(planned, fresh) {
			t.Fatal("unselected parent construction changed the selected MCP transition")
		}
	}
	changedDesired := mcpSameMutationEffect(t, aggregate.AbsentDocument(), "other")
	if effectpreparation.AggregateEffect.SameMutation(planned, changedDesired) {
		t.Fatal("suffix comparison ignored changed desired contributions")
	}
}

func mcpSameMutationEffect(t *testing.T, before aggregate.Document, command string) effectpreparation.AggregateEffect {
	t.Helper()
	decisions := aggregatefixture.ClaudeProjectMCP(t, before, []aggregatefixture.MCPEntry{{ServerID: "search", Command: command}})
	effects, err := effectpreparation.AggregateEffects(decisions)
	if err != nil || len(effects) != 1 {
		t.Fatal("MCP fixture has no selected aggregate effect", err)
	}
	return effects[0]
}
