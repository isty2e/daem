package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestNativeKeyScopeRecoveryAfterProcessLoss(t *testing.T) {
	for _, selected := range nativeKeyScopes() {
		for _, existing := range []bool{true, false} {
			for _, boundary := range []string{"host", "state"} {
				t.Run(selected.name+"/"+nativeKeyScopeFileKind(existing)+"/"+boundary, func(t *testing.T) {
					fixture := newNativeKeyScopeRecoveryFixture(t, selected, existing)
					before := nativefixture.SnapshotRecoveryPaths(t, fixture.Paths.StatefilePath)
					interruptNativeKeyScopeApply(t, fixture, boundary)
					if boundary == "host" && !reflect.DeepEqual(before, nativefixture.SnapshotRecoveryPaths(t, fixture.Paths.StatefilePath)) {
						t.Fatal("host process loss crossed the actual Before-state boundary")
					}
					if existing {
						addNativeKeyScopeUserEdit(t, fixture)
					}
					unchangedHost, err := os.Stat(fixture.Destination)
					if err != nil {
						t.Fatal(err)
					}
					unchangedImage := nativefixture.SnapshotRecoveryPaths(t, fixture.Destination)
					classification := "needs_rollback"
					if boundary == "state" {
						classification = "clean_after"
						if !existing {
							classification = "needs_finalize"
						}
					}
					nativefixture.RequireRecoveryDryRun(t, fixture.RecoveryFixture, classification)
					nativefixture.RunRecoveryCommand(t, fixture.RecoveryFixture, "none", "recover", "--yes")
					testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.Manifest))
					if boundary == "host" {
						if !reflect.DeepEqual(before, nativefixture.SnapshotRecoveryPaths(t, fixture.Paths.StatefilePath)) {
							t.Fatal("rollback changed persisted Before state")
						}
						requireNativeKeyScopeBefore(t, fixture, existing)
						requireNativeKeyScopeMetadata(t, fixture, existing)
						nativefixture.RunRecoveryCommand(t, fixture.RecoveryFixture, "none", "apply", "--yes")
					} else {
						after, err := os.Stat(fixture.Destination)
						if err != nil || !os.SameFile(unchangedHost, after) || !reflect.DeepEqual(unchangedImage, nativefixture.SnapshotRecoveryPaths(t, fixture.Destination)) {
							t.Fatal("state-After finalization replayed the host document", err)
						}
					}
					requireNativeKeyScopeDesired(t, fixture, "after-model")
					if existing {
						requireNativeKeyScopeUserEdit(t, fixture)
					}
				})
			}
		}
	}
}

func TestNativeKeyScopeRecoveryReturnedFailures(t *testing.T) {
	for _, selected := range nativeKeyScopes() {
		for _, existing := range []bool{true, false} {
			for _, boundary := range []string{"return-error", "cancel", "return-state-error"} {
				t.Run(selected.name+"/"+nativeKeyScopeFileKind(existing)+"/"+boundary, func(t *testing.T) {
					fixture := newNativeKeyScopeRecoveryFixture(t, selected, existing)
					before := nativefixture.SnapshotRecoveryPaths(t, fixture.Paths.StatefilePath)
					code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture.RecoveryFixture, boundary, "apply", "--yes", "--verbose")
					t.Logf("fault result exit=%d stdout=%s stderr=%s", code, stdout, stderr)
					message := "native recovery fixture: returned storage error"
					if boundary == "cancel" {
						message = "native recovery requested cancellation"
					}
					if code == 0 || code == nativefixture.RecoveryExit || !strings.Contains(stderr, message) || !strings.Contains(stderr, nativefixture.RecoveryReceipt(fixture.Destination)) {
						t.Fatalf("Apply did not return the injected failure after actual host publication: exit=%d stdout=%s stderr=%s", code, stdout, stderr)
					}
					if boundary == "return-state-error" {
						if !strings.Contains(stderr, nativefixture.RecoveryReceipt(fixture.Paths.StatefilePath)) || strings.Count(stderr, nativefixture.RecoveryReceipt(fixture.Destination)) != 1 {
							t.Fatal("state-After returned error did not settle without replaying the host", stderr)
						}
					} else {
						if strings.Contains(stderr, nativefixture.RecoveryReceipt(fixture.Paths.StatefilePath)) {
							t.Fatal("host failure crossed state publication", stderr)
						}
						requireNativeKeyScopeBefore(t, fixture, false)
						if !reflect.DeepEqual(before, nativefixture.SnapshotRecoveryPaths(t, fixture.Paths.StatefilePath)) {
							t.Fatal("automatic compensation changed persisted Before state")
						}
						requireNativeKeyScopeMetadata(t, fixture, existing)
					}
					testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.Manifest))
					if boundary != "return-state-error" {
						nativefixture.RunRecoveryCommand(t, fixture.RecoveryFixture, "none", "apply", "--yes")
					}
					requireNativeKeyScopeDesired(t, fixture, "after-model")
				})
			}
		}
	}
}

