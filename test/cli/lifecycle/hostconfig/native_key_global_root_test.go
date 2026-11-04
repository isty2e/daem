package cli_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func newNativeMissingGlobalRootFixture(t *testing.T, selected nativeKeyScope) nativeKeyScopeFixture {
	t.Helper()
	fixture := newNativeKeyScopeFixture(t, selected, false)
	root := filepath.Dir(fixture.Destination)
	if err := os.Remove(root); err != nil {
		t.Fatal("remove the empty fixture-selected global root", err)
	}
	return fixture
}

func TestNativeKeyMissingGlobalRootLifecycle(t *testing.T) {
	for _, selected := range nativeKeyScopes() {
		if selected.scope != "global" {
			continue
		}
		t.Run(selected.name, func(t *testing.T) {
			fixture := newNativeMissingGlobalRootFixture(t, selected)
			nativefixture.RunRecoveryCommand(t, fixture.RecoveryFixture, "none", "status", "--json")
			nativefixture.RunRecoveryCommand(t, fixture.RecoveryFixture, "none", "apply", "--dry-run", "--json")
			testkit.AssertPathMissing(t, filepath.Dir(fixture.Destination))

			nativefixture.RunRecoveryCommand(t, fixture.RecoveryFixture, "none", "apply", "--yes", "--verbose")
			requireNativeKeyScopeDesired(t, fixture, "after-model")
		})
	}
}

func TestNativeKeyMissingGlobalRootRecovery(t *testing.T) {
	for _, selected := range nativeKeyScopes() {
		if selected.scope != "global" {
			continue
		}
		for _, boundary := range []string{"host", "state", "return-error", "cancel", "return-state-error"} {
			t.Run(selected.name+"/"+boundary, func(t *testing.T) {
				fixture := newNativeMissingGlobalRootFixture(t, selected)
				beforeState := nativefixture.SnapshotRecoveryPaths(t, fixture.Paths.StatefilePath)

				if boundary == "host" || boundary == "state" {
					interruptNativeKeyScopeApply(t, fixture, boundary)
					hostBefore := nativefixture.SnapshotRecoveryPaths(t, fixture.Destination)
					identity, err := os.Stat(fixture.Destination)
					if err != nil {
						t.Fatal(err)
					}

					classification := "needs_rollback"
					if boundary == "state" {
						classification = "needs_finalize"
					}
					nativefixture.RequireRecoveryDryRun(t, fixture.RecoveryFixture, classification)
					nativefixture.RunRecoveryCommand(t, fixture.RecoveryFixture, "none", "recover", "--yes")
					if boundary == "state" {
						after, err := os.Stat(fixture.Destination)
						if err != nil || !os.SameFile(identity, after) || !reflect.DeepEqual(hostBefore, nativefixture.SnapshotRecoveryPaths(t, fixture.Destination)) {
							t.Fatal("new-root After finalization replayed the host", err)
						}
					}
				} else {
					code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture.RecoveryFixture, boundary, "apply", "--yes", "--verbose")
					if code == 0 || code == nativefixture.RecoveryExit {
						t.Fatalf("injected failure was not returned: %d %s %s", code, stdout, stderr)
					}
					message := "native recovery fixture: returned storage error"
					if boundary == "cancel" {
						message = "native recovery requested cancellation"
					}
					if !strings.Contains(stderr, message) || !strings.Contains(stderr, nativefixture.RecoveryReceipt(fixture.Destination)) {
						t.Fatal("failure was not injected after actual new-root host publication", stderr)
					}
					if boundary == "return-state-error" {
						if !strings.Contains(stderr, nativefixture.RecoveryReceipt(fixture.Paths.StatefilePath)) || strings.Count(stderr, nativefixture.RecoveryReceipt(fixture.Destination)) != 1 {
							t.Fatal("new-root state error did not finalize without host replay", stderr)
						}
					} else if strings.Contains(stderr, nativefixture.RecoveryReceipt(fixture.Paths.StatefilePath)) {
						t.Fatal("new-root host failure crossed state commit", stderr)
					}
				}

				testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.Manifest))
				if boundary != "state" && boundary != "return-state-error" {
					requireNativeKeyScopeBefore(t, fixture, false)
					requireNativeKeyScopeMetadata(t, fixture, false)
					if !reflect.DeepEqual(beforeState, nativefixture.SnapshotRecoveryPaths(t, fixture.Paths.StatefilePath)) {
						t.Fatal("new-root rollback changed persisted Before state")
					}
					nativefixture.RunRecoveryCommand(t, fixture.RecoveryFixture, "none", "apply", "--yes")
				}

				requireNativeKeyScopeDesired(t, fixture, "after-model")
			})
		}
	}
}
