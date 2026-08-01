package execute

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/observe"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/payload"
	"github.com/isty2e/daem/internal/realization"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	reconciliation "github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/topology"
	"github.com/isty2e/daem/test/outputtest"
)

func TestApplyRetainsClassifiableJournalForPostVisibilityStatefileFaults(t *testing.T) {
	for _, phase := range []string{"verify_entry", "sync_parent", "sync_ancestors"} {
		t.Run(phase, func(t *testing.T) {
			fixture := newApplyEventFixture(t)
			action := fixture.createAction("create", "CREATE.md", "created\n")
			var events []Event

			_, err := ApplyWithOptions(context.Background(), fixture.input([]applyEventAction{action}), ApplyOptions{
				Events: func(event Event) { events = append(events, event) },
				commitStatefile: func(ctx context.Context, path string, content []byte, mode os.FileMode) statefileCommitOutcome {
					if err := commitFile(ctx, testFilesystem(), path, content, mode); err != nil {
						t.Fatalf("commit visible statefile before injected %s fault: %v", phase, err)
					}
					return statefileCommitOutcome{
						status: statefileCommitIndeterminate,
						err:    fmt.Errorf("injected %s fault", phase),
					}
				},
			})
			if err == nil || !strings.Contains(err.Error(), "statefile commit is indeterminate") {
				t.Fatalf("ApplyWithOptions error = %v, want indeterminate statefile commit", err)
			}
			assertHostFileContent(t, fixture.hostPath("CREATE.md"), "created\n")
			assertNoEventKind(t, events, EventRollbackRestoreStarted)
			assertNoEventKind(t, events, EventJournalCleaned)

			recoveryPlan, planErr := journal.LoadActivePlanWithOptions(
				context.Background(),
				fixture.paths.journalPaths(),
				testPlanLoadOptions(fixture.paths),
			)
			if planErr != nil {
				t.Fatalf("LoadActivePlan after %s fault: %v", phase, planErr)
			}
			if recoveryPlan.Classification() != recovery.ClassificationCleanAfter || recoveryPlan.HasErrors() {
				t.Fatalf("recovery plan after %s fault = %q errors=%t, want clean_after", phase, recoveryPlan.Classification(), recoveryPlan.HasErrors())
			}
		})
	}
}

func TestApplyRetainsNeedsRollbackJournalWhenIndeterminateStatefileIsNotVisible(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action := fixture.createAction("create", "CREATE.md", "created\n")

	_, err := ApplyWithOptions(context.Background(), fixture.input([]applyEventAction{action}), ApplyOptions{
		commitStatefile: func(context.Context, string, []byte, os.FileMode) statefileCommitOutcome {
			return statefileCommitOutcome{status: statefileCommitIndeterminate, err: errors.New("injected uncertain visibility")}
		},
	})
	if err == nil || !strings.Contains(err.Error(), "statefile commit is indeterminate") {
		t.Fatalf("ApplyWithOptions error = %v, want indeterminate statefile commit", err)
	}
	assertHostFileContent(t, fixture.hostPath("CREATE.md"), "created\n")
	assertHostMissing(t, fixture.paths.StatefilePath)

	recoveryPlan, planErr := journal.LoadActivePlanWithOptions(
		context.Background(),
		fixture.paths.journalPaths(),
		testPlanLoadOptions(fixture.paths),
	)
	if planErr != nil {
		t.Fatalf("LoadActivePlan after invisible indeterminate commit: %v", planErr)
	}
	if recoveryPlan.Classification() != recovery.ClassificationNeedsRollback || recoveryPlan.HasErrors() {
		t.Fatalf("recovery plan = %q errors=%t, want needs_rollback", recoveryPlan.Classification(), recoveryPlan.HasErrors())
	}
}

func TestApplyRollsBackOnlyProvenUncommittedStatefileFailure(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action := fixture.createAction("create", "CREATE.md", "created\n")

	_, err := ApplyWithOptions(context.Background(), fixture.input([]applyEventAction{action}), ApplyOptions{
		commitStatefile: func(context.Context, string, []byte, os.FileMode) statefileCommitOutcome {
			return statefileCommitOutcome{status: statefileUncommitted, err: errors.New("injected pre-visibility failure")}
		},
	})
	if err == nil || !strings.Contains(err.Error(), "host changes rolled back") {
		t.Fatalf("ApplyWithOptions error = %v, want rollback after uncommitted failure", err)
	}
	assertHostMissing(t, fixture.hostPath("CREATE.md"))
	assertHostMissing(t, fixture.paths.StatefilePath)
	assertNoActiveRecoveryOperation(t, fixture.paths.RecoveryDir)
}

