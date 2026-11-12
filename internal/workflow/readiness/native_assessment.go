package readiness

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"

	lockobserve "github.com/isty2e/daem/internal/assurance/observe/lock"

	ownershipobserve "github.com/isty2e/daem/internal/assurance/observe/ownership"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output/ownership"

	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"

	targetselection "github.com/isty2e/daem/internal/target/selection"
)

// NativeAssessmentInput borrows observation-only facts for this assessment.
// The caller keeps the batch's selected roots and pending allocation alive.
type NativeAssessmentInput struct {
	Documents      *journalpublication.NativeDocumentObservationBatch
	Registry       ownership.Registry
	Owner          stateauthority.Authority
	OwnerReference pathauthority.Reference
	Budget         *physicalwork.Budget
}

type nativeAssessmentSources struct {
	input       *NativeAssessmentInput
	sources     []lockobserve.NativeSourceObservation
	preparation *NativeSourcePreparation
}

func nativeAssessmentError(err error) error {
	if errors.Is(err, ownership.ErrClaimMissing) {
		return fmt.Errorf("%s: %w", reconcile.ReasonOwnershipClaimMissing, err)
	}
	return err
}

var errOrdinaryNativeSourceMismatch = errors.New("source differs from its retained native namespace")

func requireOrdinaryNativeSourceAgreement(ctx context.Context, resolver liveDestinationResolver, sources []nativeAssessmentSources, address aggregate.DocumentAddress) error {
	logical := address.AggregateRoot()
	if len(sources) == 0 || sources[0].input == nil || sources[0].input.Documents == nil {
		return fmt.Errorf("ordinary document %q has no retained native namespace", logical)
	}
	input := sources[0].input
	for _, namespace := range input.Documents.Evidence().Namespaces() {
		if !slices.Contains(namespace.Addresses(), address) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		path, err := resolver(logical)
		if err != nil {
			return fmt.Errorf("resolve ordinary document %q: %w", logical, err)
		}
		referent, err := rootedpath.ResolveFutureReferentPathBounded(path, physicalwork.MaximumPhysicalPathDepth, input.Budget)
		if err != nil {
			return fmt.Errorf("resolve ordinary document referent %q: %w", logical, err)
		}
		observed, err := mutation.ObserveDirectoryEntryAuthorityBounded(referent, physicalwork.MaximumPhysicalPathDepth, input.Budget)
		if err != nil {
			return fmt.Errorf("observe ordinary document source %q: %w", logical, err)
		}
		if !observed.Equal(namespace.Observation()) {
			return fmt.Errorf("ordinary document %q: %w", logical, errOrdinaryNativeSourceMismatch)
		}
		return ctx.Err()
	}
	return fmt.Errorf("ordinary document %q has no retained native namespace", logical)
}

func prepareNativeAssessmentSources(ctx context.Context, input Input, before durable.Snapshot, epoch lockobserve.SourceEpoch) (nativeAssessmentSources, error) {
	facts := nativeAssessmentSources{input: input.Native}
	for _, config := range input.Environment.HostConfigs() {
		if !input.Selection.Includes(config.Target()) {
			continue
		}
		if input.Native == nil || input.Native.Documents == nil {
			return nativeAssessmentSources{}, fmt.Errorf("native source requires its selected document observation")
		}
		source, err := epoch.NativeSource(ctx, config)
		if err != nil {
			return nativeAssessmentSources{}, err
		}
		facts.sources = append(facts.sources, source)
	}
	if input.Native != nil && input.Native.Documents != nil {
		if err := requireNativeSourcePermissions(facts.sources, input.Native.Documents, before); err != nil {
			return nativeAssessmentSources{}, err
		}
	}
	if input.Native != nil && input.Native.Documents != nil {
		if input.PersistenceEpoch == nil {
			return nativeAssessmentSources{}, fmt.Errorf("native assessment requires its selected persistence epoch")
		}
		budget, err := input.PersistenceEpoch.PhysicalBudget()
		if err != nil {
			return nativeAssessmentSources{}, err
		}
		if budget != input.Native.Budget {
			return nativeAssessmentSources{}, fmt.Errorf("native observation requires the persistence epoch's pending allocation")
		}
		if !input.Native.Documents.Evidence().BeforeState().Equal(before) {
			return nativeAssessmentSources{}, fmt.Errorf("native observation differs from complete persistence epoch")
		}
		if err := input.Native.Documents.Evidence().ValidatePlanningContext(ctx, input.Native.Owner, input.Native.Registry, input.Native.Budget); err != nil {
			return nativeAssessmentSources{}, err
		}
	}

	requireDocument := func(address aggregate.DocumentAddress) error {
		if _, _, present := retainedNativeDocument([]nativeAssessmentSources{facts}, address); !present {
			return fmt.Errorf("native aggregate requires its retained rooted document observation")
		}
		return nil
	}
	for _, state := range before.ManagedAggregates() {
		if _, native := aggregate.HostConfigSyntaxForCodec(state.Contribution().CodecContractID()); native && input.Selection.Includes(state.Contribution().Target()) {
			if err := requireDocument(state.Contribution().Contract().Address().Document()); err != nil {
				return nativeAssessmentSources{}, err
			}
		}
	}

	for _, source := range facts.sources {
		if len(source.Projections()) == 0 {
			continue
		}
		placement, _ := source.SourceContract().HostConfigPlacement()
		address, err := aggregate.NewDocumentAddress(placement.Target(), placement.Scope(), placement.Destination())
		if err != nil {
			return nativeAssessmentSources{}, err
		}
		if err := requireDocument(address); err != nil {
			return nativeAssessmentSources{}, err
		}
	}
	if input.Native != nil && input.Native.Documents != nil {
		limits := input.Native.Documents.Evidence().DocumentLimits()
		prepared, err := PrepareNativeSources(ctx, NativeSourcePreparationInput{Environment: input.Environment, Selection: input.Selection, SourceEpoch: epoch, Documents: *input.Native.Documents, DocumentLimits: limits, ValueLimits: limits, ManageUnmanagedMatches: input.ManageUnmanagedMatches})
		if err != nil {
			return nativeAssessmentSources{}, err
		}
		facts.preparation = &prepared
	}
	return facts, nil
}

