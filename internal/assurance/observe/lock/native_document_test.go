package lock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/desired/instructions"
	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	daempaths "github.com/isty2e/daem/internal/paths"
	lockmodel "github.com/isty2e/daem/internal/realization/lock"
	lockbuild "github.com/isty2e/daem/internal/realization/lock/build"
	"github.com/isty2e/daem/internal/supply/source"
	sourceresolution "github.com/isty2e/daem/internal/supply/source/resolution"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

type nativeObservationFixture struct {
	root        string
	paths       daempaths.Paths
	resource    hostconfig.Config
	environment desired.Environment
	locked      lockmodel.File
	selection   targetselection.Selection
}

func nativeObservationEnvironment(t *testing.T, configs []hostconfig.Config, ordinary []instructions.Instructions) desired.Environment {
	t.Helper()
	defaults, err := desired.NewDefaults(target.ScopeProject, desiredskill.InstallModeCopy)
	if err != nil {
		t.Fatal(err)
	}
	environment, err := desired.New(desired.Spec{Targets: target.SupportedTargets(), Defaults: defaults, HostConfigs: configs, Instructions: ordinary})
	if err != nil {
		t.Fatal(err)
	}
	return environment
}

func nativeObservationConfig(t *testing.T, original hostconfig.Config, spec source.Source, path string, syntax hostconfig.Syntax) hostconfig.Config {
	t.Helper()
	config, err := hostconfig.New(hostconfig.Spec{Name: original.ID().Name(), Source: spec, Target: original.Target(), Scope: original.Scope(), Path: path, Ownership: hostconfig.OwnershipDocument, Syntax: syntax})
	if err != nil {
		t.Fatal(err)
	}
	return config
}

func newNativeObservationFixture(t *testing.T, selected target.Target, scope target.Scope, mixed bool) nativeObservationFixture {
	t.Helper()
	root := t.TempDir()
	writeTestFile(t, root, "source.md", "# original\n")
	writeTestFile(t, root, "ordinary.md", "# ordinary\n")
	paths := resolveTestPaths(t, filepath.Join(root, "daem.toml"))
	spec, err := source.NewLocalSource(filepath.Join(root, "source.md"), source.LocalSourceModeVendor)
	if err != nil {
		t.Fatal(err)
	}
	resource, err := hostconfig.New(hostconfig.Spec{Name: "native", Source: spec, Target: selected, Scope: scope, Path: "prompts/review.md", Ownership: hostconfig.OwnershipDocument})
	if err != nil {
		t.Fatal(err)
	}
	var ordinary []instructions.Instructions
	if mixed {
		ordinarySource, err := source.NewLocalSource(filepath.Join(root, "ordinary.md"), source.LocalSourceModeVendor)
		if err != nil {
			t.Fatal(err)
		}
		value, err := instructions.New(instructions.Spec{Name: "ordinary", Source: ordinarySource, Scope: target.ScopeProject, Targets: []target.Target{selected}})
		if err != nil {
			t.Fatal(err)
		}
		ordinary = append(ordinary, value)
	}
	environment := nativeObservationEnvironment(t, []hostconfig.Config{resource}, ordinary)
	resolver, err := sourceresolution.NewResolver(paths)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := lockbuild.BuildNativeDocumentsWithOptions(t.Context(), environment, resolver, lockbuild.Options{}, configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := targetselection.ForAvailableTargets(target.SupportedTargets(), []string{string(selected)})
	if err != nil {
		t.Fatal(err)
	}
	return nativeObservationFixture{root, paths, resource, environment, locked, selection}
}

func TestNativeDocumentSourceEpochObservesRealFreshAndChangedIdentity(t *testing.T) {
	for _, selected := range []target.Target{target.TargetPi, target.TargetCodex} {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			t.Run(string(selected)+"/"+string(scope), func(t *testing.T) {
				fixture := newNativeObservationFixture(t, selected, scope, false)
				check := func(wantStale bool) {
					t.Helper()
					epoch, err := ResolveNativeDocumentSourceEpoch(t.Context(), fixture.paths, fixture.environment, fixture.locked, fixture.selection, configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64})
					if err != nil {
						t.Fatal(err)
					}
					observations, err := epoch.Observations(t.Context())
					if err != nil {
						t.Fatal(err)
					}
					values := observations.ExactSupplies()
					resource := mustExactSupplySubject(t, fixture.locked, fixture.resource.ID())
					if len(values) != 1 || values[0].Subject() != resource.SubjectID() || values[0].Stale() != wantStale {
						t.Fatalf("wrong actual source observation: %#v", values)
					}
					if _, err := epoch.FileResolution(fixture.resource.ID()); err != nil {
						t.Fatalf("actual raw file resolution not retained: %v", err)
					}
				}
				check(false)
				writeTestFile(t, fixture.root, "source.md", "# changed\n")
				check(true)
			})
		}
	}
}

