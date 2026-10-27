package cli_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func newNativeMissingGlobalRootFixture(t *testing.T, selected nativeKeyScope) nativeKeyScopeFixture {
	t.Helper()
	fixture := newNativeKeyScopeFixture(t, selected, false)
	root := filepath.Dir(fixture.destination)
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
			runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "status", "--json")
			runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "apply", "--dry-run", "--json")
			testkit.AssertPathMissing(t, filepath.Dir(fixture.destination))

			runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "apply", "--yes", "--verbose")
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
				beforeState := snapshotNativeRecoveryPaths(t, fixture.paths.StatefilePath)

				if boundary == "host" || boundary == "state" {
					interruptNativeKeyScopeApply(t, fixture, boundary)
					hostBefore := snapshotNativeRecoveryPaths(t, fixture.destination)
					identity, err := os.Stat(fixture.destination)
					if err != nil {
						t.Fatal(err)
					}

					classification := "needs_rollback"
					if boundary == "state" {
						classification = "needs_finalize"
					}
					requireNativeRecoveryDryRun(t, fixture.nativeRecoveryFixture, classification)
					runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "recover", "--yes")
					if boundary == "state" {
						after, err := os.Stat(fixture.destination)
						if err != nil || !os.SameFile(identity, after) || !reflect.DeepEqual(hostBefore, snapshotNativeRecoveryPaths(t, fixture.destination)) {
							t.Fatal("new-root After finalization replayed the host", err)
						}
					}
				} else {
					code, stdout, stderr := invokeNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, boundary, "apply", "--yes", "--verbose")
					if code == 0 || code == nativeRecoveryExit {
						t.Fatalf("injected failure was not returned: %d %s %s", code, stdout, stderr)
					}
					message := "native recovery fixture: returned storage error"
					if boundary == "cancel" {
						message = "native recovery requested cancellation"
					}
					if !strings.Contains(stderr, message) || !strings.Contains(stderr, nativeRecoveryReceipt(fixture.destination)) {
						t.Fatal("failure was not injected after actual new-root host publication", stderr)
					}
					if boundary == "return-state-error" {
						if !strings.Contains(stderr, nativeRecoveryReceipt(fixture.paths.StatefilePath)) || strings.Count(stderr, nativeRecoveryReceipt(fixture.destination)) != 1 {
							t.Fatal("new-root state error did not finalize without host replay", stderr)
						}
					} else if strings.Contains(stderr, nativeRecoveryReceipt(fixture.paths.StatefilePath)) {
						t.Fatal("new-root host failure crossed state commit", stderr)
					}
				}

				testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.manifest))
				if boundary != "state" && boundary != "return-state-error" {
					requireNativeKeyScopeBefore(t, fixture, false)
					requireNativeKeyScopeMetadata(t, fixture, false)
					if !reflect.DeepEqual(beforeState, snapshotNativeRecoveryPaths(t, fixture.paths.StatefilePath)) {
						t.Fatal("new-root rollback changed persisted Before state")
					}
				}

				runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "apply", "--yes")
				requireNativeKeyScopeDesired(t, fixture, "after-model")
			})
		}
	}
}
