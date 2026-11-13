package authoring

import (
	"context"
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

func (candidate hostConfigReleaseCandidate) observeDocument(ctx context.Context, budget *physicalwork.Budget) (mutation.DirectoryEntryAuthorityObservation, error) {
	binding, err := configuration.RootBindingForAlias(configuration.AliasInput{Target: candidate.selected, Scope: candidate.scope, Destination: candidate.destination})
	if err != nil {
		return mutation.DirectoryEntryAuthorityObservation{}, err
	}
	root := candidate.paths.ManifestRoot
	if binding.RootReference().Identity() != configuration.ProjectRootIdentity {
		locations, err := nativehostpath.CaptureLocations(candidate.paths.ManifestRoot)
		if err != nil {
			return mutation.DirectoryEntryAuthorityObservation{}, err
		}
		location, err := locations.Resolve(binding.RootReference())
		if err != nil {
			return mutation.DirectoryEntryAuthorityObservation{}, err
		}
		root = location.Path()
	}
	root, err = rootedpath.ResolveRootSelectorBounded(root, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return mutation.DirectoryEntryAuthorityObservation{}, err
	}
	child, directory := binding.Coordinate().DirectoryChild()
	if !directory {
		return mutation.DirectoryEntryAuthorityObservation{}, fmt.Errorf("host configuration release requires a native directory-child document")
	}
	path, err := rootedpath.ResolveReferentPathBounded(filepath.Join(root, filepath.FromSlash(child)), physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return mutation.DirectoryEntryAuthorityObservation{}, err
	}
	if err := ctx.Err(); err != nil {
		return mutation.DirectoryEntryAuthorityObservation{}, err
	}
	return mutation.ObserveDirectoryEntryAuthorityBounded(path, physicalwork.MaximumPhysicalPathDepth, budget)
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
	current, err := candidate.observeDocument(ctx, budget)
	if err != nil {
		return err
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
	if !exact.IsZero() {
		candidate.namespace, err = journalrecord.NewNativeReleaseExactNamespace(exact)
		if err == nil {
			candidate.registryScope, err = ownership.NewDocumentScope([]pathauthority.Exact{exact}, nil)
		}
	} else {
		candidate.namespace, err = journalrecord.NewNativeReleaseProvisionalNamespace(missing)
		if err == nil {
			candidate.registryScope, err = ownership.NewDocumentScope(nil, []pathauthority.Provisional{missing})
		}
	}
	if err != nil {
		return err
	}
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
		if err := journaldocument.RequireHostConfigAuthoringOnly(ctx, candidate.before, id, candidate.destination, aliases, candidate.owner, registry, candidate.registryScope); err != nil {
			return err
		}
	} else {
		if !candidate.before.Equal(durable.EmptySnapshot()) {
			return fmt.Errorf("unqualified absent State cannot prove authoring-only removal")
		}
		// An unresolved future own State must be disjoint from every retained
		// authority; absence alone cannot hide orphan metadata.
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
	}
	return candidate.requireMetadataDisjoint(ctx, current, registry, budget)
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
