package hostmutation

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestRecoveryHostActionsRequireAggregateContractForContentPath(t *testing.T) {
	_, err := prepareRecoveryHostExecutionPlan(nil, []RestorationAction{{
		Kind: recovery.ActionKindRestoreDelete, ContentPath: "/mcpServers/context7",
	}})
	if err == nil || !strings.Contains(err.Error(), "has no aggregate contract") {
		t.Fatalf("error = %v, want missing aggregate contract rejection", err)
	}
}

func TestRecoveryHostActionsRejectMismatchedAggregateContract(t *testing.T) {
	contract := restorationMCPContractForTest(t, "context7")
	valid := RestorationAction{
		Kind: recovery.ActionKindRestoreDelete, Scope: target.ScopeProject,
		Destination:       contract.Address().Document().AggregateRoot().String(),
		ContentPath:       string(outputtest.LegacyContentPath(t, contract.Address())),
		AggregateContract: &contract,
	}
	for _, test := range []struct {
		name string
		edit func(*RestorationAction)
		want string
	}{
		{name: "scope", edit: func(action *RestorationAction) { action.Scope = target.ScopeGlobal }, want: "scope"},
		{name: "destination", edit: func(action *RestorationAction) { action.Destination = "other.json" }, want: "aggregate address"},
		{name: "content path", edit: func(action *RestorationAction) { action.ContentPath = "/mcpServers/other" }, want: "aggregate address"},
	} {
		t.Run(test.name, func(t *testing.T) {
			action := valid
			test.edit(&action)
			_, err := prepareRecoveryHostExecutionPlan(nil, []RestorationAction{action})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("prepareRecoveryHostExecutionPlan error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestRecoveryStageUsesAggregateCodecDocumentLimit(t *testing.T) {
	t.Parallel()
	contract := restorationMCPContractForTest(t, "context7")
	codecs := testAggregateCodecs()
	codec, ok := codecs.Lookup(contract.CodecContractID())
	if !ok {
		t.Fatalf("aggregate codec %q is missing", contract.CodecContractID())
	}
	maximumBytes, err := recoveryStageMaximumFileBytes([]rollbackStageAction{{action: RestorationAction{
		Scope:       target.ScopeProject,
		Destination: contract.Address().Document().AggregateRoot().String(),
		ContentPath: string(outputtest.LegacyContentPath(t, contract.Address())), AggregateContract: &contract,
	}}}, codecs)
	if err != nil {
		t.Fatal(err)
	}
	if maximumBytes != codec.MaximumDocumentBytes() {
		t.Fatalf("recovery stage maximum bytes = %d, want codec limit %d", maximumBytes, codec.MaximumDocumentBytes())
	}
}

func TestRecoveryStageRejectsMixedAggregateAndWholePathActions(t *testing.T) {
	t.Parallel()
	contract := restorationMCPContractForTest(t, "context7")
	actions := []rollbackStageAction{
		{action: RestorationAction{
			Scope: target.ScopeProject, Destination: contract.Address().Document().AggregateRoot().String(),
			ContentPath: string(outputtest.LegacyContentPath(t, contract.Address())), AggregateContract: &contract,
		}},
		{action: RestorationAction{Scope: target.ScopeProject, Destination: ".agents/skills/example"}},
	}
	if _, err := recoveryStageMaximumFileBytes(actions, testAggregateCodecs()); err == nil ||
		!strings.Contains(err.Error(), "mixes aggregate and whole-path") {
		t.Fatalf("recoveryStageMaximumFileBytes error = %v, want mixed-mode rejection", err)
	}
}

func restorationMCPContractForTest(t *testing.T, serverID string) aggregate.ProjectionContract {
	t.Helper()
	placement, ok := aggregate.ImplementedMCPPlacement(target.TargetClaudeCode, target.ScopeProject)
	if !ok {
		t.Fatal("Claude project MCP placement is missing")
	}
	contract, err := placement.ProjectionContract(serverID)
	if err != nil {
		t.Fatal(err)
	}
	return contract
}
