package lock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	lockmodel "github.com/isty2e/daem/internal/realization/lock"
	lockbuild "github.com/isty2e/daem/internal/realization/lock/build"
	"github.com/isty2e/daem/internal/supply/source"
	sourceresolution "github.com/isty2e/daem/internal/supply/source/resolution"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func TestNativeKeyEpochObservesEmptyAndNonemptySources(t *testing.T) {
	for _, payload := range []string{`{}`, `{"a":1,"b":{"":true}}`} {
		fixture := newNativeKeyObservationFixture(t, payload)
		check := func(wantStale bool) {
			t.Helper()
			epoch, err := ResolveNativeDocumentSourceEpoch(t.Context(), fixture.paths, fixture.environment, fixture.locked, fixture.selection, nativeKeyObservationLimits())
			if err != nil {
				t.Fatal(err)
			}
			bundle, err := epoch.Observations(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			values := bundle.ExactSupplies()
			resource, _ := fixture.locked.Locked.ExactSupplySubject(fixture.resource.ID())
			if len(values) != 1 || values[0].Subject() != resource.SubjectID() || values[0].Stale() != wantStale {
				t.Fatal("key source, including zero projected keys, was silently skipped or misclassified")
			}
			if _, err := epoch.FileResolution(fixture.resource.ID()); err != nil {
				t.Fatal("key source lost its retained exact view", err)
			}
		}
		check(false)
		// A changed source is stale before refinement, even if its new bytes
		// would not be valid input to a successful lock build.
		if err := os.WriteFile(filepath.Join(fixture.root, "source.json"), []byte("[not current locked source"), 0o600); err != nil {
			t.Fatal(err)
		}
		check(true)
		if _, err := ResolveSourceEpoch(t.Context(), fixture.paths, fixture.environment, fixture.locked, fixture.selection); err == nil {
			t.Fatal("ordinary ingress admitted native keys")
		}
	}
}

func TestNativeKeyEpochRejectsIncompleteOrAlteredProjectionMeaning(t *testing.T) {
	fixture := newNativeKeyObservationFixture(t, `{"a":1,"b":2}`)
	resource, _ := fixture.locked.Locked.ExactSupplySubject(fixture.resource.ID())
	for _, test := range []struct {
		name, payload string
		drop          int
	}{
		{name: "missing all", drop: 2},
		{name: "missing one", drop: 1},
		{name: "changed value", payload: `{"a":99,"b":2}`},
		{name: "extra key", payload: `{"a":1,"b":2,"c":3}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			from := fixture
			if test.payload != "" {
				from = newNativeKeyObservationFixture(t, test.payload)
			}
			subjects := []lockmodel.LockedSubjectContract{resource}
			for _, contract := range from.locked.Locked.Subjects() {
				if _, realized := contract.Realization(); realized {
					if test.drop > 0 {
						test.drop--
						continue
					}
					subjects = append(subjects, contract)
				}
			}
			section, err := lockmodel.NewLockedSection(subjects, nil)
			if err != nil {
				t.Fatal("fixture must pass static correlation so the source check is exercised", err)
			}
			tampered := fixture.locked
			tampered.Locked = section
			epoch, err := ResolveNativeDocumentSourceEpoch(t.Context(), fixture.paths, fixture.environment, tampered, fixture.selection, nativeKeyObservationLimits())
			if err != nil {
				t.Fatal(err)
			}
			bundle, err := epoch.Observations(t.Context())
			if err != nil || len(bundle.ExactSupplies()) != 1 || !bundle.ExactSupplies()[0].Stale() {
				t.Fatal("unchanged exact source incorrectly proved altered/missing desired projections fresh", err)
			}
		})
	}
}

func TestNativeKeyEpochRetainedViewFailureReturnsNoUsableBundle(t *testing.T) {
	fixture := newNativeKeyObservationFixture(t, `{"a":1}`)
	epoch, err := ResolveNativeDocumentSourceEpoch(t.Context(), fixture.paths, fixture.environment, fixture.locked, fixture.selection, nativeKeyObservationLimits())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.root, "source.json"), []byte(`{"a":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	bundle, err := epoch.Observations(t.Context())
	if err == nil || len(bundle.ExactSupplies()) != 0 {
		t.Fatal("retained-view mismatch became fresh or a partial observation", err)
	}
}

func TestNativeKeyEpochLimitAndCancellationDiscardEarlierSourceFacts(t *testing.T) {
	root := t.TempDir()
	paths := resolveTestPaths(t, filepath.Join(root, "daem.toml"))
	var configs []hostconfig.Config
	for _, input := range []struct{ name, payload string }{{"a-empty", `{}`}, {"z-large", `{"long":"not admitted by the later observation limit"}`}} {
		writeTestFile(t, root, input.name, input.payload)
		spec, err := source.NewLocalSource(filepath.Join(root, input.name), source.LocalSourceModeVendor)
		if err != nil {
			t.Fatal(err)
		}
		config, err := hostconfig.New(hostconfig.Spec{Name: input.name, Source: spec, Target: target.TargetPi, Scope: target.ScopeProject, Path: input.name + ".json", Ownership: hostconfig.OwnershipKeys, Syntax: hostconfig.SyntaxJSON})
		if err != nil {
			t.Fatal(err)
		}
		configs = append(configs, config)
	}
	environment := nativeObservationEnvironment(t, configs, nil)
	resolver, err := sourceresolution.NewResolver(paths)
	if err != nil {
		t.Fatal(err)
	}
	file, err := lockbuild.BuildNativeDocumentsWithOptions(t.Context(), environment, resolver, lockbuild.Options{}, nativeKeyObservationLimits())
	if err != nil {
		t.Fatal(err)
	}
	selection, err := targetselection.ForAvailableTargets(target.SupportedTargets(), []string{"pi"})
	if err != nil {
		t.Fatal(err)
	}
	epoch, err := ResolveNativeDocumentSourceEpoch(t.Context(), paths, environment, file, selection, configdocument.Limits{MaximumBytes: 10, MaximumDepth: 64})
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := epoch.Observations(t.Context())
	if !errors.Is(err, configdocument.ErrLimitExceeded) || len(bundle.ExactSupplies()) != 0 {
		t.Fatal("later source-limit refusal retained an earlier empty-source observation", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	bundle, err = epoch.Observations(ctx)
	if !errors.Is(err, context.Canceled) || len(bundle.ExactSupplies()) != 0 {
		t.Fatal("canceled native-key observation exposed source facts", err)
	}
}

func nativeKeyObservationLimits() configdocument.Limits {
	return configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64}
}

func newNativeKeyObservationFixture(t *testing.T, payload string) nativeObservationFixture {
	t.Helper()
	root := t.TempDir()
	writeTestFile(t, root, "source.json", payload)
	paths := resolveTestPaths(t, filepath.Join(root, "daem.toml"))
	spec, err := source.NewLocalSource(filepath.Join(root, "source.json"), source.LocalSourceModeVendor)
	if err != nil {
		t.Fatal(err)
	}
	resource, err := hostconfig.New(hostconfig.Spec{Name: "native", Source: spec, Target: target.TargetPi, Scope: target.ScopeProject, Path: "custom.json", Ownership: hostconfig.OwnershipKeys, Syntax: hostconfig.SyntaxJSON})
	if err != nil {
		t.Fatal(err)
	}
	environment := nativeObservationEnvironment(t, []hostconfig.Config{resource}, nil)
	resolver, err := sourceresolution.NewResolver(paths)
	if err != nil {
		t.Fatal(err)
	}
	file, err := lockbuild.BuildNativeDocumentsWithOptions(t.Context(), environment, resolver, lockbuild.Options{}, nativeKeyObservationLimits())
	if err != nil {
		t.Fatal(err)
	}
	selection, err := targetselection.ForAvailableTargets(target.SupportedTargets(), []string{"pi"})
	if err != nil {
		t.Fatal(err)
	}
	return nativeObservationFixture{root, paths, resource, environment, file, selection}
}
