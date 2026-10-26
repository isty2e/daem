package readiness

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	lockobserve "github.com/isty2e/daem/internal/assurance/observe/lock"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	lockbuild "github.com/isty2e/daem/internal/realization/lock/build"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	sourceresolution "github.com/isty2e/daem/internal/supply/source/resolution"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

type nativePreparationFixture struct {
	input        NativeSourcePreparationInput
	observation  journal.NativeDocumentObservationInput
	config       hostconfig.Config
	paths        daempaths.Paths
	sourcePath   string
	hostPath     string
	registryPath string
	original     []byte
	codec        durable.SnapshotCodec
}

func newNativePreparationFixture(t *testing.T, scope target.Scope, fragment string) nativePreparationFixture {
	t.Helper()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "fragment.json")
	requireNativePreparationFile(t, sourcePath, []byte(fragment))
	spec, err := source.NewLocalSource(sourcePath, source.LocalSourceModeVendor)
	if err != nil {
		t.Fatal(err)
	}
	config, err := hostconfig.New(hostconfig.Spec{Name: "settings", Source: spec, Target: target.TargetPi, Scope: scope, Path: "extension-settings/test.json", Ownership: hostconfig.OwnershipKeys, Syntax: hostconfig.SyntaxJSON})
	if err != nil {
		t.Fatal(err)
	}
	defaults, err := desired.NewDefaults(target.ScopeProject, desiredskill.InstallModeCopy)
	if err != nil {
		t.Fatal(err)
	}
	environment, err := desired.New(desired.Spec{Targets: []target.Target{target.TargetPi}, Defaults: defaults, HostConfigs: []hostconfig.Config{config}})
	if err != nil {
		t.Fatal(err)
	}
	paths, err := daempaths.Resolve(filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	selection, err := targetselection.ForAvailableTargets(environment.Targets(), []string{"pi"})
	if err != nil {
		t.Fatal(err)
	}
	placement, err := profile.ResolveHostConfigPlacement(profile.HostConfigPlacementInput{Target: config.Target(), Scope: scope, Path: config.Path(), Ownership: config.Ownership(), Syntax: config.Syntax()})
	if err != nil {
		t.Fatal(err)
	}
	alias, err := aggregate.NewDocumentAddress(config.Target(), scope, placement.Destination())
	if err != nil {
		t.Fatal(err)
	}
	hostRoot := filepath.Join(root, "global")
	selectedRoot := root
	if scope == target.ScopeGlobal {
		selectedRoot = hostRoot
	}
	original := []byte(`{"a":1,"released":9}`)
	hostPath := filepath.Join(selectedRoot, filepath.FromSlash(placement.Destination().RelativePath()))
	requireNativePreparationFile(t, hostPath, original)
	subject, err := aggregate.HostConfigDocumentSubject(config.ID(), config.Target(), scope)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := durable.NewManagedPathState(subject, []target.Target{config.Target()}, scope, placement.Destination(), artifact.HashFileContent(original), realization.PathProjectionFile, realization.PathPermissionsExact, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	before, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: []durable.ManagedPathState{baseline}})
	if err != nil {
		t.Fatal(err)
	}
	codecID, _ := aggregate.HostConfigCodecForSyntax("json")
	contributions, err := aggregatecodec.ForContributions(t.Context(), []aggregate.CodecContractID{codecID}, narrowingLimits)
	if err != nil {
		t.Fatal(err)
	}
	codec, err := statefile.NewNativeCodec(contributions)
	if err != nil {
		t.Fatal(err)
	}
	stateBytes, err := codec.Encode(before)
	if err != nil {
		t.Fatal(err)
	}
	requireNativePreparationFile(t, paths.StatefilePath, stateBytes)
	stateAuthority, err := mutation.ObservePersistedDirectoryEntryAuthority(paths.StatefilePath)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(stateAuthority.Exact(), paths.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	budget := recovery.NewPhysicalCaptureBudget(nil)
	loaded, err := statefile.LoadOptionalWithOptions(t.Context(), paths.StatefilePath, statefile.ReadOptions{Codec: codec, PhysicalPathBudget: budget})
	if err != nil || !loaded.Equal(before) {
		t.Fatal("actual native state did not retain its baseline", err)
	}
	rootGrant := func(path string) journal.NativeDocumentRoot {
		t.Helper()
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		captured, err := rootedpath.CaptureRoot(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = captured.Close() })
		return journal.NativeDocumentRoot{SelectedPath: path, Root: captured}
	}
	roots := journal.NativeDocumentRoots{Manifest: rootGrant(root), PiAgent: rootGrant(hostRoot)}
	namespace, err := journal.ObserveNativeDocumentNamespace(t.Context(), []aggregate.DocumentAddress{alias}, roots, storagecommit.Adapter{}, budget)
	if err != nil {
		t.Fatal(err)
	}
	exact, known := namespace.Observation().Exact()
	if !known {
		t.Fatal("actual existing document has no exact namespace")
	}
	binding, err := ownership.NewDocumentBinding(owner, placement.Destination(), exact)
	if err != nil {
		t.Fatal(err)
	}
	var claims []ownership.Claim
	if scope == target.ScopeGlobal {
		address, err := ownership.NewManagedAddress(exact, output.WholePathFootprint())
		if err != nil {
			t.Fatal(err)
		}
		claim, err := ownership.NewActiveClaim(address, owner)
		if err != nil {
			t.Fatal(err)
		}
		claims = append(claims, claim)
	}
	registry, err := ownership.NewRegistry(claims, binding)
	if err != nil {
		t.Fatal(err)
	}
	registryBytes, err := ownershipstore.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(root, "registry.json")
	requireNativePreparationFile(t, registryPath, registryBytes)
	registryRoot, registryDestination, err := rootedpath.CaptureDestination(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registryRoot.Close() })
	store, err := ownershipstore.BindRooted(registryRoot, registryDestination, recovery.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	observation := journal.NativeDocumentObservationInput{Owner: owner, Before: loaded, Aliases: []aggregate.DocumentAddress{alias}, Roots: roots, Filesystem: storagecommit.Adapter{}, Registry: store, Limits: narrowingLimits, Budget: budget}
	documents, err := journal.ObserveNativeDocuments(t.Context(), observation)
	if err != nil {
		t.Fatal(err)
	}
	fixture := nativePreparationFixture{
		input:       NativeSourcePreparationInput{Environment: environment, Selection: selection, Documents: documents, DocumentLimits: narrowingLimits, ValueLimits: narrowingLimits},
		observation: observation, config: config, paths: paths, sourcePath: sourcePath,
		hostPath: hostPath, registryPath: registryPath, original: original, codec: codec,
	}
	fixture.refreshEpoch(t)
	return fixture
}