func nativeOwnershipObservations(ctx context.Context, paths daempaths.Paths, resolver liveDestinationResolver, managed managedPathPlanningInputs, aggregates managedAggregatePlanningInputs, before durable.Snapshot, selection targetselection.Selection, native *NativeAssessmentInput) (ownershipobserve.Result, error) {
	if err := requireNativeAggregatePermissions(aggregates.desired, native.Documents); err != nil {
		return ownershipobserve.Result{}, err
	}
	projection, err := native.Documents.Evidence().Ownership(ctx)
	if err != nil {
		return ownershipobserve.Result{}, err
	}
	var ordinaryProjections []aggregate.ProjectionAddress
	var nativeProjections []aggregate.ProjectionAddress
	for _, address := range aggregates.ownershipProjections() {
		if !selection.Includes(address.Document().Target()) {
			continue
		}
		if _, _, retained := native.Documents.Evidence().Document(address.Document()); retained {
			nativeProjections = append(nativeProjections, address)
		} else {
			ordinaryProjections = append(ordinaryProjections, address)
		}
	}
	var ordinaryStates []durable.ManagedAggregateState
	for _, state := range before.ManagedAggregates() {
		if _, _, retained := native.Documents.Evidence().Document(state.Contribution().Contract().Address().Document()); !retained {
			ordinaryStates = append(ordinaryStates, state)
		}
	}
	ordinary, err := ownershipobserve.Build(ownershipobserve.Input{
		Paths: paths, Resolver: ownershipobserve.DestinationResolver(resolver),
		ManagedPaths: managed.ownership, Aggregates: ordinaryProjections,
		StatePaths: before.ManagedPaths(), StateAggregates: ordinaryStates,
		Selection: selection, Registry: native.Registry,
	})
	if err != nil {
		return ownershipobserve.Result{}, err
	}
	if !ordinary.Owner.Equal(native.Owner) {
		return ownershipobserve.Result{}, fmt.Errorf("state authority changed during native assessment")
	}
	result := ordinary.Observations
	for _, address := range nativeProjections {
		if address.Document().Scope() != target.ScopeGlobal {
			continue
		}
		observed, err := projection.Global(ctx, address.Document(), address.Footprint())
		if err != nil {
			return ownershipobserve.Result{}, err
		}
		result = append(result, observed)
	}
	ordinary.Owner = native.Owner
	ordinary.Observations = result
	return ordinary, nil
}

func retainedNativeDocument(native []nativeAssessmentSources, address aggregate.DocumentAddress) (aggregate.Document, *recovery.PermissionMode, bool) {
	if len(native) == 0 || native[0].input == nil || native[0].input.Documents == nil {
		return aggregate.Document{}, nil, false
	}
	return native[0].input.Documents.Evidence().Document(address)
}
