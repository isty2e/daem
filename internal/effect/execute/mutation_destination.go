package execute

import (
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/execute/hostmutation"
	"github.com/isty2e/daem/internal/effect/execute/removaloperation"
	"github.com/isty2e/daem/internal/effect/execute/statefileoperation"

	"github.com/isty2e/daem/internal/effect/execute/controlwitness"
	"github.com/isty2e/daem/internal/effect/execute/mutationnamespace"

	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	"github.com/isty2e/daem/internal/effect/mutation"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"

	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"

	"github.com/isty2e/daem/internal/target"
)

type mutationAuthority struct {
	destinations               mutationnamespace.Namespace
	control                    *controlwitness.Guard
	removals                   *removaloperation.Operation
	lexical                    mutationnamespace.Resolver
	filesystem                 mutationfs.Store
	capturedRoot               *rootedpath.CapturedRoot
	projectStatefile           *statefileoperation.Selected
	recoveryJournal            *rootedpath.EntryAuthority
	recoveryJournalRecord      *rootedpath.EntryAuthority
	journalBasis               journalExecutionBasis
	physicalAuthorityRequests  []mutation.PhysicalAuthorityRequest
	physicalWorkBudget         *physicalwork.Budget
	generalTraversalPhase      *physicalwork.TraversalPhase
	recoveryBackups            map[string]recoveryBackup
	recoveryBackupExecution    *physicalwork.Budget
	generalExecutionWorkBudget *physicalwork.Budget
	preparedRetirement         *journal.RetirementContinuation
	ownershipRegistry          ownershipmutation.RegistryStore
	ownershipRegistryBinder    ownershipmutation.RootedRegistryBinder
	hasOwnershipRegistry       bool
	ownsCapturedRoot           bool
}

func (authority *mutationAuthority) hostMutation() *hostmutation.Operation {
	if authority == nil {
		return nil
	}
	return hostmutation.New(authority.destinations, authority.filesystem, authority.removals)
}

func newMutationAuthorityWithProjectionEffects(
	paths Paths,
	managedPaths []effectpreparation.ManagedPathEffect,
	aggregates []effectpreparation.AggregateEffect,
	removalDemands recovery.RemovalDemandSet,
	borrowedRoot *rootedpath.CapturedRoot,
	resolver mutationnamespace.Resolver,
	filesystem mutationfs.Store,
	ownershipRegistryBinder ownershipmutation.RootedRegistryBinder,
) (*mutationAuthority, error) {
	if err := removalDemands.Validate(); err != nil {
		return nil, fmt.Errorf("removal demands: %w", err)
	}
	physicalWorkBudget, err := physicalwork.NewBudget(removalDemands.Len())
	if err != nil {
		return nil, err
	}
	authority, err := captureMutationAuthorityWithPhysicalWorkBudget(
		paths,
		paths.ManifestRoot != "" || hasProjectAuthorityUse(managedPaths, aggregates),
		borrowedRoot,
		resolver,
		filesystem,
		physicalWorkBudget,
	)
	if err != nil {
		return nil, err
	}
	authority.ownershipRegistryBinder = ownershipRegistryBinder
	if err := authority.removals.PrepareDemands(removalDemands, physicalWorkBudget); err != nil {
		_ = authority.close()
		return nil, err
	}
	for _, effect := range managedPaths {
		consumers := effect.ConsumerTargets()
		if len(consumers) == 0 {
			if previous, present := effect.PreviousState(); present {
				consumers = previous.ConsumerTargets()
			}
		}
		if err := authority.bindPhysicalAuthority(effect.Scope(), effect.Destination(), consumers); err != nil {
			_ = authority.close()
			return nil, err
		}
		if previous, present := effect.PreviousState(); present &&
			(previous.Scope() != effect.Scope() || previous.Destination() != effect.Destination()) {
			if err := authority.bindPhysicalAuthority(
				previous.Scope(),
				previous.Destination(),
				previous.ConsumerTargets(),
			); err != nil {
				_ = authority.close()
				return nil, err
			}
		}
	}
	for _, effect := range aggregates {
		if err := authority.bindPhysicalAuthority(
			effect.Scope(),
			effect.Destination(),
			[]target.Target{effect.Target()},
		); err != nil {
			_ = authority.close()
			return nil, err
		}
		for _, precondition := range effect.OperationPreconditions() {
			document := precondition.DocumentAddress()
			if err := authority.bindPhysicalAuthority(
				document.Scope(),
				document.AggregateRoot(),
				[]target.Target{document.Target()},
			); err != nil {
				_ = authority.close()
				return nil, err
			}
		}
	}
	return authority, nil
}

func hasProjectAuthorityUse(
	managedPaths []effectpreparation.ManagedPathEffect,
	aggregates []effectpreparation.AggregateEffect,
) bool {
	for _, effect := range managedPaths {
		if effect.Scope() == target.ScopeProject {
			return true
		}
		if previous, present := effect.PreviousState(); present && previous.Scope() == target.ScopeProject {
			return true
		}
	}
	for _, effect := range aggregates {
		if effect.Scope() == target.ScopeProject {
			return true
		}
	}
	return false
}

func captureMutationAuthority(
	paths Paths,
	projectRequired bool,
	borrowedRoot *rootedpath.CapturedRoot,
	resolver mutationnamespace.Resolver,
	filesystem mutationfs.Store,
) (*mutationAuthority, error) {
	physicalWorkBudget, err := physicalwork.NewBudget(0)
	if err != nil {
		return nil, err
	}
	return captureMutationAuthorityWithPhysicalWorkBudget(
		paths,
		projectRequired,
		borrowedRoot,
		resolver,
		filesystem,
		physicalWorkBudget,
	)
}

