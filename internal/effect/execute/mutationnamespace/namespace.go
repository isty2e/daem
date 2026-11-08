// Package mutationnamespace binds portable effect addresses to live rooted
// destinations and owns their retained roots, not their mutation policy.
package mutationnamespace

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

// Resolver expands one canonical portable destination at the boundary.
type Resolver func(output.Destination, rootedpath.PhysicalTraversalBudget) (string, error)

// Namespace copies share one binding and Close lifetime. The containing operation
// owns its project root; the namespace owns only roots explicitly retained here.
type Namespace struct {
	state *namespaceState
}

type namespaceState struct {
	resolver  Resolver
	phase     *physicalwork.TraversalPhase
	project   *rootedpath.CapturedRoot
	authority rootedpath.Authority
	globals   map[output.Destination]Destination
	retained  []*rootedpath.CapturedRoot
	closed    bool
}

// Destination retains canonical address facts, not an independently usable descriptor.
type Destination struct {
	owner       *namespaceState
	scope       target.Scope
	logical     output.Destination
	hostPath    string
	root        *rootedpath.CapturedRoot
	destination rootedpath.Destination
}

func New(resolver Resolver, phase *physicalwork.TraversalPhase) (Namespace, error) {
	if resolver == nil {
		return Namespace{}, fmt.Errorf("mutation destination resolver is required")
	}
	if err := phase.Validate(); err != nil {
		return Namespace{}, err
	}
	return Namespace{state: &namespaceState{resolver: resolver, phase: phase}}, nil
}

func (namespace Namespace) available() bool {
	return namespace.state != nil && !namespace.state.closed
}

func (namespace Namespace) BindProject(root, borrowed *rootedpath.CapturedRoot) error {
	if !namespace.available() {
		return fmt.Errorf("project mutation authority is unavailable")
	}
	state := namespace.state
	if state.project != nil {
		return fmt.Errorf("project mutation root is already captured")
	}
	authority, err := root.AuthorityBounded(state.phase)
	if err != nil {
		return fmt.Errorf("read bounded project mutation authority: %w", err)
	}
	if borrowed != nil {
		selected, err := borrowed.AuthorityBounded(state.phase)
		if err != nil || !authority.Equal(selected) {
			return fmt.Errorf("borrowed project root differs from bounded mutation authority")
		}
	}
	state.project, state.authority = root, authority
	return nil
}

func (destination Destination) Scope() target.Scope                { return destination.scope }
func (destination Destination) Logical() output.Destination        { return destination.logical }
func (destination Destination) HostPath() string                   { return destination.hostPath }
func (destination Destination) RootedPath() rootedpath.Destination { return destination.destination }

func (destination Destination) IsRooted() bool {
	if destination.owner == nil || destination.owner.closed ||
		destination.scope != target.ScopeProject && destination.scope != target.ScopeGlobal {
		return false
	}
	if destination.logical.Validate() != nil || destination.hostPath == "" ||
		destination.root == nil || destination.destination.Validate() != nil {
		return false
	}
	path, err := destination.destination.LexicalPath()
	return err == nil && filepath.Clean(destination.hostPath) == filepath.Clean(path)
}

func (namespace Namespace) Resolve(scope target.Scope, logical output.Destination) (Destination, error) {
	if !namespace.available() {
		return Destination{}, fmt.Errorf("rooted mutation capability is unavailable")
	}
	switch scope {
	case target.ScopeGlobal:
		if binding, present := namespace.state.globals[logical]; present {
			return binding, nil
		}
		return Destination{}, fmt.Errorf("global destination %q was not bound before effects", logical)
	case target.ScopeProject:
		return namespace.ResolveProject(logical)
	default:
		return Destination{}, fmt.Errorf("mutation scope %q is unsupported", scope)
	}
}

func (namespace Namespace) ResolveProject(logical output.Destination) (Destination, error) {
	if !namespace.available() || namespace.state.project == nil {
		return Destination{}, fmt.Errorf("project mutation authority is unavailable")
	}
	if err := logical.ValidateScope(target.ScopeProject); err != nil {
		return Destination{}, fmt.Errorf("project destination %q must be project-relative", logical)
	}
	relative, err := rootedpath.NewRelativeDestination(logical.RelativePath())
	if err != nil {
		return Destination{}, err
	}
	bound, err := namespace.state.authority.Bind(relative)
	if err != nil {
		return Destination{}, err
	}
	path, err := bound.LexicalPath()
	if err != nil {
		return Destination{}, err
	}
	return Destination{
		owner: namespace.state, scope: target.ScopeProject, logical: logical,
		hostPath: path, root: namespace.state.project, destination: bound,
	}, nil
}

func (namespace Namespace) ProjectProvenance() (rootedpath.AuthorityProvenance, error) {
	if !namespace.available() || namespace.state.project == nil {
		return rootedpath.AuthorityProvenance{}, fmt.Errorf("project mutation authority is unavailable")
	}
	return namespace.state.authority.Provenance()
}

// BorrowDestination retains the already-qualified binding supplied by another
// live operation. That operation keeps Close custody of the root; acquisition
// validates its root/destination correlation in rootedpath.
func (namespace Namespace) BorrowDestination(scope target.Scope, logical output.Destination, root *rootedpath.CapturedRoot, bound rootedpath.Destination) (Destination, error) {
	if !namespace.available() || root == nil {
		return Destination{}, fmt.Errorf("rooted mutation capability is unavailable")
	}
	if err := logical.ValidateScope(scope); err != nil {
		return Destination{}, err
	}
	path, err := bound.LexicalPath()
	if err != nil {
		return Destination{}, err
	}
	return Destination{
		owner: namespace.state, scope: scope, logical: logical,
		hostPath: path, root: root, destination: bound,
	}, nil
}

