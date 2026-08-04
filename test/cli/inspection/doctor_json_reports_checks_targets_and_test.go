package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/doctorenv"
)

func TestRunDoctorJSONReportsChecksTargetsAndManifestMetadata(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	testkit.WriteFile(t, homeDir, filepath.Join(".codex", "config.toml"), "model = \"gpt-5-codex\"\n")
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
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if strings.Contains(stdout.String(), "doctor:") {
		t.Fatalf("stdout = %q, want JSON without human output", stdout.String())
	}

	payload := decodeDoctorJSONTestPayload(t, stdout.Bytes())
	if payload.SchemaVersion != contractversion.DoctorJSON || payload.Command != "doctor" {
		t.Fatalf("payload header = %#v", payload)
	}
	if len(payload.Targets) != 1 || payload.Targets[0] != "codex" {
		t.Fatalf("targets = %#v", payload.Targets)
	}
	if payload.Manifest.Explicit || !strings.HasSuffix(payload.Manifest.Path, filepath.Join("daem", "daem.toml")) {
		t.Fatalf("manifest = %#v", payload.Manifest)
	}
	if payload.CheckCount != len(payload.Checks) || payload.HasErrors {
		t.Fatalf("payload = %#v", payload)
	}
	assertDoctorJSONCheck(t, payload, "warn", "manifest")
	assertDoctorJSONCheck(t, payload, "ok", "git")
	assertDoctorJSONCheck(t, payload, "ok", "target=codex capability=instructions")
	assertDoctorJSONCheckDetail(t, payload, "ok", "target=codex capability=hook", "command hook reconciliation is supported")
	assertDoctorJSONCheck(t, payload, "ok", "target=codex config_file")
	assertDoctorJSONCheck(t, payload, "ok", "target=codex skill_root=global preferred")
}

func TestRunDoctorJSONPreservesErrorExitSemantics(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	testkit.SetDefaultRootEnv(t, tempDir)
	testkit.WithWorkingDirectory(t, tempDir)
	doctorenv.WithoutGit(t)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--target", "codex", "--json"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	payload := decodeDoctorJSONTestPayload(t, stdout.Bytes())
	if !payload.HasErrors {
		t.Fatalf("payload = %#v, want errors", payload)
	}
	assertDoctorJSONCheck(t, payload, "error", "git")
}

func TestRunDoctorJSONReportsUnsupportedCapabilitiesAsWarnings(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	testkit.SetDefaultRootEnv(t, tempDir)
	testkit.WithWorkingDirectory(t, tempDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--target", "opencode", "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}

	payload := decodeDoctorJSONTestPayload(t, stdout.Bytes())
	if payload.HasErrors {
		t.Fatalf("payload = %#v, want unsupported capabilities as warnings only", payload)
	}
	assertDoctorJSONCheck(t, payload, "ok", "target=opencode capability=instructions")
	assertDoctorJSONCheck(t, payload, "ok", "target=opencode capability=skill")
	assertDoctorJSONCheckDetail(t, payload, "warn", "target=opencode capability=hook", "command hook reconciliation requires an extension bridge surface")
}

func TestRunDoctorJSONReportsInstructionCapabilityDetail(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	testkit.SetDefaultRootEnv(t, tempDir)
	testkit.WithWorkingDirectory(t, tempDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--target", "antigravity-cli", "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}

	payload := decodeDoctorJSONTestPayload(t, stdout.Bytes())
	if payload.HasErrors {
		t.Fatalf("payload = %#v, want no errors", payload)
	}
	assertDoctorJSONCheckDetail(t, payload, "ok", "target=antigravity-cli capability=instructions", "instruction rendering is supported")
}

