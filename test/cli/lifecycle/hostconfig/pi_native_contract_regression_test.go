package cli_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/test/testkit"
)

func TestPiNativeBuiltinExclusionsRefuseBeforePublication(t *testing.T) {
	for _, test := range []struct{ name, global, project string }{
		{"project bang exact", `{}`, `{"extensions":["!builtin:mcp"]}`},
		{"project bang glob", `{}`, `{"extensions":["!builtin:*"]}`},
		{"global bang glob", `{"extensions":["!builtin:*"]}`, `{}`},
		{"global minus before plus", `{"extensions":["-builtin:mcp","+builtin:mcp"]}`, `{}`},
		{"global minus before plain", `{"extensions":["-builtin:mcp","builtin:mcp"]}`, `{}`},
		{"project plain does not override", `{"extensions":["-builtin:mcp"]}`, `{"extensions":["builtin:mcp"]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			project := newMCPCLIProject(t)
			t.Setenv("SOURCE_TOKEN", "fixture-runtime-secret")
			installNativeVersionFixture(t, project.root, "1.0.2")
			agentRoot := filepath.Join(project.root, "agent")
			t.Setenv("PI_CODING_AGENT_DIR", agentRoot)
			testkit.WriteFile(t, project.root, "daem.toml", nativePiManifest("project"))
			testkit.WriteFile(t, agentRoot, "settings.json", test.global)
			testkit.WriteFile(t, project.root, ".pi/settings.json", test.project)
			runMCPLock(t, project)

			exit, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes")
			if exit == 0 {
				t.Fatalf("disabled builtin published: %s / %s", stdout, stderr)
			}
			testkit.AssertPathMissing(t, filepath.Join(project.root, aggregate.PiProjectMCPConfigPath))
			testkit.AssertPathMissing(t, filepath.Join(project.root, ".daem", "state.json"))
			testkit.AssertFileContent(t, filepath.Join(agentRoot, "settings.json"), test.global)
			testkit.AssertFileContent(t, filepath.Join(project.root, ".pi", "settings.json"), test.project)

			locked, err := lockfile.Load(t.Context(), project.lockfilePath)
			if err != nil {
				t.Fatal(err)
			}
			projection, present, err := locked.Locked.Subjects()[0].ManagedAggregateContribution()
			if err != nil || !present || projection.Contribution().CodecContractID() != aggregate.MCPCodecPiNativeStdio {
				t.Fatal("refusal changed the recorded Native contract")
			}
		})
	}
}

func TestPiNativeBuiltinProjectReenableCanPublish(t *testing.T) {
	project := newMCPCLIProject(t)
	t.Setenv("SOURCE_TOKEN", "fixture-runtime-secret")
	installNativeVersionFixture(t, project.root, "1.0.2")
	agentRoot := filepath.Join(project.root, "agent")
	t.Setenv("PI_CODING_AGENT_DIR", agentRoot)
	global := `{"extensions":["-builtin:mcp"]}`
	local := `{"extensions":["!builtin:*","+builtin:mcp"]}`
	testkit.WriteFile(t, project.root, "daem.toml", nativePiManifest("project"))
	testkit.WriteFile(t, agentRoot, "settings.json", global)
	testkit.WriteFile(t, project.root, ".pi/settings.json", local)
	runMCPLock(t, project)

	exit, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes")
	if exit != 0 {
		t.Fatalf("decisive project re-enable refused: %d, %s / %s", exit, stdout, stderr)
	}
	var config struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(testkit.ReadFile(t, filepath.Join(project.root, aggregate.PiProjectMCPConfigPath)), &config); err != nil || len(config.Servers["context7"]) == 0 {
		t.Fatalf("Native publication = %#v, %v", config, err)
	}
	testkit.AssertFileContent(t, filepath.Join(agentRoot, "settings.json"), global)
	testkit.AssertFileContent(t, filepath.Join(project.root, ".pi", "settings.json"), local)
}

func TestPiNativeGlobalRetirementDistinguishesDependentOverrides(t *testing.T) {
	for _, test := range []struct {
		name, project string
		dependent     bool
	}{
		{"enabled override", `{"mcpServers":{"context7":{"enabled":true}}}`, true},
		{"empty override", `{"mcpServers":{"context7":{}}}`, true},
		{"exposure override", `{"mcpServers":{"context7":{"exposure":"codemode"}}}`, true},
		{"full replacement", `{"mcpServers":{"context7":{"command":"echo"}}}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			project := newMCPCLIProject(t)
			t.Setenv("SOURCE_TOKEN", "fixture-runtime-secret")
			t.Setenv("HOME", filepath.Join(project.root, "home"))
			agentRoot := filepath.Join(project.root, "agent")
			t.Setenv("PI_CODING_AGENT_DIR", agentRoot)
			installNativeVersionFixture(t, project.root, "1.0.2")
			testkit.WriteFile(t, project.root, "daem.toml", nativePiManifest("global"))
			runMCPLock(t, project)
			exit, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes")
			if exit != 0 {
				t.Fatalf("initial apply = %d, %s / %s", exit, stdout, stderr)
			}

			testkit.WriteFile(t, project.root, ".pi/mcp.json", test.project)
			testkit.WriteFile(t, project.root, "daem.toml", "version = 1\ntargets = [\"pi\"]\n")
			t.Setenv("PATH", t.TempDir())
			runMCPLock(t, project)
			exit, stdout, stderr = runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--dry-run")
			if exit != 0 {
				t.Fatalf("retirement preview = %d, %s / %s", exit, stdout, stderr)
			}
			if test.dependent {
				if strings.Contains(stdout, "same-name definition remains effective") || !strings.Contains(stdout, "depends on the removed definition") {
					t.Fatalf("dependent override notice = %s", stdout)
				}
			} else if !strings.Contains(stdout, "higher-precedence same-name definition remains effective") {
				t.Fatalf("full replacement notice = %s", stdout)
			}

			exit, stdout, stderr = runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes")
			if exit != 0 {
				t.Fatalf("retirement = %d, %s / %s", exit, stdout, stderr)
			}
			testkit.AssertFileContent(t, filepath.Join(project.root, ".pi", "mcp.json"), test.project)
			var retired struct {
				Servers map[string]json.RawMessage `json:"mcpServers"`
			}
			if err := json.Unmarshal(testkit.ReadFile(t, filepath.Join(agentRoot, "mcp.json")), &retired); err != nil || len(retired.Servers) != 0 {
				t.Fatalf("managed contribution was not retired: %#v, %v", retired, err)
			}
		})
	}
}
