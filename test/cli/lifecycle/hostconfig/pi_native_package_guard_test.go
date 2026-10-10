package cli_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestPiNativeConfiguredAdapterRefusesRegardlessOfFiltersAndLayers(t *testing.T) {
	for _, entry := range []string{
		`"npm:pi-mcp-adapter@2.15.0"`,
		`{"source":"npm:pi-mcp-adapter@2.15.0","extensions":[]}`,
		`{"source":"npm:pi-mcp-adapter@2.15.0","extensions":["-index.ts"]}`,
		`{"source":"npm:pi-mcp-adapter@2.15.0","extensions":["**/*.ts"]}`,
		`{"source":"npm:pi-mcp-adapter@2.15.0","autoload":false,"extensions":[]}`,
		`{"source":"npm:pi-mcp-adapter@2.15.0","autoload":false,"extensions":["-index.ts"]}`,
		`{"source":"npm:pi-mcp-adapter@2.15.0","extensions":null}`,
		`{"source":"npm:pi-mcp-adapter@2.15.0","extensions":[]},"npm:pi-mcp-adapter@2.13.0"`,
	} {
		for _, layer := range []string{"user", "project"} {
			for _, scope := range []string{"project", "global"} {
				t.Run(entry+"/"+layer+"/"+scope, func(t *testing.T) {
					project := newMCPCLIProject(t)
					installNativeVersionFixture(t, project.root, "1.0.2")
					agentRoot := filepath.Join(project.root, "agent")
					t.Setenv("PI_CODING_AGENT_DIR", agentRoot)
					settings := `{"packages":[` + entry + `]}`
					settingsPath := filepath.Join(agentRoot, "settings.json")
					if layer == "project" {
						settingsPath = filepath.Join(project.root, ".pi", "settings.json")
					}
					testkit.WriteFile(t, filepath.Dir(settingsPath), filepath.Base(settingsPath), settings)
					testkit.WriteFile(t, project.root, "daem.toml", nativePiManifest(scope))
					runMCPLock(t, project)
					lockBefore := string(testkit.ReadFile(t, project.lockfilePath))

					exit, stdout, stderr := runMCPCLI(t, "status", "--manifest", project.manifestPath, "--json")
					if exit != 0 || stderr != "" {
						t.Fatalf("configured intent suppressed status: %d, %s, %s", exit, stdout, stderr)
					}
					report := clijson.DecodePlan(t, []byte(stdout))
					if !report.HasErrors || len(report.MCPStatuses) != 1 {
						t.Fatalf("configured intent lacks binding refusal: %#v", report)
					}
					found := false
					for _, dimension := range report.MCPStatuses[0].Host {
						if dimension.Dimension == "host_prerequisite" && dimension.State == "unqualified" && dimension.Reason == "HOST_ADAPTER_CONFIGURED" {
							found = true
						}
					}
					if !found {
						t.Fatalf("package filters changed intent classification: %#v", report.MCPStatuses)
					}
					if exit, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes"); exit == 0 {
						t.Fatalf("configured Adapter allowed publication: %s/%s", stdout, stderr)
					}
					configPath := filepath.Join(project.root, aggregate.PiProjectMCPConfigPath)
					if scope == "global" {
						configPath = filepath.Join(agentRoot, "mcp.json")
					}
					testkit.AssertPathMissing(t, configPath)
					testkit.AssertPathMissing(t, filepath.Join(project.root, ".daem", "state.json"))
					testkit.AssertFileContent(t, project.lockfilePath, lockBefore)
					testkit.AssertFileContent(t, settingsPath, settings)
				})
			}
		}
	}
}

func TestPiNativeConfiguredAdapterPreservesPublishedFilesWithoutLocationQueries(t *testing.T) {
	for _, manager := range []string{"npm", "pnpm", "bun"} {
		for _, metadata := range []string{"absent", "malformed", "installed"} {
			t.Run(manager+"/"+metadata, func(t *testing.T) {
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
				legacyRoot := filepath.Join(project.root, "legacy", "node_modules")
				log := installNativePackageLocationFixture(t, project.root, manager, "root -g", legacyRoot)
				settings := `{"packages":[{"source":"npm:pi-mcp-adapter@2.15.0","extensions":[]}],"npmCommand":["` + manager + `"]}`
				testkit.WriteFile(t, agentRoot, "settings.json", settings)
				if metadata != "absent" {
					content := `{"name":"pi-mcp-adapter","version":"2.15.0","pi":{"extensions":["index.ts"]}}`
					if metadata == "malformed" {
						content = "{"
					}
					testkit.WriteFile(t, filepath.Join(legacyRoot, "pi-mcp-adapter"), "package.json", content)
				}

				for _, args := range [][]string{{"status", "--check", "--json"}, {"apply", "--yes"}} {
					if exit, stdout, stderr := runMCPCLI(t, append(args, "--manifest", project.manifestPath)...); exit == 0 {
						t.Fatalf("configured Adapter allowed %v: %s/%s", args, stdout, stderr)
					}
					testkit.AssertFileContent(t, configPath, config)
					testkit.AssertFileContent(t, statePath, state)
					testkit.AssertFileContent(t, project.lockfilePath, lock)
					testkit.AssertFileContent(t, filepath.Join(agentRoot, "settings.json"), settings)
					testkit.AssertPathMissing(t, log)
					testkit.AssertPathMissing(t, filepath.Join(agentRoot, "npm"))
				}
			})
		}
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
