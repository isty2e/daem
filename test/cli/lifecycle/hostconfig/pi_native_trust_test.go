package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestPiNativeTrustIndependentRefusalPreservesPublishedFiles(t *testing.T) {
	for _, failure := range []string{"user adapter masked", "project command", "Git adapter", "local adapter"} {
		for _, scope := range []string{"project", "global"} {
			t.Run(failure+"/"+scope, func(t *testing.T) {
				project := newMCPCLIProject(t)
				installNativeVersionFixture(t, project.root, "1.0.2")
				t.Setenv("SOURCE_TOKEN", "fixture-runtime-secret")
				agentRoot := filepath.Join(project.root, "agent")
				t.Setenv("PI_CODING_AGENT_DIR", agentRoot)
				testkit.WriteFile(t, project.root, "daem.toml", nativePiManifest(scope))
				runMCPLock(t, project)
				if exit, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes"); exit != 0 {
					t.Fatalf("initial Native apply = %d: %s/%s", exit, stdout, stderr)
				}
				configPath := filepath.Join(project.root, ".pi", "mcp.json")
				if scope == "global" {
					configPath = filepath.Join(agentRoot, "mcp.json")
				}
				statePath := filepath.Join(project.root, ".daem", "state.json")
				config, state, lock := string(testkit.ReadFile(t, configPath)), string(testkit.ReadFile(t, statePath)), string(testkit.ReadFile(t, project.lockfilePath))
				userSettings := `{"packages":["npm:pi-mcp-adapter@2.15.0"]}`
				projectSettings := `{"packages":[{"source":"npm:pi-mcp-adapter@2.15.0","extensions":[]}]}`
				marker := filepath.Join(project.root, "repository-command-ran")
				switch failure {
				case "project command":
					legacyRoot := filepath.Join(project.root, "legacy", "node_modules")
					installNativePackageLocationFixture(t, project.root, "npm", "root -g", legacyRoot)
					testkit.WriteFile(t, filepath.Join(legacyRoot, "pi-mcp-adapter"), "package.json", `{"name":"pi-mcp-adapter","version":"2.15.0","pi":{"extensions":["index.ts"]}}`)
					commandPath := filepath.Join(project.root, "repository", "npm")
					testkit.WriteFile(t, project.root, "repository/npm", "#!/bin/sh\nprintf executed > '"+marker+"'\nprintf '%s\\n' '"+legacyRoot+"'\n")
					if err := os.Chmod(commandPath, 0o700); err != nil {
						t.Fatal(err)
					}
					command, err := json.Marshal([]string{commandPath})
					if err != nil {
						t.Fatal(err)
					}
					userSettings = `{"packages":[{"source":"npm:pi-mcp-adapter@2.15.0","extensions":["-index.ts"]}]}`
					projectSettings = `{"npmCommand":` + string(command) + `}`
				case "Git adapter", "local adapter":
					source := "git:github.com/nicobailon/pi-mcp-adapter@main"
					if failure == "local adapter" {
						source = filepath.Join(project.root, "renamed-checkout", "index.ts")
						testkit.WriteFile(t, project.root, "renamed-checkout/package.json", `{"name":"pi-mcp-adapter","pi":{"extensions":["index.ts"]}}`)
						testkit.WriteFile(t, project.root, "renamed-checkout/index.ts", "export {};\n")
					}
					packages := []map[string]any{
						{"source": "npm:pi-mcp-adapter@2.15.0", "extensions": []string{}},
						{"source": source, "extensions": []string{}},
					}
					wire, err := json.Marshal(map[string]any{"packages": packages})
					if err != nil {
						t.Fatal(err)
					}
					userSettings = string(wire)
				}
				testkit.WriteFile(t, agentRoot, "settings.json", userSettings)
				testkit.WriteFile(t, project.root, ".pi/settings.json", projectSettings)

				for _, command := range []string{"status", "apply"} {
					args := []string{command, "--manifest", project.manifestPath}
					if command == "status" {
						args = append(args, "--check")
					} else {
						args = append(args, "--yes")
					}
					if exit, stdout, stderr := runMCPCLI(t, args...); exit == 0 {
						t.Errorf("%s accepted trust-dependent qualification: %s/%s", command, stdout, stderr)
					}
					testkit.AssertPathMissing(t, marker)
					testkit.AssertFileContent(t, configPath, config)
					testkit.AssertFileContent(t, statePath, state)
					testkit.AssertFileContent(t, project.lockfilePath, lock)
					testkit.AssertFileContent(t, filepath.Join(agentRoot, "settings.json"), userSettings)
					testkit.AssertFileContent(t, filepath.Join(project.root, ".pi", "settings.json"), projectSettings)
				}
			})
		}
	}
}
