package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestPiNativeQualificationReportsMixedTargetsWithoutPublishing(t *testing.T) {
	for _, test := range []struct {
		name, version, settings, reason string
	}{
		{"missing Pi", "", `{}`, "HOST_VERSION_UNQUALIFIED"},
		{"unsupported Pi", "2.0.0", `{}`, "HOST_VERSION_UNQUALIFIED"},
		{"malformed settings", "1.0.2", `{"credential":"private-fixture",`, "HOST_SETTINGS_UNOBSERVED"},
		{"disabled builtin", "1.0.2", `{"extensions":["-builtin:mcp"]}`, "HOST_BUILTIN_DISABLED"},
		{"unobserved builtin", "1.0.2", `{"extensions":["!**/mcp"]}`, "HOST_BUILTIN_UNOBSERVED"},
		{"disabled adapter", "1.0.2", `{"packages":[{"source":"npm:pi-mcp-adapter@2.15.0","extensions":[]}]}`, "HOST_ADAPTER_CONFIGURED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			project := newMCPCLIProject(t)
			t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(project.root, "agent"))
			if test.version == "" {
				t.Setenv("PATH", t.TempDir())
			} else {
				installNativeVersionFixture(t, project.root, test.version)
			}
			testkit.WriteFile(t, project.root, "daem.toml", `version = 1
targets = ["pi", "claude-code"]

[[mcp_server]]
name = "native"
targets = ["pi"]
scope = "project"
backend = "native"
transport = "stdio"
command = "fixture-server"

[[mcp_server]]
name = "claude"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = "fixture-server"
`)
			testkit.WriteFile(t, project.root, ".pi/settings.json", test.settings)
			runMCPLock(t, project)

			for _, args := range [][]string{{"status", "--json"}, {"status", "--check", "--json"}, {"apply", "--dry-run", "--json"}} {
				exit, stdout, stderr := runMCPCLI(t, append(args, "--manifest", project.manifestPath)...)
				wantExit := 1
				if len(args) == 2 && args[0] == "status" {
					wantExit = 0
				}
				if exit != wantExit || stderr != "" {
					t.Fatalf("%v = %d, %s, %s", args, exit, stdout, stderr)
				}
				var report struct {
					HasErrors bool `json:"has_errors"`
					Statuses  []struct {
						Subject struct{ Name string }                               `json:"subject"`
						Host    []struct{ Dimension, State, Reason, Detail string } `json:"host_dimensions"`
					} `json:"mcp_statuses"`
				}
				if err := json.Unmarshal([]byte(stdout), &report); err != nil {
					t.Fatal(err)
				}
				if !report.HasErrors || len(report.Statuses) != 2 {
					t.Fatalf("mixed report lost status or refusal: %+v", report)
				}
				found := false
				for _, status := range report.Statuses {
					if status.Subject.Name != "native" {
						continue
					}
					for _, dimension := range status.Host {
						if dimension.Dimension == "host_prerequisite" && dimension.State == "unqualified" && dimension.Reason == test.reason && dimension.Detail != "" {
							found = true
						}
					}
				}
				if !found || strings.Contains(stdout, "private-fixture") {
					t.Fatalf("missing safe host qualification: %s", stdout)
				}
			}

			exit, stdout, stderr := runMCPCLI(t, "status", "--manifest", project.manifestPath, "--target", "claude-code", "--json")
			if exit != 0 || stderr != "" {
				t.Fatalf("independent target status = %d, %s, %s", exit, stdout, stderr)
			}
			exit, stdout, stderr = runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--dry-run", "--json")
			if exit != 0 || stderr != "" {
				t.Fatalf("independent target plan = %d, %s, %s", exit, stdout, stderr)
			}
			exit, stdout, stderr = runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes")
			if exit == 0 {
				t.Fatalf("unqualified plan published: %s, %s", stdout, stderr)
			}
			testkit.AssertPathMissing(t, filepath.Join(project.root, ".pi/mcp.json"))
			testkit.AssertPathMissing(t, filepath.Join(project.root, ".mcp.json"))
			testkit.AssertFileContent(t, filepath.Join(project.root, ".pi/settings.json"), test.settings)
		})
	}
}

