package qualification

import (
	"context"
	"errors"
	"fmt"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

// GlobalPathObservation binds one logical global destination to the
// exact retained root incarnation used for capture or recovery observation.
// It is runtime authority, not persistence or ownership identity.
type GlobalPathObservation struct {
	resolvedPath  string
	rootAuthority rootedpath.Authority
}

type GlobalPathBindings struct {
	values map[output.Destination]GlobalPathObservation
}

func CaptureGlobalPathBindings(
	actions []journalrecord.CaptureMutation,
	resolver func(output.Destination) (string, error),
	rootedCapability journalnamespace.CapabilityResolver,
	budget *physicalwork.Budget,
) (GlobalPathBindings, error) {
	if err := journalrecord.ValidateCaptureMutations(actions); err != nil {
		return GlobalPathBindings{}, err
	}
	bindings := GlobalPathBindings{values: make(map[output.Destination]GlobalPathObservation)}
	for _, action := range actions {
		if action.Scope() != target.ScopeGlobal {
			continue
		}
		if _, present := bindings.values[action.Destination()]; present {
			continue
		}
		var observed GlobalPathObservation
		var err error
		if budget == nil {
			observed, err = ObserveGlobalBinding(action.Destination(), resolver, rootedCapability)
		} else {
			if rootedCapability == nil {
				return GlobalPathBindings{}, fmt.Errorf("native ordinary capture requires retained global authority")
			}
			observed, err = ObserveRecoveryGlobalBinding(action.Destination(), resolver, rootedCapability, budget)
		}
		if err != nil {
			return GlobalPathBindings{}, err
		}
		bindings.values[action.Destination()] = observed
	}
	return bindings, nil
}

func (bindings GlobalPathBindings) Resolver(
	fallback func(output.Destination) (string, error),
) func(output.Destination) (string, error) {
	return func(destination output.Destination) (string, error) {
		if binding, present := bindings.values[destination]; present {
			return binding.resolvedPath, nil
		}
		return fallback(destination)
	}
}

func (bindings GlobalPathBindings) Captured(
	scope target.Scope,
	destination output.Destination,
) (*journalrecord.GlobalBinding, error) {
	if scope != target.ScopeGlobal {
		return nil, nil
	}
	binding, present := bindings.values[destination]
	if !present {
		return nil, fmt.Errorf("global destination %q has no capture-time path binding", destination)
	}
	return binding.CaptureFacts()
}

func ValidateGlobalPathBindings(
	ctx context.Context,
	entries []journalrecord.PathEntry,
	resolver func(output.Destination) (string, error),
	rootedCapability journalnamespace.CapabilityResolver,
	budget *physicalwork.Budget,
) error {
	validated := make(map[output.Destination]journalrecord.GlobalBindingKey)
	currentRoots := make(map[string]rootedpath.Authority)
	for index, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := entry.Validate(); err != nil {
			return err
		}
		if entry.Scope() != target.ScopeGlobal {
			continue
		}
		destination, err := output.Parse(entry.Path())
		if err != nil {
			return fmt.Errorf("recovery entries[%d] global destination: %w", index, err)
		}
		if !entry.HasGlobalBinding() {
			return fmt.Errorf("recovery entries[%d] global destination has no capture-time path binding", index)
		}
		if previous, present := validated[destination]; present {
			if previous != entry.BindingIdentity() {
				return fmt.Errorf(
					"recovery entries[%d] global destination %q has inconsistent capture-time bindings",
					index,
					destination,
				)
			}
			continue
		}
		current, err := ObserveRecoveryGlobalBinding(
			destination,
			resolver,
			rootedCapability,
			budget,
		)
		if err != nil {
			return fmt.Errorf("recovery entries[%d]: %w", index, err)
		}
		if err := MatchRecordedGlobalBinding(entry, current, currentRoots, budget); err != nil {
			return fmt.Errorf("recovery entries[%d] global destination %q: %w", index, destination, err)
		}
		validated[destination] = entry.BindingIdentity()
	}
	return nil
}

func ObserveRecoveryGlobalBinding(
	destination output.Destination,
	resolver func(output.Destination) (string, error),
	rootedCapability journalnamespace.CapabilityResolver,
	budget *physicalwork.Budget,
) (GlobalPathObservation, error) {
	if budget == nil {
		return GlobalPathObservation{}, fmt.Errorf("recovery path work budget is required")
	}
	if resolver == nil {
		return GlobalPathObservation{}, fmt.Errorf("recovery global destination resolver is required")
	}
	resolved, err := resolver(destination)
	if err != nil {
		return GlobalPathObservation{}, fmt.Errorf("resolve global destination %q: %w", destination, err)
	}
	if rootedCapability != nil {
		capability, present, err := journalnamespace.AcquireMatchingCapability(
			destination,
			resolved,
			rootedCapability,
			budget,
		)
		if err != nil {
			return GlobalPathObservation{}, err
		}
		if !present {
			return GlobalPathObservation{}, fmt.Errorf(
				"global destination %q has no retained root authority",
				destination,
			)
		}
		bound := capability.Destination()
		path, pathErr := bound.LexicalPath()
		closeErr := capability.Close()
		if pathErr != nil || closeErr != nil {
			return GlobalPathObservation{}, errors.Join(pathErr, closeErr)
		}
		return GlobalPathObservation{
			resolvedPath:  path,
			rootAuthority: bound.Root(),
		}, nil
	}
	root, bound, err := rootedpath.CaptureDestinationBounded(
		resolved,
		physicalwork.MaximumPhysicalPathDepth,
		budget,
	)
	if err != nil {
		return GlobalPathObservation{}, fmt.Errorf("bind global destination %q: %w", destination, err)
	}
	path, pathErr := bound.LexicalPath()
	if pathErr != nil {
		pathErr = fmt.Errorf("read global destination %q lexical path: %w", destination, pathErr)
	}
	authority := bound.Root()
	closeErr := root.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("close global destination %q root authority: %w", destination, closeErr)
	}
	if pathErr != nil || closeErr != nil {
		return GlobalPathObservation{}, errors.Join(pathErr, closeErr)
	}
	return GlobalPathObservation{resolvedPath: path, rootAuthority: authority}, nil
}

