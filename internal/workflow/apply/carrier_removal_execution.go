package apply

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/operationplan"
)

func scheduledCarrierRemovalCallWithFailureCleanup(
	execution *applyContinuationExecution,
	ref string,
	kind operationplan.EffectStepKind,
	call func() error,
	cleanup func() error,
) error {
	if execution == nil {
		callErr := call()
		if callErr == nil {
			return nil
		}
		if cleanup == nil {
			return callErr
		}
		return errors.Join(callErr, cleanup())
	}
	if err := execution.consume(ref, kind); err != nil {
		if cleanup == nil {
			return err
		}
		return errors.Join(err, cleanup())
	}
	callErr := call()
	return settleCarrierRemovalCallWithFailureCleanup(
		execution,
		ref+"/outcome",
		callErr,
		cleanup,
	)
}

func settleCarrierRemovalCallWithFailureCleanup(
	execution *applyContinuationExecution,
	outcomeRef string,
	callErr error,
	cleanup func() error,
) error {
	if callErr == nil {
		settleErr := execution.settleFailFast(outcomeRef, nil)
		if settleErr == nil || cleanup == nil {
			return settleErr
		}
		return errors.Join(settleErr, cleanup())
	}
	if err := execution.selectAlternative(outcomeRef, 1); err != nil {
		if cleanup == nil {
			return errors.Join(callErr, err)
		}
		return errors.Join(callErr, err, cleanup())
	}
	cleanupRef := outcomeRef + "/failure-cleanup"
	consumeCleanupErr := execution.consume(cleanupRef, operationplan.EffectStepCleanup)
	cleanupErr := error(nil)
	if cleanup != nil {
		cleanupErr = cleanup()
	}
	terminalErr := execution.consumeTerminal(outcomeRef + "/failure")
	return errors.Join(callErr, consumeCleanupErr, cleanupErr, terminalErr)
}

func scheduledCarrierRemovalEnsure(
	ctx context.Context,
	execution *applyContinuationExecution,
	prefix string,
	authority *statefileEffectAuthority,
	failureCleanup func() error,
) error {
	if execution == nil {
		ensureErr := authority.Ensure(ctx)
		if ensureErr != nil && failureCleanup != nil {
			return errors.Join(ensureErr, failureCleanup())
		}
		return ensureErr
	}
	if !execution.statefileBound {
		alternative := 0
		stepID := prefix + "/bind"
		kind := operationplan.EffectStepBindDescendant
		if authority.isBound() {
			alternative = 1
			stepID = prefix + "/validate-existing"
			kind = operationplan.EffectStepValidateDescendant
		}
		if err := execution.selectAlternative(prefix+"/initial-authority", alternative); err != nil {
			if failureCleanup == nil {
				return err
			}
			return errors.Join(err, failureCleanup())
		}
		if err := execution.consume(stepID, kind); err != nil {
			if failureCleanup == nil {
				return err
			}
			return errors.Join(err, failureCleanup())
		}
	} else {
		if err := execution.consume(
			prefix+"/ensure-validate",
			operationplan.EffectStepValidateDescendant,
		); err != nil {
			if failureCleanup == nil {
				return err
			}
			return errors.Join(err, failureCleanup())
		}
		if !authority.isBound() {
			ensureErr := fmt.Errorf("scheduled statefile authority is unexpectedly unbound")
			if failureCleanup == nil {
				return errors.Join(
					ensureErr,
					execution.settleFailFast(prefix+"/ensure-outcome", ensureErr),
				)
			}
			return settleCarrierRemovalCallWithFailureCleanup(
				execution,
				prefix+"/ensure-outcome",
				ensureErr,
				failureCleanup,
			)
		}
	}
	ensureErr := authority.Ensure(ctx)
	if failureCleanup == nil {
		settleErr := execution.settleFailFast(prefix+"/ensure-outcome", ensureErr)
		if ensureErr == nil && settleErr == nil {
			execution.statefileBound = true
		}
		return errors.Join(ensureErr, settleErr)
	}
	settleErr := settleCarrierRemovalCallWithFailureCleanup(
		execution,
		prefix+"/ensure-outcome",
		ensureErr,
		failureCleanup,
	)
	if ensureErr == nil && settleErr == nil {
		execution.statefileBound = true
	}
	return settleErr
}

func scheduledCarrierRemovalStatefileValidation(
	ctx context.Context,
	execution *applyContinuationExecution,
	prefix string,
	authority *statefileEffectAuthority,
	failureCleanup func() error,
) error {
	ref := prefix + "/validate"
	call := func() error { return authority.Validate(ctx) }
	if failureCleanup == nil {
		return scheduledContinuationCall(
			execution,
			ref,
			operationplan.EffectStepValidateDescendant,
			call,
		)
	}
	return scheduledCarrierRemovalCallWithFailureCleanup(
		execution,
		ref,
		operationplan.EffectStepValidateDescendant,
		call,
		failureCleanup,
	)
}

func scheduledCarrierRemovalStatefilePublication(
	execution *applyContinuationExecution,
	prefix string,
	publish func() error,
	failureCleanup func() error,
) error {
	ref := prefix + "/publish"
	if failureCleanup == nil {
		return scheduledContinuationCall(
			execution,
			ref,
			operationplan.EffectStepPublishDescendant,
			publish,
		)
	}
	return scheduledCarrierRemovalCallWithFailureCleanup(
		execution,
		ref,
		operationplan.EffectStepPublishDescendant,
		publish,
		failureCleanup,
	)
}

func scheduledCarrierRemovalForward(
	execution *applyContinuationExecution,
	ref string,
	call func() error,
	failureCleanup func() error,
) error {
	if failureCleanup == nil {
		return scheduledContinuationCall(
			execution,
			ref,
			operationplan.EffectStepForwardEffect,
			call,
		)
	}
	return scheduledCarrierRemovalCallWithFailureCleanup(
		execution,
		ref,
		operationplan.EffectStepForwardEffect,
		call,
		failureCleanup,
	)
}
