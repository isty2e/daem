package authoring

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/desired/entity"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	nativehostpath "github.com/isty2e/daem/internal/output/hostpath/native"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
)

func qualifiedReleaseStateMatches(observation mutation.DirectoryEntryAuthorityObservation, retained pathauthority.Exact) bool {
	if fresh, present := observation.Exact(); present {
		return fresh.Equal(retained)
	}
	missing, present := observation.Provisional()
	return present && missing.CandidateKey() == retained.Key() && missing.CandidateWitness() == retained.Witness()
}

func selectHostConfigReleaseOwner(pathsManifest string, observation mutation.DirectoryEntryAuthorityObservation, registry ownership.Registry) (stateauthority.Authority, error) {
	var selected pathauthority.Exact
	consider := func(owner stateauthority.Authority) error {
		retained := owner.StatefileAuthority()
		matches := qualifiedReleaseStateMatches(observation, retained)
		// These provenance paths are already absolute and clean. Do not open
		// a retained foreign manifest to classify its recorded owner.
		sameManifest := owner.ManifestPath() == pathsManifest
		if matches && !sameManifest {
			return fmt.Errorf("selected State authority is shared with another manifest")
		}
		if sameManifest && !matches {
			return fmt.Errorf("selected manifest has a conflicting State authority")
		}
		if !matches {
			return nil
		}
		if !selected.IsZero() && !selected.Equal(retained) {
			return fmt.Errorf("release has ambiguous retained own State witnesses")
		}
		selected = retained
		return nil
	}
	for _, claim := range registry.Claims() {
		if err := consider(claim.Owner()); err != nil {
			return stateauthority.Authority{}, err
		}
	}
	for _, binding := range registry.DocumentBindings() {
		if err := consider(binding.Owner()); err != nil {
			return stateauthority.Authority{}, err
		}
	}
	for _, protection := range registry.PendingProtections() {
		if err := consider(protection.Owner()); err != nil {
			return stateauthority.Authority{}, err
		}
	}
	if selected.IsZero() {
		fresh, present := observation.Exact()
		if !present {
			return stateauthority.Authority{}, nil
		}
		selected = fresh
	}
	return stateauthority.New(selected, pathsManifest)
}

type hostConfigReleaseLocation struct {
	document  *mutation.DirectoryEntryAuthorityObservation
	exclusion mutation.DirectoryEntryAuthorityObservation
}

func (candidate hostConfigReleaseCandidate) observeDocument(ctx context.Context, budget *physicalwork.Budget) (hostConfigReleaseLocation, error) {
	binding, err := configuration.RootBindingForAlias(configuration.AliasInput{Target: candidate.selected, Scope: candidate.scope, Destination: candidate.destination})
	if err != nil {
		return hostConfigReleaseLocation{}, err
	}
	root := candidate.paths.ManifestRoot
	if binding.RootReference().Identity() != configuration.ProjectRootIdentity {
		locations, err := nativehostpath.CaptureLocations(candidate.paths.ManifestRoot)
		if err != nil {
			return hostConfigReleaseLocation{}, err
		}
		location, err := locations.Resolve(binding.RootReference())
		if err != nil {
			return hostConfigReleaseLocation{}, err
		}
		root = location.Path()
	}
	resolvedRoot, err := rootedpath.ResolveFutureReferentPathBounded(root, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return observeHostReleaseBlocker(ctx, root, budget, err)
	}
	child, directory := binding.Coordinate().DirectoryChild()
	if !directory {
		return hostConfigReleaseLocation{}, fmt.Errorf("host configuration release requires a native directory-child document")
	}
	selected := filepath.Join(resolvedRoot, filepath.FromSlash(child))
	path, err := rootedpath.ResolveFutureReferentPathBounded(selected, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return observeHostReleaseBlocker(ctx, selected, budget, err)
	}
	if err := ctx.Err(); err != nil {
		return hostConfigReleaseLocation{}, err
	}
	current, err := mutation.ObserveDirectoryEntryAuthorityBounded(path, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return observeHostReleaseBlocker(ctx, selected, budget, err)
	}
	return hostConfigReleaseLocation{document: &current, exclusion: current}, nil
}