func TestManagedPathApplyRollsBackOnlyProvenUncommittedStatefileFailure(t *testing.T) {
	fixture := newApplyEventFixture(t)
	destination := outputtest.Parse(t, ".agents/skills/oracle")
	projection := testManagedPathEffectState(t, "oracle", destination)
	source := filepath.Join(t.TempDir(), "oracle")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatalf("create managed path payload: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("---\nname: oracle\n---\n"), 0o600); err != nil {
		t.Fatalf("write managed path payload: %v", err)
	}
	view, err := access.OpenView(source)
	if err != nil {
		t.Fatalf("open managed path payload: %v", err)
	}
	hash, err := view.Hash(t.Context())
	if err != nil {
		t.Fatalf("hash managed path payload: %v", err)
	}
	identity, err := artifact.NewExactIdentity(
		"test:managed-path-statefile-failure",
		"",
		artifact.ArtifactKindDirectory,
		hash,
	)
	if err != nil {
		t.Fatalf("construct managed path payload identity: %v", err)
	}
	desiredHash := hash
	effect := ManagedPathEffect{create: &managedPathCreateEffect{facts: managedPathEffectFacts{
		subject:          projection.Subject(),
		consumerTargets:  projection.ConsumerTargets(),
		scope:            projection.Scope(),
		destination:      destination,
		desiredHash:      desiredHash,
		contentKind:      realization.PathProjectionDirectory,
		permissionPolicy: realization.PathPermissionsNone,
	}}}
	directoryPayload, err := payload.NewDirectoryPayload(t.Context(), projection.Subject(), identity, view)
	if err != nil {
		t.Fatalf("construct managed path payload: %v", err)
	}
	payloads, err := payload.NewPayloadSet([]payload.Payload{directoryPayload}, nil)
	if err != nil {
		t.Fatalf("construct managed path payload set: %v", err)
	}
	evidence, err := observe.NewManagedPathEvidence(projection.Subject(), destination, false, "", 0)
	if err != nil {
		t.Fatalf("construct managed path evidence: %v", err)
	}

	_, err = ApplyWithOptions(t.Context(), ApplyInput{
		Paths:               fixture.paths,
		Resolver:            destinationResolver(fixture.paths),
		ManagedPathEffects:  []ManagedPathEffect{effect},
		ManagedPathEvidence: []observe.ManagedPathEvidence{evidence},
		CurrentState:        durable.EmptySnapshot(),
		Payloads:            payloads,
		StateCodec:          testStateCodec(),
		Filesystem:          testFilesystem(),
	}, ApplyOptions{
		commitStatefile: func(context.Context, string, []byte, os.FileMode) statefileCommitOutcome {
			return statefileCommitOutcome{status: statefileUncommitted, err: errors.New("injected statefile failure")}
		},
	})
	if err == nil || !strings.Contains(err.Error(), "host changes rolled back") {
		t.Fatalf("ApplyWithOptions error = %v, want guarded managed-path rollback", err)
	}
	assertHostMissing(t, filepath.Join(fixture.root, filepath.FromSlash(destination.RelativePath())))
	assertHostMissing(t, fixture.paths.StatefilePath)
	assertNoActiveRecoveryOperation(t, fixture.paths.RecoveryDir)
}

func TestStateOnlyApplyRetainsJournalForIndeterminateStatefileCommit(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action, pending := statefileCommitRelationAction(
		t,
		fixture.paths.StatefilePath,
		filepath.Join(fixture.root, "daem.toml"),
	)
	input := fixture.input(nil)
	input.CurrentState = statefileCommitSnapshot(t, pending)
	input.ConfirmedRelationActions = []reconciliation.RelationAction{action}
	writeRecoveryTestStatefile(t, fixture.paths.StatefilePath, input.CurrentState)

	_, err := ApplyWithOptions(context.Background(), input, ApplyOptions{
		commitStatefile: func(ctx context.Context, path string, content []byte, mode os.FileMode) statefileCommitOutcome {
			if err := commitFile(ctx, testFilesystem(), path, content, mode); err != nil {
				t.Fatalf("commit visible state-only statefile: %v", err)
			}
			return statefileCommitOutcome{status: statefileCommitIndeterminate, err: errors.New("injected sync_parent fault")}
		},
	})
	if err == nil || !strings.Contains(err.Error(), "statefile commit is indeterminate") {
		t.Fatalf("ApplyWithOptions error = %v, want indeterminate state-only commit", err)
	}

	recoveryPlan, planErr := journal.LoadActivePlanWithOptions(
		context.Background(),
		fixture.paths.journalPaths(),
		testPlanLoadOptions(fixture.paths),
	)
	if planErr != nil {
		t.Fatalf("LoadActivePlan after state-only commit: %v", planErr)
	}
	if recoveryPlan.Classification() != recovery.ClassificationCleanAfter || recoveryPlan.HasErrors() {
		t.Fatalf("state-only recovery plan = %q errors=%t, want clean_after", recoveryPlan.Classification(), recoveryPlan.HasErrors())
	}
	if guarded := recoveryPlan.GuardedActions(); len(guarded) != 0 {
		t.Fatalf("state-only journal acquired host path authority: %#v", guarded)
	}
}

