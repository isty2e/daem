package apply

import (
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/reconcile"
	projectionbuild "github.com/isty2e/daem/internal/reconcile/build/projection"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func nativeDecisionConstructionInput(decision reconcile.AggregateDecision) reconcile.AggregateDecisionInput {
	operation, _ := decision.NativeOperationContract()
	input := reconcile.AggregateDecisionInput{
		Kind: decision.Kind(), Reason: decision.Reason(), Detail: decision.Detail(),
		DocumentAddress: decision.DocumentAddress(), CodecContractID: decision.CodecContractID(),
		Document: decision.BeforeDocument(), Snapshot: decision.BeforeSnapshot(),
		CodecPlan: decision.CodecPlan(), Rendered: decision.Rendered(), Evidence: decision.Evidence(),
		NativeOperation: &operation,
	}
	for _, projection := range decision.Projections() {
		value := reconcile.AggregateProjectionDecisionInput{
			Kind: projection.Kind(), Reason: projection.Reason(), Detail: projection.Detail(),
			Contract: projection.Contract(), Previous: projection.PreviousStates(),
			Before: projection.Before(), Expected: projection.Expected(),
		}
		if desired, present := projection.Desired(); present {
			value.Desired = &desired
		}
		for _, delta := range projection.SubjectDecisions() {
			subject := reconcile.AggregateSubjectDecisionInput{
				Subject: delta.Subject(), Contract: delta.Contract(), Kind: delta.Kind(),
				Reason: delta.Reason(), Detail: delta.Detail(), MutatesHost: delta.MutatesHost(),
			}
			if previous, present := delta.PreviousContribution(); present {
				subject.Previous = &previous
			}
			if occupancy, present := delta.ContributionOccupancy(); present {
				subject.Occupancy = occupancy
			}
			value.Subjects = append(value.Subjects, subject)
		}
		input.Projections = append(input.Projections, value)
	}
	return input
}

func TestNativeOperationDecisionBindsExactContributionSubjects(t *testing.T) {
	for _, kind := range []reconcile.AggregateDecisionKind{
		reconcile.AggregateCreate, reconcile.AggregateReplace, reconcile.AggregateRecord,
		reconcile.AggregateNoOp, reconcile.AggregateRemove,
	} {
		t.Run(string(kind), func(t *testing.T) {
			planning, _ := nativeAggregatePlanningInput(t, kind)
			decisions, err := projectionbuild.BuildAggregateDecisions(planning)
			if err != nil || len(decisions) != 1 {
				t.Fatal(err)
			}
			input := nativeDecisionConstructionInput(decisions[0])
			if _, err := reconcile.NewAggregateDecision(input); err != nil {
				t.Fatal("actual source-produced transition was refused", err)
			}
			contract := input.Projections[0].Contract
			owner, err := entity.New(entity.KindHostConfig, "another-config")
			if err != nil {
				t.Fatal(err)
			}
			other, err := topologyprojection.HostConfigKeySubject(owner, contract.Address().Document().Target(), contract.Address().Document().Scope(), contract.Address().KeyPath())
			if err != nil {
				t.Fatal(err)
			}
			if err := aggregate.ValidateSubjectContract(other, contract); err != nil {
				t.Fatal("control subject is not valid for the same physical key", err)
			}
			for _, mutation := range []struct {
				name   string
				change func(*reconcile.AggregateProjectionDecisionInput)
			}{
				{"other subject at same key", func(value *reconcile.AggregateProjectionDecisionInput) {
					value.Subjects[0].Subject = other
				}},
				{"no delta coverage", func(value *reconcile.AggregateProjectionDecisionInput) {
					value.Subjects = nil
				}},
				{"wrong prior presence", func(value *reconcile.AggregateProjectionDecisionInput) {
					if value.Subjects[0].Previous != nil {
						value.Subjects[0].Previous = nil
					} else {
						previous := value.Desired.Contributions()[0].Contribution()
						value.Subjects[0].Previous = &previous
					}
				}},
			} {
				t.Run(mutation.name, func(t *testing.T) {
					changed := nativeDecisionConstructionInput(decisions[0])
					mutation.change(&changed.Projections[0])
					if _, err := reconcile.NewAggregateDecision(changed); err == nil {
						t.Fatal("native decision accepted a delta unrelated to its actual contribution")
					}
				})
			}

			if kind == reconcile.AggregateReplace {
				changed := nativeDecisionConstructionInput(decisions[0])
				wrong := changed.Projections[0].Desired.Contributions()[0].Contribution()
				changed.Projections[0].Subjects[0].Previous = &wrong
				if _, err := reconcile.NewAggregateDecision(changed); err == nil {
					t.Fatal("native delta accepted another value as its actual prior contribution")
				}
			}
			if kind == reconcile.AggregateRecord {
				changed := nativeDecisionConstructionInput(decisions[0])
				item, err := aggregate.NewSubjectContribution(other, changed.Projections[0].Desired.Contributions()[0].Contribution())
				if err != nil {
					t.Fatal(err)
				}
				desired, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{item})
				if err != nil {
					t.Fatal(err)
				}
				changed.Projections[0].Desired = &desired
				if _, err := reconcile.NewAggregateDecision(changed); err == nil {
					t.Fatal("native decision accepted desired membership not covered by its delta")
				}
			}
		})
	}
}
