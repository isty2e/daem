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

func TestNativeKeyRecoveryCleanupDoesNotReadHostStateOrRegistry(t *testing.T) {
	for _, kind := range []string{"existing", "absent", "mixed-new"} {
		t.Run(kind, func(t *testing.T) {
			fixture := nativefixture.NewRecoveryFixture(t, kind)
			code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture, "retirement", "apply", "--yes")
			if code != nativefixture.RecoveryExit || !strings.Contains(stderr, "native recovery confirmed retired journal") {
				t.Fatalf("producer did not lose the process after journal retirement: exit=%d stdout=%s stderr=%s", code, stdout, stderr)
			}
			for _, path := range []string{fixture.Destination, fixture.Paths.StatefilePath, fixture.Paths.OwnershipRegistryPath} {
				if err := os.WriteFile(path, []byte("deliberately invalid unrelated content\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			before := nativefixture.SnapshotRecoveryPaths(t, fixture.Destination, fixture.Paths.StatefilePath, fixture.Paths.OwnershipRegistryPath)
			nativefixture.RequireRecoveryDryRun(t, fixture, "retained_cleanup_residue")
			nativefixture.RunRecoveryCommand(t, fixture, "none", "recover", "--yes")
			testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.Manifest))
			if !reflect.DeepEqual(before, nativefixture.SnapshotRecoveryPaths(t, fixture.Destination, fixture.Paths.StatefilePath, fixture.Paths.OwnershipRegistryPath)) {
				t.Fatal("cleanup-only native recovery changed host, state or Registry")
			}
			code, _, _ = nativefixture.InvokeRecoveryCommand(t, fixture, "none", "recover", "--yes")
			if code != 1 || !reflect.DeepEqual(before, nativefixture.SnapshotRecoveryPaths(t, fixture.Destination, fixture.Paths.StatefilePath, fixture.Paths.OwnershipRegistryPath)) {
				t.Fatal("repeated recovery did not report no operation without changing unrelated files")
			}
		})
	}
}