func TestPiNativeNewPeerInheritsRecordedCohortWithoutVersionQuery(t *testing.T) {
	for _, version := range []string{"", "2.0.0"} {
		t.Run("host-"+version, func(t *testing.T) {
			project := newMCPCLIProject(t)
			log := installNativeVersionFixture(t, project.root, "1.0.2")
			testkit.WriteFile(t, project.root, "daem.toml", "version = 1\ntargets = [\"pi\"]\n")
			exit, stdout, stderr := runMCPCLI(t, "add", "mcp-server", "first", "fixture-server", "--manifest", project.manifestPath, "--target", "pi", "--scope", "project")
			if exit != 0 {
				t.Fatalf("first authoring = %d, %s, %s", exit, stdout, stderr)
			}
			if version == "" {
				t.Setenv("PATH", t.TempDir())
			} else {
				installNativeVersionFixture(t, project.root, version)
			}
			beforeCalls := string(testkit.ReadFile(t, log))

			exit, stdout, stderr = runMCPCLI(t, "add", "mcp-server", "second", "fixture-server", "--manifest", project.manifestPath, "--target", "pi", "--scope", "global")
			if exit != 0 {
				t.Fatalf("cohort authoring = %d, %s, %s", exit, stdout, stderr)
			}
			testkit.AssertFileContent(t, log, beforeCalls)
			manifest, err := declarationmanifest.Decode(testkit.ReadFile(t, project.manifestPath))
			if err != nil || len(manifest.MCPServers()) != 2 || len(manifest.Extensions()) != 0 {
				t.Fatalf("authoring changed the cohort: %v", err)
			}
			for _, server := range manifest.MCPServers() {
				if server.Bindings()[0].Backend() != desiredmcp.BackendNative {
					t.Fatal("new server did not inherit the Native cohort")
				}
			}
		})
	}
}

func TestPiNativeDirectBackendTransitionHasContextAndPreservesStoredConfig(t *testing.T) {
	project := newManagedPiAdapterForBackendChange(t)
	configPath := filepath.Join(project.root, ".pi/mcp.json")
	statePath := filepath.Join(project.root, ".daem/state.json")
	before := string(testkit.ReadFile(t, configPath))
	stateBefore := string(testkit.ReadFile(t, statePath))
	testkit.WriteFile(t, project.root, "daem.toml", nativePiManifest("project"))
	testkit.WriteFile(t, project.root, ".pi/settings.json", `{}`)
	runMCPLock(t, project)

	for _, args := range [][]string{{"apply", "--dry-run"}, {"apply", "--yes"}, {"status"}} {
		var output, errorOutput strings.Builder
		exit := testkit.RunCLI(append(args, "--manifest", project.manifestPath), &output, &errorOutput)
		stdout, stderr := output.String(), errorOutput.String()
		if exit == 0 || !strings.Contains(stderr, "context7") || !strings.Contains(stderr, "adapter") || !strings.Contains(stderr, "native") || !strings.Contains(stderr, "retire") {
			t.Fatalf("transition refusal lacks context: %v = %d, %s, %s", args, exit, stdout, stderr)
		}
		testkit.AssertFileContent(t, configPath, before)
		testkit.AssertFileContent(t, statePath, stateBefore)
		if strings.Contains(stderr, project.root) {
			t.Fatalf("public transition context exposed a physical path: %s", stderr)
		}
	}
}

