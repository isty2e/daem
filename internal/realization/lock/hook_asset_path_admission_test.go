package lock

import (
	"os"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	resourcetopology "github.com/isty2e/daem/internal/topology/resource"
)

func TestLockedHookAssetProjectionRejectsExactModeOutsideFamilyPolicy(t *testing.T) {
	supply, projection := testHookAssetExactModeContracts(t, true, 0o640)
	if _, err := NewLockedSection([]LockedSubjectContract{supply, projection}); err == nil ||
		!strings.Contains(err.Error(), "must be 0600 or 0700") {
		t.Fatalf("NewLockedSection error = %v, want HookAsset exact-mode policy rejection", err)
	}
}

func TestLockedHookAssetProjectionCorrelatesExactModeWithFileUse(t *testing.T) {
	supply, projection := testHookAssetExactModeContracts(t, true, 0o600)
	if _, err := NewLockedSection([]LockedSubjectContract{supply, projection}); err == nil ||
		!strings.Contains(err.Error(), "does not match file use") {
		t.Fatalf("NewLockedSection error = %v, want HookAsset file-use correlation rejection", err)
	}
}

func testHookAssetExactModeContracts(
	t *testing.T,
	executable bool,
	fileMode uint32,
) (LockedSubjectContract, LockedSubjectContract) {
	t.Helper()
	entityID, err := entity.New(entity.KindHookAsset, "guard")
	if err != nil {
		t.Fatal(err)
	}
	contentHash := artifact.HashFileContentWithExecutable([]byte("#!/bin/sh\n"), executable)
	identity, err := artifact.NewExactIdentity(
		"local:hooks/guard?mode=vendor",
		"",
		artifact.ArtifactKindFile,
		contentHash,
	)
	if err != nil {
		t.Fatal(err)
	}
	fileUse, err := NewExactFileUse(target.ScopeProject, executable)
	if err != nil {
		t.Fatal(err)
	}
	derivation, err := NewDirectResolutionDerivation(identity)
	if err != nil {
		t.Fatal(err)
	}
	resourceSubject, err := resourcetopology.Subject(entityID)
	if err != nil {
		t.Fatal(err)
	}
	supply, err := NewExactSupplySubjectContract(ExactSupplySubjectInput{
		EntityID: entityID, SubjectID: resourceSubject, ExactSupply: identity,
		ExactFileUse: &fileUse, Derivation: derivation,
	})
	if err != nil {
		t.Fatal(err)
	}

	placement, err := profile.HookAssetPlacementFor(target.ScopeProject, []target.Target{target.TargetCodex})
	if err != nil {
		t.Fatal(err)
	}
	destination, err := placement.Destination("guard", contentHash)
	if err != nil {
		t.Fatal(err)
	}
	exactMode, err := realization.NewExactPathPermissionMode(os.FileMode(fileMode))
	if err != nil {
		t.Fatal(err)
	}
	realization, err := realization.NewManagedPathProjection(realization.ManagedPathProjectionInput{
		PlacementID: placement.ID(), ConsumerTargets: placement.ConsumerTargets(),
		Scope: target.ScopeProject, Destination: destination, ContentKind: realization.PathProjectionFile,
		PlacementMode: realization.PathProjectionCopy, PermissionPolicy: realization.PathPermissionsExact,
		ExactPermissionMode: exactMode, AdapterContractVersion: "managed-hook-asset-file-v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	projectionSubject, err := topologyprojection.Subject(entityID, placement.ID())
	if err != nil {
		t.Fatal(err)
	}
	writeRoute, err := profile.HookAssetOperationRoute(placement, profile.OperationWrite)
	if err != nil {
		t.Fatal(err)
	}
	removeRoute, err := profile.HookAssetOperationRoute(placement, profile.OperationRemove)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := NewManagedPathSubjectContract(ManagedPathSubjectInput{
		EntityID: entityID, SubjectID: projectionSubject, Realization: realization,
		WriteRouteID: writeRoute.RouteID(), RemoveRouteID: removeRoute.RouteID(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return supply, projection
}
