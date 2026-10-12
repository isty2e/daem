package cli_test

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestPiNativeMCPManagedOverrideLifecycle(t *testing.T) {
	for _, removed := range []string{"global", "project", "both"} {
		for _, reversed := range []bool{false, true} {
			t.Run(fmt.Sprintf("remove=%s/reversed=%t", removed, reversed), func(t *testing.T) {
				project := newMCPCLIProject(t)
				installNativeVersionFixture(t, project.root, "1.0.2")
				t.Setenv("SOURCE_TOKEN", "fixture-runtime-secret")
				agentRoot := filepath.Join(project.root, "agent")
				t.Setenv("PI_CODING_AGENT_DIR", agentRoot)
				globalPath := filepath.Join(agentRoot, "mcp.json")
				localPath := filepath.Join(project.root, ".pi", "mcp.json")
				sibling := `{"unmanaged":true,"mcpServers":{"manual":{"command":"echo"}}}`
				testkit.WriteFile(t, agentRoot, "mcp.json", sibling)
				testkit.WriteFile(t, project.root, ".pi/mcp.json", sibling)

				for _, commands := range [][2]string{{"global-v1", "project-v1"}, {"global-v1", "project-v2"}, {"global-v2", "project-v2"}} {
					testkit.WriteFile(t, project.root, "daem.toml", nativePiOverrideManifest(commands[0], commands[1], reversed))
					runMCPLock(t, project)
					exit, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes")
					if exit != 0 {
						t.Fatalf("managed override apply %v = %d: %s/%s", commands, exit, stdout, stderr)
					}
					assertNativePeerConfig(t, globalPath, commands[0])
					assertNativePeerConfig(t, localPath, commands[1])
					globalBefore := string(testkit.ReadFile(t, globalPath))
					localBefore := string(testkit.ReadFile(t, localPath))
					exit, stdout, stderr = runMCPCLI(t, "status", "--manifest", project.manifestPath, "--check", "--json")
					if exit != 0 {
						t.Fatalf("managed override convergence %v = %d: %s/%s", commands, exit, stdout, stderr)
					}
					exit, stdout, stderr = runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes")
					if exit != 0 {
						t.Fatalf("repeat managed override apply = %d: %s/%s", exit, stdout, stderr)
					}
					testkit.AssertFileContent(t, globalPath, globalBefore)
					testkit.AssertFileContent(t, localPath, localBefore)
				}

				global, local := "global-v2", "project-v2"
				if removed == "global" || removed == "both" {
					global = ""
				}
				if removed == "project" || removed == "both" {
					local = ""
				}
				testkit.WriteFile(t, project.root, "daem.toml", nativePiOverrideManifest(global, local, reversed))
				runMCPLock(t, project)
				if removed == "both" {
					t.Setenv("PATH", t.TempDir())
				}
				exit, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes")
				if exit != 0 {
					t.Fatalf("coordinated peer removal = %d: %s/%s", exit, stdout, stderr)
				}
				assertNativePeerConfig(t, globalPath, global)
				assertNativePeerConfig(t, localPath, local)
				exit, stdout, stderr = runMCPCLI(t, "status", "--manifest", project.manifestPath, "--check", "--json")
				if exit != 0 {
					t.Fatalf("post-removal convergence = %d: %s/%s", exit, stdout, stderr)
				}
			})
		}
	}
}

