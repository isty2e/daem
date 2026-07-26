package reconcile

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
)

func TestAggregateDecisionConstructionAndResultDefensiveAccess(t *testing.T) {
	input := validAggregateDecisionInput(t)
	decision, err := NewAggregateDecision(input)
	if err != nil {
		t.Fatal(err)
	}
	values := []AggregateDecision{decision}
	result, err := NewResult(ResultInput{Context: ContextInspect, Aggregates: values})
	if err != nil {
		t.Fatal(err)
	}
	values[0] = AggregateDecision{}
	returned := result.Aggregates()
	returned[0] = AggregateDecision{}
	if got := result.Aggregates(); len(got) != 1 || got[0].Kind() != AggregateNoOp {
		t.Fatalf("aggregate accessor mutated canonical result: %#v", got)
	}
	if _, err := NewResult(ResultInput{Context: ContextInspect, Aggregates: []AggregateDecision{decision, decision}}); err == nil ||
		!strings.Contains(err.Error(), "duplicate aggregate decision") {
		t.Fatalf("duplicate aggregate error = %v", err)
	}
}

func TestNewAggregateDecisionRejectsMalformedCanonicalInputs(t *testing.T) {
	base := validAggregateDecisionInput(t)
	tests := []struct {
		name     string
		mutate   func(AggregateDecisionInput) AggregateDecisionInput
		wantPart string
	}{
		{name: "missing variant", mutate: func(value AggregateDecisionInput) AggregateDecisionInput {
			value.Kind = ""
			return value
		}, wantPart: "unsupported variant"},
		{name: "missing document", mutate: func(value AggregateDecisionInput) AggregateDecisionInput {
			value.DocumentAddress = aggregate.DocumentAddress{}
			return value
		}, wantPart: "document:"},
		{name: "missing projections", mutate: func(value AggregateDecisionInput) AggregateDecisionInput {
			value.Projections = nil
			return value
		}, wantPart: "requires at least one projection"},
		{name: "forged projection", mutate: func(value AggregateDecisionInput) AggregateDecisionInput {
			value.Projections[0].Contract = aggregate.ProjectionContract{}
			return value
		}, wantPart: "projection[0]"},
		{name: "codec mismatch", mutate: func(value AggregateDecisionInput) AggregateDecisionInput {
			value.CodecContractID = "wrong.codec.v1"
			return value
		}, wantPart: "does not match decision codec"},
		{name: "unknown projection variant", mutate: func(value AggregateDecisionInput) AggregateDecisionInput {
			value.Projections[0].Kind = "publish"
			return value
		}, wantPart: "unsupported variant"},
		{name: "unknown subject reason", mutate: func(value AggregateDecisionInput) AggregateDecisionInput {
			value.Projections[0].Subjects[0].Reason = "perhaps"
			return value
		}, wantPart: "action reason"},
		{name: "duplicate projection", mutate: func(value AggregateDecisionInput) AggregateDecisionInput {
			value.Projections = append(value.Projections, value.Projections[0])
			return value
		}, wantPart: "duplicate projection address"},
		{name: "duplicate subject", mutate: func(value AggregateDecisionInput) AggregateDecisionInput {
			value.Projections[0].Subjects = append(value.Projections[0].Subjects, value.Projections[0].Subjects[0])
			return value
		}, wantPart: "duplicate subject"},
		{name: "no-op host mutation", mutate: func(value AggregateDecisionInput) AggregateDecisionInput {
			value.Projections[0].Subjects[0].MutatesHost = true
			return value
		}, wantPart: "cannot carry host mutation"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := base
			input.Projections = append([]AggregateProjectionDecisionInput(nil), base.Projections...)
			for index := range input.Projections {
				input.Projections[index].Subjects = append(
					[]AggregateSubjectDecisionInput(nil),
					base.Projections[index].Subjects...,
				)
			}
			_, err := NewAggregateDecision(test.mutate(input))
			if err == nil || !strings.Contains(err.Error(), test.wantPart) {
				t.Fatalf("NewAggregateDecision error = %v, want substring %q", err, test.wantPart)
			}
		})
	}
}

func validAggregateDecisionInput(t *testing.T) AggregateDecisionInput {
	return aggregateDecisionInputForServer(t, "context7")
}

func aggregateDecisionInputForServer(t *testing.T, serverID string) AggregateDecisionInput {
	t.Helper()
	locked := snapshottest.MCPProjection(t, snapshottest.MCPProjectionInput{
		PlacementID:         aggregate.MCPPlacementClaudeGlobal,
		ServerID:            serverID,
		LauncherCommand:     "npx",
		CanonicalProjection: `{"args":[],"command":"npx","type":"stdio"}`,
	})
	item, present, err := locked.ManagedAggregateContribution()
	if err != nil || !present {
		t.Fatalf("ManagedAggregateContribution = %#v, %t, %v", item, present, err)
	}
	contract := item.Contribution().Contract()
	return AggregateDecisionInput{
		Kind: AggregateNoOp, Reason: ReasonAlreadyCurrent,
		DocumentAddress: contract.Address().Document(), CodecContractID: contract.CodecContractID(),
		Projections: []AggregateProjectionDecisionInput{{
			Kind: AggregateNoOp, Reason: ReasonAlreadyCurrent, Contract: contract,
			Subjects: []AggregateSubjectDecisionInput{{
				Subject: item.SubjectID(), Contract: contract,
				Kind: AggregateNoOp, Reason: ReasonAlreadyCurrent,
			}},
		}},
	}
}