func TestRunDoctorJSONAcceptsRepeatedTargetsAndCollapsesDuplicates(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	testkit.SetDefaultRootEnv(t, tempDir)
	testkit.WithWorkingDirectory(t, tempDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--target", "pi", "--target", "codex", "--target", "codex", "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	payload := decodeDoctorJSONTestPayload(t, stdout.Bytes())
	wantTargets := []string{"codex", "pi"}
	if len(payload.Targets) != len(wantTargets) {
		t.Fatalf("targets = %#v, want %#v", payload.Targets, wantTargets)
	}
	for index := range wantTargets {
		if payload.Targets[index] != wantTargets[index] {
			t.Fatalf("targets = %#v, want %#v", payload.Targets, wantTargets)
		}
	}
}

func TestRunDoctorJSONExplicitManifestDefaultsToManifestContextTargets(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.project]
source = { path = "instructions/project.md", mode = "vendor" }
targets = ["claude-code"]
`)
	t.Setenv("HOME", homeDir)
	testkit.SetDefaultRootEnv(t, tempDir)
	testkit.WithWorkingDirectory(t, tempDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--manifest", manifestPath, "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	payload := decodeDoctorJSONTestPayload(t, stdout.Bytes())
	wantTargets := []string{"codex", "claude-code"}
	if len(payload.Targets) != len(wantTargets) {
		t.Fatalf("targets = %#v, want %#v", payload.Targets, wantTargets)
	}
	for index := range wantTargets {
		if payload.Targets[index] != wantTargets[index] {
			t.Fatalf("targets = %#v, want %#v", payload.Targets, wantTargets)
		}
	}
	for _, check := range payload.Checks {
		if strings.Contains(check.Name, "capability=hook") || strings.Contains(check.Name, "capability=skill") {
			t.Fatalf("checks = %#v, want manifest instruction capability scope only", payload.Checks)
		}
	}
}

func TestRunDoctorJSONImplicitCWDManifestDefaultsToManifestContextTargets(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	testkit.WriteFile(t, tempDir, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: Oracle review.\n---\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["claude-code"]
`)
	t.Setenv("HOME", homeDir)
	testkit.SetDefaultRootEnv(t, tempDir)
	testkit.WithWorkingDirectory(t, tempDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	payload := decodeDoctorJSONTestPayload(t, stdout.Bytes())
	if payload.Manifest.Explicit {
		t.Fatalf("manifest = %#v", payload.Manifest)
	}
	assertDoctorJSONManifestPath(t, payload.Manifest.Path, filepath.Join(tempDir, "daem.toml"))
	wantTargets := []string{"codex", "claude-code"}
	if len(payload.Targets) != len(wantTargets) {
		t.Fatalf("targets = %#v, want %#v", payload.Targets, wantTargets)
	}
	for index := range wantTargets {
		if payload.Targets[index] != wantTargets[index] {
			t.Fatalf("targets = %#v, want %#v", payload.Targets, wantTargets)
		}
	}
	assertDoctorJSONCheck(t, payload, "ok", "target=codex skill_root=project preferred")
	assertDoctorJSONCheck(t, payload, "ok", "target=claude-code skill_root=project preferred")
	for _, check := range payload.Checks {
		if strings.Contains(check.Name, "target=opencode") || strings.Contains(check.Name, "target=pi") {
			t.Fatalf("checks = %#v, want only manifest context targets", payload.Checks)
		}
		if strings.Contains(check.Name, "capability=hook") || strings.Contains(check.Name, "capability=instructions") {
			t.Fatalf("checks = %#v, want manifest skill capability scope only", payload.Checks)
		}
	}
}

