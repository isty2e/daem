package cli_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestPiNativePhysicalAliasesRefuseBeforePublication(t *testing.T) {
	for _, scope := range []string{"global", "project"} {
		for _, ancestor := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/ancestor=%t", scope, ancestor), func(t *testing.T) {
				project := newMCPCLIProject(t)
				versionLog := installNativeVersionFixture(t, project.root, "1.0.2")
				config := `{"unmanaged":true,"mcpServers":{"manual":{"command":"echo"}}}`
				testkit.WriteFile(t, project.root, ".pi/mcp.json", config)
				alias := filepath.Join(t.TempDir(), "agent")
				linkTarget, agentRoot := filepath.Join(project.root, ".pi"), alias
				if ancestor {
					linkTarget, agentRoot = project.root, filepath.Join(alias, ".pi")
				}
				if err := os.Symlink(linkTarget, alias); err != nil {
					t.Skipf("directory symlinks unavailable: %v", err)
				}
				t.Setenv("PI_CODING_AGENT_DIR", agentRoot)
				manifest := fmt.Sprintf("version = 1\ntargets = [\"pi\"]\n\n[[mcp_server]]\nname = \"context7\"\ntargets = [\"pi\"]\nscope = %q\nbackend = \"native\"\ntransport = \"stdio\"\ncommand = \"node\"\n", scope)
				testkit.WriteFile(t, project.root, "daem.toml", manifest)
				runMCPLock(t, project)
				beforeLock := string(testkit.ReadFile(t, project.lockfilePath))

				exit, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes")
				if exit == 0 {
					t.Fatalf("physical alias was accepted: %s/%s", stdout, stderr)
				}
				testkit.AssertFileContent(t, filepath.Join(project.root, ".pi/mcp.json"), config)
				testkit.AssertFileContent(t, project.lockfilePath, beforeLock)
				testkit.AssertPathMissing(t, versionLog)
				testkit.AssertPathMissing(t, filepath.Join(project.root, ".daem", "state.json"))
			})
		}
	}
}

func TestPiNativeSourceNormalizationPreservesManagedOutputsOnRefusal(t *testing.T) {
	t.Setenv("SOURCE_TOKEN", "fixture-runtime-secret")
	for _, scope := range []string{"global", "project"} {
		for _, packageScope := range []string{"global", "project"} {
			for _, sourceKind := range []string{"alias", "local", "Unix filter", "unrelated alias"} {
				t.Run(scope+"/"+packageScope+"/"+sourceKind, func(t *testing.T) {
					if sourceKind == "Unix filter" && filepath.Separator == '\\' {
						t.Skip("backslash escape refusal is a Unix fixture")
					}
					project := newMCPCLIProject(t)
					installNativeVersionFixture(t, project.root, "1.0.2")
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
					configBefore := string(testkit.ReadFile(t, configPath))
					stateBefore := string(testkit.ReadFile(t, statePath))
					lockBefore := string(testkit.ReadFile(t, project.lockfilePath))
					base := agentRoot
					if packageScope == "project" {
						base = filepath.Join(project.root, ".pi")
					}
					entry := map[string]any{"source": "npm:mcp-shim@npm:pi-mcp-adapter@2.15.0", "extensions": []string{}}
					switch sourceKind {
					case "local":
						testkit.WriteFile(t, base, "renamed/package.json", `{"name":"pi-mcp-adapter"}`)
						testkit.WriteFile(t, base, "renamed/index.ts", "export {}")
						entry["source"] = " ./renamed "
					case "Unix filter":
						testkit.WriteFile(t, base, "npm/node_modules/pi-mcp-adapter/package.json", `{"name":"pi-mcp-adapter","version":"2.15.0","pi":{"extensions":["nested/index.ts"]}}`)
						testkit.WriteFile(t, base, "npm/node_modules/pi-mcp-adapter/nested/index.ts", "export {}")
						entry["source"] = "npm:pi-mcp-adapter@2.15.0"
						entry["extensions"] = []string{`-nested\index.ts`}
					case "unrelated alias":
						entry["source"] = "npm:mcp-shim@npm:unrelated-extension@1.0.0"
					}
					settings, err := json.Marshal(map[string]any{"packages": []any{entry}})
					if err != nil {
						t.Fatal(err)
					}
					settingsPath := filepath.Join(base, "settings.json")
					testkit.WriteFile(t, base, "settings.json", string(settings))

					exit, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes")
					if sourceKind == "unrelated alias" {
						if exit != 0 {
							t.Fatalf("unrelated alias changed Native eligibility: %s/%s", stdout, stderr)
						}
						if exit, stdout, stderr := runMCPCLI(t, "status", "--manifest", project.manifestPath, "--check"); exit != 0 {
							t.Fatalf("unrelated alias convergence failed: %s/%s", stdout, stderr)
						}
					} else {
						if exit == 0 {
							t.Fatalf("unsafe selection published Native: %s/%s", stdout, stderr)
						}
						testkit.AssertFileContent(t, statePath, stateBefore)
					}
					testkit.AssertFileContent(t, configPath, configBefore)
					testkit.AssertFileContent(t, project.lockfilePath, lockBefore)
					testkit.AssertFileContent(t, settingsPath, string(settings))
				})
			}
		}
	}
}
