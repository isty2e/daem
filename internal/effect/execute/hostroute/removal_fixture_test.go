package hostroute

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func newClaudeRemovalAction(
	t *testing.T,
	scope target.Scope,
	sourceRef string,
) carrierabsence.Action {
	t.Helper()
	return newDelegatedRemovalAction(
		t,
		desiredextension.CarrierClaudeCodePlugin,
		target.TargetClaudeCode,
		scope,
		desiredextension.SourceKindMarketplace,
		sourceRef,
		"claude-code.plugin-carrier",
		"context7",
	)
}

func newPiRemovalAction(
	t *testing.T,
	scope target.Scope,
	sourceRef string,
) carrierabsence.Action {
	t.Helper()
	return newDelegatedRemovalAction(
		t,
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		scope,
		desiredextension.SourceKindHostSource,
		sourceRef,
		"pi.package-carrier",
		sourceRef,
	)
}

func newCodexRemovalAction(
	t *testing.T,
	sourceRef string,
) carrierabsence.Action {
	t.Helper()
	return newDelegatedRemovalAction(
		t,
		desiredextension.CarrierCodexPlugin,
		target.TargetCodex,
		target.ScopeGlobal,
		desiredextension.SourceKindMarketplace,
		sourceRef,
		"codex.plugin-carrier",
		sourceRef,
	)
}

func newAntigravityRemovalAction(
	t *testing.T,
	sourceRef string,
) carrierabsence.Action {
	t.Helper()
	return newDelegatedRemovalAction(
		t,
		desiredextension.CarrierAntigravityCLIPlugin,
		target.TargetAntigravityCLI,
		target.ScopeGlobal,
		desiredextension.SourceKindHostSource,
		sourceRef,
		"antigravity-cli.plugin-carrier",
		sourceRef,
	)
}

func newDelegatedRemovalAction(
	t *testing.T,
	carrier desiredextension.Carrier,
	selectedTarget target.Target,
	scope target.Scope,
	sourceKind desiredextension.SourceKind,
	sourceRef string,
	namespace string,
	subjectKey string,
) carrierabsence.Action {
	t.Helper()
	record, relation := mustCarrierRecordAndRelation(
		t,
		carrier,
		selectedTarget,
		scope,
		sourceKind,
		sourceRef,
		namespace,
		"tools-managed",
		subjectKey,
	)
	identity := managedCarrierIdentityForRecord(t, record)
	installRequest, err := lock.DelegatedOperationRequest(record, lock.OperationInstall)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	owner, err := durablecarrier.NewStateAuthority(
		filepath.Join(root, ".daem", "state.json"),
		filepath.Join(root, "daem.toml"),
	)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := durablecarrier.NewManagedCarrierClaim(
		owner,
		identity,
		installRequest,
		durablecarrier.ClaimProvenanceInstalledObserved,
	)
	if err != nil {
		t.Fatal(err)
	}
	occupancy, err := durablecarrier.NewCarrierOccupancy(
		identity.Carrier(),
		[]durablecarrier.ManagedCarrierClaim{claim},
	)
	if err != nil {
		t.Fatal(err)
	}
	expected := relation.ExpectedRelation()
	rowSpec := observerelation.RowSpec{SubjectKey: string(expected.SubjectKey())}
	evidenceClass, err := identity.Carrier().RelationEvidence()
	if err != nil {
		t.Fatal(err)
	}
	if evidenceClass == extensiontopology.RelationEvidenceSourceExact {
		rowSpec.HasManagedInstanceKey = true
		rowSpec.ManagedInstanceKey = string(expected.ManagedInstanceKey())
	}
	row, err := observerelation.NewRow(rowSpec)
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := observerelation.NewInventory(observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows:         []observerelation.Row{row},
	})
	if err != nil {
		t.Fatal(err)
	}
	key, err := observerelation.NewCorrelationKey(record.SubjectID(), expected)
	if err != nil {
		t.Fatal(err)
	}
	carrierKey, admitted, err := lock.DelegatedRelationCarrierKey(record)
	if err != nil || !admitted {
		t.Fatalf("DelegatedRelationCarrierKey = (%#v, %t, %v)", carrierKey, admitted, err)
	}
	removal, admitted, err := lock.ResolveDelegatedCarrierRemoval(
		carrierKey,
		record.SubjectID(),
		expected,
		installRequest,
	)
	if err != nil || !admitted {
		t.Fatalf("ResolveDelegatedCarrierRemoval = (%#v, %t, %v)", removal, admitted, err)
	}
	route, err := carrierabsence.NewRouteAdmission(carrierabsence.RouteAdmissionInput{
		Operation:              removal.Operation(),
		Request:                removal.Request(),
		PreservesSharedCarrier: removal.PreservesSharedCarrier(),
		RemovedEffects:         removal.RemovedEffects(),
		RetainedEffects:        removal.RetainedEffects(),
		NonClaims:              removal.NonClaims(),
	})
	if err != nil {
		t.Fatal(err)
	}
	action, err := carrierabsence.NewAction(carrierabsence.ActionInput{
		Claim:   claim,
		Desired: carrierabsence.DesiredAbsent,
		Observation: observerelation.Correlation{
			Key:    key,
			Result: observerelation.Correlate(expected, inventory),
		},
		Occupancy: occupancy,
		Route:     route,
	})
	if err != nil {
		t.Fatal(err)
	}
	return action
}