func TestStateOnlyApplyRetainsCleanBeforeJournalWhenIndeterminateCommitIsNotVisible(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action, pending := statefileCommitRelationAction(
		t,
		fixture.paths.StatefilePath,
		filepath.Join(fixture.root, "daem.toml"),
	)
	input := fixture.input(nil)
	input.CurrentState = statefileCommitSnapshot(t, pending)
	input.ConfirmedRelationActions = []reconciliation.RelationAction{action}
	writeRecoveryTestStatefile(t, fixture.paths.StatefilePath, input.CurrentState)

	_, err := ApplyWithOptions(context.Background(), input, ApplyOptions{
		commitStatefile: func(context.Context, string, []byte, os.FileMode) statefileCommitOutcome {
			return statefileCommitOutcome{status: statefileCommitIndeterminate, err: errors.New("injected uncertain visibility")}
		},
	})
	if err == nil || !strings.Contains(err.Error(), "statefile commit is indeterminate") {
		t.Fatalf("ApplyWithOptions error = %v, want indeterminate state-only commit", err)
	}
	visibleState, loadErr := statefile.Load(t.Context(), fixture.paths.StatefilePath)
	if loadErr != nil {
		t.Fatalf("load before-visible state-only statefile: %v", loadErr)
	}
	pendingInstalls := visibleState.PendingCarrierInstalls()
	if len(pendingInstalls) != 1 || !pendingInstalls[0].ExactEqual(pending) {
		t.Fatalf("state-only statefile = %#v, want pending before state", visibleState)
	}

	recoveryPlan, planErr := journal.LoadActivePlanWithOptions(
		context.Background(),
		fixture.paths.journalPaths(),
		testPlanLoadOptions(fixture.paths),
	)
	if planErr != nil {
		t.Fatalf("LoadActivePlan after invisible state-only commit: %v", planErr)
	}
	if recoveryPlan.Classification() != recovery.ClassificationCleanBefore || recoveryPlan.HasErrors() {
		t.Fatalf("state-only recovery plan = %q errors=%t, want clean_before", recoveryPlan.Classification(), recoveryPlan.HasErrors())
	}
}

func TestInvalidStatefileCommitOutcomesRequireRecovery(t *testing.T) {
	for _, outcome := range []statefileCommitOutcome{
		{},
		{status: statefileCommitted, err: errors.New("contradictory success")},
		{status: statefileUncommitted},
		{status: statefileCommitIndeterminate},
	} {
		if outcome.committed() || !outcome.requiresRecovery() {
			t.Fatalf("invalid outcome %#v was treated as committed or rollback-safe", outcome)
		}
	}
}

func statefileCommitRelationAction(
	t *testing.T,
	statefilePath string,
	manifestPath string,
) (reconciliation.RelationAction, durablecarrier.PendingCarrierInstall) {
	t.Helper()
	subjectKey, err := hostrelation.NewSubjectKey("context7@market")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topology.NewSubjectID(topology.SubjectHostRelation, "claude-code.plugin-carrier", "context7-managed")
	if err != nil {
		t.Fatal(err)
	}
	identity, relation := testManagedCarrierIdentity(t, subject, subjectKey)
	row, err := observerelation.NewRow(observerelation.RowSpec{
		SubjectKey:            string(subjectKey),
		HasManagedInstanceKey: true,
		ManagedInstanceKey:    string(relation.ManagedInstanceKey()),
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
	admission, err := reconciliation.NewRelationRouteAdmissionDecision(reconciliation.RelationRouteAdmissionSpec{
		Row:               reconciliation.RouteAdmissionRowInstallCarrier,
		RequestedOutcome:  reconciliation.AdmissionOutcomeOrdinaryMutation,
		SelectedOutcome:   reconciliation.AdmissionOutcomeHostDelegated,
		ObservationPolicy: reconciliation.ObservationRequireCurrent,
	})
	if err != nil {
		t.Fatal(err)
	}
	routeRequest, err := realizationdelegate.NewRequest(
		"claude-code.plugin-carrier.install",
		"claude-plugin-carrier-v1",
		"sha256:0000000000000000000000000000000000000000000000000000000000000000",
	)
	if err != nil {
		t.Fatal(err)
	}
	action, err := reconciliation.NewRelationAction(reconciliation.RelationActionInput{
		CarrierIdentity:       identity,
		RouteRequest:          routeRequest,
		Correlation:           observerelation.Correlate(relation, inventory),
		RouteAdmission:        admission,
		PendingInstallPresent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	statefileKey, err := mutation.CanonicalDirectoryEntryKey(statefilePath)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(mustObservedPathAuthority(t, statefileKey), manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := durablecarrier.NewPendingCarrierInstall(
		owner,
		action.CarrierIdentity(),
		action.RouteRequest(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return action, pending
}

func statefileCommitSnapshot(
	t *testing.T,
	pending durablecarrier.PendingCarrierInstall,
) durable.Snapshot {
	t.Helper()
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{
		PendingCarrierInstalls: []durablecarrier.PendingCarrierInstall{pending},
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