func ObserveGlobalBinding(
	destination output.Destination,
	resolver func(output.Destination) (string, error),
	rootedCapability journalnamespace.CapabilityResolver,
) (GlobalPathObservation, error) {
	if resolver == nil {
		return GlobalPathObservation{}, fmt.Errorf("recovery global destination resolver is required")
	}
	resolved, err := resolver(destination)
	if err != nil {
		return GlobalPathObservation{}, fmt.Errorf("resolve global destination %q: %w", destination, err)
	}
	if rootedCapability != nil {
		capability, present, err := journalnamespace.AcquireMatchingCapability(
			destination,
			resolved,
			rootedCapability,
			nil,
		)
		if err != nil {
			return GlobalPathObservation{}, err
		}
		if !present {
			return GlobalPathObservation{}, fmt.Errorf(
				"global destination %q has no retained root authority",
				destination,
			)
		}
		if err := capability.Validate(); err != nil {
			return GlobalPathObservation{}, errors.Join(err, capability.Close())
		}
		bound := capability.Destination()
		path, pathErr := bound.LexicalPath()
		closeErr := capability.Close()
		if pathErr != nil || closeErr != nil {
			return GlobalPathObservation{}, errors.Join(pathErr, closeErr)
		}
		return GlobalPathObservation{
			resolvedPath:  path,
			rootAuthority: bound.Root(),
		}, nil
	}
	root, bound, err := rootedpath.CaptureDestination(resolved)
	if err != nil {
		return GlobalPathObservation{}, fmt.Errorf("bind global destination %q: %w", destination, err)
	}
	path, pathErr := bound.LexicalPath()
	if pathErr != nil {
		pathErr = fmt.Errorf("read global destination %q lexical path: %w", destination, pathErr)
	}
	authority, authorityErr := root.Authority()
	if authorityErr != nil {
		authorityErr = fmt.Errorf("read global destination %q root authority: %w", destination, authorityErr)
	}
	closeErr := root.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("close global destination %q root authority: %w", destination, closeErr)
	}
	if pathErr != nil || authorityErr != nil || closeErr != nil {
		return GlobalPathObservation{}, errors.Join(pathErr, authorityErr, closeErr)
	}
	return GlobalPathObservation{resolvedPath: path, rootAuthority: authority}, nil
}

func (binding GlobalPathObservation) CaptureFacts() (*journalrecord.GlobalBinding, error) {
	provenance, err := binding.rootAuthority.Provenance()
	if err != nil {
		return nil, err
	}
	return journalrecord.NewGlobalBinding(binding.resolvedPath, provenance), nil
}

func MatchRecordedGlobalBinding(
	binding journalrecord.PathEntry,
	current GlobalPathObservation,
	currentRoots map[string]rootedpath.Authority,
	budget *physicalwork.Budget,
) error {
	expectedPath, expectedRoot, err := binding.GlobalBinding()
	if err != nil {
		return err
	}
	if current.resolvedPath != expectedPath {
		return fmt.Errorf(
			"resolved to %q, not capture-time path %q; root selection changed; select the original host directory explicitly before retrying recovery",
			current.resolvedPath,
			expectedPath,
		)
	}
	if err := expectedRoot.MatchDescendant(current.rootAuthority); err != nil {
		return fmt.Errorf("current destination root violates capture-time authority: %w", err)
	}
	currentRoot, present := currentRoots[expectedRoot.PhysicalRoot()]
	if !present && current.rootAuthority.PhysicalRoot() == expectedRoot.PhysicalRoot() {
		currentRoot = current.rootAuthority
		currentRoots[expectedRoot.PhysicalRoot()] = currentRoot
		present = true
	}
	if !present {
		recaptured, err := rootedpath.CaptureCanonicalRootNoFollowBounded(
			expectedRoot.PhysicalRoot(),
			physicalwork.MaximumPhysicalPathDepth,
			budget,
		)
		if err != nil {
			return fmt.Errorf("recapture capture-time global root: %w", err)
		}
		currentRoot, err = recaptured.AuthorityBounded(budget)
		if err != nil {
			return errors.Join(err, recaptured.Close())
		}
		currentRoots[expectedRoot.PhysicalRoot()] = currentRoot
		if err := recaptured.Close(); err != nil {
			return fmt.Errorf("close recaptured global root authority: %w", err)
		}
	}
	if err := expectedRoot.Match(currentRoot); err != nil {
		return fmt.Errorf("capture-time root provenance changed: %w", err)
	}
	return nil
}

func (binding GlobalPathObservation) ResolvedPath() string { return binding.resolvedPath }
