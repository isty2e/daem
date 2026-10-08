package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestPiNativeLegacyInventoryQualifiesApplyAndStatusWithoutInstall(t *testing.T) {
	for _, manager := range []string{"npm", "pnpm", "bun"} {
		t.Run(manager, func(t *testing.T) {
			project := newMCPCLIProject(t)
			installNativeVersionFixture(t, project.root, "1.0.2")
			t.Setenv("SOURCE_TOKEN", "fixture-runtime-secret")
			agentRoot := filepath.Join(project.root, "agent")
			t.Setenv("PI_CODING_AGENT_DIR", agentRoot)
			legacyRoot := filepath.Join(project.root, "legacy")
			packageRoot := filepath.Join(legacyRoot, "node_modules", "pi-mcp-adapter")
			response, argv := filepath.Dir(packageRoot), "root -g"
			if manager == "pnpm" {
				packageRoot = filepath.Join(legacyRoot, "pnpm-store", "adapter")
				wire, err := json.Marshal([]map[string]map[string]map[string]string{{"dependencies": {"pi-mcp-adapter": {"path": packageRoot}}}})
				if err != nil {
					t.Fatal(err)
				}
				response, argv = string(wire), "list -g --depth 0 --json"
			} else if manager == "bun" {
				packageRoot = filepath.Join(legacyRoot, "install", "global", "node_modules", "pi-mcp-adapter")
				response, argv = filepath.Join(legacyRoot, "bin"), "pm bin -g"
			}
			log := installNativePackageLocationFixture(t, project.root, manager, argv, response)
			settings := `{"packages":[{"source":"npm:pi-mcp-adapter@2.15.0","extensions":["-index.ts"]}],"npmCommand":["` + manager + `"]}`
			testkit.WriteFile(t, agentRoot, "settings.json", settings)
			testkit.WriteFile(t, packageRoot, "package.json", `{"name":"pi-mcp-adapter","version":"2.15.0","pi":{"extensions":["index.ts"]}}`)
			testkit.WriteFile(t, packageRoot, "index.ts", "export {};\n")
			testkit.WriteFile(t, project.root, "daem.toml", nativePiManifest("project"))
			runMCPLock(t, project)

			for _, command := range []string{"apply", "status"} {
				args := []string{command, "--manifest", project.manifestPath}
				if command == "apply" {
					args = append(args, "--yes")
				} else {
					args = append(args, "--check")
				}
				exit, stdout, stderr := runMCPCLI(t, args...)
				if exit != 0 {
					t.Fatalf("%s using legacy inventory = %d, %s, %s", command, exit, stdout, stderr)
				}
			}
			calls := strings.Split(strings.TrimSpace(string(testkit.ReadFile(t, log))), "\n")
			if len(calls) == 0 || calls[0] == "" {
				t.Fatal("legacy inventory qualification skipped location queries")
			}
			for _, call := range calls {
				if call != argv {
					t.Fatalf("unexpected package operation: %q", call)
				}
			}
			testkit.AssertPathMissing(t, filepath.Join(agentRoot, "npm"))
			testkit.AssertFileContent(t, filepath.Join(agentRoot, "settings.json"), settings)
		})
	}
}

func TestPiNativeLegacyInventoryRefusalPreservesPublishedFiles(t *testing.T) {
	for _, failure := range []string{"query failure", "malformed selected metadata", "command prefix"} {
		t.Run(failure, func(t *testing.T) {
			project := newMCPCLIProject(t)
			installNativeVersionFixture(t, project.root, "1.0.2")
			t.Setenv("SOURCE_TOKEN", "fixture-runtime-secret")
			agentRoot := filepath.Join(project.root, "agent")
			t.Setenv("PI_CODING_AGENT_DIR", agentRoot)
			testkit.WriteFile(t, project.root, "daem.toml", nativePiManifest("project"))
			runMCPLock(t, project)
			if exit, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes"); exit != 0 {
				t.Fatalf("initial Native apply = %d, %s, %s", exit, stdout, stderr)
			}
			configPath := filepath.Join(project.root, ".pi", "mcp.json")
			statePath := filepath.Join(project.root, ".daem", "state.json")
			config, state, lock := string(testkit.ReadFile(t, configPath)), string(testkit.ReadFile(t, statePath)), string(testkit.ReadFile(t, project.lockfilePath))
			legacyBase := filepath.Join(project.root, "legacy", "node_modules")
			log := installNativePackageLocationFixture(t, project.root, "npm", "root -g", legacyBase)
			metadata := `{"name":"pi-mcp-adapter","version":"2.15.0","pi":{"extensions":["index.ts"]}}`
			command := `["npm"]`
			switch failure {
			case "query failure":
				t.Setenv("DAEM_PACKAGE_LOCATION_FAIL", "1")
			case "malformed selected metadata":
				metadata = "{"
			case "command prefix":
				command = `["npm","install"]`
			}
			settings := `{"packages":[{"source":"npm:pi-mcp-adapter@2.15.0","extensions":["-index.ts"]}],"npmCommand":` + command + `}`
			testkit.WriteFile(t, agentRoot, "settings.json", settings)
			testkit.WriteFile(t, filepath.Join(legacyBase, "pi-mcp-adapter"), "package.json", metadata)

			if exit, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes"); exit == 0 {
				t.Fatalf("unobservable legacy inventory allowed apply: %s, %s", stdout, stderr)
			}
			testkit.AssertFileContent(t, configPath, config)
			testkit.AssertFileContent(t, statePath, state)
			testkit.AssertFileContent(t, project.lockfilePath, lock)
			testkit.AssertFileContent(t, filepath.Join(agentRoot, "settings.json"), settings)
			testkit.AssertPathMissing(t, filepath.Join(agentRoot, "npm"))
			if failure == "command prefix" {
				testkit.AssertPathMissing(t, log)
			} else {
				testkit.AssertFileContent(t, log, "root -g\n")
			}
		})
	}
}

func installNativePackageLocationFixture(t *testing.T, root, manager, argv, response string) string {
	t.Helper()
	log := filepath.Join(root, "package-location-calls.txt")
	t.Setenv("DAEM_PACKAGE_LOCATION_LOG", log)
	t.Setenv("DAEM_PACKAGE_LOCATION_ARGV", argv)
	t.Setenv("DAEM_PACKAGE_LOCATION_RESPONSE", response)
	t.Setenv("DAEM_PACKAGE_LOCATION_FAIL", "")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$DAEM_PACKAGE_LOCATION_LOG\"\n[ \"$*\" = \"$DAEM_PACKAGE_LOCATION_ARGV\" ] || exit 98\n[ \"$DAEM_PACKAGE_LOCATION_FAIL\" != 1 ] || exit 97\nprintf '%s\\n' \"$DAEM_PACKAGE_LOCATION_RESPONSE\"\n"
	if err := os.WriteFile(filepath.Join(root, "bin", manager), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return log
}