func (fixture *nativePreparationFixture) refreshEpoch(t *testing.T) {
	t.Helper()
	resolver, err := sourceresolution.NewResolver(fixture.paths)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := lockbuild.BuildNativeDocumentsWithOptions(t.Context(), fixture.input.Environment, resolver, lockbuild.Options{}, narrowingLimits)
	if err != nil {
		t.Fatal(err)
	}
	fixture.input.SourceEpoch, err = lockobserve.ResolveNativeDocumentSourceEpoch(t.Context(), fixture.paths, fixture.input.Environment, locked, fixture.input.Selection, narrowingLimits)
	if err != nil {
		t.Fatal(err)
	}
}

func requireNativePreparationFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestNativeSourcePreparationConsumesActualWholeBeforeGenericRemoval(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, fragment := range []string{`{}`, `{"a":2,"new":true}`} {
			t.Run(string(scope)+"/"+fragment, func(t *testing.T) {
				fixture := newNativePreparationFixture(t, scope, fragment)
				registryBefore, err := os.ReadFile(fixture.registryPath)
				if err != nil {
					t.Fatal(err)
				}
				remainingBytes := fixture.observation.Budget.RemainingBytes()
				prepared, err := PrepareNativeSources(t.Context(), fixture.input)
				if err != nil || prepared.Validate() != nil || len(prepared.Sources()) != 1 || len(prepared.Narrowings()) != 1 || len(prepared.RemainingManagedPaths()) != 0 {
					t.Fatal("real source and persisted whole baseline did not form a pre-removal transition", err)
				}
				action := prepared.Narrowings()[0]
				if !action.Baseline().Equal(fixture.observation.Before.ManagedPaths()[0]) || !bytes.Equal(action.CurrentDocument().Content(), fixture.original) {
					t.Fatal("partition lost actual before state or original bytes")
				}
				_, hasPlan := action.KeyPlan()
				if hasPlan == (fragment == `{}`) || (len(prepared.Sources()[0].Projections()) == 0) != (fragment == `{}`) {
					t.Fatal("empty source became absent intent or a content deletion")
				}
				state, err := execute.PrepareNativeApplyState(t.Context(), execute.NativeApplyStateInput{Before: fixture.input.Documents.BeforeState(), Narrowings: prepared.Narrowings(), StateCodec: fixture.codec})
				if err != nil || len(state.ManagedPathEffects()) != 0 || len(state.AfterState().ManagedPaths()) != 0 || len(state.Narrowings()) != 1 || (len(state.AfterState().ManagedAggregates()) == 0) != (fragment == `{}`) {
					t.Fatal("produced narrowing lost its state-only release or manufactured whole deletion", err)
				}
				views := prepared.Narrowings()
				views[0] = reconcile.NativeNarrowingAction{}
				if prepared.Narrowings()[0].Validate() != nil {
					t.Fatal("returned narrowing slice mutated preparation")
				}
				sources := prepared.Sources()
				sources[0] = lockobserve.NativeSourceObservation{}
				if prepared.Sources()[0].Validate() != nil {
					t.Fatal("returned source slice mutated preparation")
				}
				hostAfter, hostErr := os.ReadFile(fixture.hostPath)
				registryAfter, registryErr := os.ReadFile(fixture.registryPath)
				if hostErr != nil || registryErr != nil || !bytes.Equal(hostAfter, fixture.original) || !bytes.Equal(registryBefore, registryAfter) || fixture.observation.Budget.RemainingBytes() != remainingBytes || fixture.observation.Budget.RequireCapturePending() != nil || fixture.observation.Budget.AdmitObservation() == nil {
					t.Fatal("readiness mutated files or changed the containing capture allocation")
				}
			})
		}
	}
}

