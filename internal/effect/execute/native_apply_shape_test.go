package execute

import (
	"testing"

	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestNativeSuffixAggregateComparisonPreservesSelectedTransition(t *testing.T) {
	specs := []mcpEffectSpec{{serverID: "search", command: "node"}}
	planned := managedMCPAggregateEffects(t, aggregate.AbsentDocument(), specs)[0]
	for _, document := range []string{`{}`, `{"foreign":true}`} {
		fresh := managedMCPAggregateEffects(t, aggregate.ExistingDocument([]byte(document)), specs)[0]
		if !effectpreparation.AggregateEffect.SameMutation(planned, fresh) {
			t.Fatal("unselected parent construction changed the selected MCP transition")
		}
	}
	changedDesired := managedMCPAggregateEffects(t, aggregate.AbsentDocument(), []mcpEffectSpec{{serverID: "search", command: "other"}})[0]
	if effectpreparation.AggregateEffect.SameMutation(planned, changedDesired) {
		t.Fatal("suffix comparison ignored changed desired contributions")
	}
}
