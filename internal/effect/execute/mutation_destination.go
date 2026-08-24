package execute

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	outputownership "github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/target"
)

// mutationDestination is one canonical effect-time rooted destination.
type mutationDestination struct {
	scope       target.Scope
	logical     output.Destination
	hostPath    string
	root        *rootedpath.CapturedRoot
	destination rootedpath.Destination
}

// physicalTraversalPhase keeps retained rooted capabilities attached to the
// currently admitted operation phase. It grants no capacity of its own.
type physicalTraversalPhase struct {
	current rootedpath.PhysicalTraversalBudget
	sealed  bool
}

func newPhysicalTraversalPhase(
	initial rootedpath.PhysicalTraversalBudget,
) (*physicalTraversalPhase, error) {
	if initial == nil {
		return nil, fmt.Errorf("initial physical traversal budget is required")
	}
	return &physicalTraversalPhase{current: initial}, nil
}

func (phase *physicalTraversalPhase) AdmitPathComponents(count int) error {
	if phase == nil || phase.current == nil {
		return fmt.Errorf("physical traversal phase is unavailable")
	}
	return phase.current.AdmitPathComponents(count)
}

// AdmitPhysicalWork forwards storage-owned physical work to the currently
// admitted phase budget. A budget that can only charge path components
// admits path-only charges through that contract and refuses entry or byte
// work.
func (phase *physicalTraversalPhase) AdmitPhysicalWork(
	pathComponents int,
	entries int,
	bytes int64,
) error {
	if phase == nil || phase.current == nil {
		return fmt.Errorf("physical traversal phase is unavailable")
	}
	if worker, ok := phase.current.(interface {
		AdmitPhysicalWork(pathComponents int, entries int, bytes int64) error
	}); ok {
		return worker.AdmitPhysicalWork(pathComponents, entries, bytes)
	}
	if entries == 0 && bytes == 0 {
		return phase.current.AdmitPathComponents(pathComponents)
	}
	return fmt.Errorf("physical traversal phase cannot admit entry or byte work")
}

func (phase *physicalTraversalPhase) advance(
	next rootedpath.PhysicalTraversalBudget,
) error {
	if phase == nil || phase.current == nil {
		return fmt.Errorf("physical traversal phase is unavailable")
	}
	if phase.sealed {
		return fmt.Errorf("physical traversal phase was already advanced")
	}
	if next == nil {
		return fmt.Errorf("next physical traversal budget is required")
	}
	phase.current = next
	phase.sealed = true
	return nil
}

// DestinationResolver expands one canonical portable destination at the boundary.
type DestinationResolver func(output.Destination) (string, error)

func (destination mutationDestination) isRooted() bool {
	if destination.scope != target.ScopeProject && destination.scope != target.ScopeGlobal {
		return false
	}
	if destination.logical.Validate() != nil || destination.hostPath == "" ||
		destination.root == nil || destination.destination.Validate() != nil {
		return false
	}
	boundPath, err := destination.destination.LexicalPath()
	return err == nil && filepath.Clean(destination.hostPath) == filepath.Clean(boundPath)
}

