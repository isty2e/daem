package readiness

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	lockobserve "github.com/isty2e/daem/internal/assurance/observe/lock"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/test/outputtest"
)

func nativePermissionLocations(t *testing.T, pi, codex string) journalnamespace.ReservationSelectors {
	t.Helper()
	var selectors []journalnamespace.ReservationSelector
	for host, path := range map[target.Target]string{target.TargetPi: pi, target.TargetCodex: codex} {
		reference, err := configuration.RootReferenceForScope(host, target.ScopeGlobal)
		if err != nil {
			t.Fatal(err)
		}
		selector, err := journalnamespace.NewReservationSelector(reference, path)
		if err != nil {
			t.Fatal(err)
		}
		selectors = append(selectors, selector)
	}
	result, err := journalnamespace.NewReservationSelectors(selectors)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestProjectNativeClaimCannotBorrowAGlobalAliasEvenWhenItsValueMatches(t *testing.T) {
	fixture := newNativePreparationFixtureAt(t, target.ScopeProject, `{"a":1}`, "agent/settings.json")
	pi := filepath.Dir(fixture.hostPath)
	fixture.observation.Protection = nativePermissionLocations(t, pi, filepath.Join(t.TempDir(), "codex"))
	reference, _ := configuration.RootReferenceForScope(target.TargetPi, target.ScopeGlobal)
	selection, err := journalnamespace.CaptureRoot(t.Context(), pi, fixture.observation.Budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = selection.Root.Close() })
	fixture.observation.Roots, err = fixture.observation.Roots.With(reference, selection)
	if err != nil {
		t.Fatal(err)
	}
	global, err := aggregate.NewDocumentAddress(target.TargetPi, target.ScopeGlobal, outputtest.Parse(t, "@pi-agent/settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	fixture.observation.Aliases = append(fixture.observation.Aliases, global)
	documents, err := journalpublication.ObserveNativeDocuments(t.Context(), fixture.observation)
	if err != nil {
		t.Fatal(err)
	}
	source, err := fixture.input.SourceEpoch.NativeSource(t.Context(), fixture.config)
	if err != nil {
		t.Fatal(err)
	}
	err = requireNativeSourcePermissions([]lockobserve.NativeSourceObservation{source}, &documents, fixture.observation.Before)
	var refusal *NativeClaimRefusal
	if !errors.As(err, &refusal) || refusal.Kind() != NativeClaimProjectGlobal {
		t.Fatal("matching bytes or a global peer alias granted project authority", err)
	}
	item, present, err := source.Projections()[0].ManagedAggregateContribution()
	if err != nil || !present {
		t.Fatal("fixture has no desired project key footprint", err)
	}
	err = requireNativeAggregatePermissions([]aggregate.SubjectContribution{item}, &documents)
	if !errors.As(err, &refusal) || refusal.Kind() != NativeClaimProjectGlobal {
		t.Fatal("aggregate participant bypassed project/global authority", err)
	}
	if err := requireNativeSourcePermissions(nil, &documents, fixture.observation.Before); err != nil {
		t.Fatal("recorded retirement was treated as new project acquisition", err)
	}
	if err := requireNativeAggregatePermissions(nil, &documents); err != nil {
		t.Fatal("retained aggregate state was treated as a desired footprint", err)
	}

	fixture.config, err = hostconfig.New(hostconfig.Spec{Name: "settings", Source: fixture.config.Source(), Target: target.TargetPi, Scope: target.ScopeGlobal, Path: "settings.json", Ownership: hostconfig.OwnershipKeys, Syntax: hostconfig.SyntaxJSON})
	if err != nil {
		t.Fatal(err)
	}
	fixture.input.Environment = nativePreparationEnvironment(t, fixture.config)
	fixture.refreshEpoch(t)
	source, err = fixture.input.SourceEpoch.NativeSource(t.Context(), fixture.config)
	if err != nil {
		t.Fatal(err)
	}
	err = requireNativeSourcePermissions([]lockobserve.NativeSourceObservation{source}, &documents, fixture.observation.Before)
	if !errors.As(err, &refusal) || refusal.Kind() != NativeClaimScopeTransfer {
		t.Fatal("cross-scope occupancy was mislabeled as unmanaged existing content", err)
	}
}

func TestPhysicalSharedRolesConstrainForeignGlobalDeclarationsWithoutRequiringAHostPeer(t *testing.T) {
	fixture := newNativePreparationFixtureAt(t, target.ScopeGlobal, `{"extensions":["fixture"]}`, "extra.json")
	root := filepath.Dir(fixture.hostPath)
	if err := os.Symlink(fixture.hostPath, filepath.Join(root, "settings.json")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	fixture.observation.Protection = nativePermissionLocations(t, root, root)
	pi, present := fixture.observation.Roots.Selection(mustNativeReference(t, target.TargetPi))
	if !present {
		t.Fatal("fixture has no Pi root")
	}
	var err error
	fixture.observation.Roots, err = fixture.observation.Roots.With(mustNativeReference(t, target.TargetCodex), pi)
	if err != nil {
		t.Fatal(err)
	}
	address, err := aggregate.NewDocumentAddress(target.TargetCodex, target.ScopeGlobal, outputtest.Parse(t, "@codex-home/extra.json"))
	if err != nil {
		t.Fatal(err)
	}
	fixture.observation.Aliases = []aggregate.DocumentAddress{address}
	documents, err := journalpublication.ObserveNativeDocuments(t.Context(), fixture.observation)
	if err != nil {
		t.Fatal(err)
	}
	fixture.input.Selection, err = targetselection.ForAvailableTargets([]target.Target{target.TargetPi, target.TargetCodex}, []string{"codex"})
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		fragment  string
		ownership hostconfig.Ownership
		syntax    hostconfig.Syntax
		refusal   error
	}{
		{`{"extensions":["fixture"]}`, hostconfig.OwnershipKeys, hostconfig.SyntaxJSON, configuration.ErrReserved},
		{`{"theme":"dark"}`, hostconfig.OwnershipKeys, hostconfig.SyntaxJSON, nil},
		{`{"theme":"dark"}`, hostconfig.OwnershipDocument, hostconfig.SyntaxJSON, configuration.ErrReserved},
		{`theme = 'dark'`, hostconfig.OwnershipKeys, hostconfig.SyntaxTOML, configuration.ErrGrammar},
	} {
		requireNativePreparationFile(t, fixture.sourcePath, []byte(scenario.fragment))
		fixture.config, err = hostconfig.New(hostconfig.Spec{Name: "settings", Source: fixture.config.Source(), Target: target.TargetCodex, Scope: target.ScopeGlobal, Path: "extra.json", Ownership: scenario.ownership, Syntax: scenario.syntax})
		if err != nil {
			t.Fatal(err)
		}
		fixture.input.Environment = nativePreparationEnvironment(t, fixture.config)
		fixture.refreshEpoch(t)
		source, err := fixture.input.SourceEpoch.NativeSource(t.Context(), fixture.config)
		if err != nil {
			t.Fatal(err)
		}
		err = requireNativeSourcePermissions([]lockobserve.NativeSourceObservation{source}, &documents, fixture.observation.Before)
		if scenario.refusal == nil {
			if err != nil {
				t.Fatal("ordinary global/global sharing required an unrelated host alias", err)
			}
			continue
		}
		var refusal *NativeClaimRefusal
		if !errors.As(err, &refusal) || refusal.Kind() != NativeClaimPhysicalRole || refusal.PhysicalRole().Target() != target.TargetPi || !errors.Is(err, scenario.refusal) {
			t.Fatal("foreign alias bypassed the actual shared document role", err)
		}
	}
}

func mustNativeReference(t *testing.T, selected target.Target) output.RootReference {
	t.Helper()
	reference, err := configuration.RootReferenceForScope(selected, target.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	return reference
}
