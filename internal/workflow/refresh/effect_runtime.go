package refresh

import (
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/recoverygate"
)

func continueRefreshEffect(
	execution *recoverygate.ForwardEffectExecution,
	choiceID string,
	proceed bool,
	failureTerminalID string,
) (bool, error) {
	alternative := 0
	if proceed {
		alternative = 1
	}
	if err := execution.SelectAlternative(choiceID, alternative); err != nil {
		return false, err
	}
	if proceed {
		return true, nil
	}
	return false, finishRefreshEffect(execution, failureTerminalID)
}

func finishRefreshEffect(
	execution *recoverygate.ForwardEffectExecution,
	terminalID string,
) error {
	if err := execution.ConsumeLifecycle(
		terminalID,
		operationplan.EffectStepTerminal,
	); err != nil {
		return err
	}
	return execution.Finish()
}

func refreshScheduleFailure(
	result CommandResult,
	attemptStarted bool,
	err error,
) (CommandResult, error) {
	return resultWithCleanupFailure(result, attemptStarted), fmt.Errorf(
		"consume refresh effect schedule: %w",
		err,
	)
}

func joinRefreshPersistenceFailure(cause error, scheduleErr error) error {
	if scheduleErr == nil {
		return cause
	}
	return errors.Join(cause, fmt.Errorf("settle refresh effect schedule: %w", scheduleErr))
}