func observeHostReleaseBlocker(ctx context.Context, selected string, budget *physicalwork.Budget, original error) (hostConfigReleaseLocation, error) {
	if err := ctx.Err(); err != nil {
		return hostConfigReleaseLocation{}, err
	}
	_, err := rootedpath.ReadDirectoryNamesBounded(ctx, selected, physicalwork.MaximumPhysicalPathDepth, budget)
	var failure *rootedpath.Failure
	if !errors.As(err, &failure) || failure.Kind() != rootedpath.FailureAncestorNotDirectory {
		return hostConfigReleaseLocation{}, errors.Join(original, err, ctx.Err())
	}
	// Reobserve diagnostic path data for exclusion only. It never supplies a
	// document identity, control-file locator, or mutation capability.
	blocker, err := mutation.ObserveDirectoryEntryAuthorityBounded(failure.Path(), physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return hostConfigReleaseLocation{}, errors.Join(original, err)
	}
	return hostConfigReleaseLocation{exclusion: blocker}, nil
}

func (candidate *hostConfigReleaseCandidate) qualifyMetadata(ctx context.Context, registry ownership.Registry, budget *physicalwork.Budget) error {
	state, err := mutation.ObserveDirectoryEntryAuthorityBounded(candidate.paths.StatefilePath, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return err
	}
	candidate.owner, err = selectHostConfigReleaseOwner(candidate.paths.ManifestPath, state, registry)
	if err != nil {
		return err
	}
	location, err := candidate.observeDocument(ctx, budget)
	if err != nil {
		return err
	}
	candidate.exclusion = location.exclusion
	var current mutation.DirectoryEntryAuthorityObservation
	if location.document != nil {
		current = *location.document
	}
	var exact pathauthority.Exact
	var missing pathauthority.Provisional
	if !candidate.owner.IsZero() {
		if binding, present := registry.DocumentBinding(candidate.owner.Key(), candidate.destination); present {
			candidate.binding, err = ownership.PresentDocumentBinding(binding)
			if err != nil {
				return err
			}
			exact = binding.PathAuthority()
		}
	}
	if exact.IsZero() && location.document == nil {
		if err := candidate.requireUnboundAuthoringOnly(ctx, registry, state); err != nil {
			return err
		}
		return candidate.requireMetadataDisjoint(ctx, location.exclusion, registry, budget)
	}
	if exact.IsZero() {
		for _, claim := range registry.Claims() {
			if !claim.Owner().Equal(candidate.owner) {
				continue
			}
			retained := claim.Address().PathAuthority()
			if !qualifiedReleaseStateMatches(current, retained) {
				ambiguous := current.MatchesRetainedExact(retained)
				if fresh, present := current.Exact(); present {
					ambiguous = ambiguous || fresh.MatchesLegacyComparison(retained)
				}
				if ambiguous {
					return fmt.Errorf("selected native document has ambiguous retained legacy ownership evidence")
				}
				continue
			}
			if !exact.IsZero() && !exact.Equal(claim.Address().PathAuthority()) {
				return fmt.Errorf("release has ambiguous retained document witnesses")
			}
			exact = claim.Address().PathAuthority()
		}
	}
	if exact.IsZero() {
		if fresh, present := current.Exact(); present {
			exact = fresh
		} else {
			missing, _ = current.Provisional()
		}
	}
	var scope ownership.DocumentScope
	if !exact.IsZero() {
		candidate.namespace, err = journalrecord.NewNativeReleaseExactNamespace(exact)
		if err == nil {
			scope, err = ownership.NewDocumentScope([]pathauthority.Exact{exact}, nil)
		}
	} else {
		candidate.namespace, err = journalrecord.NewNativeReleaseProvisionalNamespace(missing)
		if err == nil {
			scope, err = ownership.NewDocumentScope(nil, []pathauthority.Provisional{missing})
		}
	}
	if err != nil {
		return err
	}
	candidate.registryScope = &scope
	if err := candidate.registryScope.RequireWritable(registry); err != nil {
		return err
	}
	if !candidate.owner.IsZero() {
		candidate.claims, err = candidate.registryScope.OwnClaims(registry, candidate.owner)
		if err != nil {
			return err
		}
	}
	if candidate.management != nil {
		release, err := candidate.release(ctx, journalpublication.PlanningOperationID(), time.Unix(0, 0).UTC(), budget)
		if err != nil {
			return err
		}
		if err := release.Metadata().RequireBefore(registry); err != nil {
			return err
		}
		_, candidate.registryChanged, err = release.Metadata().ApplyAfterCommit(registry)
		if err != nil {
			return err
		}
	} else if !candidate.owner.IsZero() {
		aliases, err := candidate.remainingAliases(registry)
		if err != nil {
			return err
		}
		id, _ := entity.New(entity.KindHostConfig, candidate.request.ID)
		if err := journaldocument.RequireHostConfigAuthoringOnly(ctx, candidate.before, id, candidate.destination, aliases, candidate.owner, registry, *candidate.registryScope); err != nil {
			return err
		}
	} else {
		if err := candidate.requireAbsentOwner(registry, state); err != nil {
			return err
		}
	}
	return candidate.requireMetadataDisjoint(ctx, location.exclusion, registry, budget)
}