func TestRunDoctorJSONImplicitUserDefaultManifestDefaultsToManifestContextWithoutProjectRoots(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	workDir := filepath.Join(tempDir, "work")
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	testkit.SetDefaultRootEnv(t, tempDir)
	testkit.WriteFile(t, filepath.Join(tempDir, "config", "daem"), "daem.toml", `
version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { git = "https://example.invalid/oracle.git", path = "skills/oracle", ref = "main" }
targets = ["claude-code"]
scope = "global"
`)
	t.Setenv("HOME", homeDir)
	testkit.WithWorkingDirectory(t, workDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	payload := decodeDoctorJSONTestPayload(t, stdout.Bytes())
	if payload.Manifest.Explicit {
		t.Fatalf("manifest = %#v", payload.Manifest)
	}
	assertDoctorJSONManifestPath(t, payload.Manifest.Path, filepath.Join(tempDir, "config", "daem", "daem.toml"))
	wantTargets := []string{"codex", "claude-code"}
	if len(payload.Targets) != len(wantTargets) {
		t.Fatalf("targets = %#v, want %#v", payload.Targets, wantTargets)
	}
	for index := range wantTargets {
		if payload.Targets[index] != wantTargets[index] {
			t.Fatalf("targets = %#v, want %#v", payload.Targets, wantTargets)
		}
	}
	assertDoctorJSONCheck(t, payload, "ok", "target=codex skill_root=global preferred")
	assertDoctorJSONCheck(t, payload, "ok", "target=claude-code skill_root=global preferred")
	for _, check := range payload.Checks {
		if strings.Contains(check.Name, "skill_root=project") {
			t.Fatalf("checks = %#v, want no project skill roots for OS user config manifest", payload.Checks)
		}
		if strings.Contains(check.Name, "target=opencode") || strings.Contains(check.Name, "target=pi") {
			t.Fatalf("checks = %#v, want only manifest context targets", payload.Checks)
		}
		if strings.Contains(check.Name, "capability=hook") || strings.Contains(check.Name, "capability=instructions") {
			t.Fatalf("checks = %#v, want manifest skill capability scope only", payload.Checks)
		}
	}
}

func TestRunDoctorJSONImplicitCWDManifestMCPOnlyKeepsManifestContextWithoutInventedResourceRows(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
env = { SERVER_SECRET = { from_env = "CONTEXT7_API_TOKEN" } }
`)
	t.Setenv("HOME", homeDir)
	testkit.SetDefaultRootEnv(t, tempDir)
	testkit.WithWorkingDirectory(t, tempDir)
	doctorBin := doctorenv.WithFakeGit(t, "git version test")
	t.Setenv("PATH", doctorBin)
	doctorenv.WithoutEnvironmentVariable(t, "CONTEXT7_API_TOKEN")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	payload := decodeDoctorJSONTestPayload(t, stdout.Bytes())
	if payload.Manifest.Explicit {
		t.Fatalf("manifest = %#v", payload.Manifest)
	}
	assertDoctorJSONManifestPath(t, payload.Manifest.Path, filepath.Join(tempDir, "daem.toml"))
	wantTargets := []string{"claude-code"}
	if len(payload.Targets) != len(wantTargets) {
		t.Fatalf("targets = %#v, want %#v", payload.Targets, wantTargets)
	}
	for index := range wantTargets {
		if payload.Targets[index] != wantTargets[index] {
			t.Fatalf("targets = %#v, want %#v", payload.Targets, wantTargets)
		}
	}
	for _, check := range payload.Checks {
		if strings.Contains(check.Name, "target=codex") ||
			strings.Contains(check.Name, "target=opencode") ||
			strings.Contains(check.Name, "target=pi") {
			t.Fatalf("checks = %#v, want only manifest context target", payload.Checks)
		}
		if strings.Contains(check.Name, "capability=") || strings.Contains(check.Name, "skill_root") {
			t.Fatalf("checks = %#v, want no invented resource-kind rows for MCP-only manifest", payload.Checks)
		}
	}
	runnerCheck := findDoctorJSONCheck(t, payload, "warn", "target=claude-code scope=project mcp_server=context7 executable_requirement=command")
	if !strings.Contains(runnerCheck.Detail, `command "npx"`) {
		t.Fatalf("runner detail = %q, want npx command detail", runnerCheck.Detail)
	}
	envCheck := findDoctorJSONCheck(t, payload, "warn", "target=claude-code scope=project mcp_server=context7 executable_requirement=env_refs")
	if !strings.Contains(envCheck.Detail, "CONTEXT7_API_TOKEN") {
		t.Fatalf("env detail = %q, want missing host env name", envCheck.Detail)
	}
	if strings.Contains(envCheck.Detail, "SERVER_SECRET") {
		t.Fatalf("env detail = %q, want host env ref name rather than projection key", envCheck.Detail)
	}
}

func TestRunDoctorJSONReportsExplicitManifestPath(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", "version = \"wrong\"\n")
	t.Setenv("HOME", homeDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--manifest", manifestPath, "--target", "codex", "--json"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}

	payload := decodeDoctorJSONTestPayload(t, stdout.Bytes())
	if !payload.Manifest.Explicit || payload.Manifest.Path != manifestPath {
		t.Fatalf("manifest = %#v", payload.Manifest)
	}
	assertDoctorJSONCheck(t, payload, "error", "manifest")
}

func TestRunDoctorJSONReportsSkillGroupRepairabilityFields(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "skills/oracle/skill.md", "---\ndescription: Use for oracle review.\n---\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["opencode"]

[[skill_group]]
source = { path = "skills", mode = "vendor" }
include = ["glob:oracle"]
compat_repair = false
`)
	t.Setenv("HOME", homeDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--manifest", manifestPath, "--target", "opencode", "--json"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}

	payload := decodeDoctorJSONTestPayload(t, stdout.Bytes())
	check := findDoctorJSONCheck(t, payload, "error", "target=opencode skill=oracle compatibility")
	if check.Repairability != "mechanical" {
		t.Fatalf("check = %#v, want mechanical repairability", check)
	}
	if !strings.Contains(check.NextStep, "compat_repair = true") {
		t.Fatalf("check = %#v, want compat_repair next step", check)
	}
	if len(check.RepairActions) == 0 || !strings.Contains(strings.Join(check.RepairActions, "; "), "rename file: skill.md -> SKILL.md") {
		t.Fatalf("check = %#v, want rename repair action", check)
	}
}

func TestRunDoctorJSONReportsDeclaredSkillGroupRepairPolicyAsWarning(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "skills/oracle/skill.md", "---\ndescription: Use for oracle review.\n---\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["opencode"]

[[skill_group]]
source = { path = "skills", mode = "vendor" }
include = ["glob:oracle"]
compat_repair = true
`)
	t.Setenv("HOME", homeDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--manifest", manifestPath, "--target", "opencode", "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0; stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}

	payload := decodeDoctorJSONTestPayload(t, stdout.Bytes())
	if payload.HasErrors {
		t.Fatalf("payload = %#v, want declared repair policy warning without errors", payload)
	}
	check := findDoctorJSONCheck(t, payload, "warn", "target=opencode skill=oracle compatibility")
	if check.Repairability != "mechanical" {
		t.Fatalf("check = %#v, want mechanical repairability", check)
	}
	if !strings.Contains(check.NextStep, "daem lock") || strings.Contains(check.NextStep, "compat_repair = true") {
		t.Fatalf("check = %#v, want lock next step without compat_repair declaration guidance", check)
	}
	if len(check.RepairActions) == 0 || !strings.Contains(strings.Join(check.RepairActions, "; "), "rename file: skill.md -> SKILL.md") {
		t.Fatalf("check = %#v, want rename repair action", check)
	}
}
