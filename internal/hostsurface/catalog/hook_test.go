package catalog

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/hostsurface"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

func TestProductHookSurfacesMatchAggregateAndProfileOwners(t *testing.T) {
	t.Parallel()

	catalog := Product()
	placements := aggregate.ImplementedHookPlacements()
	views := catalog.HookSurfaces()
	if len(views) != len(placements) || len(views) != 4 {
		t.Fatalf("Hook views = %d, placements = %d", len(views), len(placements))
	}
	for _, placement := range placements {
		variant, err := hostsurface.ParseVariantID(string(placement.ID()))
		if err != nil {
			t.Fatal(err)
		}
		key, err := hostsurface.NewSurfaceKey(
			placement.Target(),
			placement.Scope(),
			entity.KindHook,
			variant,
		)
		if err != nil {
			t.Fatal(err)
		}
		view, ok := catalog.LookupHook(key)
		if !ok {
			t.Fatalf("missing Hook surface %s", hostsurface.MustSurfaceID(key))
		}
		byID, ok := catalog.HookSurface(view.ID())
		if !ok || byID.ID() != view.ID() {
			t.Fatalf("Hook ID lookup mismatch for %s", view.ID())
		}
		if view.Placement().ID() != placement.ID() ||
			view.Placement().CodecContractID() != placement.CodecContractID() ||
			view.Placement().AggregateRoot() != placement.AggregateRoot() {
			t.Fatalf("Hook placement mismatch for %s", view.ID())
		}
		if view.RealizationKind() != realization.RealizationManagedAggregateContribution {
			t.Fatalf("Hook realization = %q", view.RealizationKind())
		}
		wantSupport, ok := profile.TargetSupport(placement.Target(), entity.KindHook)
		if !ok || view.Support() != wantSupport || !view.Support().Supported() {
			t.Fatalf("Hook support mismatch for %s", view.ID())
		}
		write, remove, ok := profile.HookAggregateRouteIDs(placement.ID())
		if !ok || view.WriteRouteID() != write || view.RemoveRouteID() != remove {
			t.Fatalf("Hook routes = %q/%q, want %q/%q", view.WriteRouteID(), view.RemoveRouteID(), write, remove)
		}
		ownerProfile := profile.Profile(placement.Target())
		writeRoute, ok := ownerProfile.OperationRoute(entity.KindHook, string(placement.ID()), profile.OperationWrite)
		if !ok || writeRoute.RouteID() != view.WriteRouteID() {
			t.Fatalf("Hook write route parity failed for %s", view.ID())
		}
	}
	ownerOrder := catalog.HooksInOwnerOrder()
	for index, placement := range placements {
		if ownerOrder[index].Placement().ID() != placement.ID() {
			t.Fatalf("Hook owner order[%d] mismatch", index)
		}
	}
}

func TestProductHookAssetSurfacesPreserveSharedPhysicalPlacement(t *testing.T) {
	t.Parallel()

	catalog := Product()
	placements := profile.ImplementedHookAssetPlacements()
	views := catalog.HookAssetSurfaces()
	if len(placements) != 2 || len(views) != 4 {
		t.Fatalf("HookAsset placements/views = %d/%d", len(placements), len(views))
	}
	physicalCounts := make(map[string]int)
	for _, placement := range placements {
		variant, err := hostsurface.ParseVariantID(placement.ID())
		if err != nil {
			t.Fatal(err)
		}
		write, err := profile.HookAssetOperationRoute(placement, profile.OperationWrite)
		if err != nil {
			t.Fatal(err)
		}
		remove, err := profile.HookAssetOperationRoute(placement, profile.OperationRemove)
		if err != nil {
			t.Fatal(err)
		}
		for _, consumer := range placement.ConsumerTargets() {
			key, err := hostsurface.NewSurfaceKey(
				consumer,
				placement.Scope(),
				entity.KindHookAsset,
				variant,
			)
			if err != nil {
				t.Fatal(err)
			}
			view, ok := catalog.LookupHookAsset(key)
			if !ok {
				t.Fatalf("missing HookAsset surface %s", hostsurface.MustSurfaceID(key))
			}
			byID, ok := catalog.HookAssetSurface(view.ID())
			if !ok || byID.ID() != view.ID() {
				t.Fatalf("HookAsset ID lookup mismatch for %s", view.ID())
			}
			if view.Placement().ID() != placement.ID() || view.WriteRoute() != write || view.RemoveRoute() != remove {
				t.Fatalf("HookAsset owner facts mismatch for %s", view.ID())
			}
			if view.RealizationKind() != realization.RealizationManagedPathProjection {
				t.Fatalf("HookAsset realization = %q", view.RealizationKind())
			}
			if !view.HookSupport().Supported() || view.HookSupport().Target() != consumer {
				t.Fatalf("HookAsset support mismatch for %s", view.ID())
			}
			wantRealization, ok := profile.Profile(consumer).RealizationKind(entity.KindHookAsset)
			if !ok || wantRealization != view.RealizationKind() {
				t.Fatalf("HookAsset TargetProfile parity failed for %s", view.ID())
			}
			physicalCounts[view.Placement().ID()]++
		}
	}
	for placementID, count := range physicalCounts {
		if count != 2 {
			t.Fatalf("HookAsset placement %q has %d target surfaces", placementID, count)
		}
	}
	for _, unsupported := range []target.Target{target.TargetOpenCode, target.TargetPi, target.TargetAntigravityCLI} {
		for _, placement := range placements {
			variant, err := hostsurface.ParseVariantID(placement.ID())
			if err != nil {
				t.Fatal(err)
			}
			key, err := hostsurface.NewSurfaceKey(unsupported, placement.Scope(), entity.KindHookAsset, variant)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := catalog.LookupHookAsset(key); ok {
				t.Fatalf("unsupported target %q has HookAsset surface", unsupported)
			}
		}
	}
}

func TestCompileHookSurfacesRejectsIncompleteOwnerFacts(t *testing.T) {
	t.Parallel()

	t.Run("missing route", func(t *testing.T) {
		seed := productHookSeed()
		delete(seed.routes, seed.placements[0].ID())
		_, _, err := compileHookSurfaces(seed)
		if err == nil || !strings.Contains(err.Error(), "lacks write and remove routes") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("unsupported placement target", func(t *testing.T) {
		seed := productHookSeed()
		for index, support := range seed.supports {
			if support.Target() == target.TargetCodex && support.ResourceKind() == entity.KindHook {
				replacement, err := profile.NewUnsupported(
					target.TargetCodex,
					entity.KindHook,
					profile.UnsupportedReasonNotImplemented,
				)
				if err != nil {
					t.Fatal(err)
				}
				seed.supports[index] = replacement
				break
			}
		}
		_, _, err := compileHookSurfaces(seed)
		if err == nil || !strings.Contains(err.Error(), "lacks supported owner fact") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("unsupported HookAsset consumer", func(t *testing.T) {
		seed := productHookAssetSeed()
		for index, support := range seed.supports {
			if support.Target() == target.TargetClaudeCode && support.ResourceKind() == entity.KindHook {
				replacement, err := profile.NewUnsupported(
					target.TargetClaudeCode,
					entity.KindHook,
					profile.UnsupportedReasonNotImplemented,
				)
				if err != nil {
					t.Fatal(err)
				}
				seed.supports[index] = replacement
				break
			}
		}
		_, _, err := compileHookAssetSurfaces(seed)
		if err == nil || !strings.Contains(err.Error(), "unsupported consumer") {
			t.Fatalf("error = %v", err)
		}
	})
}
