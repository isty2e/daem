//go:build darwin || linux

package cli_test

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/test/testkit"
)

func TestNativeExtensionRemovalExplainsKeyOwnershipWithoutValueChanges(t *testing.T) {
	fixture := newNativePiInvocationFixture(t, "none")
	fixture.require(t, "apply", "--yes")
	fixture.require(t, "remove", "extension", "tools")
	fixture.require(t, "lock")
	paths, err := daempaths.Resolve(fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	before := snapshotNativeRecoveryPaths(t, fixture.settings, fixture.calls, paths.StatefilePath, paths.OwnershipRegistryPath)
	code, _, stderr := runOwnershipCLI("apply", "--manifest", fixture.manifest, "--yes")
	if code == 0 || !strings.Contains(stderr, "managed keys") || !strings.Contains(stderr, "next:") || !strings.Contains(stderr, "defer this extension change") {
		t.Fatalf("unchanged managed keys produced no actionable removal diagnosis: exit=%d %s", code, stderr)
	}
	if !reflect.DeepEqual(before, snapshotNativeRecoveryPaths(t, fixture.settings, fixture.calls, paths.StatefilePath, paths.OwnershipRegistryPath)) {
		t.Fatal("unsupported removal changed settings, invoked the host or changed ownership")
	}
	testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.manifest))
}
