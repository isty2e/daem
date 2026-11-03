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

func TestNativeKeyRecoveryCLIHelperProcess(t *testing.T) {
	nativefixture.RecoveryCLIHelperProcess(t)
}

func TestNativeKeyRecoveryProducerControl(t *testing.T) {
	for _, kind := range []string{"existing", "absent", "mixed", "mixed-new"} {
		t.Run(kind, func(t *testing.T) {
			fixture := nativefixture.NewRecoveryFixture(t, kind)
			_, stdout, stderr := nativefixture.RunRecoveryCommand(t, fixture, "none", "apply", "--yes")
			for _, path := range []string{fixture.Destination, fixture.Paths.StatefilePath} {
				if !strings.Contains(stderr, nativefixture.RecoveryReceipt(path)) {
					t.Fatalf("no-fault Apply did not confirm actual rooted publication at %s: stdout=%s stderr=%s", path, stdout, stderr)
				}
			}
			nativefixture.RequireRecoveryDesired(t, fixture)
		})
	}
}

func TestNativeKeyRecoveryAfterProcessLoss(t *testing.T) {
	for _, kind := range []string{"existing", "absent", "mixed", "mixed-new"} {
		for _, boundary := range []string{"host", "state"} {
			t.Run(kind+"/"+boundary, func(t *testing.T) {
				fixture := nativefixture.NewRecoveryFixture(t, kind)
				before := nativefixture.SnapshotRecoveryPaths(t, fixture.Paths.StatefilePath)
				nativefixture.InterruptRecoveryApply(t, fixture, boundary)
				nativefixture.RequireRecoveryJournalState(t, fixture, boundary)
				if boundary == "host" && !reflect.DeepEqual(before, nativefixture.SnapshotRecoveryPaths(t, fixture.Paths.StatefilePath)) {
					t.Fatal("host interruption did not retain the actual Before state")
				}

				if kind != "absent" {
					nativefixture.PrependRecoveryUserEdit(t, fixture)
				}
				unchangedHost, err := os.Stat(fixture.Destination)
				if err != nil {
					t.Fatal(err)
				}
				unchangedHostImage := nativefixture.SnapshotRecoveryPaths(t, fixture.Destination)
				classification := "needs_rollback"
				if boundary == "state" {
					classification = "clean_after"
					if kind == "absent" {
						classification = "needs_finalize"
					}
				}
				nativefixture.RequireRecoveryDryRun(t, fixture, classification)
				nativefixture.RunRecoveryCommand(t, fixture, "none", "recover", "--yes")
				testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.Manifest))

				if boundary == "host" {
					if !reflect.DeepEqual(before, nativefixture.SnapshotRecoveryPaths(t, fixture.Paths.StatefilePath)) {
						t.Fatal("rollback changed the persisted Before state")
					}
					nativefixture.RequireRecoveryBeforeHost(t, fixture, kind != "absent")
					nativefixture.RequireLifecycleMetadata(t, fixture.Manifest, kind != "absent")
					nativefixture.RunRecoveryCommand(t, fixture, "none", "apply", "--yes")
				} else {
					after, err := os.Stat(fixture.Destination)
					if err != nil || !os.SameFile(unchangedHost, after) || !reflect.DeepEqual(unchangedHostImage, nativefixture.SnapshotRecoveryPaths(t, fixture.Destination)) {
						t.Fatal("state-After finalization replayed the already committed host document", err)
					}
				}
				nativefixture.RequireRecoveryDesired(t, fixture)
				if kind != "absent" {
					nativefixture.RequireRecoveryLaterEdit(t, fixture)
				}
			})
		}
	}
}

func TestNativeKeyRecoveryRefusesSelectedThirdValue(t *testing.T) {
	fixture := nativefixture.NewRecoveryFixture(t, "existing")
	nativefixture.InterruptRecoveryApply(t, fixture, "host")
	nativefixture.RequireRecoveryJournalState(t, fixture, "host")
	// First prove that this real journal is recoverable before introducing drift.
	nativefixture.RequireRecoveryDryRun(t, fixture, "needs_rollback")
	content, err := os.ReadFile(fixture.Destination)
	if err != nil {
		t.Fatal(err)
	}
	changed := bytes.ReplaceAll(content, []byte("after-model"), []byte("third-model"))
	if bytes.Equal(changed, content) {
		t.Fatal("third-value fixture did not change the selected value")
	}
	if err := os.WriteFile(fixture.Destination, changed, 0o644); err != nil {
		t.Fatal(err)
	}
	before := nativefixture.SnapshotRecoveryEvidence(t, fixture)
	for _, mode := range []string{"--dry-run", "--yes"} {
		code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture, "none", "recover", mode, "--json")
		if code == 0 {
			t.Fatalf("selected third value was accepted by %s: stdout=%s stderr=%s", mode, stdout, stderr)
		}
		payload := clijson.DecodeRecover(t, []byte(stdout))
		if payload.Classification != "blocked" || !payload.HasErrors {
			t.Fatalf("selected third value did not produce a blocked recovery plan: %s %s", stdout, stderr)
		}
		if !reflect.DeepEqual(before, nativefixture.SnapshotRecoveryEvidence(t, fixture)) {
			t.Fatal("refused recovery changed host, state, Registry or journal evidence")
		}
	}
}