func TestNativeSourcePreparationFailureHasNoUsablePrefix(t *testing.T) {
	for _, fault := range []string{"zero documents", "zero epoch", "stale source", "canceled", "admitted capture"} {
		t.Run(fault, func(t *testing.T) {
			fixture := newNativePreparationFixture(t, target.ScopeProject, `{}`)
			ctx := t.Context()
			switch fault {
			case "zero documents":
				fixture.input.Documents = journal.NativeDocumentObservationBatch{}
			case "zero epoch":
				fixture.input.SourceEpoch = lockobserve.SourceEpoch{}
			case "stale source":
				requireNativePreparationFile(t, fixture.sourcePath, []byte(`{"changed":true}`))
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "admitted capture":
				if err := fixture.observation.Budget.AdmitRemovalDemands(recovery.RemovalDemandSet{}); err != nil {
					t.Fatal(err)
				}
			}
			prepared, err := PrepareNativeSources(ctx, fixture.input)
			if err == nil || prepared.Validate() == nil || len(prepared.Narrowings()) != 0 || len(prepared.Sources()) != 0 || len(prepared.RemainingManagedPaths()) != 0 {
				t.Fatal("failed readiness exposed a usable source/action/state prefix", err)
			}
			if fault == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation identity was lost", err)
			}
		})
	}
}

func TestNativeSourcePreparationPreservesOtherRowsInCompleteBefore(t *testing.T) {
	fixture := newNativePreparationFixture(t, target.ScopeProject, `{}`)
	config, err := hostconfig.New(hostconfig.Spec{Name: "removed-declaration", Source: fixture.config.Source(), Target: target.TargetPi, Scope: target.ScopeProject, Path: "extension-settings/old.json", Ownership: hostconfig.OwnershipDocument})
	if err != nil {
		t.Fatal(err)
	}
	placement, err := profile.ResolveHostConfigPlacement(profile.HostConfigPlacementInput{Target: config.Target(), Scope: config.Scope(), Path: config.Path(), Ownership: config.Ownership(), Syntax: config.Syntax()})
	if err != nil {
		t.Fatal(err)
	}
	subject, err := aggregate.HostConfigDocumentSubject(config.ID(), config.Target(), config.Scope())
	if err != nil {
		t.Fatal(err)
	}
	other, err := durable.NewManagedPathState(subject, []target.Target{config.Target()}, config.Scope(), placement.Destination(), artifact.HashFileContent(fixture.original), realization.PathProjectionFile, realization.PathPermissionsExact, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	rows := append(fixture.observation.Before.ManagedPaths(), other)
	complete, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: rows})
	if err != nil {
		t.Fatal(err)
	}
	content, err := fixture.codec.Encode(complete)
	if err != nil {
		t.Fatal(err)
	}
	requireNativePreparationFile(t, fixture.paths.StatefilePath, content)
	fixture.observation.Before, err = statefile.LoadOptionalWithOptions(t.Context(), fixture.paths.StatefilePath, statefile.ReadOptions{Codec: fixture.codec, PhysicalPathBudget: fixture.observation.Budget})
	if err != nil {
		t.Fatal(err)
	}
	fixture.input.Documents, err = journal.ObserveNativeDocuments(t.Context(), fixture.observation)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareNativeSources(t.Context(), fixture.input)
	if err != nil || len(prepared.Narrowings()) != 1 || len(prepared.RemainingManagedPaths()) != 1 || !prepared.RemainingManagedPaths()[0].Equal(other) || !fixture.input.Documents.BeforeState().Equal(complete) {
		t.Fatal("narrowing consumed unrelated state or truncated canonical before", err)
	}
	state, err := execute.PrepareNativeApplyState(t.Context(), execute.NativeApplyStateInput{Before: complete, Narrowings: prepared.Narrowings(), StateCodec: fixture.codec})
	if err != nil || len(state.AfterState().ManagedPaths()) != 1 || !state.AfterState().ManagedPaths()[0].Equal(other) {
		t.Fatal("state consumer lost a row outside the narrowing", err)
	}
}
