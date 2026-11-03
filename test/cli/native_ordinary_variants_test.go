package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestNativeOrdinarySkillLifecycle(t *testing.T) {
	fixture := nativefixture.NewPresentation(t, "model='native-test'\n", "foreign=true\n")
	workspace := filepath.Dir(fixture.Manifest)
	source := filepath.Join(workspace, "skills", "sample")
	installed := filepath.Join(workspace, ".agents", "skills", "sample")
	native := "[[host_config]]\nid='preferences'\npath='config.toml'\nsource='./preferences.toml'\nownership='keys'\n"
	manifest := "version=1\ntargets=['codex']\n" + native
	testkit.WriteFile(t, workspace, filepath.Base(fixture.Manifest), manifest+"[[skill]]\nname='sample'\nsource={path='skills/sample',mode='vendor'}\n")
	for _, version := range []string{"one", "two"} {
		testkit.WriteFile(t, source, "SKILL.md", "---\nname: sample\ndescription: "+version+"\n---\n")
		testkit.WriteFile(t, source, "references/detail.txt", version+"\n")
		nativefixture.RunLifecycleCommand(t, "lock", "--manifest", fixture.Manifest)
		nativefixture.RunLifecycleCommand(t, "apply", "--manifest", fixture.Manifest, "--yes")
		if testkit.HashDirectory(t, source) != testkit.HashDirectory(t, installed) {
			t.Fatal("mixed Apply did not publish the complete skill tree")
		}
		nativefixture.RunLifecycleCommand(t, "status", "--manifest", fixture.Manifest, "--check", "--json")
	}
	testkit.WriteFile(t, workspace, filepath.Base(fixture.Manifest), manifest)
	nativefixture.RunLifecycleCommand(t, "lock", "--manifest", fixture.Manifest)
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", fixture.Manifest, "--yes")
	if _, err := os.Stat(installed); !os.IsNotExist(err) {
		t.Fatalf("retired skill remains after mixed Apply: %v", err)
	}
	nativefixture.RequireSharedValues(t, configdocument.TOML, testkit.ReadFile(t, fixture.Destination), "model='native-test'\nforeign=true\n", true)
	testkit.AssertNoRecoveryArtifacts(t, workspace)
}

func TestNativeOrdinaryTargetFilteringAndLastRemoval(t *testing.T) {
	fixture := nativefixture.NewPresentation(t, "model='native-test'\n", "foreign=true\n")
	workspace := filepath.Dir(fixture.Manifest)
	write := func(native bool) {
		t.Helper()
		manifest := "version=1\ntargets=['codex','claude-code']\n[instructions.project]\nsource='./instructions.md'\n"
		if native {
			manifest += "[[host_config]]\nid='preferences'\ntargets=['codex']\npath='config.toml'\nsource='./preferences.toml'\nownership='keys'\n"
		}
		testkit.WriteFile(t, workspace, filepath.Base(fixture.Manifest), manifest)
		nativefixture.RunLifecycleCommand(t, "lock", "--manifest", fixture.Manifest)
	}
	testkit.WriteFile(t, workspace, "instructions.md", "before\n")
	write(true)
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", fixture.Manifest, "--yes")
	nativeBefore := testkit.ReadFile(t, fixture.Destination)
	testkit.WriteFile(t, workspace, "instructions.md", "filtered update\n")
	write(true)
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", fixture.Manifest, "--target", "claude-code", "--yes")
	if !bytes.Equal(nativeBefore, testkit.ReadFile(t, fixture.Destination)) {
		t.Fatal("target-filtered ordinary Apply changed the unselected native document")
	}
	if !bytes.Contains(testkit.ReadFile(t, filepath.Join(workspace, "CLAUDE.md")), []byte("filtered update\n")) {
		t.Fatal("zero native groups omitted the selected ordinary update")
	}
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", fixture.Manifest, "--yes")
	testkit.WriteFile(t, workspace, "instructions.md", "last native removed\n")
	write(false)
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", fixture.Manifest, "--yes")
	nativefixture.RequireSharedValues(t, configdocument.TOML, testkit.ReadFile(t, fixture.Destination), "model='native-test'\n", false)
	nativefixture.RequireSharedValues(t, configdocument.TOML, testkit.ReadFile(t, fixture.Destination), "foreign=true\n", true)
	testkit.WriteFile(t, workspace, "instructions.md", "ordinary after native removal\n")
	write(false)
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", fixture.Manifest, "--yes")
	nativefixture.RunLifecycleCommand(t, "status", "--manifest", fixture.Manifest, "--check", "--json")
	testkit.AssertNoRecoveryArtifacts(t, workspace)
}