type mutationAuthority struct {
	lexical                     DestinationResolver
	filesystem                  mutationfs.Store
	capturedRoot                *rootedpath.CapturedRoot
	projectAuthority            rootedpath.Authority
	projectStatefile            *rootedpath.EntryAuthority
	recoveryJournal             *rootedpath.EntryAuthority
	recoveryJournalRecord       *rootedpath.EntryAuthority
	journalBasis                journalExecutionBasis
	statefileSemanticEntry      recoverySemanticEntryBinding
	ownershipSemanticEntry      recoverySemanticEntryBinding
	semanticWitness             recoverySemanticWitness
	ownershipSuccessor          outputownership.Registry
	hasOwnershipSuccessor       bool
	recoverySemanticValidation  bool
	globalDestinationBindings   map[output.Destination]globalDestinationBinding
	physicalAuthorityRequests   []mutation.PhysicalAuthorityRequest
	retainedRoots               []*rootedpath.CapturedRoot
	removalDemands              recovery.RemovalDemandSet
	removalIntents              map[removalRelationKey]recovery.RemovalIntent
	removalDestinations         map[removalRelationKey]mutationDestination
	physicalWorkBudget          *recovery.PhysicalWorkBudget
	generalTraversalPhase       *physicalTraversalPhase
	hostExecutionTraversal      rootedpath.PhysicalTraversalBudget
	removalBindingsPrepared     bool
	removalAuthorityBound       bool
	forwardRemovalReservations  map[removalRelationKey][]forwardRemovalReservation
	forwardRemovalPrepared      bool
	forwardRemovalExecution     *recovery.PhysicalWorkBudget
	recoveryBackups             map[string]recoveryBackup
	recoveryBackupExecution     *recovery.PhysicalWorkBudget
	generalExecutionWorkBudget  *recovery.PhysicalWorkBudget
	semanticExecutionWorkBudget *recovery.PhysicalWorkBudget
	removalCleanupExecution     *recovery.PhysicalWorkBudget
	preparedRetirement          *journal.RetirementContinuation
	ownershipRegistry           ownershipmutation.RegistryStore
	ownershipRegistryBinder     ownershipmutation.RootedRegistryBinder
	hasOwnershipRegistry        bool
	ownsCapturedRoot            bool
}

type globalDestinationBinding struct {
	root        *rootedpath.CapturedRoot
	destination rootedpath.Destination
	hostPath    string
	physicalKey string
}

func newMutationAuthorityWithProjectionEffects(
	paths Paths,
	managedPaths []ManagedPathEffect,
	aggregates []AggregateEffect,
	removalDemands recovery.RemovalDemandSet,
	borrowedRoot *rootedpath.CapturedRoot,
	resolver DestinationResolver,
	filesystem mutationfs.Store,
	ownershipRegistryBinder ownershipmutation.RootedRegistryBinder,
) (*mutationAuthority, error) {
	if err := removalDemands.Validate(); err != nil {
		return nil, fmt.Errorf("removal demands: %w", err)
	}
	physicalWorkBudget, err := recovery.NewPhysicalWorkBudget(removalDemands.Len())
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
	if err := authority.prepareRemovalDemands(removalDemands, physicalWorkBudget); err != nil {
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
	managedPaths []ManagedPathEffect,
	aggregates []AggregateEffect,
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
	resolver DestinationResolver,
	filesystem mutationfs.Store,
) (*mutationAuthority, error) {
	physicalWorkBudget, err := recovery.NewPhysicalWorkBudget(0)
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
	resolver DestinationResolver,
	filesystem mutationfs.Store,
	physicalWorkBudget *recovery.PhysicalWorkBudget,
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
	traversalPhase, err := newPhysicalTraversalPhase(physicalWorkBudget)
	if err != nil {
		return nil, err
	}
	authority := &mutationAuthority{
		lexical: resolver, filesystem: filesystem,
		physicalWorkBudget: physicalWorkBudget, generalTraversalPhase: traversalPhase,
	}
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
		recovery.MaximumPhysicalPathDepth,
		authority.generalTraversalPhase,
	)
	if err != nil {
		return fmt.Errorf("capture bounded project mutation root: %w", err)
	}
	ownsCapturedRoot = true
	projectAuthority, err := captured.AuthorityBounded(authority.generalTraversalPhase)
	if err != nil {
		_ = captured.Close()
		return fmt.Errorf("read bounded project mutation authority: %w", err)
	}
	if borrowedRoot != nil {
		borrowedAuthority, borrowedErr := borrowedRoot.AuthorityBounded(authority.generalTraversalPhase)
		if borrowedErr != nil || !projectAuthority.Equal(borrowedAuthority) {
			_ = captured.Close()
			return fmt.Errorf("borrowed project root differs from bounded mutation authority")
		}
	}
	authority.capturedRoot = captured
	authority.projectAuthority = projectAuthority
	authority.ownsCapturedRoot = ownsCapturedRoot
	return nil
}

