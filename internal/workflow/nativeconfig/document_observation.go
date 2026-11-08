package nativeconfig

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	journalobservation "github.com/isty2e/daem/internal/effect/journal/observation"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/desired"
	journal "github.com/isty2e/daem/internal/effect/journal/publication"
	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	nativehostpath "github.com/isty2e/daem/internal/output/hostpath/native"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

type DocumentObservationInput struct {
	Paths            daempaths.Paths
	Environment      desired.Environment
	Lockfile         lock.File
	Selection        targetselection.Selection
	PersistenceEpoch *readiness.PersistenceEpoch
}

// ObserveDocuments retains only native document evidence, without resolving
// unrelated sources, reconciliation or mutation authority.
func ObserveDocuments(ctx context.Context, input DocumentObservationInput) (*DocumentObservation, error) {
	if ctx == nil {
		return nil, fmt.Errorf("native document observation context is required")
	}
	operation := &DocumentObservation{}
	_, err := observeRetainedDocumentInputs(ctx, readiness.Input{Paths: input.Paths, Environment: input.Environment, Lockfile: input.Lockfile, Selection: input.Selection, PersistenceEpoch: input.PersistenceEpoch}, operation)
	if err != nil {
		return nil, errors.Join(err, operation.Close())
	}
	return operation, nil
}

// ObserveSelectedDocuments retains explicit native addresses using the same
// roots and complete persistence epoch as ordinary observation, without reading
// unrelated declared documents. It grants no ownership or mutation authority.
func ObserveSelectedDocuments(ctx context.Context, paths daempaths.Paths, epoch *readiness.PersistenceEpoch, addresses []aggregate.DocumentAddress) (*DocumentObservation, error) {
	if ctx == nil || epoch == nil || len(addresses) == 0 {
		return nil, fmt.Errorf("selected native observation requires context, persistence and addresses")
	}
	operation := &DocumentObservation{}
	if _, err := observeDocumentAddresses(ctx, paths, epoch, slices.Clone(addresses), operation); err != nil {
		return nil, errors.Join(err, operation.Close())
	}
	return operation, nil
}

func observeRetainedDocumentInputs(ctx context.Context, input readiness.Input, operation *DocumentObservation) (result *readiness.NativeAssessmentInput, resultErr error) {
	if input.PersistenceEpoch == nil {
		return nil, fmt.Errorf("native observation requires its selected persistence epoch")
	}
	before, err := input.PersistenceEpoch.Snapshot()
	if err != nil {
		return nil, err
	}

	aliases := make(map[aggregate.DocumentAddress]struct{})
	add := func(address aggregate.DocumentAddress) {
		if input.Selection.Includes(address.Target()) {
			aliases[address] = struct{}{}
		}
	}
	for _, config := range input.Environment.HostConfigs() {
		if !input.Selection.Includes(config.Target()) {
			continue
		}
		placement, err := profile.ResolveHostConfigPlacement(configuration.PlacementInput{
			Target: config.Target(), Scope: config.Scope(), Path: config.Path(), Ownership: config.Ownership(), Syntax: config.Syntax(),
		})
		if err != nil {
			return nil, err
		}
		address, err := aggregate.NewDocumentAddress(placement.Target(), placement.Scope(), placement.Destination())
		if err != nil {
			return nil, err
		}
		add(address)
	}
	for _, contract := range input.Lockfile.Locked.Subjects() {
		if input.NativeReplacement != nil && contract.EntityID() != input.NativeReplacement.Declaration() {
			continue
		}
		if placement, native := contract.HostConfigPlacement(); native && input.Selection.Includes(placement.Target()) {
			address, err := aggregate.NewDocumentAddress(placement.Target(), placement.Scope(), placement.Destination())
			if err != nil {
				return nil, err
			}
			add(address)
		}
	}
	for _, state := range before.ManagedPaths() {
		if input.NativeReplacement != nil {
			id, present := topologyprojection.EntityID(state.Subject())
			if !present || id != input.NativeReplacement.Declaration() {
				continue
			}
		}
		if aggregate.IsHostConfigDocumentSubject(state.Subject()) && slices.ContainsFunc(state.ConsumerTargets(), input.Selection.Includes) {
			if len(state.ConsumerTargets()) != 1 {
				return nil, fmt.Errorf("native whole history requires one canonical consumer")
			}
			address, err := aggregate.NewDocumentAddress(state.ConsumerTargets()[0], state.Scope(), state.Destination())
			if err != nil {
				return nil, err
			}
			add(address)
		}
	}
	for _, state := range before.ManagedAggregates() {
		if _, native := aggregate.HostConfigSyntaxForCodec(state.Contribution().CodecContractID()); native {
			add(state.Contribution().Contract().Address().Document())
		}
	}
	if len(aliases) == 0 && !input.Lockfile.ContainsHostConfig() && !before.ContainsHostConfig() {
		return nil, nil
	}
	addresses := make([]aggregate.DocumentAddress, 0, len(aliases))
	for alias := range aliases {
		addresses = append(addresses, alias)
	}
	return observeDocumentAddresses(ctx, input.Paths, input.PersistenceEpoch, addresses, operation)
}