func (namespace Namespace) ReserveDestinationAccess(destination Destination, budget rootedpath.PhysicalTraversalBudget) error {
	if !namespace.available() || destination.owner != namespace.state || !destination.IsRooted() {
		return fmt.Errorf("rooted mutation capability is unavailable")
	}
	return destination.root.ReserveDestinationAccess(destination.destination, physicalwork.MaximumPhysicalPathDepth, budget)
}

func (namespace Namespace) Bind(scope target.Scope, logical output.Destination) error {
	if !namespace.available() {
		return fmt.Errorf("mutation physical work budget is required")
	}
	switch scope {
	case target.ScopeProject:
		_, err := namespace.ResolveProject(logical)
		return err
	case target.ScopeGlobal:
		return namespace.bindGlobal(logical)
	default:
		return fmt.Errorf("mutation scope %q is unsupported", scope)
	}
}

func (namespace Namespace) bindGlobal(logical output.Destination) error {
	state := namespace.state
	if _, present := state.globals[logical]; present {
		return nil
	}
	if err := logical.ValidateScope(target.ScopeGlobal); err != nil {
		return fmt.Errorf("global destination %q must be home-relative or data-root-relative", logical)
	}
	selected, err := state.resolver(logical, state.phase)
	if err != nil {
		return err
	}
	root, bound, err := rootedpath.CaptureDestinationBounded(selected, physicalwork.MaximumPhysicalPathDepth, state.phase)
	if err != nil {
		return err
	}
	path, err := bound.LexicalPath()
	if err != nil {
		_ = root.Close()
		return err
	}
	if _, err := mutation.CanonicalDirectoryEntryKeyBounded(path, physicalwork.MaximumPhysicalPathDepth, state.phase); err != nil {
		_ = root.Close()
		return err
	}
	if state.globals == nil {
		state.globals = make(map[output.Destination]Destination)
	}
	root, err = namespace.RetainRoot(root, bound)
	if err != nil {
		return err
	}
	state.globals[logical] = Destination{
		owner: state, scope: target.ScopeGlobal,
		logical: logical, hostPath: path, root: root, destination: bound,
	}
	return nil
}

func (namespace Namespace) IsGlobalBound(logical output.Destination) bool {
	if !namespace.available() {
		return false
	}
	_, present := namespace.state.globals[logical]
	return present
}

// RetainRoot consumes the candidate in an initialized namespace, closing it if
// an equal retained root already exists.
func (namespace Namespace) RetainRoot(candidate *rootedpath.CapturedRoot, destination rootedpath.Destination) (*rootedpath.CapturedRoot, error) {
	if namespace.state == nil || candidate == nil {
		return nil, fmt.Errorf("retained root authority is required")
	}
	if !namespace.available() {
		return nil, errors.Join(fmt.Errorf("mutation physical work budget is required"), candidate.Close())
	}
	state := namespace.state
	want := destination.Root()
	for _, retained := range state.retained {
		have, err := retained.AuthorityBounded(state.phase)
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
	state.retained = append(state.retained, candidate)
	return candidate, nil
}

// RetainArtifactRoot transfers Close custody without adding a host destination binding.
func (namespace Namespace) RetainArtifactRoot(root *rootedpath.CapturedRoot) error {
	if !namespace.available() || root == nil {
		return fmt.Errorf("retained artifact root authority is required")
	}
	namespace.state.retained = append(namespace.state.retained, root)
	return nil
}

func (namespace Namespace) Acquire(destination Destination) (rootedpath.CommitCapability, error) {
	if !namespace.available() {
		return nil, fmt.Errorf("rooted mutation capability is unavailable")
	}
	return namespace.AcquireBounded(destination, namespace.state.phase.Host())
}

func (namespace Namespace) AcquireBounded(destination Destination, budget rootedpath.PhysicalTraversalBudget) (rootedpath.CommitCapability, error) {
	if !namespace.available() || destination.owner != namespace.state || !destination.IsRooted() {
		return nil, fmt.Errorf("rooted mutation capability is unavailable")
	}
	return destination.root.AcquireBounded(destination.destination, physicalwork.MaximumPhysicalPathDepth, budget)
}

func (namespace Namespace) JournalResolver(fallback Resolver) func(output.Destination) (string, error) {
	return func(logical output.Destination) (string, error) {
		var budget rootedpath.PhysicalTraversalBudget
		if namespace.available() {
			if binding, present := namespace.state.globals[logical]; present {
				return binding.hostPath, nil
			}
			budget = namespace.state.phase
		}
		return fallback(logical, budget)
	}
}

func (namespace Namespace) JournalCapability(logical output.Destination, budget rootedpath.PhysicalTraversalBudget) (rootedpath.CommitCapability, bool, error) {
	if !namespace.available() {
		return nil, false, fmt.Errorf("mutation physical work budget is required")
	}
	bound, present := namespace.state.globals[logical]
	if !present {
		return nil, false, nil
	}
	if budget == nil {
		budget = namespace.state.phase
	}
	capability, err := namespace.AcquireBounded(bound, budget)
	return capability, true, err
}

func (namespace Namespace) Close() error {
	if !namespace.available() {
		return nil
	}
	state := namespace.state
	state.closed = true
	var closeErr error
	for index, root := range state.retained {
		if err := root.Close(); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("close retained root authority[%d]: %w", index, err))
		}
	}
	state.retained, state.globals, state.project, state.phase = nil, nil, nil, nil
	state.authority = rootedpath.Authority{}
	return closeErr
}
