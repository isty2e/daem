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
	// Locator tests cover target selection; this exercises missing-root creation.
	selected := nativeKeyScopeNamed(t, "codex-global")
	t.Run(selected.name, func(t *testing.T) {
		fixture := newNativeMissingGlobalRootFixture(t, selected)
		nativefixture.RunRecoveryCommand(t, fixture.RecoveryFixture, "none", "status", "--json")
		nativefixture.RunRecoveryCommand(t, fixture.RecoveryFixture, "none", "apply", "--dry-run", "--json")
		testkit.AssertPathMissing(t, filepath.Dir(fixture.Destination))

		nativefixture.RunRecoveryCommand(t, fixture.RecoveryFixture, "none", "apply", "--yes", "--verbose")
		requireNativeKeyScopeDesired(t, fixture, "after-model")
	})
}

func TestNativeKeyMissingGlobalRootRecovery(t *testing.T) {
	selected := nativeKeyScopeNamed(t, "codex-global")
	// Process loss reopens the created root; returned failure restores through the live grant.
	for _, boundary := range []string{"host", "return-error"} {
		t.Run(selected.name+"/"+boundary, func(t *testing.T) {
			fixture := newNativeMissingGlobalRootFixture(t, selected)
			beforeState := nativefixture.SnapshotRecoveryPaths(t, fixture.Paths.StatefilePath)

			if boundary == "host" {
				interruptNativeKeyScopeApply(t, fixture, boundary)
				nativefixture.RequireRecoveryDryRun(t, fixture.RecoveryFixture, "needs_rollback")
				nativefixture.RunRecoveryCommand(t, fixture.RecoveryFixture, "none", "recover", "--yes")
			} else {
				code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture.RecoveryFixture, boundary, "apply", "--yes", "--verbose")
				if code == 0 || code == nativefixture.RecoveryExit {
					t.Fatalf("injected failure was not returned: %d %s %s", code, stdout, stderr)
				}
				if !strings.Contains(stderr, "native recovery fixture: returned storage error") || !strings.Contains(stderr, nativefixture.RecoveryReceipt(fixture.Destination)) {
					t.Fatal("failure was not injected after actual new-root host publication", stderr)
				}
				if strings.Contains(stderr, nativefixture.RecoveryReceipt(fixture.Paths.StatefilePath)) {
					t.Fatal("new-root host failure crossed state commit", stderr)
				}
			}

			testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.Manifest))
			requireNativeKeyScopeBefore(t, fixture, false)
			requireNativeKeyScopeMetadata(t, fixture, false)
			if !reflect.DeepEqual(beforeState, nativefixture.SnapshotRecoveryPaths(t, fixture.Paths.StatefilePath)) {
				t.Fatal("new-root rollback changed persisted Before state")
			}
			nativefixture.RunRecoveryCommand(t, fixture.RecoveryFixture, "none", "apply", "--yes")
			requireNativeKeyScopeDesired(t, fixture, "after-model")
		})
	}
}
