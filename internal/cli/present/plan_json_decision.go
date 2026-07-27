package clipresent

import (
	"os"

	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func planJSONAggregateAction(decision reconcile.AggregateSubjectDecision) planJSONAction {
	resourceID, resourceValue := planJSONEntityResource(decision.Subject())
	row := planJSONAction{
		Kind: aggregatePublicKind(decision), Reason: string(decision.Reason()),
		Subject: planJSONSubjectFor(decision.Subject()), Target: string(decision.Target()),
		Scope: string(decision.Scope()), Destination: decision.Destination().String(),
		ContentPath: string(decision.ContentPath()), Detail: decision.Detail(),
		ResourceID: resourceID, Resource: resourceValue,
	}
	if decision.Reason() != reconcile.ReasonMissingLock {
		address := decision.Contract().Address()
		document := address.Document()
		row.Projection = &planJSONProjection{
			Target:      string(document.Target()),
			Scope:       string(document.Scope()),
			ConfigPath:  document.AggregateRoot().String(),
			ContentPath: string(address.ContentPath()),
		}
	}
	if previous, present := decision.PreviousContribution(); present {
		row.PreviousState = &planJSONPreviousState{
			Subject:     planJSONSubjectFor(decision.Subject()),
			Target:      string(previous.Target()),
			Scope:       string(previous.Scope()),
			Destination: previous.AggregateRoot().String(),
			ContentPath: previous.ContentPath(),
			ContentHash: string(artifact.HashFileContent([]byte(previous.CanonicalContribution()))),
		}
	}
	return row
}

func planJSONManagedPathAction(decision reconcile.ManagedPathDecision) planJSONAction {
	resourceID, resourceValue := planJSONEntityResource(decision.Subject())
	row := planJSONAction{
		Kind: managedPathPublicKind(decision), Reason: string(decision.Reason()),
		Subject: planJSONSubjectFor(decision.Subject()), Targets: targetStrings(decision.ConsumerTargets()),
		Scope: string(decision.Scope()), Destination: decision.Destination().String(),
		PlacementMode: string(decision.PlacementMode()), ContentKind: string(decision.ContentKind()),
		PermissionPolicy: string(decision.PermissionPolicy()),
		DesiredHash:      string(decision.DesiredHash()), LiveHash: string(decision.LiveHash()), Detail: decision.Detail(),
		ResourceID: resourceID, Resource: resourceValue,
	}
	if decision.ContentKind() == realization.PathProjectionFile && decision.PermissionPolicy() != realization.PathPermissionsNone {
		row.DesiredFileMode = fileModeJSONPointer(decision.DesiredFileMode())
	}
	if decision.ContentKind() == realization.PathProjectionFile && decision.LiveHash() != "" {
		row.LiveFileMode = fileModeJSONPointer(decision.LiveFileMode())
	}
	if safety, ok := managedPathSafetyState(decision); ok {
		row.Safety = safety
	}
	if previous, present := decision.PreviousState(); present {
		previousResourceID, previousResource := planJSONEntityResource(previous.Subject())
		row.PreviousState = &planJSONPreviousState{
			Subject: planJSONSubjectFor(previous.Subject()), Targets: targetStrings(previous.ConsumerTargets()),
			Scope: string(previous.Scope()), Destination: previous.Destination().String(),
			ContentHash: string(previous.ContentHash()), ContentKind: string(previous.ContentKind()),
			PermissionPolicy: string(previous.PermissionPolicy()),
			ResourceID:       previousResourceID, Resource: previousResource,
		}
		if previous.PermissionPolicy() == realization.PathPermissionsExact {
			row.PreviousState.FileMode = fileModeJSONPointer(previous.FileMode())
		}
	}
	return row
}

func fileModeJSONPointer(fileMode os.FileMode) *uint32 {
	value := uint32(fileMode.Perm())
	return &value
}

func planJSONEntityResource(subject topology.SubjectID) (string, *planJSONResource) {
	entityID, ok := topologyprojection.EntityID(subject)
	if !ok {
		return "", nil
	}
	value := &planJSONResource{Kind: string(entityID.Kind()), Name: entityID.Name()}
	return value.Kind + "/" + value.Name, value
}

func targetStrings(values []target.Target) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, string(value))
	}
	return result
}