func TestNativeDocumentSourceEpochIncludesDeclarationIntentAndSourceID(t *testing.T) {
	fixture := newNativeObservationFixture(t, target.TargetPi, target.ScopeProject, false)
	writeTestFile(t, fixture.root, "other.md", "# original\n")
	other, err := source.NewLocalSource(filepath.Join(fixture.root, "other.md"), source.LocalSourceModeVendor)
	if err != nil {
		t.Fatal(err)
	}
	for _, changed := range []hostconfig.Config{
		nativeObservationConfig(t, fixture.resource, fixture.resource.Source(), "prompts/other.md", fixture.resource.Syntax()),
		nativeObservationConfig(t, fixture.resource, fixture.resource.Source(), fixture.resource.Path(), hostconfig.SyntaxJSON),
		nativeObservationConfig(t, fixture.resource, other, fixture.resource.Path(), fixture.resource.Syntax()),
	} {
		environment := nativeObservationEnvironment(t, []hostconfig.Config{changed}, nil)
		epoch, err := ResolveNativeDocumentSourceEpoch(t.Context(), fixture.paths, environment, fixture.locked, fixture.selection, configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64})
		if err != nil {
			t.Fatal(err)
		}
		observations, err := epoch.Observations(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		values := observations.ExactSupplies()
		if len(values) != 1 || !values[0].Stale() {
			t.Fatal("source-ID or declaration intent change was classified fresh")
		}
	}
}

func TestNativeDocumentSourceEpochSharesBatchAndRetainsRawSiblingWithoutPartialObservations(t *testing.T) {
	fixture := newNativeObservationFixture(t, target.TargetPi, target.ScopeProject, true)
	resolver, err := sourceresolution.NewResolver(fixture.paths)
	if err != nil {
		t.Fatal(err)
	}
	recorder := &recordingSourceBatchResolver{inner: resolver}
	candidates, err := nativeDocumentLockObservationCandidates(t.Context(), fixture.environment.HostConfigs(), fixture.locked, fixture.selection, configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64})
	if err != nil {
		t.Fatal(err)
	}
	epoch, err := resolveSourceEpochCandidates(t.Context(), recorder, fixture.environment, fixture.locked, fixture.selection, candidates)
	if err != nil {
		t.Fatal(err)
	}
	if len(recorder.requests) != 2 {
		t.Fatalf("mixed source batch lost a family: %d", len(recorder.requests))
	}
	observations, err := epoch.Observations(t.Context())
	if err != nil || len(observations.ExactSupplies()) != 2 {
		t.Fatalf("mixed observations: %v", err)
	}
	if err := os.Remove(filepath.Join(fixture.root, "source.md")); err != nil {
		t.Fatal(err)
	}
	failed, err := ResolveNativeDocumentSourceEpoch(t.Context(), fixture.paths, fixture.environment, fixture.locked, fixture.selection, configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64})
	if err != nil {
		t.Fatalf("batch lost entity-specific failure semantics: %v", err)
	}
	if _, err := failed.FileResolution(fixture.environment.Instructions()[0].ID()); err != nil {
		t.Fatalf("successful raw sibling discarded: %v", err)
	}
	partial, err := failed.Observations(t.Context())
	if err == nil || len(partial.ExactSupplies()) != 0 {
		t.Fatalf("failed native source returned partial fresh evidence: %v", err)
	}
}

