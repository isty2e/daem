package hostroute

import (
	"errors"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	"github.com/isty2e/daem/internal/desired/entity"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/realization/relation"
	reconciliation "github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

type builtFixture struct {
	subject  realization.DelegatedRelation
	record   lock.LockedSubjectContract
	lockfile lock.File
	action   reconciliation.RelationAction
	workDir  string
}

func relationActionFor(
	t *testing.T,
	record lock.LockedSubjectContract,
	subject realization.DelegatedRelation,
	inventorySpec observeclaudeplugin.InventorySpec,
	admission reconciliation.RelationRouteAdmissionDecision,
) reconciliation.RelationAction {
	t.Helper()
	inventory, err := observeclaudeplugin.NewInventory(inventorySpec)
	if err != nil {
		t.Fatalf("NewInventory returned error: %v", err)
	}
	relation := lockedDelegatedRelation(t, record)
	identity := managedCarrierIdentityForRecord(t, record)
	action, err := reconciliation.NewRelationAction(reconciliation.RelationActionInput{
		CarrierIdentity: identity,
		RouteRequest:    relation.RouteRequest(),
		Correlation:     observeclaudeplugin.Correlate(subject, inventory),
		RouteAdmission:  admission,
	})
	if err != nil {
		t.Fatalf("relation.Plan returned error: %v", err)
	}
	return action
}

func managedCarrierIdentityForRecord(
	t *testing.T,
	record lock.LockedSubjectContract,
) durablecarrier.ManagedCarrierIdentity {
	t.Helper()
	identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(record)
	if err != nil || !admitted {
		t.Fatalf("ManagedCarrierIdentityFromLockedRecord = (%#v, %t, %v)", identity, admitted, err)
	}
	return identity
}

func hostDelegatedAdmission(t *testing.T) reconciliation.RelationRouteAdmissionDecision {
	t.Helper()
	admission, err := reconciliation.NewRelationRouteAdmissionDecision(reconciliation.RelationRouteAdmissionSpec{
		Row:               reconciliation.RouteAdmissionRowInstallCarrier,
		RequestedOutcome:  reconciliation.AdmissionOutcomeOrdinaryMutation,
		SelectedOutcome:   reconciliation.AdmissionOutcomeHostDelegated,
		ObservationPolicy: reconciliation.ObservationRequireCurrent,
	})
	if err != nil {
		t.Fatalf("NewRouteAdmissionDecision returned error: %v", err)
	}
	return admission
}

func attemptWhenUnsupportedAdmission(t *testing.T) reconciliation.RelationRouteAdmissionDecision {
	t.Helper()
	admission, err := reconciliation.NewRelationRouteAdmissionDecision(reconciliation.RelationRouteAdmissionSpec{
		Row:               reconciliation.RouteAdmissionRowInstallCarrier,
		RequestedOutcome:  reconciliation.AdmissionOutcomeOrdinaryMutation,
		SelectedOutcome:   reconciliation.AdmissionOutcomeHostDelegated,
		ObservationPolicy: reconciliation.ObservationAttemptWhenUnsupported,
	})
	if err != nil {
		t.Fatalf("NewRouteAdmissionDecision returned error: %v", err)
	}
	return admission
}

func lockedDelegatedRelation(t *testing.T, record lock.LockedSubjectContract) realization.DelegatedRelation {
	t.Helper()
	return snapshottest.DelegatedRelation(t, record)
}

func mustCarrierRecordAndRelation(
	t *testing.T,
	carrier desiredextension.Carrier,
	selectedTarget target.Target,
	scope target.Scope,
	sourceKind desiredextension.SourceKind,
	sourceRef string,
	subjectNamespace string,
	declarationID string,
	subjectKeyValue string,
) (lock.LockedSubjectContract, realization.DelegatedRelation) {
	t.Helper()
	source, err := desiredextension.NewSourceRef(sourceKind, sourceRef)
	if err != nil {
		t.Fatalf("NewSourceRef returned error: %v", err)
	}
	carrierKey, err := desiredextension.NewCarrierKey(carrier, selectedTarget, scope, source)
	if err != nil {
		t.Fatalf("NewCarrierKey returned error: %v", err)
	}
	subject := mustHostRelationSubjectID(t, subjectNamespace, declarationID)
	subjectKey, err := hostrelation.NewSubjectKey(subjectKeyValue)
	if err != nil {
		t.Fatalf("NewSubjectKey returned error: %v", err)
	}
	entityID, err := entity.New(entity.KindExtension, declarationID)
	if err != nil {
		t.Fatalf("entity.New returned error: %v", err)
	}
	record, err := lock.NewDelegatedRelationCarrierContract(entityID, carrierKey, subject, subjectKey)
	if err != nil {
		t.Fatalf("NewDelegatedRelationCarrierContract returned error: %v", err)
	}
	return record, snapshottest.DelegatedRelation(t, record)
}

func testLockedFile(t *testing.T, records ...lock.LockedSubjectContract) lock.File {
	t.Helper()
	return snapshottest.File(t, records...)
}

func assertValidationCode(t *testing.T, err error, want ReasonCode) *ValidationError {
	t.Helper()
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error = %v, want ValidationError", err)
	}
	if validation.Code() != want {
		t.Fatalf("code = %q, want %q (error %v)", validation.Code(), want, err)
	}
	return validation
}

func mustHostRelationSubjectID(t *testing.T, namespace string, key string) topology.SubjectID {
	t.Helper()
	subject, err := topology.NewSubjectID(topology.SubjectHostRelation, namespace, key)
	if err != nil {
		t.Fatalf("NewSubjectID returned error: %v", err)
	}
	return subject
}
