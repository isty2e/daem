package recoverygate

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/operationplan"
)

// ForwardEffectExecution couples one reserved State Barrier authority to the
// exact immutable owner schedule that justified the reservation. It validates
// structure only; callers retain all effect outcome and successor semantics.
type ForwardEffectExecution struct {
	cursor    *operationplan.EffectCursor
	authority *ForwardEffectAuthority
	settled   bool
	closed    bool
}

// ReserveForwardEffectExecution reserves the branch-aware structure and
// returns its operation-local ordered consumption authority.
func (authority EffectAuthority) ReserveForwardEffectExecution(
	structure operationplan.EffectStructure,
	legacyDemand operationplan.Demand,
) (*ForwardEffectExecution, error) {
	forward, err := authority.reserveForwardEffectStructure(structure, legacyDemand)
	if err != nil {
		return nil, err
	}
	return &ForwardEffectExecution{
		cursor:    structure.Begin(),
		authority: forward,
	}, nil
}

// SelectAlternative selects the pending owner-declared closed branch.
func (execution *ForwardEffectExecution) SelectAlternative(
	choiceID string,
	alternative int,
) error {
	if err := execution.requireActive(); err != nil {
		return err
	}
	return execution.cursor.SelectAlternative(choiceID, alternative)
}

// ValidateBarrier consumes the exact barrier checkpoint before performing it.
func (execution *ForwardEffectExecution) ValidateBarrier(
	ctx context.Context,
	stepID string,
) error {
	if err := execution.consume(stepID, operationplan.EffectStepValidateBarrier); err != nil {
		return err
	}
	return execution.authority.Validate(ctx)
}

// ValidateStateDir consumes the exact retained-incarnation checkpoint before
// performing it.
func (execution *ForwardEffectExecution) ValidateStateDir(
	ctx context.Context,
	stepID string,
) error {
	if err := execution.consume(stepID, operationplan.EffectStepValidateStateDir); err != nil {
		return err
	}
	return execution.authority.ValidateStateDir(ctx)
}

// EstablishStateDir consumes an explicit first-incarnation checkpoint before
// performing the complete peer and recovery-barrier protocol.
func (execution *ForwardEffectExecution) EstablishStateDir(
	ctx context.Context,
	stepID string,
	validatePeer func(context.Context) error,
) (bool, error) {
	if err := execution.consume(stepID, operationplan.EffectStepEstablishStateDir); err != nil {
		return false, err
	}
	return execution.authority.EnsureStateDirForEffect(ctx, validatePeer)
}

// ConsumeForwardEffect consumes one phase-relative forward obligation and
// performs either first-incarnation establishment or later validation.
func (execution *ForwardEffectExecution) ConsumeForwardEffect(
	ctx context.Context,
	stepID string,
	validatePeer func(context.Context) error,
) (bool, error) {
	if err := execution.requireActive(); err != nil {
		return false, err
	}
	checkpoint, err := execution.cursor.ConsumeForwardEffect(stepID)
	if err != nil {
		return false, err
	}
	switch checkpoint {
	case operationplan.ForwardEffectEstablishStateDir:
		return execution.authority.EnsureStateDirForEffect(ctx, validatePeer)
	case operationplan.ForwardEffectValidateStateDir:
		return false, execution.authority.ValidateStateDir(ctx)
	default:
		return false, fmt.Errorf(
			"forward effect %q has invalid checkpoint %d",
			stepID,
			checkpoint,
		)
	}
}

// ConsumeDescendantValidation authorizes one exact descendant identity check.
// The caller immediately performs the check through its retained descendant
// authority.
func (execution *ForwardEffectExecution) ConsumeDescendantValidation(
	stepID string,
) error {
	return execution.consume(stepID, operationplan.EffectStepValidateDescendant)
}

// ConsumeDescendantPublication authorizes one exact descendant publication.
// The caller immediately performs the publication through its retained entry
// capability.
func (execution *ForwardEffectExecution) ConsumeDescendantPublication(
	stepID string,
) error {
	return execution.consume(stepID, operationplan.EffectStepPublishDescendant)
}

// ConsumeLifecycle consumes one owner-local non-State-Barrier lifecycle step.
func (execution *ForwardEffectExecution) ConsumeLifecycle(
	stepID string,
	kind operationplan.EffectStepKind,
) error {
	switch kind {
	case operationplan.EffectStepNoOp,
		operationplan.EffectStepExternal,
		operationplan.EffectStepObservation,
		operationplan.EffectStepPersistence,
		operationplan.EffectStepCompensation,
		operationplan.EffectStepCleanup,
		operationplan.EffectStepRetirement,
		operationplan.EffectStepTerminal:
		return execution.consume(stepID, kind)
	default:
		return fmt.Errorf(
			"effect step %q/%d requires a typed State Barrier checkpoint",
			stepID,
			kind,
		)
	}
}

// TakeDescendant transfers the transitional scalar descendant reservation.
func (execution *ForwardEffectExecution) TakeDescendant() (
	*StateDirDescendantReservation,
	error,
) {
	if err := execution.requireActive(); err != nil {
		return nil, err
	}
	return execution.authority.TakeDescendant()
}

// Finish requires the selected structural path to be fully consumed. It does
// not interpret the owning operation's semantic result.
func (execution *ForwardEffectExecution) Finish() error {
	if err := execution.requireActive(); err != nil {
		return err
	}
	if err := execution.cursor.FinishSuccess(); err != nil {
		return err
	}
	execution.settled = true
	return nil
}

// Close aborts an incomplete pre-effect path and rejects any incomplete path
// after an effect boundary. Completed paths close idempotently.
func (execution *ForwardEffectExecution) Close() error {
	if execution == nil || execution.closed {
		return nil
	}
	execution.closed = true
	if execution.settled {
		return nil
	}
	if err := execution.cursor.AbortBeforeEffect(); err != nil {
		return fmt.Errorf("forward effect execution did not settle: %w", err)
	}
	execution.settled = true
	return nil
}

func (execution *ForwardEffectExecution) consume(
	stepID string,
	kind operationplan.EffectStepKind,
) error {
	if err := execution.requireActive(); err != nil {
		return err
	}
	return execution.cursor.Consume(stepID, kind)
}

func (execution *ForwardEffectExecution) requireActive() error {
	if execution == nil || execution.cursor == nil || execution.authority == nil {
		return fmt.Errorf("forward effect execution is required")
	}
	if execution.closed {
		return fmt.Errorf("forward effect execution is closed")
	}
	if execution.settled {
		return fmt.Errorf("forward effect execution is already settled")
	}
	return nil
}