func (authority *mutationAuthority) resolveBoundDestination(
	scope target.Scope,
	destination output.Destination,
) (mutationDestination, error) {
	switch scope {
	case target.ScopeGlobal:
		if binding, bound := authority.globalDestinationBindings[destination]; bound {
			return mutationDestination{
				scope: scope, logical: destination, hostPath: binding.hostPath,
				root: binding.root, destination: binding.destination,
			}, nil
		}
		return mutationDestination{}, fmt.Errorf("global destination %q was not bound before effects", destination)
	case target.ScopeProject:
		return authority.resolveProject(destination)
	default:
		return mutationDestination{}, fmt.Errorf("mutation scope %q is unsupported", scope)
	}
}

func (authority *mutationAuthority) bindScopedDestination(scope target.Scope, destination output.Destination) error {
	if authority == nil || authority.physicalWorkBudget == nil {
		return fmt.Errorf("mutation physical work budget is required")
	}
	switch scope {
	case target.ScopeProject:
		_, err := authority.resolveProject(destination)
		return err
	case target.ScopeGlobal:
	default:
		return fmt.Errorf("mutation scope %q is unsupported", scope)
	}
	return authority.bindGlobalDestination(destination)
}

func (authority *mutationAuthority) bindGlobalDestination(
	destination output.Destination,
) error {
	if authority == nil || authority.physicalWorkBudget == nil {
		return fmt.Errorf("mutation physical work budget is required")
	}
	if _, bound := authority.globalDestinationBindings[destination]; bound {
		return nil
	}
	resolvedHostPath, err := authority.resolveGlobalHostPath(destination)
	if err != nil {
		return err
	}
	root, destinationBinding, err := rootedpath.CaptureDestinationBounded(
		resolvedHostPath,
		recovery.MaximumPhysicalPathDepth,
		authority.generalTraversalPhase,
	)
	if err != nil {
		return err
	}
	hostPath, err := destinationBinding.LexicalPath()
	if err != nil {
		_ = root.Close()
		return err
	}
	physicalKey, err := mutation.CanonicalDirectoryEntryKeyBounded(
		hostPath,
		recovery.MaximumPhysicalPathDepth,
		authority.generalTraversalPhase,
	)
	if err != nil {
		_ = root.Close()
		return err
	}
	if authority.globalDestinationBindings == nil {
		authority.globalDestinationBindings = make(map[output.Destination]globalDestinationBinding)
	}
	root, err = authority.retainRoot(root, destinationBinding)
	if err != nil {
		return err
	}
	binding := globalDestinationBinding{
		root: root, destination: destinationBinding, hostPath: hostPath, physicalKey: physicalKey,
	}
	authority.globalDestinationBindings[destination] = binding
	return nil
}