func validRemovalAdapter(request RemovalRequest) (subprocess.CommandAttemptRequest, error) {
	return subprocess.CommandAttemptRequest{
		Command: "fake-host",
		Args:    []string{"remove"},
		WorkDir: request.WorkDir(),
	}, nil
}

type removalFixture struct {
	claim         durablecarrier.ManagedCarrierClaim
	occupancy     durablecarrier.CarrierOccupancy
	subject       topology.SubjectID
	observation   observerelation.Correlation
	removeRoute   carrierabsence.RouteAdmission
	removeRequest realizationdelegate.Request
}

func newRemovalFixture(t *testing.T) removalFixture {
	t.Helper()
	source, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindMarketplace,
		"context7@market",
	)
	if err != nil {
		t.Fatal(err)
	}
	key, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierClaudeCodePlugin,
		target.TargetClaudeCode,
		target.ScopeProject,
		source,
	)
	if err != nil {
		t.Fatal(err)
	}
	carrier, err := extensiontopology.NewCarrier(key)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topology.NewSubjectID(
		topology.SubjectHostRelation,
		"claude-code.plugin-carrier",
		"context7",
	)
	if err != nil {
		t.Fatal(err)
	}
	subjectKey, err := hostrelation.NewSubjectKey("context7@market")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := hostrelation.Derive(key, subject, subjectKey)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := durablecarrier.NewManagedCarrierIdentity(carrier, subject, expected)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	owner, err := durablecarrier.NewStateAuthority(
		filepath.Join(root, ".daem", "state.json"),
		filepath.Join(root, "daem.toml"),
	)
	if err != nil {
		t.Fatal(err)
	}
	installRequest, err := realizationdelegate.NewRequest(
		"test.install",
		"test-install-v1",
		removalTestHash("install"),
	)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := durablecarrier.NewManagedCarrierClaim(
		owner,
		identity,
		installRequest,
		durablecarrier.ClaimProvenanceInstalledObserved,
	)
	if err != nil {
		t.Fatal(err)
	}
	occupancy, err := durablecarrier.NewCarrierOccupancy(carrier, []durablecarrier.ManagedCarrierClaim{claim})
	if err != nil {
		t.Fatal(err)
	}
	row, err := observerelation.NewRow(observerelation.RowSpec{
		SubjectKey:            string(expected.SubjectKey()),
		HasManagedInstanceKey: true,
		ManagedInstanceKey:    string(expected.ManagedInstanceKey()),
	})
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := observerelation.NewInventory(observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows:         []observerelation.Row{row},
	})
	if err != nil {
		t.Fatal(err)
	}
	correlationKey, err := observerelation.NewCorrelationKey(subject, expected)
	if err != nil {
		t.Fatal(err)
	}
	removeRequest, err := realizationdelegate.NewRequest(
		"test.remove",
		"test-removal-v1",
		removalTestHash("remove"),
	)
	if err != nil {
		t.Fatal(err)
	}
	operation, err := lock.NewOperationContract(lock.OperationContractInput{
		Operation: lock.OperationRemove,
		Actuation: lock.ActuationDelegatedHostRoute,
		Authority: lock.AuthorityRemove,
		Route: lock.RouteContractRef{
			RouteID:                removeRequest.RouteID(),
			AdapterContractVersion: removeRequest.ContractVersion(),
		},
		EffectEnvelope:  lock.EffectEnvelopeComplete,
		Idempotency:     lock.ConditionallyIdempotent,
		Verification:    lock.VerificationHostRelation,
		TrustActivation: lock.TrustActivationNotRequired,
		Recovery:        lock.OperationRecoverySafeRetry,
	})
	if err != nil {
		t.Fatal(err)
	}
	removeRoute, err := carrierabsence.NewRouteAdmission(carrierabsence.RouteAdmissionInput{
		Operation:      operation,
		Request:        removeRequest,
		RemovedEffects: []string{"managed_relation"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return removalFixture{
		claim:     claim,
		occupancy: occupancy,
		subject:   subject,
		observation: observerelation.Correlation{
			Key:    correlationKey,
			Result: observerelation.Correlate(expected, inventory),
		},
		removeRoute:   removeRoute,
		removeRequest: removeRequest,
	}
}

func (fixture removalFixture) removeAction(t *testing.T) carrierabsence.Action {
	t.Helper()
	action, err := carrierabsence.NewAction(carrierabsence.ActionInput{
		Claim:       fixture.claim,
		Desired:     carrierabsence.DesiredAbsent,
		Observation: fixture.observation,
		Occupancy:   fixture.occupancy,
		Route:       fixture.removeRoute,
	})
	if err != nil {
		t.Fatal(err)
	}
	return action
}

func (fixture removalFixture) retainAction(t *testing.T) carrierabsence.Action {
	t.Helper()
	action, err := carrierabsence.NewAction(carrierabsence.ActionInput{
		Claim:     fixture.claim,
		Desired:   carrierabsence.DesiredRetained,
		Occupancy: fixture.occupancy,
		Route:     carrierabsence.UnavailableRoute(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return action
}

func removalTestHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(digest[:])
}
