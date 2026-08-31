package catalog

import (
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/hostsurface"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

func TestProductManagedPathSurfacesMatchOwnerProfiles(t *testing.T) {
	t.Parallel()

	facets := profile.StaticManagedPathFacets()
	admissions := facets.Admissions()
	catalog := Product()
	views := catalog.ManagedPathSurfaces()
	if len(views) != len(admissions) || len(views) != 28 {
		t.Fatalf("managed-path views = %d, admissions = %d", len(views), len(admissions))
	}
	for index := 1; index < len(views); index++ {
		if hostsurface.CompareIDs(views[index-1].ID(), views[index].ID()) >= 0 {
			t.Fatalf("managed-path views are not in SurfaceID order at %d", index)
		}
	}

	placements := make(map[string]profile.ManagedPathPlacement)
	for _, placement := range facets.Placements() {
		placements[placement.ID()] = placement
	}
	for _, admission := range admissions {
		placement, ok := placements[admission.PlacementID()]
		if !ok {
			t.Fatalf("owner admission references missing placement %q", admission.PlacementID())
		}
		variant, err := hostsurface.ParseVariantID(placement.ID())
		if err != nil {
			t.Fatal(err)
		}
		key, err := hostsurface.NewSurfaceKey(
			admission.Target(),
			placement.Scope(),
			placement.ResourceKind(),
			variant,
		)
		if err != nil {
			t.Fatal(err)
		}
		view, ok := catalog.LookupManagedPath(key)
		if !ok {
			t.Fatalf("missing managed-path surface %s", hostsurface.MustSurfaceID(key))
		}
		byID, ok := catalog.ManagedPathSurface(view.ID())
		if !ok || byID.ID() != view.ID() {
			t.Fatalf("managed-path ID lookup mismatch for %s", view.ID())
		}
		if !view.ID().Key().Equal(key) || view.Placement().ID() != placement.ID() {
			t.Fatalf("managed-path identity mismatch for %s", view.ID())
		}
		if view.Key().Variant() != hostsurface.VariantID(placement.ID()) {
			t.Fatalf("variant = %q, want placement %q", view.Key().Variant(), placement.ID())
		}
		if view.Admission() != admission || view.IsDefaultPlacement() != admission.Default() {
			t.Fatalf("admission mismatch for %s", view.ID())
		}
		wantSupport, ok := profile.TargetSupport(admission.Target(), placement.ResourceKind())
		if !ok || view.Support() != wantSupport || !view.Support().Supported() {
			t.Fatalf("support mismatch for %s", view.ID())
		}
		if view.RealizationKind() != realization.RealizationManagedPathProjection {
			t.Fatalf("realization = %q", view.RealizationKind())
		}

		ownerProfile := profile.Profile(admission.Target())
		if got, want := view.DiscoveryLocations(), ownerProfile.DiscoveryLocations(placement.ResourceKind(), placement.Scope()); !slices.Equal(got, want) {
			t.Fatalf("discovery mismatch for %s: got %#v want %#v", view.ID(), got, want)
		}
		if got, want := view.RuntimeLocations(), ownerProfile.RuntimeLocations(placement.ResourceKind(), placement.Scope()); !slices.Equal(got, want) {
			t.Fatalf("runtime mismatch for %s: got %#v want %#v", view.ID(), got, want)
		}
		wantWrite, ok := ownerProfile.OperationRoute(
			placement.ResourceKind(),
			placement.ID(),
			profile.OperationWrite,
		)
		if !ok || view.WriteRoute() != wantWrite {
			t.Fatalf("write route mismatch for %s", view.ID())
		}
		wantRemove, ok := ownerProfile.OperationRoute(
			placement.ResourceKind(),
			placement.ID(),
			profile.OperationRemove,
		)
		if !ok || view.RemoveRoute() != wantRemove {
			t.Fatalf("remove route mismatch for %s", view.ID())
		}
	}

	ownerOrder := catalog.ManagedPathsInOwnerOrder()
	if len(ownerOrder) != len(admissions) {
		t.Fatalf("owner-order views = %d, want %d", len(ownerOrder), len(admissions))
	}
	for index, admission := range admissions {
		if ownerOrder[index].Admission() != admission {
			t.Fatalf("owner-order admission[%d] mismatch", index)
		}
	}

	shared := make(map[target.Target]struct{})
	for _, view := range views {
		if view.Placement().ID() == "skill.project.agents" {
			shared[view.Key().Target()] = struct{}{}
		}
	}
	if len(shared) != 4 {
		t.Fatalf("skill.project.agents target surfaces = %v", shared)
	}

	first := views[0].DiscoveryLocations()
	if len(first) > 0 {
		first[0] = profile.DiscoveryLocation{}
		if catalog.ManagedPathSurfaces()[0].DiscoveryLocations()[0] == (profile.DiscoveryLocation{}) {
			t.Fatal("managed-path discovery slice is not defensively copied")
		}
	}
	if len(catalog.Surfaces()) != 9 {
		t.Fatalf("MCP surface count changed to %d", len(catalog.Surfaces()))
	}
}

func TestManagedPathGroupQueriesMatchTargetProfileOrder(t *testing.T) {
	t.Parallel()

	compiled := Product()
	for _, selectedTarget := range target.SupportedTargets() {
		owner := profile.Profile(selectedTarget)
		for _, kind := range []entity.Kind{entity.KindInstructions, entity.KindSkill} {
			if got, want := compiled.HasManagedPathTarget(selectedTarget, kind), owner.Supports(kind); got != want {
				t.Fatalf("%s/%s target support = %t, want %t", selectedTarget, kind, got, want)
			}
		}
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			for _, kind := range []entity.Kind{entity.KindInstructions, entity.KindSkill} {
				views := compiled.ManagedPathViews(selectedTarget, scope, kind)
				admissions := owner.PlacementAdmissions(kind, scope)
				placements := owner.Placements(kind, scope)
				if len(views) != len(admissions) || len(views) != len(placements) {
					t.Fatalf(
						"%s/%s/%s views = %d, admissions = %d, placements = %d",
						selectedTarget,
						scope,
						kind,
						len(views),
						len(admissions),
						len(placements),
					)
				}
				for index, admission := range admissions {
					if views[index].Admission() != admission {
						t.Fatalf("%s/%s/%s admission[%d] mismatch", selectedTarget, scope, kind, index)
					}
					if views[index].Placement().ID() != placements[index].ID() {
						t.Fatalf("%s/%s/%s placement[%d] order mismatch", selectedTarget, scope, kind, index)
					}
					root := views[index].Placement().Root().String()
					byRoot, ok := compiled.ManagedPathAt(selectedTarget, scope, kind, root)
					if !ok || byRoot.ID() != views[index].ID() {
						t.Fatalf("%s/%s/%s root %q lookup mismatch", selectedTarget, scope, kind, root)
					}
				}
				ownerDefault, ownerDefaultErr := owner.DefaultPlacement(kind, scope)
				compiledDefault, compiledDefaultOK := compiled.ManagedPathDefault(selectedTarget, scope, kind)
				if ownerDefaultErr != nil {
					if compiledDefaultOK {
						t.Fatalf("%s/%s/%s compiled unexpected default %s", selectedTarget, scope, kind, compiledDefault.ID())
					}
				} else if !compiledDefaultOK || compiledDefault.Placement().Root() != ownerDefault.Root() {
					t.Fatalf("%s/%s/%s default mismatch", selectedTarget, scope, kind)
				}
				if _, ok := compiled.ManagedPathAt(selectedTarget, scope, kind, "missing"); ok {
					t.Fatalf("%s/%s/%s missing root unexpectedly admitted", selectedTarget, scope, kind)
				}
				if got, want := compiled.ManagedPathDiscoveryLocations(selectedTarget, scope, kind), owner.DiscoveryLocations(kind, scope); !slices.Equal(got, want) {
					t.Fatalf("%s/%s/%s discovery mismatch", selectedTarget, scope, kind)
				}
				if got, want := compiled.ManagedPathRuntimeLocations(selectedTarget, scope, kind), owner.RuntimeLocations(kind, scope); !slices.Equal(got, want) {
					t.Fatalf("%s/%s/%s runtime mismatch", selectedTarget, scope, kind)
				}
			}
		}
	}
}