func TestNativeDocumentSourceEpochSelectionMissingLockAndDefaultIsolation(t *testing.T) {
	fixture := newNativeObservationFixture(t, target.TargetPi, target.ScopeProject, false)
	if err := os.Remove(filepath.Join(fixture.root, "source.md")); err != nil {
		t.Fatal(err)
	}
	otherSelection, err := targetselection.ForAvailableTargets(target.SupportedTargets(), []string{"codex"})
	if err != nil {
		t.Fatal(err)
	}
	section, err := lockmodel.NewNativeLockedSection(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct {
		locked    lockmodel.File
		selection targetselection.Selection
	}{
		{fixture.locked, otherSelection}, {lockmodel.File{Version: lockmodel.NativeVersion, Locked: section}, fixture.selection},
	} {
		epoch, err := ResolveNativeDocumentSourceEpoch(t.Context(), fixture.paths, fixture.environment, input.locked, input.selection, configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64})
		if err != nil {
			t.Fatal(err)
		}
		observations, err := epoch.Observations(t.Context())
		if err != nil || len(observations.ExactSupplies()) != 0 {
			t.Fatalf("unselected/missing-lock source was resolved: %v", err)
		}
		if _, err := epoch.FileResolution(fixture.resource.ID()); err == nil {
			t.Fatal("unselected source retained an invented raw result")
		}
	}
	if _, err := ResolveSourceEpoch(t.Context(), fixture.paths, fixture.environment, fixture.locked, fixture.selection); err == nil {
		t.Fatal("default source observation admitted native declarations")
	}
	wrong := fixture.locked
	wrong.Version = lockmodel.CurrentVersion
	if _, err := ResolveNativeDocumentSourceEpoch(t.Context(), fixture.paths, fixture.environment, wrong, fixture.selection, configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64}); err == nil {
		t.Fatal("native source observer admitted default generation")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ResolveNativeDocumentSourceEpoch(ctx, fixture.paths, fixture.environment, fixture.locked, fixture.selection, configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled source epoch: %v", err)
	}
}

func TestNativeDocumentSourceEpochRejectsSourceKindAndPreservesCancellation(t *testing.T) {
	fixture := newNativeObservationFixture(t, target.TargetPi, target.ScopeProject, false)
	epoch, err := ResolveNativeDocumentSourceEpoch(t.Context(), fixture.paths, fixture.environment, fixture.locked, fixture.selection, configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if values, err := epoch.Observations(ctx); !errors.Is(err, context.Canceled) || len(values.ExactSupplies()) != 0 {
		t.Fatalf("canceled observation returned evidence: %v", err)
	}
	if err := os.Remove(filepath.Join(fixture.root, "source.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(fixture.root, "source.md"), 0o700); err != nil {
		t.Fatal(err)
	}
	directory, err := ResolveNativeDocumentSourceEpoch(t.Context(), fixture.paths, fixture.environment, fixture.locked, fixture.selection, configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64})
	if err != nil {
		t.Fatal(err)
	}
	if values, err := directory.Observations(t.Context()); err == nil || len(values.ExactSupplies()) != 0 {
		t.Fatal("directory source became valid native file evidence")
	}
}

func TestNativeDocumentSourceEpochDetectsExecutableAndPlacementChanges(t *testing.T) {
	fixture := newNativeObservationFixture(t, target.TargetPi, target.ScopeProject, false)
	for _, axis := range []struct {
		selected target.Target
		scope    target.Scope
	}{
		{target.TargetCodex, target.ScopeProject}, {target.TargetPi, target.ScopeGlobal},
	} {
		changed, err := hostconfig.New(hostconfig.Spec{Name: fixture.resource.ID().Name(), Source: fixture.resource.Source(), Target: axis.selected, Scope: axis.scope, Path: fixture.resource.Path(), Ownership: fixture.resource.Ownership()})
		if err != nil {
			t.Fatal(err)
		}
		epoch, err := ResolveNativeDocumentSourceEpoch(t.Context(), fixture.paths, nativeObservationEnvironment(t, []hostconfig.Config{changed}, nil), fixture.locked, fixture.selection, configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64})
		if err != nil {
			t.Fatal(err)
		}
		observed, err := epoch.Observations(t.Context())
		if err != nil || len(observed.ExactSupplies()) != 1 || !observed.ExactSupplies()[0].Stale() {
			t.Fatalf("changed placement axis became fresh: %v", err)
		}
	}
	path := filepath.Join(fixture.root, "source.md")
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Skip("filesystem does not expose an executable class")
	}
	epoch, err := ResolveNativeDocumentSourceEpoch(t.Context(), fixture.paths, fixture.environment, fixture.locked, fixture.selection, configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64})
	if err != nil {
		t.Fatal(err)
	}
	observed, err := epoch.Observations(t.Context())
	if err != nil || len(observed.ExactSupplies()) != 1 || !observed.ExactSupplies()[0].Stale() {
		t.Fatalf("changed executable class became fresh: %v", err)
	}
}
