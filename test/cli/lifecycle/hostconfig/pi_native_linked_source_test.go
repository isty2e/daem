package cli_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestPiNativeLinkedAdapterRefusalPreservesOutputsAndRetirement(t *testing.T) {
	t.Setenv("SOURCE_TOKEN", "fixture-runtime-secret")
	for _, field := range []string{"packages", "extensions"} {
		for _, scope := range []string{"global", "project"} {
			for _, settingsScope := range []string{"global", "project"} {
				for _, managed := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/%s/managed=%t", field, scope, settingsScope, managed), func(t *testing.T) {
						project := newMCPCLIProject(t)
						installNativeVersionFixture(t, project.root, "1.0.2")
						agentRoot := filepath.Join(project.root, "agent")
						t.Setenv("PI_CODING_AGENT_DIR", agentRoot)
						testkit.WriteFile(t, project.root, "daem.toml", nativePiManifest(scope))
						configBase := filepath.Join(project.root, ".pi")
						if scope == "global" {
							configBase = agentRoot
						}
						configPath := filepath.Join(configBase, "mcp.json")
						testkit.WriteFile(t, configBase, "mcp.json", `{"mcpServers":{"manual":{"command":"echo"}},"unmanaged":true}`)
						runMCPLock(t, project)
						if managed {
							if exit, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes"); exit != 0 {
								t.Fatalf("initial Native apply = %d: %s/%s", exit, stdout, stderr)
							}
						}
						configBefore := string(testkit.ReadFile(t, configPath))
						lockBefore := string(testkit.ReadFile(t, project.lockfilePath))
						statePath := filepath.Join(project.root, ".daem", "state.json")
						stateBefore := ""
						if managed {
							stateBefore = string(testkit.ReadFile(t, statePath))
						}

						base := agentRoot
						if settingsScope == "project" {
							base = filepath.Join(project.root, ".pi")
						}
						targetRoot := t.TempDir()
						testkit.WriteFile(t, targetRoot, "package.json", `{"name":"pi-mcp-adapter"}`)
						extension := "export default () => { throw new Error('inert linked fixture'); };\n"
						testkit.WriteFile(t, targetRoot, "index.ts", extension)
						testkit.WriteFile(t, base, "links/package.json", `{"name":"unrelated"}`)
						link := filepath.Join(base, "links", "index.ts")
						target := filepath.Join(targetRoot, "index.ts")
						if err := os.Symlink(target, link); err != nil {
							t.Skipf("file symlink unavailable: %v", err)
						}
						wire, err := json.Marshal(map[string][]string{field: {link}})
						if err != nil {
							t.Fatal(err)
						}
						settingsPath := filepath.Join(base, "settings.json")
						testkit.WriteFile(t, base, "settings.json", string(wire))

						for _, command := range []string{"apply", "status"} {
							flag := "--yes"
							if command == "status" {
								flag = "--check"
							}
							if exit, stdout, stderr := runMCPCLI(t, command, "--manifest", project.manifestPath, flag); exit == 0 {
								t.Fatalf("linked Adapter permitted %s: %s/%s", command, stdout, stderr)
							}
							testkit.AssertFileContent(t, configPath, configBefore)
							testkit.AssertFileContent(t, project.lockfilePath, lockBefore)
							testkit.AssertFileContent(t, settingsPath, string(wire))
							testkit.AssertFileContent(t, target, extension)
							if got, err := os.Readlink(link); err != nil || got != target {
								t.Fatalf("refusal changed the configured symlink: %q/%v", got, err)
							}
							if managed {
								testkit.AssertFileContent(t, statePath, stateBefore)
							} else {
								testkit.AssertPathMissing(t, statePath)
							}
						}

						if managed {
							testkit.WriteFile(t, project.root, "daem.toml", "version = 1\ntargets = [\"pi\"]\n")
							t.Setenv("PATH", t.TempDir())
							runMCPLock(t, project)
							if exit, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes"); exit != 0 {
								t.Fatalf("retirement gained linked-extension prerequisites: %s/%s", stdout, stderr)
							}
							var config struct {
								Unmanaged bool `json:"unmanaged"`
								Servers   map[string]struct {
									Command string `json:"command"`
								} `json:"mcpServers"`
							}
							if err := json.Unmarshal(testkit.ReadFile(t, configPath), &config); err != nil {
								t.Fatal(err)
							}
							if !config.Unmanaged || len(config.Servers) != 1 || config.Servers["manual"].Command != "echo" {
								t.Fatalf("retirement changed an unmanaged sibling: %#v", config)
							}
							testkit.AssertFileContent(t, settingsPath, string(wire))
						}
					})
				}
			}
		}
	}
}

func TestPiNativeLinkedUnrelatedSourcesConverge(t *testing.T) {
	t.Setenv("SOURCE_TOKEN", "fixture-runtime-secret")
	for _, field := range []string{"packages", "extensions"} {
		for _, scope := range []string{"global", "project"} {
			t.Run(field+"/"+scope, func(t *testing.T) {
				project := newMCPCLIProject(t)
				installNativeVersionFixture(t, project.root, "1.0.2")
				agentRoot, targetRoot := filepath.Join(project.root, "agent"), t.TempDir()
				t.Setenv("PI_CODING_AGENT_DIR", agentRoot)
				testkit.WriteFile(t, project.root, "daem.toml", nativePiManifest(scope))
				testkit.WriteFile(t, targetRoot, "package.json", `{"name":"unrelated"}`)
				testkit.WriteFile(t, targetRoot, "index.ts", "export {};\n")
				testkit.WriteFile(t, agentRoot, "links/package.json", `{}`)
				link, target := filepath.Join(agentRoot, "links", "index.ts"), filepath.Join(targetRoot, "index.ts")
				if err := os.Symlink(target, link); err != nil {
					t.Skipf("file symlink unavailable: %v", err)
				}
				wire, err := json.Marshal(map[string][]string{field: {"./links/index.ts"}})
				if err != nil {
					t.Fatal(err)
				}
				testkit.WriteFile(t, agentRoot, "settings.json", string(wire))
				runMCPLock(t, project)

				for range 2 {
					if exit, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes"); exit != 0 {
						t.Fatalf("unrelated linked source changed eligibility: %s/%s", stdout, stderr)
					}
					if exit, stdout, stderr := runMCPCLI(t, "status", "--manifest", project.manifestPath, "--check"); exit != 0 {
						t.Fatalf("unrelated linked source did not converge: %s/%s", stdout, stderr)
					}
				}
				testkit.AssertFileContent(t, filepath.Join(agentRoot, "settings.json"), string(wire))
				if got, err := os.Readlink(link); err != nil || got != target {
					t.Fatalf("apply changed an unrelated symlink: %q/%v", got, err)
				}
			})
		}
	}
}