func TestCompileManagedPathSurfacesRejectsInvalidOwnerJoins(t *testing.T) {
	t.Parallel()

	t.Run("duplicate admission", func(t *testing.T) {
		seed := productManagedPathSeed()
		seed.admissions = append(seed.admissions, seed.admissions[0])
		_, _, err := compileManagedPathSurfaces(seed)
		if err == nil || !strings.Contains(err.Error(), "duplicate admission") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("missing remove route", func(t *testing.T) {
		seed := productManagedPathSeed()
		for index, route := range seed.routes {
			if route.CorrelationID() == seed.placements[0].ID() && route.Operation() == profile.OperationRemove {
				seed.routes = append(seed.routes[:index:index], seed.routes[index+1:]...)
				break
			}
		}
		_, _, err := compileManagedPathSurfaces(seed)
		if err == nil || !strings.Contains(err.Error(), "lacks remove route") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("unsupported admitted target", func(t *testing.T) {
		seed := productManagedPathSeed()
		for index, support := range seed.supports {
			if support.Target() == target.TargetCodex && support.ResourceKind() == entity.KindSkill {
				replacement, err := profile.NewUnsupported(
					target.TargetCodex,
					entity.KindSkill,
					profile.UnsupportedReasonNotImplemented,
				)
				if err != nil {
					t.Fatal(err)
				}
				seed.supports[index] = replacement
				break
			}
		}
		_, _, err := compileManagedPathSurfaces(seed)
		if err == nil || !strings.Contains(err.Error(), "lacks supported owner fact") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("missing default", func(t *testing.T) {
		seed := productManagedPathSeed()
		for index, admission := range seed.admissions {
			if admission.Target() == target.TargetClaudeCode && admission.PlacementID() == "skill.project.claude" {
				replacement, err := profile.NewPlacementAdmission(
					admission.Target(),
					admission.PlacementID(),
					false,
				)
				if err != nil {
					t.Fatal(err)
				}
				seed.admissions[index] = replacement
				break
			}
		}
		_, _, err := compileManagedPathSurfaces(seed)
		if err == nil || !strings.Contains(err.Error(), "0 defaults") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("unreferenced placement", func(t *testing.T) {
		seed := productManagedPathSeed()
		placement, err := profile.NewManagedPathPlacement(profile.ManagedPathPlacementInput{
			ID:           "skill.project.unreferenced",
			ResourceKind: entity.KindSkill,
			Scope:        target.ScopeProject,
			Root:         ".unreferenced/skills",
			ContentKind:  realization.PathProjectionDirectory,
		})
		if err != nil {
			t.Fatal(err)
		}
		seed.placements = append(seed.placements, placement)
		_, _, err = compileManagedPathSurfaces(seed)
		if err == nil || !strings.Contains(err.Error(), "referenced 18 of 19 placements") {
			t.Fatalf("error = %v", err)
		}
	})
}
