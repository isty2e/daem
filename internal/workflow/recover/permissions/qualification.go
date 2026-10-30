package permissions

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	journalrecovery "github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	managedhostpath "github.com/isty2e/daem/internal/output/hostpath/managed"
	"github.com/isty2e/daem/internal/output/ownership"
	daempaths "github.com/isty2e/daem/internal/paths"
)

func requireNamespace(path string, expected pathauthority.Exact, budget rootedpath.PhysicalTraversalBudget) error {
	current, err := mutation.ObserveDirectoryEntryAuthorityBounded(path, journalrecovery.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return refusal(DocumentNamespaceUnqualified, err)
	}
	if exact, present := current.Exact(); present && exact.Equal(expected) {
		return nil
	}
	if missing, present := current.Provisional(); present && missing.MatchesMissingExact(expected) {
		return nil
	}
	return refusal(DocumentNamespaceUnqualified, fmt.Errorf("protected document namespace changed"))
}

func qualifyTarget(ctx context.Context, paths daempaths.Paths, owner stateauthority.Authority, document ownership.ProtectedDocument, budget rootedpath.PhysicalTraversalBudget) (*rootedpath.CapturedRoot, rootedpath.Destination, error) {
	var selected string
	for _, binding := range document.Bindings() {
		if err := ctx.Err(); err != nil {
			return nil, rootedpath.Destination{}, err
		}
		contextPaths, err := bindingContext(paths, owner, binding.Owner(), budget)
		if err != nil {
			return nil, rootedpath.Destination{}, refusal(ResolutionContextUnqualified, err)
		}
		path, err := managedhostpath.Resolver(contextPaths).Resolve(binding.Destination())
		if err != nil {
			return nil, rootedpath.Destination{}, err
		}
		if err := requireNamespace(path, document.PathAuthority(), budget); err != nil {
			return nil, rootedpath.Destination{}, err
		}
		owner, err := mutation.ObservePersistedDirectoryEntryAuthorityBounded(binding.Owner().StatefileKey(), journalrecovery.MaximumPhysicalPathDepth, budget)
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
	root, destination, err := rootedpath.CaptureDestinationBounded(selected, journalrecovery.MaximumPhysicalPathDepth, budget)
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

func bindingContext(selected daempaths.Paths, selectedOwner, owner stateauthority.Authority, budget rootedpath.PhysicalTraversalBudget) (daempaths.Paths, error) {
	if selectedOwner.Equal(owner) {
		return selected, nil
	}
	// Provenance locates candidates; only the current state namespace can admit one.
	candidate, err := daempaths.Resolve(owner.ManifestPath())
	if err != nil {
		return daempaths.Paths{}, err
	}
	candidate, err = candidate.WithDataDir(selected.DataDir)
	if err != nil {
		return daempaths.Paths{}, err
	}
	actual, err := observeOwner(candidate, budget)
	if err == nil && actual.Equal(owner) {
		return candidate, nil
	}
	if legacy, legacyErr := candidate.LegacyUserState(); legacyErr == nil {
		actual, err = observeOwner(legacy, budget)
		if err == nil && actual.Equal(owner) {
			return legacy, nil
		}
	}
	return daempaths.Paths{}, refusal(ResolutionContextUnqualified, errors.Join(fmt.Errorf("protected binding resolution context is unqualified"), err))
}

func inspectTarget(ctx context.Context, prepared Prepared, registry ownership.Registry, row ownership.PendingProtection, document ownership.ProtectedDocument) (target Target) {
	target = Target{destination: document.Bindings()[0].Destination(), state: TargetCheck}
	if err := registry.RequireProtectedDocument(row, document); err != nil {
		target.cause = err
		return target
	}
	root, destination, err := qualifyTarget(ctx, prepared.paths, prepared.owner, document, prepared.budget)
	if err != nil {
		target.cause = err
		return target
	}
	defer func() { target.cause = errors.Join(target.cause, root.Close()) }()
	capability, err := root.AcquireBounded(destination, journalrecovery.MaximumPhysicalPathDepth, prepared.budget)
	if err != nil {
		target.cause = err
		return target
	}
	request, err := storagecommit.NewRootedPermissionRestriction(capability, document.Restriction)
	if err != nil {
		target.cause = errors.Join(err, capability.Close())
		return target
	}
	observation, err := storagecommit.InspectRootedFilePermissions(ctx, request)
	target.expanded = observation.Expanded()
	target.cause = refusal(MetadataUnqualified, err)
	return target
}
