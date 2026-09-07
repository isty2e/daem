package recoverygate

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/operationplan"
	daempaths "github.com/isty2e/daem/internal/paths"
)

// ForwardEffectExecution couples one reserved State Barrier authority to the
// exact immutable owner schedule that justified the reservation. It validates
// structure only; callers retain all effect outcome and successor semantics.
type ForwardEffectExecution struct {
	cursor                *operationplan.EffectCursor
	paths                 daempaths.Paths
	stateDir              *StateDirExecutionAuthority
	descendantReservation *StateDirDescendantReservation
	descendant            *StateDirDescendantAuthority
	settled               bool
	closed                bool
}

// ReserveForwardEffectExecution reserves the branch-aware structure and
// returns its operation-local ordered cursor over raw physical authority. The
// exact structure supplies semantic counts; descendantPath is the separate
// selected physical binding lowered by the State Barrier.
func (authority EffectAuthority) ReserveForwardEffectExecution(
	structure operationplan.EffectStructure,
	descendantPath string,
) (*ForwardEffectExecution, error) {
	planned, err := authority.prepareForwardEffectExecution(structure, descendantPath)
	if err != nil {
		return nil, err
	}
	reservation, err := authority.stateDir.reservePlannedOperation(planned)
	if err != nil {
		return nil, err
	}
	var descendantReservation *StateDirDescendantReservation
	if planned.hasDescendant {
		descendantReservation, err = reservation.TakeDescendant()
		if err != nil {
			return nil, err
		}
	}
	return &ForwardEffectExecution{
		cursor:                structure.Begin(),
		paths:                 authority.paths,
		stateDir:              reservation.Execution(),
		descendantReservation: descendantReservation,
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
	return validateBarrier(ctx, execution.paths, execution.stateDir)
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
	return normalizeStateDirValidation(execution.stateDir.Validate(ctx))
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
	return ensureReservedStateDirForEffect(
		ctx,
		execution.paths,
		execution.stateDir,
		validatePeer,
	)
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
		return ensureReservedStateDirForEffect(
			ctx,
			execution.paths,
			execution.stateDir,
			validatePeer,
		)
	case operationplan.ForwardEffectValidateStateDir:
		return false, normalizeStateDirValidation(execution.stateDir.Validate(ctx))
	default:
		return false, fmt.Errorf(
			"forward effect %q has invalid checkpoint %d",
			stepID,
			checkpoint,
		)
	}
}

// BindDescendant consumes the exact binding checkpoint before acquiring the
// one reserved descendant authority.
func (execution *ForwardEffectExecution) BindDescendant(
	ctx context.Context,
	stepID string,
) error {
	if err := execution.consume(stepID, operationplan.EffectStepBindDescendant); err != nil {
		return err
	}
	if execution.descendant != nil {
		return fmt.Errorf("forward effect descendant authority is already bound")
	}
	if execution.descendantReservation == nil {
		return fmt.Errorf("forward effect descendant reservation is unavailable")
	}
	reservation := execution.descendantReservation
	execution.descendantReservation = nil
	descendant, err := reservation.Bind(ctx)
	if err != nil {
		return err
	}
	execution.descendant = descendant
	return nil
}

// ValidateDescendant consumes the exact identity checkpoint before validating
// the bound descendant authority.
func (execution *ForwardEffectExecution) ValidateDescendant(
	ctx context.Context,
	stepID string,
) error {
	if err := execution.consume(stepID, operationplan.EffectStepValidateDescendant); err != nil {
		return err
	}
	if execution.descendant == nil {
		return fmt.Errorf("forward effect descendant authority is not bound")
	}
	return execution.descendant.Validate(ctx)
}

// PublishDescendant consumes the exact publication checkpoint before lending
// the bound entry capability to one immediate owner-local publication.
func (execution *ForwardEffectExecution) PublishDescendant(
	stepID string,
	publish func(*rootedpath.EntryAuthority) error,
) error {
	if publish == nil {
		return fmt.Errorf("forward effect descendant publication is required")
	}
	if err := execution.consume(stepID, operationplan.EffectStepPublishDescendant); err != nil {
		return err
	}
	if execution.descendant == nil {
		return fmt.Errorf("forward effect descendant authority is not bound")
	}
	entry := execution.descendant.Entry()
	if entry == nil {
		return fmt.Errorf("forward effect descendant entry authority is unavailable")
	}
	return publish(entry)
}

// CloseDescendant releases the bound descendant authority exactly once.
func (execution *ForwardEffectExecution) CloseDescendant() error {
	if execution == nil || execution.descendant == nil {
		return nil
	}
	descendant := execution.descendant
	execution.descendant = nil
	return descendant.Close()
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

// Finish requires the selected structural path to be fully consumed. It does
// not interpret the owning operation's semantic result.
func (execution *ForwardEffectExecution) Finish() error {
	if err := execution.requireActive(); err != nil {
		return err
	}
	if execution.descendant != nil {
		return fmt.Errorf("forward effect descendant authority remains open")
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
	descendantErr := execution.CloseDescendant()
	if execution.settled {
		return descendantErr
	}
	if err := execution.cursor.AbortBeforeEffect(); err != nil {
		return errors.Join(
			descendantErr,
			fmt.Errorf("forward effect execution did not settle: %w", err),
		)
	}
	execution.settled = true
	return descendantErr
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
	if execution == nil || execution.cursor == nil || execution.stateDir == nil {
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
