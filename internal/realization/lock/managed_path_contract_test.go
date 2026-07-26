package lock

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func TestManagedPathSubjectCorrelatesRealizationWithoutDuplicatingSupply(t *testing.T) {
	entityID := mustContractEntityID(t, entity.KindSkill, "oracle")
	placement := mustSkillPathPlacement(t, target.TargetCodex)
	realization := mustSkillPathRealization(t, placement, "oracle")
	subjectID, err := topologyprojection.Subject(entityID, placement.ID())
	if err != nil {
		t.Fatal(err)
	}
	writeRoute, removeRoute := mustManagedPathRoutes(t, placement)
	projection, err := NewManagedPathSubjectContract(ManagedPathSubjectInput{
		EntityID: entityID, SubjectID: subjectID, Realization: realization,
		WriteRouteID: writeRoute.RouteID(), RemoveRouteID: removeRoute.RouteID(),
	})
	if err != nil {
		t.Fatalf("NewManagedPathSubjectContract returned error: %v", err)
	}
	if _, supplied := projection.ExactSupply(); supplied {
		t.Fatal("managed path contract duplicated exact Supply")
	}
	write, ok := projection.OperationContract(OperationWriteProjection)
	if !ok || write.Route().RouteID != writeRoute.RouteID() || !write.OrdinaryMutationEligible() {
		t.Fatalf("write operation = %#v, present=%t", write, ok)
	}
	remove, ok := projection.OperationContract(OperationRemoveProjection)
	if !ok || remove.Route().RouteID != removeRoute.RouteID() || !remove.OrdinaryMutationEligible() {
		t.Fatalf("remove operation = %#v, present=%t", remove, ok)
	}

	supply := testExactSupplyContract(t, entity.KindSkill, "oracle", artifact.ArtifactKindDirectory)
	section, err := NewLockedSection([]LockedSubjectContract{projection, supply})
	if err != nil {
		t.Fatalf("NewLockedSection returned error: %v", err)
	}
	if section.Len() != 2 {
		t.Fatalf("section length = %d, want 2", section.Len())
	}
}

func TestLockedSectionRejectsOrphanAndProfileDriftedSkillProjection(t *testing.T) {
	entityID := mustContractEntityID(t, entity.KindSkill, "oracle")
	placement := mustSkillPathPlacement(t, target.TargetCodex)
	realization := mustSkillPathRealization(t, placement, "oracle")
	subjectID, err := topologyprojection.Subject(entityID, placement.ID())
	if err != nil {
		t.Fatal(err)
	}
	writeRoute, removeRoute := mustManagedPathRoutes(t, placement)
	projection, err := NewManagedPathSubjectContract(ManagedPathSubjectInput{
		EntityID: entityID, SubjectID: subjectID, Realization: realization,
		WriteRouteID: writeRoute.RouteID(), RemoveRouteID: removeRoute.RouteID(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewLockedSection([]LockedSubjectContract{projection}); err == nil || !strings.Contains(err.Error(), "no exact-Supply subject") {
		t.Fatalf("orphan projection error = %v", err)
	}

	drifted, err := NewManagedPathSubjectContract(ManagedPathSubjectInput{
		EntityID: entityID, SubjectID: subjectID, Realization: realization,
		WriteRouteID: "future.write", RemoveRouteID: removeRoute.RouteID(),
	})
	if err != nil {
		t.Fatal(err)
	}
	supply := testExactSupplyContract(t, entity.KindSkill, "oracle", artifact.ArtifactKindDirectory)
	if _, err := NewLockedSection([]LockedSubjectContract{supply, drifted}); err == nil || !strings.Contains(err.Error(), "canonical profile refinement") {
		t.Fatalf("profile drift error = %v", err)
	}
}

func mustSkillPathPlacement(t *testing.T, selected target.Target) profile.ManagedPathPlacement {
	t.Helper()
	placements, err := profile.ManagedPathPlacementsFor(entity.KindSkill, target.ScopeProject, []target.Target{selected})
	if err != nil {
		t.Fatal(err)
	}
	if len(placements) != 1 {
		t.Fatalf("placements = %#v", placements)
	}
	return placements[0]
}

func mustSkillPathRealization(t *testing.T, placement profile.ManagedPathPlacement, name string) realization.RealizationSpec {
	t.Helper()
	destination, err := placement.ChildDestination(name)
	if err != nil {
		t.Fatal(err)
	}
	writeRoute, _ := mustManagedPathRoutes(t, placement)
	spec, err := placement.Realize(destination, realization.PathProjectionCopy, writeRoute)
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func mustManagedPathRoutes(
	t *testing.T,
	placement profile.ManagedPathPlacement,
) (profile.OperationRoute, profile.OperationRoute) {
	t.Helper()
	writeRoute, err := profile.ManagedPathOperationRoute(placement, profile.OperationWrite)
	if err != nil {
		t.Fatal(err)
	}
	removeRoute, err := profile.ManagedPathOperationRoute(placement, profile.OperationRemove)
	if err != nil {
		t.Fatal(err)
	}
	return writeRoute, removeRoute
}