func captureMutationAuthorityWithPhysicalWorkBudget(
	paths Paths,
	projectRequired bool,
	borrowedRoot *rootedpath.CapturedRoot,
	resolver mutationnamespace.Resolver,
	filesystem mutationfs.Store,
	physicalWorkBudget *physicalwork.Budget,
) (*mutationAuthority, error) {
	if resolver == nil {
		return nil, fmt.Errorf("mutation destination resolver is required")
	}
	if filesystem == nil {
		return nil, fmt.Errorf("mutation filesystem is required")
	}
	if physicalWorkBudget == nil {
		return nil, fmt.Errorf("mutation physical work budget is required")
	}
	traversalPhase, err := physicalwork.NewTraversalPhase(physicalWorkBudget)
	if err != nil {
		return nil, err
	}
	destinations, err := mutationnamespace.New(resolver, traversalPhase)
	if err != nil {
		return nil, err
	}
	authority := &mutationAuthority{
		lexical: resolver, filesystem: filesystem,
		destinations:       destinations,
		control:            controlwitness.New(filesystem),
		physicalWorkBudget: physicalWorkBudget, generalTraversalPhase: traversalPhase,
	}
	authority.removals = removaloperation.New(destinations, filesystem, authority.control, physicalWorkBudget, traversalPhase)
	if projectRequired {
		if err := authority.captureProjectRoot(paths, borrowedRoot); err != nil {
			return nil, err
		}
	}
	return authority, nil
}

func (authority *mutationAuthority) captureProjectRoot(
	paths Paths,
	borrowedRoot *rootedpath.CapturedRoot,
) error {
	if authority == nil {
		return fmt.Errorf("mutation authority is required")
	}
	if authority.capturedRoot != nil {
		return fmt.Errorf("project mutation root is already captured")
	}
	if authority.physicalWorkBudget == nil {
		return fmt.Errorf("mutation physical work budget is required")
	}
	var captured *rootedpath.CapturedRoot
	ownsCapturedRoot := false
	var err error
	captured, err = rootedpath.CaptureRootBounded(
		paths.ManifestRoot,
		physicalwork.MaximumPhysicalPathDepth,
		authority.generalTraversalPhase,
	)
	if err != nil {
		return fmt.Errorf("capture bounded project mutation root: %w", err)
	}
	ownsCapturedRoot = true
	if err := authority.destinations.BindProject(captured, borrowedRoot); err != nil {
		_ = captured.Close()
		return err
	}
	authority.capturedRoot = captured
	authority.ownsCapturedRoot = ownsCapturedRoot
	return nil
}

func (authority *mutationAuthority) bindPhysicalAuthority(
	scope target.Scope,
	destination output.Destination,
	consumers []target.Target,
) error {
	if err := authority.destinations.Bind(scope, destination); err != nil {
		return err
	}
	bound, err := authority.destinations.Resolve(scope, destination)
	if err != nil {
		return err
	}
	for _, consumer := range consumers {
		if consumer == "" {
			continue
		}
		request := mutation.PhysicalAuthorityRequest{
			Path: bound.HostPath(), Target: string(consumer), Scope: string(scope),
		}
		if _, err := mutation.NewPhysicalAuthoritySet(request); err != nil {
			return err
		}
		authority.physicalAuthorityRequests = append(authority.physicalAuthorityRequests, request)
	}
	return nil
}

func (authority *mutationAuthority) physicalAuthority() (mutation.PhysicalAuthoritySet, error) {
	if authority == nil {
		return mutation.PhysicalAuthoritySet{}, fmt.Errorf("mutation authority is required")
	}
	return mutation.NewPhysicalAuthoritySet(authority.physicalAuthorityRequests...)
}

func (authority *mutationAuthority) close() error {
	if authority == nil {
		return nil
	}
	closeErr := authority.destinations.Close()
	if err := authority.projectStatefile.Close(); err != nil {
		closeErr = errors.Join(closeErr, fmt.Errorf("close project statefile authority: %w", err))
	}
	authority.projectStatefile = nil
	if err := authority.recoveryJournal.Close(); err != nil {
		closeErr = errors.Join(closeErr, fmt.Errorf("close recovery journal authority: %w", err))
	}
	authority.recoveryJournal = nil
	if err := authority.recoveryJournalRecord.Close(); err != nil {
		closeErr = errors.Join(closeErr, fmt.Errorf("close recovery journal record authority: %w", err))
	}
	authority.recoveryJournalRecord = nil
	authority.journalBasis = journalExecutionBasis{}
	authority.physicalWorkBudget = nil
	authority.recoveryBackups = nil
	authority.recoveryBackupExecution = nil
	authority.generalExecutionWorkBudget = nil
	authority.removals = nil
	authority.generalTraversalPhase = nil
	if err := authority.preparedRetirement.Close(); err != nil {
		closeErr = errors.Join(closeErr, fmt.Errorf("close prepared journal retirement: %w", err))
	}
	authority.preparedRetirement = nil
	authority.ownershipRegistry = nil
	authority.ownershipRegistryBinder = nil
	authority.filesystem = nil
	authority.hasOwnershipRegistry = false
	if authority.capturedRoot == nil {
		return closeErr
	}
	if !authority.ownsCapturedRoot {
		authority.capturedRoot = nil
		return closeErr
	}
	err := authority.capturedRoot.Close()
	authority.capturedRoot = nil
	return errors.Join(closeErr, err)
}
