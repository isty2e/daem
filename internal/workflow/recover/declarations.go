package recover

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/effect/fileset"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/operationplan"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
)

var ErrNoDeclarationTransaction = errors.New("no interrupted declaration transaction for the selected workspace")

type DeclarationRecoveryPhase string

const (
	DeclarationRecoveryPlanned     DeclarationRecoveryPhase = "planned"
	DeclarationRecoveryCompleted   DeclarationRecoveryPhase = "completed"
	DeclarationRecoveryRefused     DeclarationRecoveryPhase = "refused"
	DeclarationRecoveryUnconfirmed DeclarationRecoveryPhase = "unconfirmed"
)

// DeclarationRecoveryPlan is a preview of one exact declaration pair.
type DeclarationRecoveryPlan struct {
	ManifestPath, LockfilePath string
	Action                     fileset.RecoveryDisposition
	RemovesLockfile            bool
}

type declarationPreparation struct {
	paths   daempaths.Paths
	barrier recoverygate.EffectAuthority
	marker  string
	pair    *transaction.PairRecovery
}

func (declarationPreparation) recoveryOperation() {}

func (prepared declarationPreparation) disclose() DeclarationRecoveryPlan {
	manifest, lockfile := prepared.pair.Referents()
	return DeclarationRecoveryPlan{
		ManifestPath: manifest, LockfilePath: lockfile,
		Action: prepared.pair.Disposition(), RemovesLockfile: prepared.pair.RemovesLockfile(),
	}
}

func planDeclarationRecovery(ctx context.Context, manifestPath string) (declarationPreparation, error) {
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		return declarationPreparation{}, err
	}
	barrier, err := recoverygate.NewEffectAuthority(ctx, paths)
	if err != nil {
		return declarationPreparation{}, err
	}
	if err := requireDeclarationRecoveryFence(ctx, barrier); err != nil {
		return declarationPreparation{}, err
	}
	stateDir, err := barrier.ExistingStateDir(ctx)
	if err != nil {
		return declarationPreparation{}, err
	}
	marker, err := fileset.FileSetAuthorityPath(stateDir)
	if err != nil {
		return declarationPreparation{}, err
	}
	selection, err := transaction.CaptureFinalizationPair(ctx, paths.ManifestPath, paths.LockfilePath)
	if err != nil {
		return declarationPreparation{}, err
	}
	pair, err := selection.ObserveRecovery(ctx, stateDir, transaction.DeclarationPairControls(paths, marker), physicalwork.NewPathBudget())
	if err != nil {
		return declarationPreparation{}, err
	}
	if pair == nil {
		return declarationPreparation{}, mutation.StaleSnapshotError{}
	}
	return declarationPreparation{paths: paths, barrier: barrier, marker: marker, pair: pair}, nil
}

func requireDeclarationRecoveryFence(ctx context.Context, barrier recoverygate.EffectAuthority) error {
	err := barrier.Validate(ctx)
	if err == nil {
		return ErrNoDeclarationTransaction
	}
	state := recoverygate.StateOf(err)
	if !recoverygate.IsCancellation(err) && !recoverygate.IsGlobalMetadataRefusal(err) &&
		state.JournalKnown() && state.Journal() == journal.InterruptionClear &&
		state.FileSetKnown() && state.FileSet() == fileset.FileSetFencePublishedTransaction {
		return nil
	}
	return err
}

func (prepared declarationPreparation) execute(ctx context.Context) (phase DeclarationRecoveryPhase, returnErr error) {
	if ctx == nil {
		return DeclarationRecoveryRefused, fmt.Errorf("declaration recovery context is required")
	}
	steps := operationplan.CompileMetadataDomains(operationplan.MetadataDomainInput{
		TargetPaths:     []string{prepared.paths.ManifestPath, prepared.paths.LockfilePath},
		MarkerPath:      prepared.marker,
		TrailingDomains: prepared.barrier.Domains(),
	})
	domains, err := recoverygate.LowerLogicalDomainSteps(steps)
	if err != nil {
		return DeclarationRecoveryRefused, err
	}
	store, err := mutation.NewStore(prepared.paths.DataDir)
	if err != nil {
		return DeclarationRecoveryRefused, err
	}
	leases, err := store.Acquire(ctx, domains...)
	if err != nil {
		return DeclarationRecoveryRefused, err
	}
	defer func() { returnErr = errors.Join(returnErr, leases.Release()) }()
	if err := prepared.barrier.ValidateFileSetRecovery(ctx); err != nil {
		return DeclarationRecoveryRefused, err
	}

	controls := transaction.DeclarationPairControls(prepared.paths, prepared.marker)
	attempted, err := prepared.pair.Settle(ctx, controls, physicalwork.NewPathBudget(), func(ctx context.Context) error {
		if err := requireDeclarationRecoveryFence(ctx, prepared.barrier); err != nil {
			return err
		}
		matches, err := leases.DomainsMatchCurrent(ctx)
		if err != nil {
			return err
		}
		if !matches {
			return mutation.StaleSnapshotError{}
		}
		return nil
	})
	if err == nil {
		return DeclarationRecoveryCompleted, nil
	}
	if attempted {
		return DeclarationRecoveryUnconfirmed, err
	}
	return DeclarationRecoveryRefused, err
}
