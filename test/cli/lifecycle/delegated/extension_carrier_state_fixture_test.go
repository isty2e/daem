package cli_test

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	carrierclaimstore "github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	"github.com/isty2e/daem/test/testkit"
)

type cliClaudeGlobalExtensionCarrierFixture struct {
	root         string
	manifestPath string
	lockfilePath string
	subjectID    topology.SubjectID
	subject      realization.DelegatedRelation
}

func writeCLIClaudeGlobalExtensionCarrierLockFixture(t *testing.T) cliClaudeGlobalExtensionCarrierFixture {
	t.Helper()

	tempDir := t.TempDir()
	testkit.SetDataRootEnv(t, tempDir)
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", claudeGlobalExtensionManifest())

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("lock write exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	locked, err := lockfile.Load(lockfilePath)
	if err != nil {
		t.Fatalf("load lockfile: %v", err)
	}
	if len(locked.Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %#v, want one Claude plugin carrier subject", locked.Locked.Subjects())
	}
	record := locked.Locked.Subjects()[0]
	subject := snapshottest.DelegatedRelation(t, record)
	if record.SubjectID().Key() != "context7-global" || subject.Scope() != target.ScopeGlobal {
		t.Fatalf("subject = %#v, want explicit-global context7 carrier", subject)
	}
	return cliClaudeGlobalExtensionCarrierFixture{
		root:         tempDir,
		manifestPath: manifestPath,
		lockfilePath: lockfilePath,
		subjectID:    record.SubjectID(),
		subject:      subject,
	}
}

func writeCLIClaudeGlobalObservedPresentAttemptStatefile(
	t *testing.T,
	fixture cliClaudeGlobalExtensionCarrierFixture,
) {
	t.Helper()
	locked, err := lockfile.Load(fixture.lockfilePath)
	if err != nil {
		t.Fatalf("load lockfile: %v", err)
	}
	if len(locked.Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %#v, want one Claude plugin carrier subject", locked.Locked.Subjects())
	}
	record := locked.Locked.Subjects()[0]
	relation := testkit.LockedDelegatedRelation(t, record)
	subject := record.SubjectID()
	attempt := mustCLIObservedPresentHostRouteAttempt(
		t,
		subject,
		relation.Target(),
		relation.Scope(),
		relation.RouteID(),
		relation.CanonicalRequestHash(),
	)
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{
		HostRouteAttempts: []durableattempt.HostRouteAttempt{attempt},
	})
	if err != nil {
		t.Fatal(err)
	}
	testkit.WriteStatefile(t, filepath.Join(fixture.root, ".daem", "state.json"), snapshot)
}

func writeCLIClaudeGlobalManagedCarrierState(
	t *testing.T,
	fixture cliClaudeGlobalExtensionCarrierFixture,
) {
	t.Helper()
	locked, err := lockfile.Load(fixture.lockfilePath)
	if err != nil {
		t.Fatalf("load lockfile: %v", err)
	}
	if len(locked.Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %#v, want one Claude plugin carrier subject", locked.Locked.Subjects())
	}
	record := locked.Locked.Subjects()[0]
	relation := testkit.LockedDelegatedRelation(t, record)
	identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(record)
	if err != nil || !admitted {
		t.Fatalf("derive managed carrier identity = (%#v, %t, %v)", identity, admitted, err)
	}
	request, err := lock.DelegatedOperationRequest(record, lock.OperationInstall)
	if err != nil {
		t.Fatalf("derive install request: %v", err)
	}
	statefilePath := filepath.Join(fixture.root, ".daem", "state.json")
	owner, err := stateauthority.New(testkit.MustObservedPathAuthority(t, statefilePath), fixture.manifestPath)
	if err != nil {
		t.Fatalf("construct carrier state authority: %v", err)
	}
	claim, err := durablecarrier.NewManagedCarrierClaim(
		owner,
		identity,
		request,
		durablecarrier.ClaimProvenanceInstalledObserved,
	)
	if err != nil {
		t.Fatalf("construct managed carrier claim: %v", err)
	}
	resolved, err := daempaths.Resolve(fixture.manifestPath)
	if err != nil {
		t.Fatalf("resolve carrier claim registry: %v", err)
	}
	store, err := carrierclaimstore.New(resolved.CarrierClaimRegistryPath)
	if err != nil {
		t.Fatalf("open carrier claim registry: %v", err)
	}
	if _, err := store.Upsert(context.Background(), claim); err != nil {
		t.Fatalf("persist global managed carrier claim: %v", err)
	}
	attempt := mustCLIObservedPresentHostRouteAttempt(
		t,
		record.SubjectID(),
		relation.Target(),
		relation.Scope(),
		relation.RouteID(),
		relation.CanonicalRequestHash(),
	)
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{
		HostRouteAttempts: []durableattempt.HostRouteAttempt{attempt},
	})
	if err != nil {
		t.Fatal(err)
	}
	testkit.WriteStatefile(t, statefilePath, snapshot)
}

func loadCLIGlobalCarrierClaims(
	t *testing.T,
	manifestPath string,
) []durablecarrier.ManagedCarrierClaim {
	t.Helper()
	resolved, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatalf("resolve carrier claim registry: %v", err)
	}
	store, err := carrierclaimstore.New(resolved.CarrierClaimRegistryPath)
	if err != nil {
		t.Fatalf("open carrier claim registry: %v", err)
	}
	registry, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load carrier claim registry: %v", err)
	}
	return registry.Claims()
}

func mustCLIObservedPresentHostRouteAttempt(
	t *testing.T,
	subject topology.SubjectID,
	selectedTarget target.Target,
	scope target.Scope,
	routeID string,
	routeRequestHash string,
) durableattempt.HostRouteAttempt {
	t.Helper()
	attempt, err := durableattempt.NewHostRouteAttempt(durableattempt.HostRouteAttemptInput{
		Subject:          subject,
		Target:           selectedTarget,
		Scope:            scope,
		Operation:        lock.OperationInstall,
		RouteID:          routeID,
		RouteRequestHash: routeRequestHash,
		ObservedAt:       time.Date(2026, time.July, 6, 12, 0, 0, 0, time.UTC),
		ResultClass:      durableattempt.HostRouteResultAttemptedObservedPresent,
		Reason:           durableattempt.HostRouteReasonObservedPresent,
		AttemptObserved:  true,
		Observation:      relationobserve.ObservationPresent,
		Postcondition:    relationobserve.PostconditionObserved,
	})
	if err != nil {
		t.Fatal(err)
	}
	return attempt
}
