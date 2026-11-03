package apply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	liveobserve "github.com/isty2e/daem/internal/assurance/observe/live"
	lockobserve "github.com/isty2e/daem/internal/assurance/observe/lock"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/realization/lock"
	lockbuild "github.com/isty2e/daem/internal/realization/lock/build"
	"github.com/isty2e/daem/internal/reconcile"
	projectionbuild "github.com/isty2e/daem/internal/reconcile/build/projection"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	sourceresolution "github.com/isty2e/daem/internal/supply/source/resolution"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

func nativePreparationStateCodec(t *testing.T) durable.SnapshotCodec {
	t.Helper()
	id, _ := aggregate.HostConfigCodecForSyntax("json")
	catalog, err := aggregatecodec.ForContributions(t.Context(), []aggregate.CodecContractID{id}, narrowingLimits)
	if err != nil {
		t.Fatal(err)
	}
	codec, err := statefile.NewNativeCodec(catalog)
	if err != nil {
		t.Fatal(err)
	}
	return codec
}

func nativePreparationOtherWholeState(t *testing.T) durable.ManagedPathState {
	t.Helper()
	id, err := entity.New(entity.KindHostConfig, "other")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := aggregate.HostConfigDocumentSubject(id, target.TargetPi, target.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := output.Parse("@pi-agent/extension-settings/other.json")
	if err != nil {
		t.Fatal(err)
	}
	state, err := durable.NewManagedPathState(subject, []target.Target{target.TargetPi}, target.ScopeGlobal, destination,
		artifact.HashFileContent([]byte(`{"other":1}`)), realization.PathProjectionFile, realization.PathPermissionsExact, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestNativeApplyStateDerivesNarrowingWithoutDeletingReleasedContent(t *testing.T) {
	for _, fragment := range []string{`{}`, `{"a":2,"new":true}`} {
		input := nativeActionPreparationInput(t, configdocument.JSON, `{"a":1,"released":9}`, `{"a":1,"released":9}`, fragment, hostconfig.OwnershipKeys)
		action, err := readiness.BuildNativeNarrowingAction(t.Context(), input)
		if err != nil {
			t.Fatal(err)
		}
		other := nativePreparationOtherWholeState(t)
		before, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: []durable.ManagedPathState{input.Baseline, other}})
		if err != nil {
			t.Fatal(err)
		}
		codec := nativePreparationStateCodec(t)
		prepared, err := execute.PrepareNativeApplyState(t.Context(), execute.NativeApplyStateInput{
			Before: before, Narrowings: []reconcile.NativeNarrowingAction{action}, StateCodec: codec,
		})
		if err != nil || prepared.Validate() != nil {
			t.Fatal("actual source/action did not derive native after-state", err)
		}
		after := prepared.AfterState()
		if !prepared.BeforeState().Equal(before) || len(after.ManagedPaths()) != 1 || !after.ManagedPaths()[0].Equal(other) {
			t.Fatal("narrowing failed to retire only its own whole baseline")
		}
		if len(after.ManagedAggregates()) != len(action.DesiredContributions()) || len(prepared.ManagedPathEffects()) != 0 || len(prepared.AggregateEffects()) != 0 {
			t.Fatal("state-only scope change manufactured legacy content effects")
		}
		for _, state := range after.ManagedAggregates() {
			matched := false
			for _, item := range action.DesiredContributions() {
				matched = matched || state.Subject() == item.SubjectID() && state.Contribution().Equal(item.Contribution())
			}
			if !matched {
				t.Fatal("narrowed state differs from its actual source-derived plan")
			}
		}
		encoded, err := codec.Encode(after)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := codec.Decode(encoded)
		if err != nil || !decoded.Equal(after) {
			t.Fatal("derived native after-state lost its selected-codec roundtrip", err)
		}
		copied := prepared.Narrowings()
		copied[0] = reconcile.NativeNarrowingAction{}
		if prepared.Narrowings()[0].Validate() != nil {
			t.Fatal("caller changed the prepared action membership")
		}
	}
}

func TestNativeApplyStateRejectsStaleAndDuplicateNarrowing(t *testing.T) {
	input := nativeActionPreparationInput(t, configdocument.JSON, `{"a":1}`, `{"a":1}`, `{"a":2}`, hostconfig.OwnershipKeys)
	action, err := readiness.BuildNativeNarrowingAction(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	before, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: []durable.ManagedPathState{input.Baseline}})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := durable.NewManagedPathState(input.Baseline.Subject(), input.Baseline.ConsumerTargets(), input.Baseline.Scope(), input.Baseline.Destination(), artifact.HashFileContent([]byte(`{"a":7}`)), input.Baseline.ContentKind(), input.Baseline.PermissionPolicy(), input.Baseline.FileMode())
	if err != nil {
		t.Fatal(err)
	}
	stale, err := before.WithManagedPaths([]durable.ManagedPathState{changed})
	if err != nil {
		t.Fatal(err)
	}
	codec := nativePreparationStateCodec(t)
	for _, candidate := range []execute.NativeApplyStateInput{
		{Before: durable.EmptySnapshot(), Narrowings: []reconcile.NativeNarrowingAction{action}, StateCodec: codec},
		{Before: stale, Narrowings: []reconcile.NativeNarrowingAction{action}, StateCodec: codec},
		{Before: before, Narrowings: []reconcile.NativeNarrowingAction{action, action}, StateCodec: codec},
		{Before: before, Narrowings: []reconcile.NativeNarrowingAction{action}},
	} {
		if prepared, err := execute.PrepareNativeApplyState(t.Context(), candidate); err == nil || prepared.Validate() == nil {
			t.Fatal("invalid or duplicate narrowing returned a usable state preparation")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if prepared, err := execute.PrepareNativeApplyState(ctx, execute.NativeApplyStateInput{Before: before, Narrowings: []reconcile.NativeNarrowingAction{action}, StateCodec: codec}); !errors.Is(err, context.Canceled) || prepared.Validate() == nil {
		t.Fatal("canceled native state preparation exposed a result", err)
	}
}

type nativeProjectSourceFixture struct {
	root      string
	paths     daempaths.Paths
	file      lock.File
	epoch     lockobserve.SourceEpoch
	observed  lockobserve.NativeSourceObservation
	selection targetselection.Selection
}

func nativeProjectStateSource(t *testing.T, root, name, fragment string, ownership hostconfig.Ownership) nativeProjectSourceFixture {
	t.Helper()
	path := filepath.Join(root, name+".source")
	if err := os.WriteFile(path, []byte(fragment), 0o600); err != nil {
		t.Fatal(err)
	}
	spec, err := source.NewLocalSource(path, source.LocalSourceModeVendor)
	if err != nil {
		t.Fatal(err)
	}
	config, err := hostconfig.New(hostconfig.Spec{Name: name, Source: spec, Target: target.TargetPi, Scope: target.ScopeProject, Path: "custom.json", Ownership: ownership, Syntax: hostconfig.SyntaxJSON})
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
	resolver, err := sourceresolution.NewResolver(paths)
	if err != nil {
		t.Fatal(err)
	}
	file, err := lockbuild.BuildNativeDocumentsWithOptions(t.Context(), environment, resolver, lockbuild.Options{}, narrowingLimits)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := targetselection.ForAvailableTargets(environment.Targets(), []string{"pi"})
	if err != nil {
		t.Fatal(err)
	}
	epoch, err := lockobserve.ResolveNativeDocumentSourceEpoch(t.Context(), paths, environment, file, selection, narrowingLimits)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := epoch.NativeSource(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	return nativeProjectSourceFixture{root: root, paths: paths, file: file, epoch: epoch, observed: observed, selection: selection}
}

func nativeStateFromObservedKeys(t *testing.T, observed lockobserve.NativeSourceObservation) []durable.ManagedAggregateState {
	t.Helper()
	var states []durable.ManagedAggregateState
	for _, contract := range observed.Projections() {
		contribution, present, err := contract.ManagedAggregateContribution()
		if err != nil || !present {
			t.Fatal("fixture source has no native key contribution", err)
		}
		state, err := durable.NewManagedAggregateState(contract.SubjectID(), contribution.Contribution())
		if err != nil {
			t.Fatal(err)
		}
		states = append(states, state)
	}
	return states
}

func nativeProjectWholeDecisions(t *testing.T, fixture nativeProjectSourceFixture, before durable.Snapshot, manageExisting bool) []reconcile.ManagedPathDecision {
	t.Helper()
	projection := fixture.observed.Projections()[0]
	expectation, err := projectionbuild.NewManagedPathExpectation(projection)
	if err != nil {
		t.Fatal(err)
	}
	placement, _ := fixture.observed.SourceContract().HostConfigPlacement()
	evidence, err := liveobserve.ManagedPathEvidence(t.Context(), func(destination output.Destination) (string, error) {
		return filepath.Join(fixture.root, filepath.FromSlash(destination.RelativePath())), nil
	}, []liveobserve.ManagedPathRequest{{Subject: projection.SubjectID(), Destination: placement.Destination(), ContentKind: realization.PathProjectionFile}})
	if err != nil {
		t.Fatal(err)
	}
	observations, err := fixture.epoch.Observations(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	selected, err := reconcile.NewSelectedTargets(fixture.selection.Targets())
	if err != nil {
		t.Fatal(err)
	}
	decisions, err := projectionbuild.BuildManagedPathDecisions(projectionbuild.ManagedPathInput{
		Locked: fixture.file.Locked, Expectations: []projectionbuild.ManagedPathExpectation{expectation}, SelectedTargets: selected,
		SupplyObservations: observations.ExactSupplies(), States: before.ManagedPaths(), Evidence: evidence, ManageUnmanagedMatches: manageExisting,
	})
	if err != nil {
		t.Fatal(err)
	}
	return decisions
}

func TestNativeApplyStateWholeAdoptionRetiresKeysWithoutContentEffects(t *testing.T) {
	root := t.TempDir()
	old := nativeProjectStateSource(t, root, "settings", `{"a":1,"b":2}`, hostconfig.OwnershipKeys)
	previous := nativeStateFromObservedKeys(t, old.observed)
	before, err := durable.NewSnapshot(durable.SnapshotInput{ManagedAggregates: previous})
	if err != nil {
		t.Fatal(err)
	}
	content := []byte(`{"a":7,"b":2,"released":9}`)
	whole := nativeProjectStateSource(t, root, "settings", string(content), hostconfig.OwnershipDocument)
	placement, _ := whole.observed.SourceContract().HostConfigPlacement()
	hostPath := filepath.Join(root, filepath.FromSlash(placement.Destination().RelativePath()))
	if err := os.MkdirAll(filepath.Dir(hostPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hostPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	decisions := nativeProjectWholeDecisions(t, whole, before, true)
	if len(decisions) != 1 || decisions[0].Kind() != reconcile.ManagedPathRecord || decisions[0].Reason() != reconcile.ReasonManagedExisting {
		t.Fatal("actual matching whole source did not produce explicit record-only adoption")
	}
	prepared, err := execute.PrepareNativeApplyState(t.Context(), execute.NativeApplyStateInput{
		Before: before, ManagedPaths: decisions, WholeSources: []lock.LockedSubjectContract{whole.observed.SourceContract()}, StateCodec: nativePreparationStateCodec(t),
	})
	if err != nil || prepared.Validate() != nil {
		t.Fatal("record/source did not derive state-only key retirement", err)
	}
	after := prepared.AfterState()
	if len(after.ManagedAggregates()) != 0 || len(after.ManagedPaths()) != 1 || after.ManagedPaths()[0].Subject() != decisions[0].Subject() || after.ManagedPaths()[0].ContentHash() != decisions[0].DesiredHash() {
		t.Fatal("native whole adoption retained stale key rows or lost whole identity")
	}
	if len(prepared.AggregateEffects()) != 0 || len(prepared.ManagedPathEffects()) != 1 || prepared.ManagedPathEffects()[0].Kind() != execute.ManagedPathEffectRecord || prepared.ManagedPathEffects()[0].RequiresPayload() {
		t.Fatal("metadata retirement manufactured a content delete or rewrite")
	}
	bindings := prepared.WholeAdoptions()
	if len(bindings) != 1 || !bindings[0].SourceContract().Equal(whole.observed.SourceContract()) || len(bindings[0].RetiredKeys()) != len(previous) {
		t.Fatal("operation-local adoption lost its actual before/source relation")
	}
	retired := bindings[0].RetiredKeys()
	original := retired[0]
	retired[0] = durable.ManagedAggregateState{}
	if !bindings[0].RetiredKeys()[0].Equal(original) {
		t.Fatal("caller changed the bound retirement set")
	}
	actual, err := os.ReadFile(hostPath)
	if err != nil || string(actual) != string(content) {
		t.Fatal("pure native state preparation changed host bytes", err)
	}
	if _, err := os.Stat(whole.paths.StatefilePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("state preparation crossed the publication boundary", err)
	}
}
