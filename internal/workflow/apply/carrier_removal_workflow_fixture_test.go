package apply

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	assurancehostroute "github.com/isty2e/daem/internal/assurance/hostroute"
	observepostcondition "github.com/isty2e/daem/internal/assurance/observe/postcondition"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	executehostroute "github.com/isty2e/daem/internal/effect/execute/hostroute"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	lock "github.com/isty2e/daem/internal/realization/lock"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

type workflowFixture struct {
	root            string
	statePath       string
	projectRoot     *rootedpath.CapturedRoot
	action          carrierabsence.Action
	claim           durablecarrier.ManagedCarrierClaim
	expected        hostrelation.ExpectedRelation
	current         durable.Snapshot
	globalClaims    durablecarrier.GlobalCarrierClaims
	removeRequest   realizationdelegate.Request
	executorCalls   int
	runnerResult    subprocess.CommandResult
	postObservation observerelation.CorrelationResult
	effectEvidence  observepostcondition.EvidenceState
}

func newWorkflowFixture(t *testing.T, scope target.Scope) *workflowFixture {
	t.Helper()
	return newWorkflowFixtureWithPostconditions(
		t,
		scope,
		effectpostcondition.Set{},
		"",
	)
}

func newCoupledWorkflowFixture(t *testing.T, scope target.Scope) *workflowFixture {
	t.Helper()
	requirements, err := effectpostcondition.NewSet(
		[]effectpostcondition.Requirement{
			effectpostcondition.CarrierArtifactsAbsent,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return newWorkflowFixtureWithPostconditions(
		t,
		scope,
		requirements,
		observepostcondition.EvidenceSatisfied,
	)
}

func newWorkflowFixtureWithPostconditions(
	t *testing.T,
	scope target.Scope,
	effectPostconditions effectpostcondition.Set,
	effectEvidence observepostcondition.EvidenceState,
) *workflowFixture {
	t.Helper()
	root := t.TempDir()
	statePath := filepath.Join(root, ".daem", "state.json")
	manifestPath := filepath.Join(root, "daem.toml")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte("version = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(statePath, manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	source, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindMarketplace,
		"context7@market",
	)
	if err != nil {
		t.Fatal(err)
	}
	carrierKey, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierClaudeCodePlugin,
		target.TargetClaudeCode,
		scope,
		source,
	)
	if err != nil {
		t.Fatal(err)
	}
	carrier, err := extensiontopology.NewCarrier(carrierKey)
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
	expected, err := hostrelation.Derive(carrierKey, subject, subjectKey)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := durablecarrier.NewManagedCarrierIdentity(carrier, subject, expected)
	if err != nil {
		t.Fatal(err)
	}
	installRequest := workflowRequest(t, "test.install", "test-install-v1", "install")
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
		carrier,
		[]durablecarrier.ManagedCarrierClaim{claim},
	)
	if err != nil {
		t.Fatal(err)
	}
	preObservation := exactCorrelation(t, expected)
	correlationKey, err := observerelation.NewCorrelationKey(subject, expected)
	if err != nil {
		t.Fatal(err)
	}
	removeRequest := workflowRequest(t, "test.remove", "test-removal-v1", "remove")
	operation, err := lock.NewOperationContract(lock.OperationContractInput{
		Operation: lock.OperationRemove,
		Actuation: lock.ActuationDelegatedHostRoute,
		Authority: lock.AuthorityRemove,
		Route: lock.RouteContractRef{
			RouteID:                removeRequest.RouteID(),
			AdapterContractVersion: removeRequest.ContractVersion(),
		},
		EffectEnvelope:       lock.EffectEnvelopeComplete,
		Idempotency:          lock.ConditionallyIdempotent,
		Verification:         lock.VerificationHostRelation,
		TrustActivation:      lock.TrustActivationNotRequired,
		Recovery:             lock.OperationRecoverySafeRetry,
		EffectPostconditions: effectPostconditions.Requirements(),
	})
	if err != nil {
		t.Fatal(err)
	}
	route, err := carrierabsence.NewRouteAdmission(carrierabsence.RouteAdmissionInput{
		Operation:       operation,
		Request:         removeRequest,
		RemovedEffects:  []string{"managed_relation"},
		RetainedEffects: []string{"external_store"},
		NonClaims:       []string{"ambient_consumers"},
	})
	if err != nil {
		t.Fatal(err)
	}
	action, err := carrierabsence.NewAction(carrierabsence.ActionInput{
		Claim:   claim,
		Desired: carrierabsence.DesiredAbsent,
		Observation: observerelation.Correlation{
			Key:    correlationKey,
			Result: preObservation,
		},
		Occupancy: occupancy,
		Route:     route,
	})
	if err != nil {
		t.Fatal(err)
	}
	current := durable.EmptySnapshot()
	globalClaims := durablecarrier.EmptyGlobalCarrierClaims()
	if scope == target.ScopeProject {
		current, err = durable.NewSnapshot(durable.SnapshotInput{
			ManagedCarrierClaims: []durablecarrier.ManagedCarrierClaim{claim},
		})
	} else {
		globalClaims, err = durablecarrier.NewGlobalCarrierClaims(
			[]durablecarrier.ManagedCarrierClaim{claim},
		)
	}
	if err != nil {
		t.Fatal(err)
	}
	content, err := (statefile.Codec{}).Encode(current)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	projectRoot, err := rootedpath.CaptureRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := projectRoot.Close(); err != nil {
			t.Errorf("close project root: %v", err)
		}
	})
	return &workflowFixture{
		root:            root,
		statePath:       statePath,
		projectRoot:     projectRoot,
		action:          action,
		claim:           claim,
		expected:        expected,
		current:         current,
		globalClaims:    globalClaims,
		removeRequest:   removeRequest,
		runnerResult:    subprocess.CommandResult{Started: true, HasExitCode: true},
		postObservation: missingCorrelation(t, expected),
		effectEvidence:  effectEvidence,
	}
}

