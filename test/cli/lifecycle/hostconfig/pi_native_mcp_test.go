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
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/test/testkit"
)

func TestPiNativeMCPAuthoringRecordsOnceAndRegeneratesWithoutPi(t *testing.T) {
	project := newMCPCLIProject(t)
	log := installNativeVersionFixture(t, project.root, "1.0.2")
	testkit.WriteFile(t, project.root, "daem.toml", "version = 1\ntargets = [\"pi\"]\n")
	exit, stdout, stderr := runMCPCLI(t, "add", "mcp-server", "context7", "node", "--manifest", project.manifestPath, "--target", "pi", "--scope", "project", "--arg", "server.js")
	if exit != 0 {
		t.Fatalf("add = %d, %s, %s", exit, stdout, stderr)
	}
	calls := string(testkit.ReadFile(t, log))
	if calls != "--version\n" {
		t.Fatalf("version observation was not invocation-local: %q", calls)
	}
	manifest, err := declarationmanifest.Decode(testkit.ReadFile(t, project.manifestPath))
	if err != nil || len(manifest.Extensions()) != 0 || len(manifest.MCPServers()) != 1 || manifest.MCPServers()[0].Bindings()[0].Backend() != desiredmcp.BackendNative {
		t.Fatalf("native manifest = %#v, %v", manifest, err)
	}
	before := testkit.ReadFile(t, project.lockfilePath)
	locked, err := lockfile.Load(t.Context(), project.lockfilePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(locked.Locked.Subjects()) != 1 {
		t.Fatalf("native has fictional package subjects: %#v", locked.Locked.Subjects())
	}
	projection, present, err := locked.Locked.Subjects()[0].ManagedAggregateContribution()
	if err != nil || !present || projection.Contribution().CodecContractID() != aggregate.MCPCodecPiNativeStdio {
		t.Fatalf("recorded native projection = %#v, %v", projection, err)
	}
	if _, provider := locked.Locked.Subjects()[0].MCPProviderContribution(); provider {
		t.Fatal("native has an adapter dependency")
	}
	testkit.AssertPathMissing(t, filepath.Join(project.root, aggregate.PiProjectMCPConfigPath))
	t.Setenv("PATH", t.TempDir())
	runMCPLock(t, project)
	if string(before) != string(testkit.ReadFile(t, project.lockfilePath)) {
		t.Fatal("lock regeneration reselected backend")
	}
}

func TestPiNativeMCPApplyConvergesAndRemovesWithMissingPi(t *testing.T) {
	project := newMCPCLIProject(t)
	log := installNativeVersionFixture(t, project.root, "1.0.2")
	t.Setenv("SOURCE_TOKEN", "fixture-runtime-secret")
	testkit.WriteFile(t, project.root, "daem.toml", nativePiManifest("project"))
	testkit.WriteFile(t, project.root, aggregate.PiProjectMCPConfigPath, `{"unmanaged":true,"mcpServers":{"manual":{"command":"echo","env":{"TOKEN":"host-only"}}}}`)
	runMCPLock(t, project)
	exit, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes")
	if exit != 0 {
		t.Fatalf("native apply = %d, %s, %s", exit, stdout, stderr)
	}
	configPath := filepath.Join(project.root, aggregate.PiProjectMCPConfigPath)
	var config struct {
		Unmanaged bool `json:"unmanaged"`
		Servers   map[string]struct {
			Command  string            `json:"command"`
			Args     []string          `json:"args"`
			Env      map[string]string `json:"env"`
			Enabled  bool              `json:"enabled"`
			Exposure string            `json:"exposure"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(testkit.ReadFile(t, configPath), &config); err != nil {
		t.Fatal(err)
	}
	managed := config.Servers["context7"]
	if !config.Unmanaged || len(config.Servers) != 2 || managed.Command != "node" || !managed.Enabled || managed.Exposure != "codemode" || managed.Env["TOKEN"] != "${SOURCE_TOKEN}" {
		t.Fatalf("native config = %#v", config)
	}
	calls := strings.Fields(string(testkit.ReadFile(t, log)))
	if len(calls) == 0 {
		t.Fatal("native apply skipped fixed-contract qualification")
	}
	for _, call := range calls {
		if call != "--version" {
			t.Fatalf("unexpected package/server operation: %q", calls)
		}
	}

	testkit.WriteFile(t, project.root, "daem.toml", "version = 1\ntargets = [\"pi\"]\n")
	t.Setenv("PATH", t.TempDir())
	runMCPLock(t, project)
	exit, stdout, stderr = runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes")
	if exit != 0 {
		t.Fatalf("stored-native removal with missing Pi = %d, %s, %s", exit, stdout, stderr)
	}
	config.Servers = nil
	if err := json.Unmarshal(testkit.ReadFile(t, configPath), &config); err != nil {
		t.Fatal(err)
	}
	if !config.Unmanaged || len(config.Servers) != 1 || config.Servers["manual"].Env["TOKEN"] != "host-only" {
		t.Fatalf("removal changed unmanaged config = %#v", config)
	}
}

func TestPiNativeMCPQualificationRefusesBeforePublication(t *testing.T) {
	for _, test := range []struct{ name, version, settings string }{
		{"old host", "1.0.1", "{}"},
		{"disabled builtin", "1.0.2", `{"extensions":["-builtin:mcp"]}`},
		{"adapter replacement", "1.0.2", `{"packages":["npm:pi-mcp-adapter@2.15.0"]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			project := newMCPCLIProject(t)
			installNativeVersionFixture(t, project.root, test.version)
			testkit.WriteFile(t, project.root, "daem.toml", nativePiManifest("project"))
			testkit.WriteFile(t, project.root, ".pi/settings.json", test.settings)
			runMCPLock(t, project)
			exit, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes")
			if exit == 0 {
				t.Fatalf("unqualified native publication succeeded: %s/%s", stdout, stderr)
			}
			testkit.AssertPathMissing(t, filepath.Join(project.root, aggregate.PiProjectMCPConfigPath))
			testkit.AssertFileContent(t, filepath.Join(project.root, ".pi/settings.json"), test.settings)
			locked, err := lockfile.Load(t.Context(), project.lockfilePath)
			if err != nil {
				t.Fatal(err)
			}
			projection, present, err := locked.Locked.Subjects()[0].ManagedAggregateContribution()
			if err != nil || !present || projection.Contribution().CodecContractID() != aggregate.MCPCodecPiNativeStdio {
				t.Fatal("qualification failure changed the recorded backend")
			}
		})
	}
}

func nativePiManifest(scope string) string {
	return "version = 1\ntargets = [\"pi\"]\n\n[[mcp_server]]\nname = \"context7\"\ntargets = [\"pi\"]\nscope = \"" + scope + "\"\nbackend = \"native\"\ntransport = \"stdio\"\ncommand = \"node\"\nargs = [\"server.js\"]\nenv = { TOKEN = { from_env = \"SOURCE_TOKEN\" } }\n"
}

func installNativeVersionFixture(t *testing.T, root string, version string) string {
	t.Helper()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(root, "pi-calls.txt")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$DAEM_PI_TEST_LOG\"\n[ \"$1\" = --version ] || exit 97\nprintf '%s\\n' '" + version + "'\n"
	if err := os.WriteFile(filepath.Join(bin, "pi"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("DAEM_PI_TEST_LOG", log)
	return log
}

func TestPiNativeMCPGlobalRemovalWithMissingPi(t *testing.T) {
	project := newMCPCLIProject(t)
	t.Setenv("HOME", filepath.Join(project.root, "home"))
	t.Setenv("PI_CODING_AGENT_DIR", "")
	installNativeVersionFixture(t, project.root, "1.0.2")
	t.Setenv("SOURCE_TOKEN", "fixture-runtime-secret")
	testkit.WriteFile(t, project.root, "daem.toml", nativePiManifest("global"))
	runMCPLock(t, project)
	exit, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes")
	if exit != 0 {
		t.Fatalf("global native apply = %d, %s, %s", exit, stdout, stderr)
	}
	globalPath := filepath.Join(project.root, "home", ".pi", "agent", "mcp.json")
	if _, err := os.Stat(globalPath); err != nil {
		t.Fatal(err)
	}
	testkit.WriteFile(t, project.root, "daem.toml", "version = 1\ntargets = [\"pi\"]\n")
	t.Setenv("PATH", t.TempDir())
	runMCPLock(t, project)
	exit, stdout, stderr = runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes")
	if exit != 0 {
		t.Fatalf("stored global retirement = %d, %s, %s", exit, stdout, stderr)
	}
	var retired struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(testkit.ReadFile(t, globalPath), &retired); err != nil || len(retired.Servers) != 0 {
		t.Fatalf("global contribution was not removed: %#v, %v", retired, err)
	}
}
