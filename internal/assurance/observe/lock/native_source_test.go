package lock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/desired/hostconfig"
	lockmodel "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func TestNativeSourceObservationPreservesWholeAndEmptyKeyIntent(t *testing.T) {
	for _, fixture := range []nativeObservationFixture{
		newNativeKeyObservationFixture(t, `{}`),
		newNativeKeyObservationFixture(t, `{"a":1,"b":{"":true}}`),
		newNativeObservationFixture(t, target.TargetCodex, target.ScopeGlobal, false),
	} {
		epoch, err := ResolveNativeDocumentSourceEpoch(t.Context(), fixture.paths, fixture.environment, fixture.locked, fixture.selection, nativeKeyObservationLimits())
		if err != nil {
			t.Fatal(err)
		}
		observed, err := epoch.NativeSource(t.Context(), fixture.resource)
		if err != nil || observed.Validate() != nil {
			t.Fatal("actual selected native source did not produce its bound intent", err)
		}
		exact, _ := fixture.locked.Locked.ExactSupplySubject(fixture.resource.ID())
		placement, _ := observed.SourceContract().HostConfigPlacement()
		if !observed.SourceContract().Equal(exact) || observed.SourceContract().EntityID() != fixture.resource.ID() || placement.Ownership() != fixture.resource.Ownership() {
			t.Fatal("native source identity or explicit ownership mode was lost")
		}
		want := fixture.locked.Locked.Len() - 1
		projections := observed.Projections()
		if len(projections) != want {
			t.Fatal("native source lost a projection or invented whole ownership for empty keys")
		}
		if len(projections) > 0 {
			subject := projections[0].SubjectID()
			projections[0] = lockmodel.LockedSubjectContract{}
			if observed.Projections()[0].SubjectID() != subject {
				t.Fatal("caller altered the observed projection set")
			}
		}
	}
}

func TestNativeSourceObservationRefusesOtherEpochInputs(t *testing.T) {
	fixture := newNativeKeyObservationFixture(t, `{}`)
	epoch, err := ResolveNativeDocumentSourceEpoch(t.Context(), fixture.paths, fixture.environment, fixture.locked, fixture.selection, nativeKeyObservationLimits())
	if err != nil {
		t.Fatal(err)
	}
	otherSource, err := source.NewLocalSource(filepath.Join(fixture.root, "other.json"), source.LocalSourceModeVendor)
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range []hostconfig.Spec{
		{Name: "native", Source: fixture.resource.Source(), Target: target.TargetPi, Scope: target.ScopeProject, Path: "other.json", Ownership: hostconfig.OwnershipKeys},
		{Name: "native", Source: fixture.resource.Source(), Target: target.TargetPi, Scope: target.ScopeProject, Path: "custom.json", Ownership: hostconfig.OwnershipDocument},
		{Name: "native", Source: otherSource, Target: target.TargetPi, Scope: target.ScopeProject, Path: "custom.json", Ownership: hostconfig.OwnershipKeys},
		{Name: "other", Source: fixture.resource.Source(), Target: target.TargetPi, Scope: target.ScopeProject, Path: "custom.json", Ownership: hostconfig.OwnershipKeys},
	} {
		resource, err := hostconfig.New(spec)
		if err != nil {
			t.Fatal(err)
		}
		observed, err := epoch.NativeSource(t.Context(), resource)
		if err == nil || observed.Validate() == nil {
			t.Fatal("changed source/declaration/mode/placement yielded observed intent")
		}
	}
	selection, err := targetselection.ForAvailableTargets(target.SupportedTargets(), []string{"codex"})
	if err != nil {
		t.Fatal(err)
	}
	unselected, err := ResolveNativeDocumentSourceEpoch(t.Context(), fixture.paths, fixture.environment, fixture.locked, selection, nativeKeyObservationLimits())
	if err != nil {
		t.Fatal(err)
	}
	if observed, err := unselected.NativeSource(t.Context(), fixture.resource); err == nil || observed.Validate() == nil {
		t.Fatal("unselected source became an empty fragment")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if observed, err := epoch.NativeSource(ctx, fixture.resource); !errors.Is(err, context.Canceled) || observed.Validate() == nil {
		t.Fatal("canceled source lookup exposed native intent", err)
	}
	if observed, err := (SourceEpoch{}).NativeSource(t.Context(), fixture.resource); err == nil || observed.Validate() == nil {
		t.Fatal("zero epoch exposed native intent")
	}
}

func TestNativeSourceObservationRechecksCompleteSourceAndRetainedView(t *testing.T) {
	fixture := newNativeKeyObservationFixture(t, `{"a":1}`)
	epoch, err := ResolveNativeDocumentSourceEpoch(t.Context(), fixture.paths, fixture.environment, fixture.locked, fixture.selection, nativeKeyObservationLimits())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.root, "source.json"), []byte(`{"a":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if observed, err := epoch.NativeSource(t.Context(), fixture.resource); err == nil || observed.Validate() == nil {
		t.Fatal("changed retained source view exposed an intent")
	}
	fresh, err := ResolveNativeDocumentSourceEpoch(t.Context(), fixture.paths, fixture.environment, fixture.locked, fixture.selection, nativeKeyObservationLimits())
	if err != nil {
		t.Fatal(err)
	}
	if observed, err := fresh.NativeSource(t.Context(), fixture.resource); !errors.Is(err, ErrNativeSourceStale) || observed.Validate() == nil {
		t.Fatal("stale native source became a successful action input", err)
	}
}
