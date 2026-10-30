package protection

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

func qualifyTarget(ctx context.Context, prepared Prepared, document ownership.ProtectedDocument) (*rootedpath.CapturedRoot, rootedpath.Destination, error) {
	budget := prepared.budget
	var selected string
	for _, binding := range document.Bindings() {
		if err := ctx.Err(); err != nil {
			return nil, rootedpath.Destination{}, err
		}
		contextPaths, err := prepared.bindingContext(binding.Owner())
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

func (prepared Prepared) bindingContext(owner stateauthority.Authority) (daempaths.Paths, error) {
	if prepared.contexts == nil {
		return bindingContext(prepared.paths, prepared.owner, owner, prepared.budget)
	}
	paths, present := prepared.contexts[owner]
	if !present {
		return daempaths.Paths{}, fmt.Errorf("protected binding has no prepared resolution context")
	}
	actual, err := observeOwner(paths, prepared.budget)
	if err != nil || !actual.Equal(owner) {
		return daempaths.Paths{}, errors.Join(fmt.Errorf("prepared binding resolution context changed"), err)
	}
	return paths, nil
}

func (prepared *Prepared) freezeBindingContexts(ctx context.Context, row ownership.PendingProtection) error {
	contexts := make(map[stateauthority.Authority]daempaths.Paths)
	for _, document := range row.Documents() {
		for _, binding := range document.Bindings() {
			if err := ctx.Err(); err != nil {
				return err
			}
			if _, present := contexts[binding.Owner()]; present {
				continue
			}
			paths, err := bindingContext(prepared.paths, prepared.owner, binding.Owner(), prepared.budget)
			if err != nil {
				return refusal(ResolutionContextUnqualified, err)
			}
			contexts[binding.Owner()] = paths
		}
	}
	prepared.contexts = contexts
	return nil
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
	observation, err := inspectPermissions(ctx, prepared, document, document.Restriction)
	target.expanded = observation.Expanded()
	target.cause = err
	return target
}

func inspectPermissions(ctx context.Context, prepared Prepared, document ownership.ProtectedDocument, policy storagecommit.PermissionRestrictionPolicy) (observation storagecommit.PermissionRestrictionResult, returnErr error) {
	root, destination, err := qualifyTarget(ctx, prepared, document)
	if err != nil {
		return observation, err
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	capability, err := root.AcquireBounded(destination, journalrecovery.MaximumPhysicalPathDepth, prepared.budget)
	if err != nil {
		return observation, err
	}
	request, err := storagecommit.NewRootedPermissionRestriction(capability, policy)
	if err != nil {
		return observation, errors.Join(err, capability.Close())
	}
	observation, err = storagecommit.InspectRootedFilePermissions(ctx, request)
	return observation, refusal(MetadataUnqualified, err)
}