func TestNativeKeyScopeRecoveryRefusesSelectedThirdValue(t *testing.T) {
	for _, selected := range nativeKeyScopes() {
		t.Run(selected.name, func(t *testing.T) {
			fixture := newNativeKeyScopeRecoveryFixture(t, selected, true)
			interruptNativeKeyScopeApply(t, fixture, "host")
			nativefixture.RequireRecoveryDryRun(t, fixture.RecoveryFixture, "needs_rollback")
			content := testkit.ReadFile(t, fixture.Destination)
			changed := bytes.ReplaceAll(content, []byte("after-model"), []byte("third-model"))
			if bytes.Equal(changed, content) {
				t.Fatal("third-value edit did not change the selected value")
			}
			if err := os.WriteFile(fixture.Destination, changed, 0o644); err != nil {
				t.Fatal(err)
			}
			nativefixture.RequireSharedValues(t, selected.syntax, changed, nativeKeyScopeSource(selected, "third-model"), true)
			before := nativefixture.SnapshotRecoveryEvidence(t, fixture.RecoveryFixture)
			for _, mode := range []string{"--dry-run", "--yes"} {
				code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture.RecoveryFixture, "none", "recover", mode, "--json")
				if code == 0 {
					t.Fatalf("recovery accepted a selected third value: stdout=%s stderr=%s", stdout, stderr)
				}
				plan := clijson.DecodeRecover(t, []byte(stdout))
				if plan.Classification != "blocked" || !plan.HasErrors {
					t.Fatal("selected third value did not produce a blocked recovery plan", stdout, stderr)
				}
				if !reflect.DeepEqual(before, nativefixture.SnapshotRecoveryEvidence(t, fixture.RecoveryFixture)) {
					t.Fatal("refused recovery changed host, state, Registry or journal evidence")
				}
			}
		})
	}
}

func newNativeKeyScopeRecoveryFixture(t *testing.T, selected nativeKeyScope, existing bool) nativeKeyScopeFixture {
	t.Helper()
	fixture := newNativeKeyScopeFixture(t, selected, existing)
	if existing {
		writeNativeKeyScopeDesired(t, fixture, "before-model")
		nativefixture.RunRecoveryCommand(t, fixture.RecoveryFixture, "none", "lock")
		nativefixture.RunRecoveryCommand(t, fixture.RecoveryFixture, "none", "apply", "--yes")
		requireNativeKeyScopeDesired(t, fixture, "before-model")
		writeNativeKeyScopeDesired(t, fixture, "after-model")
		nativefixture.RunRecoveryCommand(t, fixture.RecoveryFixture, "none", "lock")
	}
	return fixture
}

func interruptNativeKeyScopeApply(t *testing.T, fixture nativeKeyScopeFixture, boundary string) {
	t.Helper()
	code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture.RecoveryFixture, boundary, "apply", "--yes")
	if code != nativefixture.RecoveryExit || !strings.Contains(stderr, nativefixture.RecoveryReceipt(fixture.Destination)) {
		t.Fatalf("helper did not lose the process after actual host publication: exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	statePublished := strings.Contains(stderr, nativefixture.RecoveryReceipt(fixture.Paths.StatefilePath))
	if statePublished != (boundary == "state") {
		t.Fatal("process loss crossed the wrong publication boundary", stderr)
	}
	t.Logf("confirmed rooted %s publication; helper exited %d without unwinding Apply", boundary, code)
	nativefixture.RequireRecoveryJournalState(t, fixture.RecoveryFixture, boundary)
	content := testkit.ReadFile(t, fixture.Destination)
	nativefixture.RequireSharedValues(t, fixture.selected.syntax, content, nativeKeyScopeSource(fixture.selected, "after-model"), true)
	nativefixture.RequireSharedMode(t, fixture.Destination, nativeKeyScopeMode(fixture.existing))
}

func requireNativeKeyScopeBefore(t *testing.T, fixture nativeKeyScopeFixture, later bool) {
	t.Helper()
	content, err := os.ReadFile(fixture.Destination)
	if !fixture.existing {
		if err != nil {
			t.Fatal("selective rollback lost the created structural parent", err)
		}
		nativefixture.RequireSharedValues(t, fixture.selected.syntax, content, nativeKeyScopeSource(fixture.selected, "after-model"), false)
		requireNativeEmptyParent(t, fixture.selected.syntax, content, fixture.selected.parent)
		nativefixture.RequireSharedMode(t, fixture.Destination, 0o600)
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	nativefixture.RequireSharedValues(t, fixture.selected.syntax, content, nativeKeyScopeSource(fixture.selected, "before-model"), true)
	requireNativeKeyScopeOutside(t, fixture, content)
	nativefixture.RequireSharedMode(t, fixture.Destination, 0o644)
	if later {
		requireNativeKeyScopeUserEdit(t, fixture)
	}
}