func observeDocumentAddresses(ctx context.Context, paths daempaths.Paths, epoch *readiness.PersistenceEpoch, addresses []aggregate.DocumentAddress, operation *DocumentObservation) (result *readiness.NativeAssessmentInput, resultErr error) {
	locations, err := nativehostpath.CaptureLocations(paths.ManifestRoot)
	if err != nil {
		return nil, err
	}
	protection, err := SelectProtectionRoots(locations)
	if err != nil {
		return nil, err
	}
	before, err := epoch.Snapshot()
	if err != nil {
		return nil, err
	}
	budget, err := epoch.PhysicalBudget()
	if err != nil {
		return nil, err
	}
	var roots journalnamespace.Roots
	var retained []*rootedpath.CapturedRoot
	defer func() {
		if resultErr == nil && operation != nil {
			operation.roots = retained
			return
		}
		for _, root := range retained {
			resultErr = errors.Join(resultErr, root.Close())
		}
		if resultErr != nil {
			result = nil
		}
	}()
	slices.SortFunc(addresses, func(left, right aggregate.DocumentAddress) int {
		return strings.Compare(string(left.Target())+"\x00"+string(left.Scope())+"\x00"+left.AggregateRoot().String(), string(right.Target())+"\x00"+string(right.Scope())+"\x00"+right.AggregateRoot().String())
	})
	manifestRoot, err := rootedpath.CaptureCanonicalRootBounded(paths.ManifestRoot, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return nil, err
	}
	retained = append(retained, manifestRoot)
	manifestReference, err := configuration.ProjectRootReference()
	if err != nil {
		return nil, err
	}
	roots, err = roots.With(manifestReference, journalnamespace.RootSelection{SelectedPath: paths.ManifestRoot, Root: manifestRoot})
	if err != nil {
		return nil, err
	}
	for _, alias := range addresses {
		reference, _, err := configuration.RootBindingForAlias(configuration.AliasInput{Target: alias.Target(), Scope: alias.Scope(), Destination: alias.AggregateRoot()})
		if err != nil {
			return nil, err
		}
		if _, supplied := roots.Selection(reference); supplied {
			continue
		}
		location, err := locations.Resolve(reference)
		if err != nil {
			return nil, err
		}
		selection, err := journalnamespace.CaptureRootSelector(ctx, location.Path(), budget)
		if err != nil {
			return nil, fmt.Errorf("observe selected native root %q without creating it: %w", reference.Identity(), err)
		}
		retained = append(retained, selection.Root)
		roots, err = roots.With(reference, selection)
		if err != nil {
			return nil, err
		}
	}
	stateAuthority, err := mutation.ObservePersistedDirectoryEntryAuthorityBounded(paths.StatefilePath, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return nil, err
	}
	owner, err := stateauthority.New(stateAuthority.Exact(), paths.ManifestPath)
	if err != nil {
		return nil, err
	}
	reader, err := ownershipstore.NewRecoveryReader(paths.OwnershipRegistryPath)
	if err != nil {
		return nil, err
	}
	registry := &observedRegistry{RegistryReader: reader}
	documents, err := journal.ObserveNativeDocuments(ctx, journalobservation.Input{
		Owner: owner, Before: before, Aliases: addresses, Roots: roots,
		Protection: protection,
		Filesystem: storagecommit.Adapter{}, Registry: registry, Limits: DocumentLimits(), Budget: budget,
	})
	if err != nil {
		return nil, err
	}
	native := &readiness.NativeAssessmentInput{
		Documents: &documents, Registry: registry.actual, Owner: owner, Budget: budget,
	}
	if operation != nil {
		operation.native = native
	}
	return native, nil
}