func TestPiNativeManualMigrationRetiresAdapterBeforeAuthoringNative(t *testing.T) {
	project := newManagedPiAdapterForBackendChange(t)
	t.Setenv("PATH", t.TempDir())
	runMCPCLIExpect(t, 0, "retire Adapter declaration", "remove", "mcp-server", "context7", "--manifest", project.manifestPath, "--target", "pi", "--scope", "project")
	runMCPCLIExpect(t, 0, "apply stored Adapter retirement", "apply", "--manifest", project.manifestPath, "--target", "pi", "--yes")
	manifest, err := declarationmanifest.Decode(testkit.ReadFile(t, project.manifestPath))
	if err != nil || len(manifest.MCPServers()) != 0 || len(manifest.Extensions()) != 1 {
		t.Fatalf("MCP retirement did not retain its provider declaration: %v", err)
	}
	var retired struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	configPath := filepath.Join(project.root, ".pi/mcp.json")
	if err := json.Unmarshal(testkit.ReadFile(t, configPath), &retired); err != nil || len(retired.Servers) != 0 {
		t.Fatalf("Adapter config contribution survived retirement: %#v, %v", retired, err)
	}

	runMCPCLIExpect(t, 0, "remove unused provider declaration", "remove", "extension", "adapter", "--manifest", project.manifestPath)
	// Model external provider removal as already absent; this fixture does not execute an uninstaller.
	testkit.WriteFile(t, project.root, ".pi/settings.json", `{}`)
	runMCPCLIExpect(t, 0, "settle absent provider", "apply", "--manifest", project.manifestPath, "--target", "pi", "--yes")
	installNativeVersionFixture(t, project.root, "1.0.2")
	runMCPCLIExpect(t, 0, "author Native after retirement", "add", "mcp-server", "context7", "node", "--arg", "server.js", "--manifest", project.manifestPath, "--target", "pi", "--scope", "project")
	manifest, err = declarationmanifest.Decode(testkit.ReadFile(t, project.manifestPath))
	if err != nil || len(manifest.Extensions()) != 0 || len(manifest.MCPServers()) != 1 || manifest.MCPServers()[0].Bindings()[0].Backend() != desiredmcp.BackendNative {
		t.Fatalf("retired cohort did not permit Native authoring: %v", err)
	}
	runMCPCLIExpect(t, 0, "publish Native", "apply", "--manifest", project.manifestPath, "--target", "pi", "--yes")
	stdout := runMCPCLIExpect(t, 0, "check Native migration", "status", "--manifest", project.manifestPath, "--target", "pi", "--check", "--json")
	report := clijson.DecodePlan(t, []byte(stdout))
	if report.HasErrors || len(report.MCPStatuses) != 1 {
		t.Fatalf("manual migration did not converge: %#v", report)
	}
	if strings.Contains(string(testkit.ReadFile(t, configPath)), `"lifecycle"`) {
		t.Fatal("Native publication retained Adapter lifecycle fields")
	}
}

func TestPiNativeQualifiedHostDoesNotBypassConfigFailure(t *testing.T) {
	project := newMCPCLIProject(t)
	installNativeVersionFixture(t, project.root, "1.0.2")
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(project.root, "agent"))
	testkit.WriteFile(t, project.root, "daem.toml", nativePiManifest("project"))
	testkit.WriteFile(t, project.root, ".pi/mcp.json", `{"private-fixture":`)
	runMCPLock(t, project)
	for _, version := range []string{"1.0.2", "2.0.0"} {
		installNativeVersionFixture(t, project.root, version)
		exit, stdout, stderr := runMCPCLI(t, "status", "--manifest", project.manifestPath, "--json")
		if exit != 0 || stderr != "" || strings.Contains(stdout, "private-fixture") {
			t.Fatalf("combined config/host failure suppressed safe status: %d, %s, %s", exit, stdout, stderr)
		}
		report := clijson.DecodePlan(t, []byte(stdout))
		if !report.HasErrors || len(report.MCPStatuses) != 1 {
			t.Fatalf("config refusal was not retained: %#v", report)
		}
		if exit, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes"); exit == 0 {
			t.Fatalf("host qualification bypassed invalid config: %s, %s", stdout, stderr)
		}
		testkit.AssertFileContent(t, filepath.Join(project.root, ".pi/mcp.json"), `{"private-fixture":`)
		testkit.AssertPathMissing(t, filepath.Join(project.root, ".daem/state.json"))
	}
}

func newManagedPiAdapterForBackendChange(t *testing.T) mcpCLIProject {
	t.Helper()
	project := newMCPCLIProject(t)
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(project.root, "agent"))
	installNativeVersionFixture(t, project.root, "1.0.2")
	testkit.WriteFile(t, project.root, "daem.toml", `version = 1
targets = ["pi"]

[[extension]]
id = "adapter"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "npm:pi-mcp-adapter@^2.13.0" }

[[mcp_server]]
name = "context7"
targets = ["pi"]
scope = "project"
transport = "stdio"
command = "node"
args = ["server.js"]
`)
	placement, _ := aggregate.ImplementedMCPPlacement("pi", "project")
	writeMCPPublicExamplePiProviderState(t, project.root, "", placement, "npm:pi-mcp-adapter@^2.13.0", "2.15.0")
	runMCPLock(t, project)
	exit, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes", "--manage-existing")
	if exit != 0 {
		t.Fatalf("Adapter adoption = %d, %s, %s", exit, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(project.root, ".daem/state.json")); err != nil {
		t.Fatal(err)
	}
	return project
}
