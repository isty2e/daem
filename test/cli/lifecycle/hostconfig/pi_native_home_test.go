package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/test/testkit"
)

func TestPiNativeMCPExplicitRootLifecycleWithoutHome(t *testing.T) {
	for _, scope := range []string{"project", "global"} {
		t.Run(scope, func(t *testing.T) {
			project := newMCPCLIProject(t)
			installNativeVersionFixture(t, project.root, "1.0.2")
			for _, variable := range []string{"HOME", "USERPROFILE", "home"} {
				t.Setenv(variable, "")
			}
			if _, err := os.UserHomeDir(); err == nil {
				t.Skip("platform does not support an unavailable user-home fixture")
			}
			agentRoot := filepath.Join(project.root, "custom-agent")
			t.Setenv("PI_CODING_AGENT_DIR", agentRoot)
			t.Setenv("SOURCE_TOKEN", "fixture-runtime-secret")
			testkit.WriteFile(t, project.root, "daem.toml", nativePiManifest(scope))
			selectedRelative := aggregate.PiProjectMCPConfigPath
			if scope == "global" {
				selectedRelative = "custom-agent/mcp.json"
			}
			selectedPath := filepath.Join(project.root, selectedRelative)
			sibling := `{"mcpServers":{"manual":{"command":"echo"}},"unmanaged":true}`
			testkit.WriteFile(t, project.root, selectedRelative, sibling)
			runMCPLock(t, project)

			exit, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes")
			if exit != 0 {
				t.Fatalf("Native explicit-root apply without HOME = %d: %s/%s", exit, stdout, stderr)
			}
			exit, stdout, stderr = runMCPCLI(t, "status", "--manifest", project.manifestPath, "--check", "--json")
			if exit != 0 {
				t.Fatalf("Native explicit-root status without HOME = %d: %s/%s", exit, stdout, stderr)
			}
			var config struct {
				Unmanaged bool `json:"unmanaged"`
				Servers   map[string]struct {
					Command string            `json:"command"`
					Env     map[string]string `json:"env"`
				} `json:"mcpServers"`
			}
			if err := json.Unmarshal(testkit.ReadFile(t, selectedPath), &config); err != nil {
				t.Fatal(err)
			}
			if !config.Unmanaged || len(config.Servers) != 2 || config.Servers["manual"].Command != "echo" || config.Servers["context7"].Env["TOKEN"] != "${SOURCE_TOKEN}" {
				t.Fatalf("explicit-root config lost ownership/runtime-reference facts: %#v", config)
			}

			testkit.WriteFile(t, project.root, "daem.toml", "version = 1\ntargets = [\"pi\"]\n")
			t.Setenv("PATH", t.TempDir())
			runMCPLock(t, project)
			exit, stdout, stderr = runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes")
			if exit != 0 {
				t.Fatalf("stored Native retirement without HOME/Pi = %d: %s/%s", exit, stdout, stderr)
			}
			config.Servers = nil
			if err := json.Unmarshal(testkit.ReadFile(t, selectedPath), &config); err != nil {
				t.Fatal(err)
			}
			if !config.Unmanaged || len(config.Servers) != 1 || config.Servers["manual"].Command != "echo" {
				t.Fatalf("explicit-root retirement changed unmanaged sibling: %#v", config)
			}
			testkit.AssertPathMissing(t, filepath.Join(project.root, "home", ".pi", "agent", "mcp.json"))
		})
	}
}
