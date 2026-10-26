package reconcile

import (
	"fmt"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
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
	before, err := decision.snapshot.Selection()
	if err != nil {
		return err
	}
	return decision.nativeOperation.ValidateSelection(before)
}
