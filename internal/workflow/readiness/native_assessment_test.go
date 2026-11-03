package readiness

import (
	"context"
	"testing"

	liveobserve "github.com/isty2e/daem/internal/assurance/observe/live"
	lockobserve "github.com/isty2e/daem/internal/assurance/observe/lock"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	carrierclaimstore "github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	managedhostpath "github.com/isty2e/daem/internal/output/hostpath/managed"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	hookcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	lockbuild "github.com/isty2e/daem/internal/realization/lock/build"
	"github.com/isty2e/daem/internal/reconcile"
	sourceresolution "github.com/isty2e/daem/internal/supply/source/resolution"
	"github.com/isty2e/daem/internal/target"
)

type assessmentRegistryReader struct {
	ownershipmutation.RegistryReader
	actual ownership.Registry
	loads  int
}

func (reader *assessmentRegistryReader) LoadForClaimRemovals(ctx context.Context, claims []ownership.Claim, depth int, budget rootedpath.PhysicalTraversalBudget) (ownership.Registry, error) {
	actual, err := reader.RegistryReader.LoadForClaimRemovals(ctx, claims, depth, budget)
	if err == nil {
		reader.actual = actual
		reader.loads++
	}
	return actual, err
}

func (reader *assessmentRegistryReader) LoadForDocuments(ctx context.Context, owner stateauthority.Authority, missing []pathauthority.Provisional, removals []ownership.Claim, depth int, budget rootedpath.PhysicalTraversalBudget) (ownership.Registry, error) {
	actual, err := reader.RegistryReader.(ownershipmutation.DocumentRegistryReader).LoadForDocuments(ctx, owner, missing, removals, depth, budget)
	if err == nil {
		reader.actual = actual
		reader.loads++
	}
	return actual, err
}

func TestNativeAssessmentRequiresRetainedStateDocumentsBeforeRetirementPlanning(t *testing.T) {
	fixture := newNativePlanningFixture(t, target.ScopeProject, "extension-settings/test.json")
	environment, err := declarationmanifest.Decode([]byte("version=1\ntargets=['pi']\n"))
	if err != nil {
		t.Fatal(err)
	}
	input := Input{Environment: environment, Selection: fixture.input.Selection}
	facts, err := prepareNativeAssessmentSources(t.Context(), input, fixture.observation.Before, lockobserve.SourceEpoch{})
	if err == nil || facts.input != nil || len(facts.sources) != 0 {
		t.Fatal("native state retirement planning did not require retained document evidence", err)
	}
}

func TestNativeAssessmentBorrowsLiveBatchAndOriginalAllocation(t *testing.T) {
	fixture := newNativePlanningFixture(t, target.ScopeProject, "extension-settings/test.json")
	var err error
	fixture.config, err = hostconfig.New(hostconfig.Spec{
		Name: fixture.config.ID().Name(), Source: fixture.config.Source(), Target: fixture.config.Target(),
		Scope: fixture.config.Scope(), Path: fixture.config.Path(), Ownership: hostconfig.OwnershipKeys,
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.input.Environment = nativePreparationEnvironment(t, fixture.config)
	requireNativePreparationFile(t, fixture.sourcePath, []byte(`{"a":1}`))
	fixture.refreshEpoch(t)
	reader := &assessmentRegistryReader{RegistryReader: fixture.observation.Registry}
	fixture.observation.Registry = reader
	documents, err := journal.ObserveNativeDocuments(t.Context(), fixture.observation)
	if err != nil {
		t.Fatal(err)
	}
	store, err := carrierclaimstore.New(fixture.paths.CarrierClaimRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := store.LoadForSelectedAuthority(t.Context(), fixture.paths.StatefilePath, fixture.paths.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	epoch := NewPersistenceEpochWithBudget(fixture.observation.Before, claims, fixture.observation.Budget)
	id, _ := aggregate.HostConfigCodecForSyntax("json")
	codecs, err := aggregatecodec.ForOperation(t.Context(), []aggregate.CodecContractID{id}, narrowingLimits, narrowingLimits)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := sourceresolution.NewResolver(fixture.paths)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := lockbuild.BuildNativeDocumentsWithOptions(t.Context(), fixture.input.Environment, resolver, lockbuild.Options{}, narrowingLimits)
	if err != nil {
		t.Fatal(err)
	}
	input := Input{
		Context: reconcile.ContextInspect, Paths: fixture.paths,
		Resolver:    liveobserve.DestinationResolver(managedhostpath.Resolver(fixture.paths).Resolve),
		Environment: fixture.input.Environment, Lockfile: locked, Selection: fixture.input.Selection,
		SourceEpoch: &fixture.input.SourceEpoch, PersistenceEpoch: &epoch, Codecs: codecs,
		HookContributionEncoder: hookcodec.CanonicalHookContribution, MCPContributionEncoder: mcpcodec.CanonicalMCPBindingContribution,
		Native: &NativeAssessmentInput{Documents: &documents, Owner: fixture.observation.Owner, Registry: reader.actual, Budget: fixture.observation.Budget},
	}
	remaining := fixture.observation.Budget.RemainingBytes()
	assessment, err := Assess(t.Context(), input)
	if err != nil || len(assessment.NativeSources) != 1 || !assessment.CurrentState.Equal(fixture.observation.Before) {
		t.Fatalf("live native assessment lost source or complete before: %v", err)
	}
	for name, change := range map[string]func(*NativeAssessmentInput){
		"unqualified owner": func(native *NativeAssessmentInput) { native.Owner = stateauthority.Authority{} },
		"other registry":    func(native *NativeAssessmentInput) { native.Registry = ownership.EmptyRegistry() },
	} {
		t.Run(name, func(t *testing.T) {
			misbound := input
			facts := *input.Native
			change(&facts)
			misbound.Native = &facts
			result, err := Assess(t.Context(), misbound)
			if err == nil || len(result.NativeSources) != 0 || len(result.Reconciliation.Aggregates()) != 0 {
				t.Fatal("native assessment accepted facts from outside its observed batch", err)
			}
		})
	}
	if err := documents.ValidateBefore(t.Context()); err != nil || reader.loads != 1 || fixture.observation.Budget.RemainingBytes() != remaining || fixture.observation.Budget.RequireCapturePending() != nil {
		t.Fatalf("assessment closed/reread/reset the borrowed observation: %v loads=%d", err, reader.loads)
	}
	root := fixture.observation.Roots.Manifest
	if err := root.Root.ValidateSelectionBounded(root.SelectedPath, recovery.MaximumPhysicalPathDepth, fixture.observation.Budget); err != nil {
		t.Fatalf("assessment closed the externally owned root: %v", err)
	}
	input.Native = nil
	if result, err := Assess(t.Context(), input); err == nil || len(result.NativeSources) != 0 || len(result.Reconciliation.Aggregates()) != 0 {
		t.Fatalf("default assessment admitted native input or returned a prefix: %v", err)
	}
}
