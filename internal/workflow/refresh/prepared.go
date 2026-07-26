package refresh

import (
	"errors"
	"fmt"
	"sync"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

var (
	ErrPreparedCommandUnavailable = errors.New("prepared refresh command is unavailable")
	ErrPreparedCommandClosed      = errors.New("prepared refresh command is closed")
	ErrPreparedCommandConsumed    = errors.New("prepared refresh command is already consumed")
)

type preparedCommandState uint8

const (
	preparedCommandUnavailable preparedCommandState = iota
	preparedCommandReady
	preparedCommandClosed
	preparedCommandConsumed
)

// PreparedCommand owns one exact disclosed refresh operation and the retained
// selected-root witness needed to revalidate it. Value copies share one
// single-use lifecycle.
type PreparedCommand struct {
	disclosure CommandResult
	lifecycle  *preparedCommandLifecycle
}

type preparedCommandLifecycle struct {
	mu      sync.Mutex
	state   preparedCommandState
	planned plan
	input   CommandInput
	options PlanOptions
	root    *rootedpath.CapturedRoot
}

type preparedExecution struct {
	planned plan
	input   CommandInput
	options PlanOptions
	root    *rootedpath.CapturedRoot
}

func unavailablePreparedCommand(result CommandResult) *PreparedCommand {
	return &PreparedCommand{
		disclosure: cloneCommandResult(result),
		lifecycle: &preparedCommandLifecycle{
			state: preparedCommandUnavailable,
		},
	}
}

func newPreparedCommand(
	planned plan,
	input CommandInput,
	options PlanOptions,
	root *rootedpath.CapturedRoot,
) *PreparedCommand {
	return &PreparedCommand{
		disclosure: cloneCommandResult(planned.result),
		lifecycle: &preparedCommandLifecycle{
			state:   preparedCommandReady,
			planned: planned,
			input:   input,
			options: options,
			root:    root,
		},
	}
}

// Disclosure returns a defensive presentation-only snapshot.
func (prepared *PreparedCommand) Disclosure() CommandResult {
	if prepared == nil {
		return CommandResult{}
	}
	return cloneCommandResult(prepared.disclosure)
}

func (prepared *PreparedCommand) beginExecution() (preparedExecution, error) {
	if prepared == nil || prepared.lifecycle == nil {
		return preparedExecution{}, ErrPreparedCommandUnavailable
	}

	lifecycle := prepared.lifecycle
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()

	switch lifecycle.state {
	case preparedCommandReady:
		execution := preparedExecution{
			planned: lifecycle.planned,
			input:   lifecycle.input,
			options: lifecycle.options,
			root:    lifecycle.root,
		}
		lifecycle.state = preparedCommandConsumed
		lifecycle.planned = plan{}
		lifecycle.input = CommandInput{}
		lifecycle.options = PlanOptions{}
		lifecycle.root = nil
		return execution, nil
	case preparedCommandClosed:
		return preparedExecution{}, ErrPreparedCommandClosed
	case preparedCommandConsumed:
		return preparedExecution{}, ErrPreparedCommandConsumed
	default:
		return preparedExecution{}, ErrPreparedCommandUnavailable
	}
}

// Close releases an unconsumed prepared refresh command. Once execution
// begins, the executor owns the transferred root witness.
func (prepared *PreparedCommand) Close() error {
	if prepared == nil || prepared.lifecycle == nil {
		return nil
	}

	lifecycle := prepared.lifecycle
	lifecycle.mu.Lock()
	if lifecycle.state != preparedCommandReady {
		lifecycle.mu.Unlock()
		return nil
	}
	root := lifecycle.root
	lifecycle.state = preparedCommandClosed
	lifecycle.planned = plan{}
	lifecycle.input = CommandInput{}
	lifecycle.options = PlanOptions{}
	lifecycle.root = nil
	lifecycle.mu.Unlock()

	if root == nil {
		return nil
	}
	if err := root.Close(); err != nil {
		return fmt.Errorf("close refresh project-root witness: %w", err)
	}
	return nil
}

func (prepared *PreparedCommand) cancel() error {
	if prepared == nil || prepared.lifecycle == nil {
		return ErrPreparedCommandUnavailable
	}

	lifecycle := prepared.lifecycle
	lifecycle.mu.Lock()
	switch lifecycle.state {
	case preparedCommandReady:
		root := lifecycle.root
		lifecycle.state = preparedCommandClosed
		lifecycle.planned = plan{}
		lifecycle.input = CommandInput{}
		lifecycle.options = PlanOptions{}
		lifecycle.root = nil
		lifecycle.mu.Unlock()
		if root == nil {
			return nil
		}
		if err := root.Close(); err != nil {
			return fmt.Errorf("close refresh project-root witness: %w", err)
		}
		return nil
	case preparedCommandClosed:
		lifecycle.mu.Unlock()
		return ErrPreparedCommandClosed
	case preparedCommandConsumed:
		lifecycle.mu.Unlock()
		return ErrPreparedCommandConsumed
	default:
		lifecycle.mu.Unlock()
		return ErrPreparedCommandUnavailable
	}
}

// Cancel invalidates one unconsumed plan after operator decline and returns
// the bounded pre-attempt result.
func Cancel(prepared *PreparedCommand) (CommandResult, error) {
	if prepared == nil {
		return CommandResult{}, ErrPreparedCommandUnavailable
	}
	result := prepared.Disclosure()
	if err := prepared.cancel(); err != nil {
		if errors.Is(err, ErrPreparedCommandUnavailable) ||
			errors.Is(err, ErrPreparedCommandClosed) ||
			errors.Is(err, ErrPreparedCommandConsumed) {
			return result, err
		}
		result.ResultClass = ResultRefused
		result.ReasonCode = ReasonMutationAuthority
		result.Remediation = []string{
			"inspect workspace authority before planning refresh again",
		}
		return result, err
	}
	result.ResultClass = ResultCancelled
	result.ReasonCode = ReasonCancelled
	result.Attempted = false
	result.ProcessOutcome = nil
	result.AttemptHistory = AttemptHistory{}
	result.Remediation = []string{"rerun refresh when ready"}
	return result, nil
}