func (candidate hostConfigReleaseCandidate) requireAbsentOwner(registry ownership.Registry, state mutation.DirectoryEntryAuthorityObservation) error {
	if !candidate.before.Equal(durable.EmptySnapshot()) {
		return fmt.Errorf("unqualified absent State cannot prove authoring-only removal")
	}
	for _, claim := range registry.Claims() {
		if state.MayOverlapExact(claim.Owner().StatefileAuthority()) {
			return fmt.Errorf("absent own State overlaps retained Registry authority")
		}
	}
	for _, binding := range registry.DocumentBindings() {
		if state.MayOverlapExact(binding.Owner().StatefileAuthority()) {
			return fmt.Errorf("absent own State overlaps retained binding authority")
		}
	}
	for _, protection := range registry.PendingProtections() {
		if state.MayOverlapExact(protection.Owner().StatefileAuthority()) {
			return fmt.Errorf("absent own State overlaps retained protection authority")
		}
		for _, document := range protection.Documents() {
			for _, binding := range document.Bindings() {
				if state.MayOverlapExact(binding.Owner().StatefileAuthority()) {
					return fmt.Errorf("absent own State overlaps protected binding authority")
				}
			}
		}
	}
	return nil
}

func (candidate hostConfigReleaseCandidate) requireUnboundAuthoringOnly(ctx context.Context, registry ownership.Registry, state mutation.DirectoryEntryAuthorityObservation) error {
	if candidate.management != nil {
		return fmt.Errorf("retained management requires a qualified native namespace or binding")
	}
	if len(registry.PendingProtections()) != 0 {
		return fmt.Errorf("unqualified native namespace cannot disprove pending protection")
	}
	if candidate.owner.IsZero() {
		return candidate.requireAbsentOwner(registry, state)
	}
	for _, claim := range registry.Claims() {
		if claim.Owner().Equal(candidate.owner) {
			return fmt.Errorf("unqualified native namespace cannot disprove an own claim")
		}
	}
	for _, binding := range registry.DocumentBindings() {
		if binding.Owner().Equal(candidate.owner) && candidate.exclusion.MayOverlapExact(binding.PathAuthority()) {
			return fmt.Errorf("unqualified native namespace overlaps a retained own binding")
		}
	}
	index, err := journaldocument.IndexSnapshotForClaims(ctx, candidate.before, journaldocument.NativeClaims)
	if err != nil {
		return err
	}
	destinations, err := index.NativeDestinations(ctx)
	if err != nil {
		return err
	}
	if _, retained := destinations[candidate.destination]; retained {
		return fmt.Errorf("unqualified native namespace has remaining managed members")
	}
	return nil
}

