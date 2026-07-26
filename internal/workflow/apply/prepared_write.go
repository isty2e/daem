package apply

import (
	"errors"
	"fmt"
	"sync"

	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	mcpobserve "github.com/isty2e/daem/internal/assurance/observe/mcp"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/findings"
	"github.com/isty2e/daem/internal/reconcile"
	targetavailability "github.com/isty2e/daem/internal/target/availability"
)

var (
	ErrPreparedWriteUnavailable = errors.New("prepared apply write is unavailable")
	ErrPreparedWriteClosed      = errors.New("prepared apply write is closed")
	ErrPreparedWriteConsumed    = errors.New("prepared apply write is already consumed")
)

type preparedWriteState uint8

const (
	preparedWriteUnavailable preparedWriteState = iota
	preparedWriteReady
	preparedWriteClosed
	preparedWriteConsumed
)

// PreparedWrite owns one exact apply operation and its retained root
// capability. The embedded CommandResult is a defensive disclosure snapshot;
// execution uses only the private canonical operation. Value copies remain
// aliases of one shared lifecycle and cannot duplicate execution authority.
type PreparedWrite struct {
	CommandResult

	lifecycle *preparedWriteLifecycle
}

type preparedWriteLifecycle struct {
	mu                sync.Mutex
	state             preparedWriteState
	planned           commandPlan
	request           CommandInput
	operationContext  reconcile.OperationContext
	operationEvidence mutation.OperationFingerprint
	authorityEvidence applyAuthorityEvidence
}

type preparedExecution struct {
	planned           commandPlan
	request           CommandInput
	operationContext  reconcile.OperationContext
	operationEvidence mutation.OperationFingerprint
	authorityEvidence applyAuthorityEvidence
}

func newDryRunPlan(planned commandPlan) DryRunPlan {
	return DryRunPlan{
		CommandResult: cloneCommandResult(planned.result),
		planned:       planned,
	}
}

func unavailablePreparedWrite(result CommandResult) *PreparedWrite {
	return &PreparedWrite{
		CommandResult: cloneCommandResult(result),
		lifecycle: &preparedWriteLifecycle{
			state: preparedWriteUnavailable,
		},
	}
}

func newPreparedWrite(
	planned commandPlan,
	request CommandInput,
	operationContext reconcile.OperationContext,
	operationEvidence mutation.OperationFingerprint,
	authorityEvidence applyAuthorityEvidence,
) *PreparedWrite {
	return &PreparedWrite{
		CommandResult: cloneCommandResult(planned.result),
		lifecycle: &preparedWriteLifecycle{
			state:             preparedWriteReady,
			planned:           planned,
			request:           request,
			operationContext:  operationContext,
			operationEvidence: operationEvidence,
			authorityEvidence: authorityEvidence,
		},
	}
}

func (prepared *PreparedWrite) beginExecution() (preparedExecution, error) {
	if prepared == nil || prepared.lifecycle == nil {
		return preparedExecution{}, ErrPreparedWriteUnavailable
	}

	lifecycle := prepared.lifecycle
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()

	switch lifecycle.state {
	case preparedWriteReady:
		execution := preparedExecution{
			planned:           lifecycle.planned,
			request:           cloneCommandInput(lifecycle.request),
			operationContext:  lifecycle.operationContext,
			operationEvidence: lifecycle.operationEvidence,
			authorityEvidence: lifecycle.authorityEvidence,
		}
		lifecycle.state = preparedWriteConsumed
		lifecycle.planned = commandPlan{}
		lifecycle.request = CommandInput{}
		lifecycle.operationEvidence = mutation.OperationFingerprint{}
		lifecycle.authorityEvidence = applyAuthorityEvidence{}
		return execution, nil
	case preparedWriteClosed:
		return preparedExecution{}, ErrPreparedWriteClosed
	case preparedWriteConsumed:
		return preparedExecution{}, ErrPreparedWriteConsumed
	default:
		return preparedExecution{}, ErrPreparedWriteUnavailable
	}
}

// Close releases an unconsumed prepared write. Once execution begins, the
// executor owns the transferred capability and Close is an idempotent no-op.
func (prepared *PreparedWrite) Close() error {
	if prepared == nil || prepared.lifecycle == nil {
		return nil
	}

	lifecycle := prepared.lifecycle
	lifecycle.mu.Lock()
	if lifecycle.state != preparedWriteReady {
		lifecycle.mu.Unlock()
		return nil
	}
	planned := lifecycle.planned
	lifecycle.state = preparedWriteClosed
	lifecycle.planned = commandPlan{}
	lifecycle.request = CommandInput{}
	lifecycle.operationEvidence = mutation.OperationFingerprint{}
	lifecycle.authorityEvidence = applyAuthorityEvidence{}
	lifecycle.mu.Unlock()

	return closeCommandPlan(&planned)
}

func closeCommandPlan(planned *commandPlan) error {
	if planned == nil || planned.projectRoot == nil {
		return nil
	}
	root := planned.projectRoot
	planned.projectRoot = nil
	if err := root.Close(); err != nil {
		return fmt.Errorf("close apply project-root witness: %w", err)
	}
	return nil
}

func cloneCommandResult(result CommandResult) CommandResult {
	cloned := result
	cloned.Reconciliation = result.Reconciliation.Clone()
	cloned.DelegateAttempts = append([]DelegateAttemptResult(nil), result.DelegateAttempts...)
	cloned.HostRouteAttempts = append([]durableattempt.HostRouteAttempt(nil), result.HostRouteAttempts...)
	cloned.CarrierAdoptionResults = append(
		[]durablecarrier.ManagedCarrierClaim(nil),
		result.CarrierAdoptionResults...,
	)
	cloned.Diagnostics = make([]findings.Diagnostic, len(result.Diagnostics))
	for index, diagnostic := range result.Diagnostics {
		cloned.Diagnostics[index] = diagnostic
		cloned.Diagnostics[index].RepairActions = append([]string(nil), diagnostic.RepairActions...)
		cloned.Diagnostics[index].ManualReasons = append([]string(nil), diagnostic.ManualReasons...)
	}
	cloned.LockOnly = append([]targetavailability.UnsupportedProjection(nil), result.LockOnly...)
	cloned.MCPProjections = append([]mcpobserve.LockedProjectionObservation(nil), result.MCPProjections...)
	return cloned
}
