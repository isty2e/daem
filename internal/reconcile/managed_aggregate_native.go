package reconcile

import (
	"fmt"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/topology"
)

func (decision AggregateDecision) NativeOperationContract() (profile.NativeAggregateOperationContract, bool) {
	return decision.nativeOperation, decision.hasNativeOperation
}

func validateNativeAggregateOperation(decision AggregateDecision) error {
	_, native := aggregate.HostConfigSyntaxForCodec(decision.codecContractID)
	if !native {
		if decision.hasNativeOperation {
			return fmt.Errorf("ordinary aggregate decision carries a native operation contract")
		}
		return nil
	}
	if !decision.hasNativeOperation {
		if decision.kind == AggregateBlocked {
			return nil
		}
		return fmt.Errorf("native aggregate decision requires its selected operation contract")
	}
	if len(decision.preconditions) != 0 {
		return fmt.Errorf("native observation requirements are not fixed aggregate preconditions")
	}
	selection, err := aggregate.NewSelection(decision.Contracts())
	if err != nil {
		return err
	}
	if err := decision.nativeOperation.ValidateSelection(selection); err != nil {
		return err
	}
	if decision.kind == AggregateBlocked {
		return nil
	}
	for index, projection := range decision.projections {
		if err := validateNativeAggregateSubjects(projection); err != nil {
			return fmt.Errorf("native projection[%d]: %w", index, err)
		}
	}
	before, err := decision.snapshot.Selection()
	if err != nil {
		return err
	}
	return decision.nativeOperation.ValidateSelection(before)
}

func validateNativeAggregateSubjects(projection AggregateProjectionDecision) error {
	subjects := make(map[topology.SubjectID]struct{})
	previous := make(map[topology.SubjectID]aggregate.ManagedContribution, len(projection.previous))
	for _, state := range projection.previous {
		contribution := state.Contribution()
		if !contribution.Contract().Equal(projection.contract) {
			return fmt.Errorf("previous contribution differs from projection contract")
		}
		if err := aggregate.ValidateSubjectContract(state.Subject(), projection.contract); err != nil {
			return err
		}
		if _, duplicate := previous[state.Subject()]; duplicate {
			return fmt.Errorf("native projection repeats previous subject")
		}
		previous[state.Subject()] = contribution
		subjects[state.Subject()] = struct{}{}
	}
	if projection.desired != nil {
		if !projection.desired.Contract().Equal(projection.contract) {
			return fmt.Errorf("desired contribution differs from projection contract")
		}
		for _, item := range projection.desired.Contributions() {
			if err := aggregate.ValidateSubjectContract(item.SubjectID(), projection.contract); err != nil {
				return err
			}
			subjects[item.SubjectID()] = struct{}{}
		}
	}

	if len(subjects) == 0 || len(projection.deltas) != len(subjects) {
		return fmt.Errorf("native deltas do not cover exact contribution subjects")
	}
	for _, delta := range projection.deltas {
		if err := aggregate.ValidateSubjectContract(delta.subject, projection.contract); err != nil {
			return err
		}
		if _, selected := subjects[delta.subject]; !selected {
			return fmt.Errorf("native delta subject has no desired or previous contribution")
		}
		prior, present := previous[delta.subject]
		if present != delta.hasPrevious || present && !prior.Equal(delta.previous) {
			return fmt.Errorf("native delta differs from its previous contribution")
		}
	}
	return nil
}
