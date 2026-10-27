package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestNativeKeyScopeRecoveryAfterProcessLoss(t *testing.T) {
	for _, selected := range nativeKeyScopes() {
		for _, existing := range []bool{true, false} {
			for _, boundary := range []string{"host", "state"} {
				t.Run(selected.name+"/"+nativeKeyScopeFileKind(existing)+"/"+boundary, func(t *testing.T) {
					fixture := newNativeKeyScopeRecoveryFixture(t, selected, existing)
					before := snapshotNativeRecoveryPaths(t, fixture.paths.StatefilePath)
					interruptNativeKeyScopeApply(t, fixture, boundary)
					if boundary == "host" && !reflect.DeepEqual(before, snapshotNativeRecoveryPaths(t, fixture.paths.StatefilePath)) {
						t.Fatal("host process loss crossed the actual Before-state boundary")
					}
					if existing {
						addNativeKeyScopeUserEdit(t, fixture)
					}
					unchangedHost, err := os.Stat(fixture.destination)
					if err != nil {
						t.Fatal(err)
					}
					unchangedImage := snapshotNativeRecoveryPaths(t, fixture.destination)
					classification := "needs_rollback"
					if boundary == "state" {
						classification = "clean_after"
						if !existing {
							classification = "needs_finalize"
						}
					}
					requireNativeRecoveryDryRun(t, fixture.nativeRecoveryFixture, classification)
					runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "recover", "--yes")
					testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.manifest))
					if boundary == "host" {
						if !reflect.DeepEqual(before, snapshotNativeRecoveryPaths(t, fixture.paths.StatefilePath)) {
							t.Fatal("rollback changed persisted Before state")
						}
						requireNativeKeyScopeBefore(t, fixture, existing)
						requireNativeKeyScopeMetadata(t, fixture, existing)
					} else {
						after, err := os.Stat(fixture.destination)
						if err != nil || !os.SameFile(unchangedHost, after) || !reflect.DeepEqual(unchangedImage, snapshotNativeRecoveryPaths(t, fixture.destination)) {
							t.Fatal("state-After finalization replayed the host document", err)
						}
						requireNativeKeyScopeDesired(t, fixture, "after-model")
					}
					runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "apply", "--yes")
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
					before := snapshotNativeRecoveryPaths(t, fixture.paths.StatefilePath)
					code, stdout, stderr := invokeNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, boundary, "apply", "--yes", "--verbose")
					t.Logf("fault result exit=%d stdout=%s stderr=%s", code, stdout, stderr)
					message := "native recovery fixture: returned storage error"
					if boundary == "cancel" {
						message = "native recovery requested cancellation"
					}
					if code == 0 || code == nativeRecoveryExit || !strings.Contains(stderr, message) || !strings.Contains(stderr, nativeRecoveryReceipt(fixture.destination)) {
						t.Fatalf("Apply did not return the injected failure after actual host publication: exit=%d stdout=%s stderr=%s", code, stdout, stderr)
					}
					if boundary == "return-state-error" {
						if !strings.Contains(stderr, nativeRecoveryReceipt(fixture.paths.StatefilePath)) || strings.Count(stderr, nativeRecoveryReceipt(fixture.destination)) != 1 {
							t.Fatal("state-After returned error did not settle without replaying the host", stderr)
						}
					} else {
						if strings.Contains(stderr, nativeRecoveryReceipt(fixture.paths.StatefilePath)) {
							t.Fatal("host failure crossed state publication", stderr)
						}
						requireNativeKeyScopeBefore(t, fixture, false)
						if !reflect.DeepEqual(before, snapshotNativeRecoveryPaths(t, fixture.paths.StatefilePath)) {
							t.Fatal("automatic compensation changed persisted Before state")
						}
						requireNativeKeyScopeMetadata(t, fixture, existing)
					}
					testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.manifest))
					if boundary != "return-state-error" {
						runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "apply", "--yes")
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
			requireNativeRecoveryDryRun(t, fixture.nativeRecoveryFixture, "needs_rollback")
			content := testkit.ReadFile(t, fixture.destination)
			changed := bytes.ReplaceAll(content, []byte("after-model"), []byte("third-model"))
			if bytes.Equal(changed, content) {
				t.Fatal("third-value edit did not change the selected value")
			}
			if err := os.WriteFile(fixture.destination, changed, 0o644); err != nil {
				t.Fatal(err)
			}
			requireNativeSharedValues(t, selected.syntax, changed, nativeKeyScopeSource(selected, "third-model"), true)
			before := snapshotNativeRecoveryEvidence(t, fixture.nativeRecoveryFixture)
			for _, mode := range []string{"--dry-run", "--yes"} {
				code, stdout, stderr := invokeNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "recover", mode, "--json")
				if code == 0 {
					t.Fatalf("recovery accepted a selected third value: stdout=%s stderr=%s", stdout, stderr)
				}
				plan := decodeRecoverJSONTestPayload(t, []byte(stdout))
				if plan.Classification != "blocked" || !plan.HasErrors {
					t.Fatal("selected third value did not produce a blocked recovery plan", stdout, stderr)
				}
				if !reflect.DeepEqual(before, snapshotNativeRecoveryEvidence(t, fixture.nativeRecoveryFixture)) {
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
		runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "lock")
		runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "apply", "--yes")
		requireNativeKeyScopeDesired(t, fixture, "before-model")
		writeNativeKeyScopeDesired(t, fixture, "after-model")
		runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "lock")
	}
	return fixture
}

func interruptNativeKeyScopeApply(t *testing.T, fixture nativeKeyScopeFixture, boundary string) {
	t.Helper()
	code, stdout, stderr := invokeNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, boundary, "apply", "--yes")
	if code != nativeRecoveryExit || !strings.Contains(stderr, nativeRecoveryReceipt(fixture.destination)) {
		t.Fatalf("helper did not lose the process after actual host publication: exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	statePublished := strings.Contains(stderr, nativeRecoveryReceipt(fixture.paths.StatefilePath))
	if statePublished != (boundary == "state") {
		t.Fatal("process loss crossed the wrong publication boundary", stderr)
	}
	t.Logf("confirmed rooted %s publication; helper exited %d without unwinding Apply", boundary, code)
	requireNativeRecoveryJournalState(t, fixture.nativeRecoveryFixture, boundary)
	content := testkit.ReadFile(t, fixture.destination)
	requireNativeSharedValues(t, fixture.selected.syntax, content, nativeKeyScopeSource(fixture.selected, "after-model"), true)
	requireNativeSharedMode(t, fixture.destination, nativeKeyScopeMode(fixture.existing))
}

func requireNativeKeyScopeBefore(t *testing.T, fixture nativeKeyScopeFixture, later bool) {
	t.Helper()
	content, err := os.ReadFile(fixture.destination)
	if !fixture.existing {
		if err != nil {
			t.Fatal("selective rollback lost the created structural parent", err)
		}
		requireNativeSharedValues(t, fixture.selected.syntax, content, nativeKeyScopeSource(fixture.selected, "after-model"), false)
		requireNativeEmptyParent(t, fixture.selected.syntax, content, fixture.selected.parent)
		requireNativeSharedMode(t, fixture.destination, 0o600)
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	requireNativeSharedValues(t, fixture.selected.syntax, content, nativeKeyScopeSource(fixture.selected, "before-model"), true)
	requireNativeKeyScopeOutside(t, fixture, content)
	requireNativeSharedMode(t, fixture.destination, 0o644)
	if later {
		requireNativeKeyScopeUserEdit(t, fixture)
	}
}
