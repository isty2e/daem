package recover

import (
	"errors"
	"sync"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	daempaths "github.com/isty2e/daem/internal/paths"
)

var (
	ErrPreparedRecoveryUnavailable = errors.New("prepared recovery is unavailable")
	ErrPreparedRecoveryClosed      = errors.New("prepared recovery is closed")
	ErrPreparedRecoveryConsumed    = errors.New("prepared recovery is already consumed")
)

type preparedRecoveryState uint8

const (
	preparedRecoveryUnavailable preparedRecoveryState = iota
	preparedRecoveryReady
	preparedRecoveryClosed
	preparedRecoveryConsumed
)

type recoveryPreparation struct {
	plan              recovery.Plan
	paths             daempaths.Paths
	input             PlanInput
	operationEvidence mutation.OperationFingerprint
	authorityEvidence recoveryAuthorityEvidence
}

// PreparedRecovery owns one exact recovery operation. Disclosure returns only
// an independent journal-plan snapshot; execution consumes the private
// operation and its evidence exactly once. Value copies remain aliases of one
// shared lifecycle and cannot duplicate execution authority.
type PreparedRecovery struct {
	disclosure recovery.Plan
	lifecycle  *preparedRecoveryLifecycle
}

type preparedRecoveryLifecycle struct {
	mu      sync.Mutex
	state   preparedRecoveryState
	planned recoveryPreparation
}

func newPreparedRecovery(planned recoveryPreparation) *PreparedRecovery {
	return &PreparedRecovery{
		disclosure: planned.plan.Clone(),
		lifecycle: &preparedRecoveryLifecycle{
			state:   preparedRecoveryReady,
			planned: planned,
		},
	}
}

// Disclosure returns an independent recovery-plan snapshot for presentation.
// It grants no authority to execute the disclosed actions.
func (prepared *PreparedRecovery) Disclosure() recovery.Plan {
	if prepared == nil || prepared.lifecycle == nil {
		return recovery.Plan{}
	}

	prepared.lifecycle.mu.Lock()
	defer prepared.lifecycle.mu.Unlock()
	return prepared.disclosure.Clone()
}

func (prepared *PreparedRecovery) beginExecution() (recoveryPreparation, error) {
	if prepared == nil || prepared.lifecycle == nil {
		return recoveryPreparation{}, ErrPreparedRecoveryUnavailable
	}

	lifecycle := prepared.lifecycle
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()

	switch lifecycle.state {
	case preparedRecoveryReady:
		execution := lifecycle.planned
		lifecycle.state = preparedRecoveryConsumed
		lifecycle.planned = recoveryPreparation{}
		return execution, nil
	case preparedRecoveryClosed:
		return recoveryPreparation{}, ErrPreparedRecoveryClosed
	case preparedRecoveryConsumed:
		return recoveryPreparation{}, ErrPreparedRecoveryConsumed
	default:
		return recoveryPreparation{}, ErrPreparedRecoveryUnavailable
	}
}

// Close invalidates an unconsumed prepared recovery. Once execution begins,
// the executor owns the transferred operation and Close is an idempotent no-op.
func (prepared *PreparedRecovery) Close() error {
	if prepared == nil || prepared.lifecycle == nil {
		return nil
	}

	lifecycle := prepared.lifecycle
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()
	if lifecycle.state != preparedRecoveryReady {
		return nil
	}
	lifecycle.state = preparedRecoveryClosed
	lifecycle.planned = recoveryPreparation{}
	return nil
}