func (fixture *workflowFixture) input(t *testing.T) carrierRemovalInput {
	t.Helper()
	registry := fixture.globalClaims
	return carrierRemovalInput{
		StatePath:    fixture.statePath,
		SelectedRoot: fixture.root,
		Current:      fixture.current,
		GlobalClaims: fixture.globalClaims,
		Actions:      []carrierabsence.Action{fixture.action},
		ProjectRoot:  fixture.projectRoot,
		Adapter: func(request executehostroute.RemovalRequest) (
			subprocess.CommandAttemptRequest,
			error,
		) {
			return subprocess.CommandAttemptRequest{
				Command: "fake-host",
				Args:    []string{"remove", request.RouteRequest().RouteID()},
				WorkDir: request.WorkDir(),
			}, nil
		},
		Executor: subprocess.NewCommandExecutor(subprocess.CommandOptions{
			Clock: func() time.Time { return time.Unix(123, 0).UTC() },
			Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
				fixture.executorCalls++
				return fixture.runnerResult
			},
		}),
		Observer: func(
			_ context.Context,
			pending durablecarrier.PendingCarrierRemoval,
			_ []durablecarrier.ManagedCarrierClaim,
		) assurancehostroute.ObservationFact {
			if fixture.effectEvidence == "" {
				return assurancehostroute.CurrentObservation(fixture.postObservation)
			}
			evidence, err := observepostcondition.NewEvidence(
				effectpostcondition.CarrierArtifactsAbsent,
				fixture.effectEvidence,
			)
			if err != nil {
				t.Fatal(err)
			}
			evidenceSet, err := observepostcondition.NewSet(
				observepostcondition.SetInput{
					Subject:      pending.Identity().RelationSubject(),
					RouteRequest: pending.RemoveRequest(),
					Evidence:     []observepostcondition.Evidence{evidence},
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			return assurancehostroute.CurrentObservationWithEffectEvidence(
				fixture.postObservation,
				evidenceSet,
			)
		},
		RemoveGlobalClaim: func(
			_ context.Context,
			claim durablecarrier.ManagedCarrierClaim,
		) (durablecarrier.GlobalCarrierClaims, error) {
			next, changed, err := registry.WithoutClaim(claim)
			if err != nil {
				return durablecarrier.GlobalCarrierClaims{}, err
			}
			if !changed {
				return registry, nil
			}
			registry = next
			return registry, nil
		},
		Clock: func() time.Time { return time.Unix(123, 0).UTC() },
	}
}

func (fixture *workflowFixture) persistedState(t *testing.T) durable.Snapshot {
	t.Helper()
	content, err := os.ReadFile(fixture.statePath)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := (statefile.Codec{}).Decode(content)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func (fixture *workflowFixture) preparePendingSettlement(t *testing.T) {
	t.Helper()
	route := fixture.action.RouteAdmission()
	baselines, err := durablecarrier.NewEffectBaselineSet(nil)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := durablecarrier.NewPendingCarrierRemoval(
		fixture.claim,
		route.Request(),
		route.Operation().EffectPostconditions(),
		baselines,
	)
	if err != nil {
		t.Fatal(err)
	}
	observed, present := fixture.action.Observation()
	if !present {
		t.Fatal("workflow fixture has no observation key")
	}
	action, err := carrierabsence.NewAction(carrierabsence.ActionInput{
		Claim:   fixture.claim,
		Desired: carrierabsence.DesiredAbsent,
		Observation: observerelation.Correlation{
			Key:    observed.Key,
			Result: missingCorrelation(t, fixture.expected),
		},
		Occupancy: fixture.action.Occupancy(),
		Route:     carrierabsence.UnavailableRoute(),
		Pending:   &pending,
	})
	if err != nil {
		t.Fatal(err)
	}
	input := durable.SnapshotInput{
		PendingCarrierRemovals: []durablecarrier.PendingCarrierRemoval{pending},
	}
	if fixture.claim.Identity().Scope() == target.ScopeProject {
		input.ManagedCarrierClaims = []durablecarrier.ManagedCarrierClaim{fixture.claim}
	}
	current, err := durable.NewSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	content, err := (statefile.Codec{}).Encode(current)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.statePath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.action = action
	fixture.current = current
	fixture.postObservation = missingCorrelation(t, fixture.expected)
}

func exactCorrelation(
	t *testing.T,
	expected hostrelation.ExpectedRelation,
) observerelation.CorrelationResult {
	t.Helper()
	row, err := observerelation.NewRow(observerelation.RowSpec{
		SubjectKey:            string(expected.SubjectKey()),
		HasManagedInstanceKey: true,
		ManagedInstanceKey:    string(expected.ManagedInstanceKey()),
	})
	if err != nil {
		t.Fatal(err)
	}
	return correlateRows(t, expected, observerelation.EvidenceFresh, row)
}

func missingCorrelation(
	t *testing.T,
	expected hostrelation.ExpectedRelation,
) observerelation.CorrelationResult {
	t.Helper()
	return correlateRows(t, expected, observerelation.EvidenceFresh)
}

func staleCorrelation(
	t *testing.T,
	expected hostrelation.ExpectedRelation,
) observerelation.CorrelationResult {
	t.Helper()
	return correlateRows(t, expected, observerelation.EvidenceStale)
}

func correlateRows(
	t *testing.T,
	expected hostrelation.ExpectedRelation,
	freshness observerelation.EvidenceFreshness,
	rows ...observerelation.Row,
) observerelation.CorrelationResult {
	t.Helper()
	inventory, err := observerelation.NewInventory(observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    freshness,
		Rows:         rows,
	})
	if err != nil {
		t.Fatal(err)
	}
	return observerelation.Correlate(expected, inventory)
}

func workflowRequest(
	t *testing.T,
	routeID string,
	version string,
	identity string,
) realizationdelegate.Request {
	t.Helper()
	digest := sha256.Sum256([]byte(identity))
	request, err := realizationdelegate.NewRequest(
		routeID,
		version,
		"sha256:"+hex.EncodeToString(digest[:]),
	)
	if err != nil {
		t.Fatal(err)
	}
	return request
}