func TestNativeOrdinaryGlobalFirstApply(t *testing.T) {
	fixture := nativefixture.NewPresentation(t, "model='native-test'\n", "foreign=true\n")
	workspace := filepath.Dir(fixture.Manifest)
	global := filepath.Join(os.Getenv("HOME"), ".codex")
	t.Setenv("CODEX_HOME", global)
	if err := os.RemoveAll(global); err != nil {
		t.Fatal(err)
	}
	testkit.WriteFile(t, workspace, "instructions.md", "global instructions\n")
	manifest := fmt.Sprintf("version=1\ntargets=['codex']\n[instructions.global]\nsource=%q\n[[host_config]]\nid='preferences'\nscope='global'\npath='config.toml'\nsource=%q\nownership='keys'\n", filepath.Join(workspace, "instructions.md"), filepath.Join(workspace, "preferences.toml"))
	testkit.WriteFile(t, workspace, filepath.Base(fixture.Manifest), manifest)
	nativefixture.RunLifecycleCommand(t, "lock", "--manifest", fixture.Manifest)
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", fixture.Manifest, "--yes")
	if !bytes.Contains(testkit.ReadFile(t, filepath.Join(global, "AGENTS.md")), []byte("global instructions\n")) {
		t.Fatal("joint first global Apply omitted instructions")
	}
	nativefixture.RequireSharedValues(t, configdocument.TOML, testkit.ReadFile(t, filepath.Join(global, "config.toml")), "model='native-test'\n", true)
	nativefixture.RunLifecycleCommand(t, "status", "--manifest", fixture.Manifest, "--check", "--json")
	testkit.AssertNoRecoveryArtifacts(t, workspace)
}

func TestNativeOrdinaryIndependentMCPLifecycle(t *testing.T) {
	fixture := nativefixture.NewPresentation(t, "model='native-test'\n", "foreign=true\n")
	workspace := filepath.Dir(fixture.Manifest)
	destination := filepath.Join(workspace, "opencode.json")
	testkit.WriteFile(t, workspace, "opencode.json", `{"foreign":true}`)
	manifest := "version=1\ntargets=['codex','opencode']\n[[host_config]]\nid='preferences'\ntargets=['codex']\npath='config.toml'\nsource='./preferences.toml'\nownership='keys'\n"
	for _, script := range []string{"one.js", "two.js", ""} {
		content := manifest
		if script != "" {
			content += fmt.Sprintf("[[mcp_server]]\nname='search'\ntargets=['opencode']\ntransport='stdio'\ncommand='node'\nargs=[%q]\n", script)
		}
		testkit.WriteFile(t, workspace, filepath.Base(fixture.Manifest), content)
		nativefixture.RunLifecycleCommand(t, "lock", "--manifest", fixture.Manifest)
		nativefixture.RunLifecycleCommand(t, "apply", "--manifest", fixture.Manifest, "--yes")
		var actual struct {
			Foreign bool `json:"foreign"`
			MCP     map[string]struct {
				Command []string `json:"command"`
			} `json:"mcp"`
		}
		if err := json.Unmarshal(testkit.ReadFile(t, destination), &actual); err != nil {
			t.Fatal(err)
		}
		if !actual.Foreign {
			t.Fatal("independent fixed configuration lost its unmanaged value")
		}
		server, present := actual.MCP["search"]
		if present != (script != "") || present && (len(server.Command) != 2 || server.Command[0] != "node" || server.Command[1] != script) {
			t.Fatalf("independent MCP result differs from the selected declaration: %#v", actual)
		}
		nativefixture.RequireSharedValues(t, configdocument.TOML, testkit.ReadFile(t, fixture.Destination), "model='native-test'\nforeign=true\n", true)
		nativefixture.RunLifecycleCommand(t, "status", "--manifest", fixture.Manifest, "--check", "--json")
		testkit.AssertNoRecoveryArtifacts(t, workspace)
	}
}