func (authority *mutationAuthority) bindPhysicalAuthority(
	scope target.Scope,
	destination output.Destination,
	consumers []target.Target,
) error {
	if err := authority.bindScopedDestination(scope, destination); err != nil {
		return err
	}
	bound, err := authority.resolveBoundDestination(scope, destination)
	if err != nil {
		return err
	}
	for _, consumer := range consumers {
		if consumer == "" {
			continue
		}
		request := mutation.PhysicalAuthorityRequest{
			Path: bound.hostPath, Target: string(consumer), Scope: string(scope),
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

func (authority *mutationAuthority) retainRoot(
	candidate *rootedpath.CapturedRoot,
	destination rootedpath.Destination,
) (*rootedpath.CapturedRoot, error) {
	if authority == nil || candidate == nil {
		return nil, fmt.Errorf("retained root authority is required")
	}
	if authority.physicalWorkBudget == nil {
		return nil, errors.Join(
			fmt.Errorf("mutation physical work budget is required"),
			candidate.Close(),
		)
	}
	want := destination.Root()
	for _, retained := range authority.retainedRoots {
		have, err := retained.AuthorityBounded(authority.generalTraversalPhase)
		if err != nil {
			return nil, errors.Join(err, candidate.Close())
		}
		if !have.Equal(want) {
			continue
		}
		if err := candidate.Close(); err != nil {
			return nil, err
		}
		return retained, nil
	}
	authority.retainedRoots = append(authority.retainedRoots, candidate)
	return candidate, nil
}

func (authority *mutationAuthority) rootedJournalResolver(
	fallback func(output.Destination) (string, error),
) func(output.Destination) (string, error) {
	if authority == nil || len(authority.globalDestinationBindings) == 0 {
		return fallback
	}
	return func(destination output.Destination) (string, error) {
		if binding, bound := authority.globalDestinationBindings[destination]; bound {
			return binding.hostPath, nil
		}
		return fallback(destination)
	}
}

func (authority *mutationAuthority) rootedJournalCapability(
	destination output.Destination,
	budget rootedpath.PhysicalTraversalBudget,
) (rootedpath.CommitCapability, bool, error) {
	if authority == nil || authority.physicalWorkBudget == nil {
		return nil, false, fmt.Errorf("mutation physical work budget is required")
	}
	binding, present := authority.globalDestinationBindings[destination]
	if !present {
		return nil, false, nil
	}
	if budget == nil {
		budget = authority.generalTraversalPhase
	}
	capability, err := binding.root.AcquireBounded(
		binding.destination,
		recovery.MaximumPhysicalPathDepth,
		budget,
	)
	return capability, true, err
}

func (authority *mutationAuthority) resolveGlobalHostPath(logical output.Destination) (string, error) {
	if err := logical.ValidateScope(target.ScopeGlobal); err != nil {
		return "", fmt.Errorf("global destination %q must be home-relative or data-root-relative", logical)
	}
	return authority.lexical(logical)
}

func (authority *mutationAuthority) resolveProject(logical output.Destination) (mutationDestination, error) {
	if authority == nil || authority.capturedRoot == nil {
		return mutationDestination{}, fmt.Errorf("project mutation authority is unavailable")
	}
	if err := logical.ValidateScope(target.ScopeProject); err != nil {
		return mutationDestination{}, fmt.Errorf("project destination %q must be project-relative", logical)
	}
	relative, err := rootedpath.NewRelativeDestination(logical.RelativePath())
	if err != nil {
		return mutationDestination{}, err
	}
	projectDestination, err := authority.projectAuthority.Bind(relative)
	if err != nil {
		return mutationDestination{}, err
	}
	hostPath, err := projectDestination.LexicalPath()
	if err != nil {
		return mutationDestination{}, err
	}
	return mutationDestination{
		scope: target.ScopeProject, logical: logical, hostPath: hostPath,
		root: authority.capturedRoot, destination: projectDestination,
	}, nil
}

func (authority *mutationAuthority) acquire(destination mutationDestination) (rootedpath.CommitCapability, error) {
	if authority == nil || !destination.isRooted() {
		return nil, fmt.Errorf("rooted mutation capability is unavailable")
	}
	budget := rootedpath.PhysicalTraversalBudget(authority.generalTraversalPhase)
	if authority.hostExecutionTraversal != nil {
		budget = authority.hostExecutionTraversal
	}
	return destination.root.AcquireBounded(
		destination.destination,
		recovery.MaximumPhysicalPathDepth,
		budget,
	)
}

func (authority *mutationAuthority) close() error {
	if authority == nil {
		return nil
	}
	var closeErr error
	for index, root := range authority.retainedRoots {
		if err := root.Close(); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("close retained root authority[%d]: %w", index, err))
		}
	}
	authority.retainedRoots = nil
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
	for destination := range authority.globalDestinationBindings {
		delete(authority.globalDestinationBindings, destination)
	}
	authority.removalIntents = nil
	authority.removalDemands = recovery.RemovalDemandSet{}
	authority.removalDestinations = nil
	authority.physicalWorkBudget = nil
	authority.removalBindingsPrepared = false
	authority.removalAuthorityBound = false
	authority.forwardRemovalReservations = nil
	authority.forwardRemovalPrepared = false
	authority.forwardRemovalExecution = nil
	authority.recoveryBackups = nil
	authority.recoveryBackupExecution = nil
	authority.generalExecutionWorkBudget = nil
	authority.generalTraversalPhase = nil
	authority.hostExecutionTraversal = nil
	authority.removalCleanupExecution = nil
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
