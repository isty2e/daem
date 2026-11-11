// Package namespace qualifies the current resolution contexts and physical
// document cohort used by permission protection.
package namespace

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/protection"
	"github.com/isty2e/daem/internal/output/ownership"
	daempaths "github.com/isty2e/daem/internal/paths"
)

type Resolution struct {
	paths daempaths.Paths
	owner stateauthority.Authority
}

func NewResolution(paths daempaths.Paths, owner stateauthority.Authority) Resolution {
	return Resolution{paths: paths, owner: owner}
}

func ObserveOwner(paths daempaths.Paths, budget rootedpath.PhysicalTraversalBudget) (stateauthority.Authority, error) {
	authority, err := mutation.ObservePersistedDirectoryEntryAuthorityBounded(paths.StatefilePath, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return stateauthority.Authority{}, err
	}
	return stateauthority.New(authority.Exact(), paths.ManifestPath)
}

func requireNamespace(path string, expected pathauthority.Exact, budget rootedpath.PhysicalTraversalBudget) error {
	current, err := mutation.ObserveDirectoryEntryAuthorityBounded(path, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return protection.Refusal(protection.DocumentNamespaceUnqualified, err)
	}
	return RequireObservation(current, expected)
}

func RequireObservation(current mutation.DirectoryEntryAuthorityObservation, expected pathauthority.Exact) error {
	if exact, present := current.Exact(); present && exact.Equal(expected) {
		return nil
	}
	if missing, present := current.Provisional(); present && missing.MatchesMissingExact(expected) {
		return nil
	}
	return protection.Refusal(protection.DocumentNamespaceUnqualified, fmt.Errorf("protected document namespace changed"))
}

func (resolution Resolution) CaptureDocument(ctx context.Context, document ownership.ProtectedDocument, budget rootedpath.PhysicalTraversalBudget) (*rootedpath.CapturedRoot, rootedpath.Destination, error) {
	var selected string
	for _, binding := range document.Bindings() {
		if err := ctx.Err(); err != nil {
			return nil, rootedpath.Destination{}, err
		}
		contextPaths, err := resolution.BindingContext(binding.Owner(), budget)
		if err != nil {
			return nil, rootedpath.Destination{}, protection.Refusal(protection.ResolutionContextUnqualified, err)
		}
		path, err := ResolveBindingDestination(contextPaths, binding.Destination(), binding.PathAuthority().Key(), budget)
		if err != nil {
			return nil, rootedpath.Destination{}, err
		}
		path, err = rootedpath.ResolveFutureReferentPathBounded(path, physicalwork.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return nil, rootedpath.Destination{}, err
		}
		if err := requireNamespace(path, document.PathAuthority(), budget); err != nil {
			return nil, rootedpath.Destination{}, err
		}
		owner, err := mutation.ObservePersistedDirectoryEntryAuthorityBounded(binding.Owner().StatefileKey(), physicalwork.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return nil, rootedpath.Destination{}, err
		}
		if !owner.Exact().Equal(binding.Owner().StatefileAuthority()) {
			return nil, rootedpath.Destination{}, fmt.Errorf("protected document cohort authority changed")
		}
		if selected == "" {
			selected = path
		}
	}
	root, destination, err := rootedpath.CaptureDestinationBounded(selected, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return nil, rootedpath.Destination{}, err
	}
	path, err := destination.LexicalPath()
	if err == nil {
		err = requireNamespace(path, document.PathAuthority(), budget)
	}
	if err != nil {
		return nil, rootedpath.Destination{}, errors.Join(err, root.Close())
	}
	return root, destination, nil
}

func (resolution Resolution) BindingContexts(ctx context.Context, row ownership.PendingProtection, budget rootedpath.PhysicalTraversalBudget) (map[stateauthority.Authority]daempaths.Paths, error) {
	contexts := make(map[stateauthority.Authority]daempaths.Paths)
	for _, document := range row.Documents() {
		for _, binding := range document.Bindings() {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if _, present := contexts[binding.Owner()]; present {
				continue
			}
			paths, err := resolution.BindingContext(binding.Owner(), budget)
			if err != nil {
				return nil, protection.Refusal(protection.ResolutionContextUnqualified, err)
			}
			contexts[binding.Owner()] = paths
		}
	}
	return contexts, nil
}

func (resolution Resolution) BindingContext(owner stateauthority.Authority, budget rootedpath.PhysicalTraversalBudget) (daempaths.Paths, error) {
	if resolution.owner.Equal(owner) {
		return resolution.paths, nil
	}
	// Provenance locates candidates; only the current state namespace can admit one.
	candidate, err := daempaths.Resolve(owner.ManifestPath())
	if err != nil {
		return daempaths.Paths{}, err
	}
	candidate, err = candidate.WithDataDir(resolution.paths.DataDir)
	if err != nil {
		return daempaths.Paths{}, err
	}
	actual, err := ObserveOwner(candidate, budget)
	if err == nil && actual.Equal(owner) {
		return candidate, nil
	}
	if legacy, legacyErr := candidate.LegacyUserState(); legacyErr == nil {
		actual, err = ObserveOwner(legacy, budget)
		if err == nil && actual.Equal(owner) {
			return legacy, nil
		}
	}
	return daempaths.Paths{}, protection.Refusal(protection.ResolutionContextUnqualified, errors.Join(fmt.Errorf("protected binding resolution context is unqualified"), err))
}