func (candidate hostConfigReleaseCandidate) remainingAliases(registry ownership.Registry) ([]aggregate.DocumentAddress, error) {
	destinations := map[output.Destination]bool{candidate.destination: true}
	for _, binding := range registry.DocumentBindings() {
		if !binding.Owner().Equal(candidate.owner) {
			continue
		}
		for _, claim := range candidate.claims {
			if claim.Address().PathAuthority().Equal(binding.PathAuthority()) {
				destinations[binding.Destination()] = true
			}
		}
	}
	var aliases []aggregate.DocumentAddress
	for _, row := range candidate.before.ManagedAggregates() {
		if destinations[row.Contribution().AggregateRoot()] {
			aliases = append(aliases, row.Contribution().Contract().Address().Document())
		}
	}
	for _, row := range candidate.before.ManagedPaths() {
		if !destinations[row.Destination()] {
			continue
		}
		for _, selected := range row.ConsumerTargets() {
			alias, err := aggregate.NewDocumentAddress(selected, row.Scope(), row.Destination())
			if err != nil {
				return nil, err
			}
			aliases = append(aliases, alias)
		}
	}
	return aliases, nil
}

func (candidate hostConfigReleaseCandidate) requireMetadataDisjoint(ctx context.Context, current mutation.DirectoryEntryAuthorityObservation, registry ownership.Registry, budget *physicalwork.Budget) error {
	for _, path := range []string{candidate.paths.StateDir, candidate.paths.OwnershipRegistryPath, candidate.paths.CarrierClaimRegistryPath, candidate.paths.RecoveryDir, candidate.paths.GlobalMetadataDir()} {
		if path == "" {
			continue
		}
		entry, err := mutation.ObserveDirectoryEntryAuthorityBounded(path, physicalwork.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return err
		}
		referent, err := rootedpath.ResolveFutureReferentPathBounded(path, physicalwork.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return err
		}
		resolved, err := mutation.ObserveDirectoryEntryAuthorityBounded(referent, physicalwork.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return err
		}
		for _, control := range []mutation.DirectoryEntryAuthorityObservation{entry, resolved} {
			if current.MayOverlap(control) {
				return fmt.Errorf("native host namespace overlaps release control artifacts")
			}
			if binding, present := candidate.binding.Get(); present && control.MayOverlapExact(binding.PathAuthority()) {
				return fmt.Errorf("retained native binding overlaps release control artifacts")
			}
			for _, claim := range candidate.claims {
				if control.MayOverlapExact(claim.Address().PathAuthority()) {
					return fmt.Errorf("retained native claim overlaps release control artifacts")
				}
			}
		}
	}
	for _, file := range candidate.files {
		if !file.write {
			continue
		}
		observed, err := mutation.ObserveDirectoryEntryAuthorityBounded(file.path, physicalwork.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return err
		}
		if observed.MayOverlap(current) {
			return fmt.Errorf("release metadata aliases its native host document")
		}
		for _, claim := range registry.Claims() {
			if observed.MayOverlapExact(claim.Address().PathAuthority()) {
				return fmt.Errorf("release metadata overlaps a managed host output")
			}
		}
		for _, binding := range registry.DocumentBindings() {
			if observed.MayOverlapExact(binding.PathAuthority()) {
				return fmt.Errorf("release metadata overlaps a retained host binding")
			}
		}
		for _, path := range []string{candidate.paths.OwnershipRegistryPath, candidate.paths.CarrierClaimRegistryPath, candidate.paths.RecoveryDir, candidate.paths.GlobalMetadataDir(), candidate.paths.LegacyUserStateDir, candidate.paths.CacheDir} {
			if path == "" {
				continue
			}
			control, err := mutation.ObserveDirectoryEntryAuthorityBounded(path, physicalwork.MaximumPhysicalPathDepth, budget)
			if err != nil {
				return err
			}
			if observed.MayOverlap(control) {
				return fmt.Errorf("release metadata overlaps another control artifact")
			}
		}
	}
	return ctx.Err()
}