func TestNativeKeyRecoveryCompensatesReturnedStorageError(t *testing.T) {
	for _, kind := range []string{"existing", "absent", "mixed", "mixed-new"} {
		t.Run(kind, func(t *testing.T) {
			fixture := nativefixture.NewRecoveryFixture(t, kind)
			before := nativefixture.SnapshotRecoveryPaths(t, fixture.Paths.StatefilePath)
			code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture, "return-error", "apply", "--yes", "--verbose")
			t.Logf("fault result exit=%d stdout=%s stderr=%s", code, stdout, stderr)
			if code == 0 || code == nativefixture.RecoveryExit || !strings.Contains(stderr, "native recovery fixture: returned storage error") || !strings.Contains(stderr, nativefixture.RecoveryReceipt(fixture.Destination)) {
				t.Fatalf("Apply did not return the injected error after actual host publication: exit=%d stdout=%s stderr=%s", code, stdout, stderr)
			}
			nativefixture.RequireRecoveryBeforeHost(t, fixture, false)
			if !reflect.DeepEqual(before, nativefixture.SnapshotRecoveryPaths(t, fixture.Paths.StatefilePath)) {
				t.Fatal("automatic compensation changed persisted Before state")
			}
			testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.Manifest))
			nativefixture.RunRecoveryCommand(t, fixture, "none", "apply", "--yes")
			nativefixture.RequireRecoveryDesired(t, fixture)
		})
	}
}

func TestNativeKeyRecoveryCompensatesCancellation(t *testing.T) {
	for _, kind := range []string{"existing", "absent", "mixed", "mixed-new"} {
		t.Run(kind, func(t *testing.T) {
			fixture := nativefixture.NewRecoveryFixture(t, kind)
			before := nativefixture.SnapshotRecoveryPaths(t, fixture.Paths.StatefilePath)
			code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture, "cancel", "apply", "--yes", "--verbose")
			t.Logf("fault result exit=%d stdout=%s stderr=%s", code, stdout, stderr)
			if code == 0 || code == nativefixture.RecoveryExit || !strings.Contains(stderr, "native recovery requested cancellation") || !strings.Contains(stderr, nativefixture.RecoveryReceipt(fixture.Destination)) {
				t.Fatalf("cancellation did not follow actual host publication: exit=%d stdout=%s stderr=%s", code, stdout, stderr)
			}
			nativefixture.RequireRecoveryBeforeHost(t, fixture, false)
			if !reflect.DeepEqual(before, nativefixture.SnapshotRecoveryPaths(t, fixture.Paths.StatefilePath)) {
				t.Fatal("cancellation compensation changed persisted Before state")
			}
			testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.Manifest))
			nativefixture.RunRecoveryCommand(t, fixture, "none", "apply", "--yes")
			nativefixture.RequireRecoveryDesired(t, fixture)
		})
	}
}

func TestNativeKeyRecoveryFinalizesReturnedStateErrorWithoutHostReplay(t *testing.T) {
	for _, kind := range []string{"existing", "absent", "mixed", "mixed-new"} {
		t.Run(kind, func(t *testing.T) {
			fixture := nativefixture.NewRecoveryFixture(t, kind)
			code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture, "return-state-error", "apply", "--yes", "--verbose")
			t.Logf("fault result exit=%d stdout=%s stderr=%s", code, stdout, stderr)
			if code == 0 || code == nativefixture.RecoveryExit || !strings.Contains(stderr, "native recovery fixture: returned storage error") || !strings.Contains(stderr, nativefixture.RecoveryReceipt(fixture.Paths.StatefilePath)) {
				t.Fatalf("Apply did not return the injected error after actual state publication: exit=%d stdout=%s stderr=%s", code, stdout, stderr)
			}
			if strings.Count(stderr, nativefixture.RecoveryReceipt(fixture.Destination)) != 1 {
				t.Fatalf("state-After settlement replayed host publication: %s", stderr)
			}
			nativefixture.RequireRecoveryDesired(t, fixture)
		})
	}
}
