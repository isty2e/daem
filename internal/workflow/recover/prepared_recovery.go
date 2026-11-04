package recover

import (
	"errors"
	"sync"

	"github.com/isty2e/daem/internal/effect/fileset"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
	permissions "github.com/isty2e/daem/internal/workflow/protection/recovery"
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
	plan               journal.RecoverablePlan
	paths              daempaths.Paths
	input              PlanInput
	operationEvidence  mutation.OperationFingerprint
	authorityEvidence  recoveryAuthorityEvidence
	stateDirAuthority  recoverygate.StateDirAuthority
	activeStateDir     bool
	fileSetFence       fileset.FileSetFenceKind
	physicalPathBudget rootedpath.PhysicalTraversalBudget
}

type preparedRecoveryOperation interface{ recoveryOperation() }

func (recoveryPreparation) recoveryOperation() {}

type permissionPreparation struct{ prepared permissions.Prepared }

func (permissionPreparation) recoveryOperation() {}

// PreparedRecovery owns one exact journal or permission recovery operation.
// Value copies share its single execution lifecycle; disclosures grant no rights.
type PreparedRecovery struct {
	disclosure   preparedRecoveryOperation
	fileSetFence fileset.FileSetFenceKind
	lifecycle    *preparedRecoveryLifecycle
}

type preparedRecoveryLifecycle struct {
	mu      sync.Mutex
	state   preparedRecoveryState
	planned preparedRecoveryOperation
}

func newPreparedRecovery(planned preparedRecoveryOperation) *PreparedRecovery {
	prepared := &PreparedRecovery{
		disclosure: planned,
		lifecycle: &preparedRecoveryLifecycle{
			state:   preparedRecoveryReady,
			planned: planned,
		},
	}
	if journalPlan, ok := planned.(recoveryPreparation); ok {
		journalPlan.plan = journalPlan.plan.Clone()
		prepared.disclosure = journalPlan
		prepared.fileSetFence = journalPlan.fileSetFence
	}
	return prepared
}

// ContinuingFileSetFence returns the separate file-set fence that active
// journal recovery does not clear.
func (prepared *PreparedRecovery) ContinuingFileSetFence() (fileset.FileSetFenceKind, bool) {
	if prepared == nil {
		return fileset.FileSetFenceClear, false
	}
	switch prepared.fileSetFence {
	case fileset.FileSetFencePublishedTransaction,
		fileset.FileSetFenceAbandonedResidue,
		fileset.FileSetFenceCensusLimit:
		return prepared.fileSetFence, true
	default:
		return fileset.FileSetFenceClear, false
	}
}

// Disclosure returns the independent journal snapshot, or nil for permission
// recovery. PermissionDisclosure explicitly selects the other closed variant.
func (prepared *PreparedRecovery) Disclosure() journal.RecoverablePlan {
	if prepared == nil || prepared.lifecycle == nil {
		return nil
	}

	prepared.lifecycle.mu.Lock()
	defer prepared.lifecycle.mu.Unlock()
	if journalPlan, ok := prepared.disclosure.(recoveryPreparation); ok {
		return journalPlan.plan.Clone()
	}
	return nil
}

func (prepared *PreparedRecovery) PermissionDisclosure() (permissions.Plan, bool) {
	if prepared == nil || prepared.lifecycle == nil {
		return permissions.Plan{}, false
	}
	prepared.lifecycle.mu.Lock()
	defer prepared.lifecycle.mu.Unlock()
	selection, ok := prepared.disclosure.(permissionPreparation)
	if !ok {
		return permissions.Plan{}, false
	}
	return selection.prepared.Disclosure(), true
}

func (prepared *PreparedRecovery) beginExecution() (preparedRecoveryOperation, error) {
	if prepared == nil || prepared.lifecycle == nil {
		return nil, ErrPreparedRecoveryUnavailable
	}

	lifecycle := prepared.lifecycle
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()

	switch lifecycle.state {
	case preparedRecoveryReady:
		execution := lifecycle.planned
		lifecycle.state = preparedRecoveryConsumed
		lifecycle.planned = nil
		return execution, nil
	case preparedRecoveryClosed:
		return nil, ErrPreparedRecoveryClosed
	case preparedRecoveryConsumed:
		return nil, ErrPreparedRecoveryConsumed
	default:
		return nil, ErrPreparedRecoveryUnavailable
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
	lifecycle.planned = nil
	return nil
}
