package journal

import "errors"

// RetirementExecutionStep is the closed validation and transition vocabulary
// exposed by journal retirement execution.
type RetirementExecutionStep uint8

const (
	RetirementStepValidateCleanupAuthority RetirementExecutionStep = iota + 1
	RetirementStepValidatePreparedLayout
	RetirementStepValidatePhaseAdvanceLayout
	RetirementStepAdvanceRecord
	RetirementStepValidateFinalizingLayout
	RetirementStepCleanupResidue
	RetirementStepRetireControl
	RetirementStepCleanupGarbage
	RetirementStepValidateActivePlan
	RetirementStepValidateActiveIdentity
	RetirementStepControlPresent
	RetirementStepPublishControl
	RetirementStepValidateActiveRecord
	RetirementStepRetireActiveJournal
)

// RetirementStepGate admits and settles one exact journal-retirement step.
// It grants no filesystem authority and does not interpret journal outcomes.
type RetirementStepGate interface {
	AdmitRetirementStep(RetirementExecutionStep) error
	SettleRetirementStep(RetirementExecutionStep, bool) error
}

func executeRetirementStep(
	gate RetirementStepGate,
	step RetirementExecutionStep,
	action func() error,
) error {
	if gate != nil {
		if err := gate.AdmitRetirementStep(step); err != nil {
			return err
		}
	}
	actionErr := action()
	if gate == nil {
		return actionErr
	}
	settlementErr := gate.SettleRetirementStep(step, actionErr == nil)
	if actionErr == nil {
		return settlementErr
	}
	if settlementErr == nil {
		return actionErr
	}
	return errors.Join(actionErr, settlementErr)
}
