package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/doctorenv"
)

func TestRunDoctorJSONReportsCodexPluginConfigEntriesObserveOnly(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	testkit.WriteFile(t, homeDir, filepath.Join(".codex", "config.toml"), `
[plugins."alpha@market"]
enabled = true

[plugins."beta@market"]
enabled = false

[marketplaces.private]
source = "https://token@example.invalid/repo.git"
`)
	t.Setenv("HOME", homeDir)
	testkit.SetDefaultRootEnv(t, tempDir)
	testkit.WithWorkingDirectory(t, tempDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--target", "codex", "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	payload := decodeDoctorJSONTestPayload(t, stdout.Bytes())
	assertDoctorJSONCheckDetail(
		t,
		payload,
		"ok",
		`target=codex plugin_config_entry key="alpha@market"`,
		"observe-only Codex plugin config entry from user config; activation configured true; no daem ownership, lock, install, readiness, or mutation authority",
	)
	assertDoctorJSONCheckDetail(
		t,
		payload,
		"ok",
		`target=codex plugin_config_entry key="beta@market"`,
		"observe-only Codex plugin config entry from user config; activation configured false; no daem ownership, lock, install, readiness, or mutation authority",
	)
	for _, forbidden := range []string{`"lock_subject"`, `"state_subject"`, `"installed"`, `"enabled"`, `"ready"`, `"loaded"`, `"converged"`, `"managed"`} {
		if strings.Contains(stdout.String(), forbidden) {
			t.Fatalf("doctor JSON contains forbidden field %s:\n%s", forbidden, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), "token@example.invalid") || strings.Contains(stdout.String(), "marketplaces.private") {
		t.Fatalf("doctor JSON leaked unrelated marketplace/source data:\n%s", stdout.String())
	}
}

func TestRunDoctorTextReportsCodexPluginConfigWithoutInvokingCodex(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	binDir := filepath.Join(tempDir, "bin")
	canaryPath := filepath.Join(tempDir, "codex-ran")
	testkit.WriteFile(t, homeDir, filepath.Join(".codex", "config.toml"), `
[plugins."alpha@market"]
enabled = true
`)
	testkit.WriteFile(t, binDir, "codex", "#!/bin/sh\nprintf ran > "+canaryPath+"\n")
	if err := os.Chmod(filepath.Join(binDir, "codex"), 0o700); err != nil {
		t.Fatalf("Chmod returned error: %v", err)
	}
	t.Setenv("HOME", homeDir)
	t.Setenv("PATH", binDir)
	testkit.SetDefaultRootEnv(t, tempDir)
	testkit.WithWorkingDirectory(t, tempDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--target", "codex"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if _, err := os.Stat(canaryPath); !os.IsNotExist(err) {
		t.Fatalf("codex canary path stat err=%v, want command not invoked", err)
	}
	output := stdout.String()
	if !strings.Contains(output, "ok target=codex plugin_config_entry") ||
		!strings.Contains(output, "observe-only Codex plugin config entry") ||
		!strings.Contains(output, "no daem ownership, lock, install, readiness, or mutation authority") {
		t.Fatalf("stdout = %q, want observe-only plugin config wording", output)
	}
}

func TestRunDoctorJSONReportsCodexPluginSourceDeclaredContributions(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	pluginRoot := filepath.Join(homeDir, ".codex", "plugins", "cache", "market", "alpha", "local")
	testkit.WriteFile(t, homeDir, filepath.Join(".codex", "config.toml"), `
[plugins."alpha@market"]
enabled = true
`)
	testkit.WriteFile(t, pluginRoot, filepath.Join(".codex-plugin", "plugin.json"), `{
  "name": "alpha",
  "skills": "./skills/",
  "mcpServers": {
    "context7": {
      "command": "node",
      "env": {"SECRET_TOKEN": "must-not-leak"}
    }
  },
  "apps": "./.app.json"
}`)
	testkit.WriteFile(t, pluginRoot, filepath.Join("skills", "review", "SKILL.md"), "---\nname: review\n---\n")
	testkit.WriteFile(t, pluginRoot, ".app.json", `{"secret": "must-not-leak"}`)
	t.Setenv("HOME", homeDir)
	testkit.SetDefaultRootEnv(t, tempDir)
	testkit.WithWorkingDirectory(t, tempDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--target", "codex", "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	payload := decodeDoctorJSONTestPayload(t, stdout.Bytes())
	check := findDoctorJSONCheck(t, payload, "ok", `target=codex plugin_contribution provided_by="alpha@market" kind=mcp-server key="context7"`)
	for _, want := range []string{
		"source-declared Codex plugin contribution from source_artifact_inspection",
		`provided_by="alpha@market"`,
		"provenance=source_artifact_inspection",
		"current=non-current",
		"freshness=fresh",
		`artifact="plugins/cache/market/alpha/local"`,
		"kind=mcp-server",
		`key="context7"`,
		`source_marker="mcpServers"`,
		"no current inventory",
		"no daem ownership",
	} {
		if !strings.Contains(check.Detail, want) {
			t.Fatalf("detail = %q, want %q", check.Detail, want)
		}
	}
	findDoctorJSONCheck(t, payload, "ok", `target=codex plugin_contribution provided_by="alpha@market" kind=app key="alpha"`)
	findDoctorJSONCheck(t, payload, "ok", `target=codex plugin_contribution provided_by="alpha@market" kind=skill key="review"`)
	for _, forbidden := range []string{"SECRET_TOKEN", "must-not-leak", `"installed"`, `"enabled"`, `"ready"`, `"loaded"`, `"converged"`, `"managed"`} {
		if strings.Contains(stdout.String(), forbidden) {
			t.Fatalf("doctor JSON leaked forbidden content %q:\n%s", forbidden, stdout.String())
		}
	}
}

func TestCodexPluginConfigDiagnosticsStayOnExplicitCodexDoctorRoute(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	testkit.WriteFile(t, homeDir, filepath.Join(".codex", "config.toml"), `
[plugins."alpha@market"]
enabled = true
`)
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.project]
source = { path = "AGENTS.md", mode = "vendor" }
targets = ["codex"]
`)
	t.Setenv("HOME", homeDir)
	testkit.SetDefaultRootEnv(t, tempDir)
	testkit.WithWorkingDirectory(t, tempDir)
	doctorenv.WithFakeGit(t, "git version test")

	tests := []struct {
		name string
		args []string
	}{
		{name: "implicit manifest doctor", args: []string{"doctor", "--json"}},
		{name: "other target doctor", args: []string{"doctor", "--target", "opencode", "--json"}},
		{name: "status", args: []string{"status", "--manifest", filepath.Join(tempDir, "daem.toml"), "--json"}},
		{name: "apply", args: []string{"apply", "--manifest", filepath.Join(tempDir, "daem.toml"), "--dry-run"}},
		{name: "lock", args: []string{"lock", "--manifest", filepath.Join(tempDir, "daem.toml"), "--dry-run"}},
		{name: "import", args: []string{"import", "--target", "codex", "--manifest", filepath.Join(tempDir, "imported.toml"), "--dry-run"}},
		{name: "probe", args: []string{"probe", "mcp-server", "context7", "--manifest", filepath.Join(tempDir, "daem.toml"), "--target", "codex", "--scope", "project", "--dry-run"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			_ = testkit.RunVerboseCLI(test.args, &stdout, &stderr)
			combined := stdout.String() + stderr.String()
			if strings.Contains(combined, "plugin_config") || strings.Contains(combined, "alpha@market") {
				t.Fatalf("%s output leaked Codex plugin config diagnostics:\nstdout=%s\nstderr=%s", test.name, stdout.String(), stderr.String())
			}
		})
	}
}
