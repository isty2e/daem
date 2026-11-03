//go:build darwin || linux

package cli_test

import (
	"os"
	"path/filepath"
	"testing"

	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestNativeOrderPublishesAfterDelegateConsumers(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			order := newNativePiOrderFixture(t, scope)
			workspace := filepath.Dir(order.Manifest)
			bin := filepath.Join(filepath.Dir(workspace), "bin")
			testkit.WriteFile(t, bin, "native-order-reader-fixture", "#!/bin/sh\nset -eu\n[ -f .mcp.json ]\n/bin/cp \"$DAEM_TEST_ORDER_SETTINGS\" \"$DAEM_TEST_ORDER_BEFORE\"\n/bin/cp \"$DAEM_TEST_ORDER_REGISTRY\" \"$DAEM_TEST_ORDER_BOUND\"\n")
			if err := os.Chmod(filepath.Join(bin, "native-order-reader-fixture"), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			paths, err := daempaths.Resolve(order.Manifest)
			if err != nil {
				t.Fatal(err)
			}
			before, bound := filepath.Join(workspace, "order-before.json"), filepath.Join(workspace, "order-bound.json")
			t.Setenv("DAEM_TEST_ORDER_SETTINGS", order.Destination)
			t.Setenv("DAEM_TEST_ORDER_BEFORE", before)
			t.Setenv("DAEM_TEST_ORDER_REGISTRY", paths.OwnershipRegistryPath)
			t.Setenv("DAEM_TEST_ORDER_BOUND", bound)
			nativefixture.RunLifecycleCommand(t, "add", "mcp-server", "order-reader", "native-order-reader-fixture", "--target", "claude-code", "--scope", "project", "--manifest", order.Manifest)
			nativefixture.RunLifecycleCommand(t, "lock", "--manifest", order.Manifest)
			result := nativefixture.RunLifecycleCommand(t, "apply", "--manifest", order.Manifest, "--yes", "--json")
			requireNativePiOrderResult(t, result, "converged", true, 3)
			nativefixture.PiInvocation{LaunchRegistry: bound}.LaunchNativeProtection(t)
			nativefixture.RequirePiOrderValues(t, before, "next-theme", "npm:@acme/beta@1", "npm:@acme/foreign@1", "npm:@acme/alpha@1")
			nativefixture.RequirePiOrderValues(t, order.Destination, "next-theme", "npm:@acme/alpha@1", "npm:@acme/foreign@1", "npm:@acme/beta@1")
			nativefixture.RunLifecycleCommand(t, "status", "--manifest", order.Manifest, "--check", "--json")
			testkit.AssertNoRecoveryArtifacts(t, workspace)
		})
	}
}