func TestPiNativeMCPManagedOverrideDoesNotAuthorizeConflictingOrUnownedPeer(t *testing.T) {
	t.Setenv("SOURCE_TOKEN", "fixture-runtime-secret")

	for _, peer := range []string{
		`{"mcpServers":{"context7":{"command":"external"}}}`,
		`{"mcpServers":{"context7":{"enabled":false}}}`,
		`{"mcpServers":{"context7":{"command":"project-v1","unknown":true}}}`,
		`{"mcpServers":`,
	} {
		t.Run(peer, func(t *testing.T) {
			project := newMCPCLIProject(t)
			installNativeVersionFixture(t, project.root, "1.0.2")
			agentRoot := filepath.Join(project.root, "agent")
			t.Setenv("PI_CODING_AGENT_DIR", agentRoot)
			testkit.WriteFile(t, project.root, "daem.toml", nativePiOverrideManifest("global-v1", "project-v1", false))
			runMCPLock(t, project)
			exit, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes")
			if exit != 0 {
				t.Fatalf("initial managed override = %d: %s/%s", exit, stdout, stderr)
			}
			globalPath := filepath.Join(agentRoot, "mcp.json")
			globalBefore := string(testkit.ReadFile(t, globalPath))
			statePath := filepath.Join(project.root, ".daem", "state.json")
			stateBefore := string(testkit.ReadFile(t, statePath))
			testkit.WriteFile(t, project.root, ".pi/mcp.json", peer)
			exit, stdout, stderr = runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes")
			if exit == 0 {
				t.Fatalf("declared peer bypassed drift/opaque refusal: %s/%s", stdout, stderr)
			}
			testkit.AssertFileContent(t, globalPath, globalBefore)
			testkit.AssertFileContent(t, filepath.Join(project.root, ".pi", "mcp.json"), peer)
			testkit.AssertFileContent(t, statePath, stateBefore)
		})
	}

	t.Run("unmanaged desired match", func(t *testing.T) {
		project := newMCPCLIProject(t)
		installNativeVersionFixture(t, project.root, "1.0.2")
		agentRoot := filepath.Join(project.root, "agent")
		t.Setenv("PI_CODING_AGENT_DIR", agentRoot)
		testkit.WriteFile(t, project.root, "daem.toml", nativePiOverrideManifest("global-v1", "project-v1", false))
		peer := `{"mcpServers":{"context7":{"command":"project-v1","env":{"TOKEN":"${SOURCE_TOKEN}"},"enabled":true,"exposure":"codemode"}}}`
		testkit.WriteFile(t, project.root, ".pi/mcp.json", peer)
		runMCPLock(t, project)
		exit, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes")
		if exit == 0 {
			t.Fatalf("peer declaration silently acquired unmanaged authority: %s/%s", stdout, stderr)
		}
		testkit.AssertFileContent(t, filepath.Join(project.root, ".pi", "mcp.json"), peer)
		testkit.AssertPathMissing(t, filepath.Join(agentRoot, "mcp.json"))
		testkit.AssertPathMissing(t, filepath.Join(project.root, ".daem", "state.json"))
	})
}

func nativePiOverrideManifest(global, local string, reversed bool) string {
	binding := func(command, scope string) string {
		if command == "" {
			return ""
		}
		return fmt.Sprintf("\n[[mcp_server]]\nname = \"context7\"\ntargets = [\"pi\"]\nscope = %q\nbackend = \"native\"\ntransport = \"stdio\"\ncommand = %q\nenv = { TOKEN = { from_env = \"SOURCE_TOKEN\" } }\n", scope, command)
	}
	globalBinding, localBinding := binding(global, "global"), binding(local, "project")
	if reversed {
		globalBinding, localBinding = localBinding, globalBinding
	}
	return "version = 1\ntargets = [\"pi\"]\n" + globalBinding + localBinding
}

func assertNativePeerConfig(t *testing.T, path, command string) {
	t.Helper()
	var config struct {
		Unmanaged bool `json:"unmanaged"`
		Servers   map[string]struct {
			Command string            `json:"command"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(testkit.ReadFile(t, path), &config); err != nil {
		t.Fatal(err)
	}
	server, present := config.Servers["context7"]
	if !config.Unmanaged || config.Servers["manual"].Command != "echo" || present != (command != "") {
		t.Fatalf("peer/sibling preservation at %s: %#v", path, config)
	}
	if command != "" && (server.Command != command || server.Env["TOKEN"] != "${SOURCE_TOKEN}") {
		t.Fatalf("managed command/runtime reference at %s: %#v", path, server)
	}
}
