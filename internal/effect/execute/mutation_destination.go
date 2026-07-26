package execute

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
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

// DestinationResolver expands one canonical portable destination at the boundary.
type DestinationResolver func(output.Destination) (string, error)

func (destination mutationDestination) isRooted() bool {
	if destination.scope != target.ScopeProject && destination.scope != target.ScopeGlobal {
		return false
	}
	if destination.logical == "" || destination.hostPath == "" ||
		destination.root == nil || destination.destination.Validate() != nil {
		return false
	}
	boundPath, err := destination.destination.LexicalPath()
	return err == nil && filepath.Clean(destination.hostPath) == filepath.Clean(boundPath)
}

type mutationAuthority struct {
	lexical                   DestinationResolver
	filesystem                mutationfs.Store
	capturedRoot              *rootedpath.CapturedRoot
	projectAuthority          rootedpath.Authority
	projectStatefile          *rootedpath.EntryAuthority
	recoveryJournal           *rootedpath.EntryAuthority
	globalDestinationBindings map[output.Destination]globalDestinationBinding
	physicalAuthorityRequests []mutation.PhysicalAuthorityRequest
	retainedGlobalRoots       []*rootedpath.CapturedRoot
	ownershipRegistry         ownershipmutation.RegistryStore
	ownershipRegistryBinder   ownershipmutation.RootedRegistryBinder
	hasOwnershipRegistry      bool
	ownsCapturedRoot          bool
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
	borrowedRoot *rootedpath.CapturedRoot,
	resolver DestinationResolver,
	filesystem mutationfs.Store,
	ownershipRegistryBinder ownershipmutation.RootedRegistryBinder,
) (*mutationAuthority, error) {
	authority, err := captureMutationAuthority(
		paths,
		paths.ManifestRoot != "" || hasProjectAuthorityUse(managedPaths, aggregates),
		borrowedRoot,
		resolver,
		filesystem,
	)
	if err != nil {
		return nil, err
	}
	authority.ownershipRegistryBinder = ownershipRegistryBinder
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
				output.Destination(document.AggregateRoot()),
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
	if resolver == nil {
		return nil, fmt.Errorf("mutation destination resolver is required")
	}
	if filesystem == nil {
		return nil, fmt.Errorf("mutation filesystem is required")
	}
	authority := &mutationAuthority{lexical: resolver, filesystem: filesystem}
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
	captured := borrowedRoot
	ownsCapturedRoot := false
	if captured == nil {
		var err error
		captured, err = rootedpath.CaptureRoot(paths.ManifestRoot)
		if err != nil {
			return fmt.Errorf("capture project mutation root: %w", err)
		}
		ownsCapturedRoot = true
	} else if err := captured.ValidateSelection(paths.ManifestRoot); err != nil {
		return fmt.Errorf("validate borrowed project mutation root: %w", err)
	}
	projectAuthority, err := captured.Authority()
	if err != nil {
		if ownsCapturedRoot {
			_ = captured.Close()
		}
		return fmt.Errorf("read project mutation authority: %w", err)
	}
	authority.capturedRoot = captured
	authority.projectAuthority = projectAuthority
	authority.ownsCapturedRoot = ownsCapturedRoot
	return nil
}

func hasProjectJournalEntries(
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
		if effect.MutatesHost() && effect.Scope() == target.ScopeProject {
			return true
		}
	}
	return false
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
	switch scope {
	case target.ScopeProject:
		_, err := authority.resolveProject(destination)
		return err
	case target.ScopeGlobal:
	default:
		return fmt.Errorf("mutation scope %q is unsupported", scope)
	}
	resolvedHostPath, err := authority.resolveGlobalHostPath(destination)
	if err != nil {
		return err
	}
	root, destinationBinding, err := rootedpath.CaptureDestination(resolvedHostPath)
	if err != nil {
		return err
	}
	hostPath, err := destinationBinding.LexicalPath()
	if err != nil {
		_ = root.Close()
		return err
	}
	physicalKey, err := mutation.CanonicalDirectoryEntryKey(hostPath)
	if err != nil {
		_ = root.Close()
		return err
	}
	if authority.globalDestinationBindings == nil {
		authority.globalDestinationBindings = make(map[output.Destination]globalDestinationBinding)
	}
	if existing, duplicate := authority.globalDestinationBindings[destination]; duplicate {
		if err := root.Close(); err != nil {
			return err
		}
		if existing.physicalKey != physicalKey {
			return fmt.Errorf("global destination %q has conflicting physical bindings", destination)
		}
		return nil
	}
	root, err = authority.retainGlobalRoot(root, destinationBinding)
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

func (authority *mutationAuthority) retainGlobalRoot(
	candidate *rootedpath.CapturedRoot,
	destination rootedpath.Destination,
) (*rootedpath.CapturedRoot, error) {
	if authority == nil || candidate == nil {
		return nil, fmt.Errorf("global root authority is required")
	}
	want := destination.Root()
	for _, retained := range authority.retainedGlobalRoots {
		have, err := retained.Authority()
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
	authority.retainedGlobalRoots = append(authority.retainedGlobalRoots, candidate)
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
) (rootedpath.CommitCapability, bool, error) {
	if authority == nil {
		return nil, false, fmt.Errorf("mutation authority is required")
	}
	binding, present := authority.globalDestinationBindings[destination]
	if !present {
		return nil, false, nil
	}
	capability, err := binding.root.Acquire(binding.destination)
	return capability, true, err
}

func (authority *mutationAuthority) resolveGlobalHostPath(logical output.Destination) (string, error) {
	portable, err := output.Parse(string(logical))
	if err != nil {
		return "", err
	}
	if err := portable.ValidateScope(target.ScopeGlobal); err != nil {
		return "", fmt.Errorf("global destination %q must be home-relative or data-root-relative", logical)
	}
	return authority.lexical(logical)
}

func (authority *mutationAuthority) resolveProject(logical output.Destination) (mutationDestination, error) {
	if authority == nil || authority.capturedRoot == nil {
		return mutationDestination{}, fmt.Errorf("project mutation authority is unavailable")
	}
	relative, err := rootedpath.NewRelativeDestination(string(logical))
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
	return destination.root.Acquire(destination.destination)
}

func (authority *mutationAuthority) close() error {
	if authority == nil {
		return nil
	}
	var closeErr error
	for index, root := range authority.retainedGlobalRoots {
		if err := root.Close(); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("close global root authority[%d]: %w", index, err))
		}
	}
	authority.retainedGlobalRoots = nil
	if err := authority.projectStatefile.Close(); err != nil {
		closeErr = errors.Join(closeErr, fmt.Errorf("close project statefile authority: %w", err))
	}
	authority.projectStatefile = nil
	if err := authority.recoveryJournal.Close(); err != nil {
		closeErr = errors.Join(closeErr, fmt.Errorf("close recovery journal authority: %w", err))
	}
	authority.recoveryJournal = nil
	for destination := range authority.globalDestinationBindings {
		delete(authority.globalDestinationBindings, destination)
	}
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