// SelectProtectionRoots supplies read-only selectors independently of the
// documents and writer roots selected by this operation.
func SelectProtectionRoots(locations nativehostpath.Locations) (journalnamespace.ReservationSelectors, error) {
	policies, err := configuration.NativeExclusionPolicies()
	if err != nil {
		return journalnamespace.ReservationSelectors{}, err
	}
	var selected []journalnamespace.ReservationSelector
	for _, policy := range policies {
		if policy.Coverage() == configuration.ExclusionUnknown {
			continue
		}
		facets, err := policy.Facets()
		if err != nil {
			return journalnamespace.ReservationSelectors{}, err
		}
		for _, facet := range facets {
			if !facet.RequiresObservation() {
				continue
			}
			location, err := locations.ResolveProtection(facet.Reference())
			if err != nil {
				unavailable, selectionErr := journalnamespace.UnavailableReservationSelector(facet.Reference(), location.Source(), err)
				if selectionErr != nil {
					return journalnamespace.ReservationSelectors{}, selectionErr
				}
				selected = append(selected, unavailable)
				continue
			}
			if location.Absent() {
				absent, err := journalnamespace.AbsentReservationSelector(facet.Reference(), location.Source())
				if err != nil {
					return journalnamespace.ReservationSelectors{}, err
				}
				selected = append(selected, absent)
				continue
			}
			selector, err := journalnamespace.NewReservationSelector(facet.Reference(), location.Path())
			if err != nil {
				return journalnamespace.ReservationSelectors{}, err
			}
			selected = append(selected, selector.DescribedBy(location.Source()))
		}
	}
	return journalnamespace.NewReservationSelectors(selected)
}

type observedRegistry struct {
	ownershipmutation.RegistryReader
	actual ownership.Registry
}

func (reader *observedRegistry) LoadForClaimRemovals(ctx context.Context, claims []ownership.Claim, depth int, budget rootedpath.PhysicalTraversalBudget) (ownership.Registry, error) {
	registry, err := reader.RegistryReader.LoadForClaimRemovals(ctx, claims, depth, budget)
	if err == nil {
		reader.actual = registry
	}
	return registry, err
}

func (reader *observedRegistry) LoadForDocuments(ctx context.Context, owner stateauthority.Authority, missing []pathauthority.Provisional, removals []ownership.Claim, depth int, budget rootedpath.PhysicalTraversalBudget) (ownership.Registry, error) {
	qualified, ok := reader.RegistryReader.(ownershipmutation.DocumentRegistryReader)
	if !ok {
		return ownership.Registry{}, fmt.Errorf("selected Registry reader cannot qualify missing native history")
	}
	registry, err := qualified.LoadForDocuments(ctx, owner, missing, removals, depth, budget)
	if err == nil {
		reader.actual = registry
	}
	return registry, err
}
