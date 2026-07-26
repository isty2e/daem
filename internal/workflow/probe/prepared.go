package probe

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"

	"github.com/isty2e/daem/internal/assurance/runtimeprobe"
	runtimeprobemcp "github.com/isty2e/daem/internal/assurance/runtimeprobe/mcp"
	"github.com/isty2e/daem/internal/subprocess"
)

var (
	errPreparedProbeUnavailable = errors.New("prepared MCP probe is unavailable")
	errPreparedProbeClosed      = errors.New("prepared MCP probe is closed")
	errPreparedProbeConsumed    = errors.New("prepared MCP probe is already consumed")
)

type preparedProbeState uint8

const (
	preparedProbeReady preparedProbeState = iota + 1
	preparedProbeClosed
	preparedProbeConsumed
)

// PreparedCommand owns one exact disclosed MCP probe request and retained
// project-root authority. Value copies share one single-use lifecycle.
type PreparedCommand struct {
	disclosure CommandResult
	lifecycle  *preparedProbeLifecycle
}

type preparedProbeLifecycle struct {
	mu      sync.Mutex
	state   preparedProbeState
	request runtimeprobemcp.ProbeRequest
	binding subprocess.WorkingDirectoryBinding
}

type preparedProbeExecution struct {
	request runtimeprobemcp.ProbeRequest
	binding subprocess.WorkingDirectoryBinding
}

func newPreparedCommand(
	disclosure CommandResult,
	request runtimeprobemcp.ProbeRequest,
	binding subprocess.WorkingDirectoryBinding,
) *PreparedCommand {
	return &PreparedCommand{
		disclosure: cloneProbeCommandResult(disclosure),
		lifecycle: &preparedProbeLifecycle{
			state:   preparedProbeReady,
			request: cloneProbeRequest(request),
			binding: binding,
		},
	}
}

// Disclosure returns a defensive dry-run projection of the retained operation.
func (prepared *PreparedCommand) Disclosure() CommandResult {
	if prepared == nil {
		return CommandResult{}
	}
	return cloneProbeCommandResult(prepared.disclosure)
}

// Execute consumes the retained operation exactly once.
func (prepared *PreparedCommand) Execute(
	ctx context.Context,
	executor RuntimeProbeExecutor,
) (CommandResult, error) {
	if ctx == nil {
		return CommandResult{}, fmt.Errorf("probe context is required")
	}
	if err := ctx.Err(); err != nil {
		return CommandResult{}, err
	}
	execution, err := prepared.beginExecution()
	if err != nil {
		return prepared.Disclosure(), err
	}
	defer execution.binding.Close()

	if executor == nil {
		executor = runtimeprobemcp.NewExecutor(prepared.disclosure.Timeout)
	}
	binder := singleUsePreparedBinder(execution.binding)
	facts, err := executor.Probe(ctx, cloneProbeRequest(execution.request), binder)
	result := prepared.Disclosure()
	result.Mode = ModeExecute
	if err != nil {
		return result, err
	}
	runtimeObservation, err := runtimeprobe.FoldFacts(facts)
	if err != nil {
		return result, err
	}
	result.Runtime = runtimeObservation
	return result, nil
}

func (prepared *PreparedCommand) beginExecution() (preparedProbeExecution, error) {
	if prepared == nil || prepared.lifecycle == nil {
		return preparedProbeExecution{}, errPreparedProbeUnavailable
	}

	lifecycle := prepared.lifecycle
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()
	switch lifecycle.state {
	case preparedProbeReady:
		execution := preparedProbeExecution{
			request: cloneProbeRequest(lifecycle.request),
			binding: lifecycle.binding,
		}
		lifecycle.state = preparedProbeConsumed
		lifecycle.request = runtimeprobemcp.ProbeRequest{}
		lifecycle.binding = nil
		return execution, nil
	case preparedProbeClosed:
		return preparedProbeExecution{}, errPreparedProbeClosed
	case preparedProbeConsumed:
		return preparedProbeExecution{}, errPreparedProbeConsumed
	default:
		return preparedProbeExecution{}, errPreparedProbeUnavailable
	}
}

// Close releases an unconsumed prepared probe. It is idempotent.
func (prepared *PreparedCommand) Close() error {
	if prepared == nil || prepared.lifecycle == nil {
		return nil
	}

	lifecycle := prepared.lifecycle
	lifecycle.mu.Lock()
	if lifecycle.state != preparedProbeReady {
		lifecycle.mu.Unlock()
		return nil
	}
	binding := lifecycle.binding
	lifecycle.state = preparedProbeClosed
	lifecycle.request = runtimeprobemcp.ProbeRequest{}
	lifecycle.binding = nil
	lifecycle.mu.Unlock()
	if binding == nil {
		return nil
	}
	return binding.Close()
}

func singleUsePreparedBinder(binding subprocess.WorkingDirectoryBinding) subprocess.WorkingDirectoryBinder {
	var mu sync.Mutex
	used := false
	return func() (subprocess.WorkingDirectoryBinding, error) {
		mu.Lock()
		defer mu.Unlock()
		if used {
			return nil, fmt.Errorf("prepared MCP probe working-directory authority is already acquired")
		}
		used = true
		return binding, nil
	}
}

func cloneProbeCommandResult(result CommandResult) CommandResult {
	cloned := result
	cloned.ProbeRequest = cloneProbeRequest(result.ProbeRequest)
	cloned.SideEffects = append([]string(nil), result.SideEffects...)
	return cloned
}

func cloneProbeRequest(request runtimeprobemcp.ProbeRequest) runtimeprobemcp.ProbeRequest {
	cloned := request
	cloned.Args = append([]string(nil), request.Args...)
	cloned.Env = make(map[string]string, len(request.Env))
	maps.Copy(cloned.Env, request.Env)
	return cloned
}
