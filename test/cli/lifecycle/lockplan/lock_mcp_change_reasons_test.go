package cli_test

import (
	"bytes"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/target"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
	"github.com/isty2e/daem/test/testkit"
)

func TestLockExplainsPiMCPDependencyChanges(t *testing.T) {
	tests := []struct {
		name         string
		afterVersion string
		afterEnv     string
		wantReason   string
	}{
		{"provider version only", "2.14.0", "DAEM_TEST_CTX_OLD", "mcp_provider_contribution"},
		{"environment source", "2.15.0", "DAEM_TEST_CTX_NEW", "mcp_environment_sources"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			testkit.SetDefaultRootEnv(t, filepath.Join(root, "home"))
			manifest := filepath.Join(root, "daem.toml")
			lockPath := filepath.Join(root, "daem.lock.toml")
			writePiMCPDependencyManifest(t, root, "2.15.0", "DAEM_TEST_CTX_OLD")
			runLockReasonCLI(t, "lock", "--manifest", manifest)
			before := lockedPiMCPDependency(t, lockPath)
			beforeBytes := testkit.ReadFile(t, lockPath)

			writePiMCPDependencyManifest(t, root, test.afterVersion, test.afterEnv)
			var outputs [][]byte
			for _, args := range [][]string{
				{"lock", "--dry-run"},
				{"lock", "--dry-run", "--verbose"},
				{"outdated"},
				{"outdated", "--verbose"},
			} {
				outputs = append(outputs, runLockReasonCLI(t, append(args, "--manifest", manifest)...))
			}
			if !bytes.Equal(beforeBytes, testkit.ReadFile(t, lockPath)) {
				t.Fatal("lock preview or outdated rewrote the lockfile")
			}
			outputs = append(outputs, runLockReasonCLI(t, "lock", "--manifest", manifest))
			after := lockedPiMCPDependency(t, lockPath)
			if before.Equal(after) {
				t.Fatal("MCP dependency change did not change the locked contract")
			}
			beforeRealization, beforeHasRealization := before.Realization()
			afterRealization, afterHasRealization := after.Realization()
			if !beforeHasRealization || !afterHasRealization {
				t.Fatal("MCP projection lacks realization")
			}
			providerOnly := test.afterVersion != "2.15.0"
			if providerOnly {
				oldProvider, oldPresent := before.MCPProviderContribution()
				newProvider, newPresent := after.MCPProviderContribution()
				if !oldPresent || !newPresent || oldProvider.Equal(newProvider) {
					t.Fatal("version change did not change the selected provider contribution")
				}
				if !beforeRealization.Equal(afterRealization) || !slices.Equal(before.MCPEnvironmentSources(), after.MCPEnvironmentSources()) {
					t.Fatal("provider-only change also changed MCP realization or environment sources")
				}
			} else if !slices.Equal(before.MCPEnvironmentSources(), []string{"DAEM_TEST_CTX_OLD"}) ||
				!slices.Equal(after.MCPEnvironmentSources(), []string{test.afterEnv}) {
				t.Fatalf("MCP environment sources = %q -> %q", before.MCPEnvironmentSources(), after.MCPEnvironmentSources())
			}

			for _, output := range outputs {
				var row string
				for _, line := range strings.Split(string(output), "\n") {
					if strings.HasPrefix(line, `  - resource="mcp-server/ctx"`) {
						row = line
						break
					}
				}
				_, reasons, found := strings.Cut(row, " changed=")
				if !found || reasons == "" {
					t.Fatalf("changed MCP entry has no reason: %q\n%s", row, output)
				}
				names := strings.Split(reasons, ",")
				if !slices.Contains(names, test.wantReason) {
					t.Fatalf("MCP reasons = %q, want %q", names, test.wantReason)
				}
				if providerOnly && !slices.Equal(names, []string{test.wantReason}) {
					t.Fatalf("provider-only reasons = %q, want just %q", names, test.wantReason)
				}
				if slices.Contains(names, "realization") != !beforeRealization.Equal(afterRealization) {
					t.Fatalf("MCP realization reason disagrees with the locked change: %q", names)
				}
				for _, source := range []string{"DAEM_TEST_CTX_OLD", "DAEM_TEST_CTX_NEW"} {
					if strings.Contains(row, source) {
						t.Fatalf("change reason exposes environment source name %q: %q", source, row)
					}
				}
			}

			current := runLockReasonCLI(t, "lock", "--dry-run", "--verbose", "--manifest", manifest)
			for _, line := range strings.Split(string(current), "\n") {
				if strings.HasPrefix(line, "  - ") && strings.Contains(line, " changed=") {
					t.Fatalf("equal MCP contract entry reported changed reasons: %q", line)
				}
			}
		})
	}
}

func writePiMCPDependencyManifest(t *testing.T, root string, version string, envSource string) {
	t.Helper()
	testkit.WriteFile(t, root, "daem.toml", fmt.Sprintf(`
version = 1
targets = ["pi"]

[[extension]]
id = "mcp-adapter"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "npm:pi-mcp-adapter@%s" }

[[mcp_server]]
name = "ctx"
targets = ["pi"]
scope = "project"
transport = "stdio"
command = "node"
args = ["server.js"]
env = { CTX_TOKEN = { from_env = %q } }
`, version, envSource))
}

func lockedPiMCPDependency(t *testing.T, path string) lock.LockedSubjectContract {
	t.Helper()
	file, err := lockfile.Load(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologymcp.ProjectionSubject(target.TargetPi, target.ScopeProject, "ctx")
	if err != nil {
		t.Fatal(err)
	}
	contract, present := file.Locked.Subject(subject)
	if !present {
		t.Fatal("locked Pi MCP projection is missing")
	}
	return contract
}
