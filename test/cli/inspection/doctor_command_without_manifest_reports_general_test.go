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

func TestRunDoctorWithoutManifestReportsGeneralDiagnostics(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	if err := os.MkdirAll(homeDir, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	t.Setenv("HOME", homeDir)
	testkit.SetDefaultRootEnv(t, tempDir)
	testkit.WithWorkingDirectory(t, tempDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--target", "codex"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	output := stdout.String()
	for _, want := range []string{
		"doctor:",
		"warn manifest",
		"ok git",
		"ok cache",
		"ok symlink",
		"ok target=codex config_dir",
		"warn target=codex config_file",
		"ok target=codex skill_root=global preferred",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
	if _, err := os.Stat(filepath.Join(tempDir, ".daem")); !os.IsNotExist(err) {
		t.Fatalf(".daem exists or stat failed unexpectedly: %v", err)
	}
	if _, err := os.Stat(filepath.Join(homeDir, ".codex")); !os.IsNotExist(err) {
		t.Fatalf(".codex exists or stat failed unexpectedly: %v", err)
	}
	if _, err := os.Stat(filepath.Join(homeDir, ".agents")); !os.IsNotExist(err) {
		t.Fatalf(".agents exists or stat failed unexpectedly: %v", err)
	}
}

func TestRunDoctorParsesSelectedTargetConfigs(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	testkit.WriteFile(t, homeDir, filepath.Join(".codex", "config.toml"), "model = \"gpt-5-codex\"\n")
	testkit.WriteFile(t, homeDir, filepath.Join(".claude", "settings.json"), `{"env":{"API_TIMEOUT_MS":"120000"}}`)
	t.Setenv("HOME", homeDir)
	testkit.SetDefaultRootEnv(t, tempDir)
	testkit.WithWorkingDirectory(t, tempDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--target", "codex", "--target", "claude-code"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}

	output := stdout.String()
	for _, want := range []string{
		"ok target=codex config_file",
		"ok target=claude-code config_file",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
}

func TestRunDoctorReportsInvalidTargetConfigAsError(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	testkit.WriteFile(t, homeDir, filepath.Join(".codex", "config.toml"), "=\n")
	t.Setenv("HOME", homeDir)
	testkit.SetDefaultRootEnv(t, tempDir)
	testkit.WithWorkingDirectory(t, tempDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--target", "codex"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "error target=codex config_file") {
		t.Fatalf("stdout = %q, want config file error", stdout.String())
	}
}

func TestRunDoctorReportsUnavailableGitAsError(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	if err := os.MkdirAll(homeDir, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	t.Setenv("HOME", homeDir)
	testkit.SetDefaultRootEnv(t, tempDir)
	testkit.WithWorkingDirectory(t, tempDir)
	doctorenv.WithoutGit(t)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--target", "codex"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "error git") {
		t.Fatalf("stdout = %q, want git error", stdout.String())
	}
}

func TestRunDoctorReportsBlockedCachePathAsError(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	cacheHome := filepath.Join(tempDir, "blocked-cache")
	if err := os.MkdirAll(homeDir, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(cacheHome, []byte("not a directory\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempDir, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(tempDir, "state"))
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	t.Setenv("APPDATA", filepath.Join(tempDir, "appdata", "roaming"))
	t.Setenv("LOCALAPPDATA", cacheHome)
	testkit.WithWorkingDirectory(t, tempDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--target", "codex"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "error cache") {
		t.Fatalf("stdout = %q, want cache error", stdout.String())
	}
}

func TestRunDoctorReportsOpenCodeJSONCParseFailureAsWarning(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	testkit.WriteFile(t, homeDir, filepath.Join(".config", "opencode", "opencode.json"), "{\n  // JSONC comment\n}\n")
	t.Setenv("HOME", homeDir)
	testkit.SetDefaultRootEnv(t, tempDir)
	testkit.WithWorkingDirectory(t, tempDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--target", "opencode"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "warn target=opencode config_file") {
		t.Fatalf("stdout = %q, want opencode config warning", stdout.String())
	}
}

func TestRunDoctorReportsTargetCapabilityMatrix(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	if err := os.MkdirAll(homeDir, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	t.Setenv("HOME", homeDir)
	testkit.SetDefaultRootEnv(t, tempDir)
	testkit.WithWorkingDirectory(t, tempDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--target", "pi"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	for _, want := range []string{
		`ok target=pi capability=instructions detail="instruction rendering is supported"`,
		`ok target=pi capability=skill detail="skill reconciliation is supported"`,
		`warn target=pi capability=hook detail="command hook reconciliation requires an extension bridge surface"`,
		"ok target=pi config_dir",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
}

func TestRunDoctorReportsInstructionCapabilityDetail(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	if err := os.MkdirAll(homeDir, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	t.Setenv("HOME", homeDir)
	testkit.SetDefaultRootEnv(t, tempDir)
	testkit.WithWorkingDirectory(t, tempDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--target", "antigravity-cli"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	want := `ok target=antigravity-cli capability=instructions detail="instruction rendering is supported"`
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunDoctorReportsSkillRootsFromCapabilityRegistry(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	if err := os.MkdirAll(homeDir, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	t.Setenv("HOME", homeDir)
	testkit.SetDefaultRootEnv(t, tempDir)
	testkit.WithWorkingDirectory(t, tempDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--target", "opencode"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		"ok target=opencode skill_root=global preferred",
		filepath.Join(homeDir, ".config", "opencode", "skills") + " can be created",
		"ok target=opencode skill_root=global compatible[0]",
		filepath.Join(homeDir, ".claude", "skills") + " can be created",
		"ok target=opencode skill_root=global compatible[1]",
		filepath.Join(homeDir, ".agents", "skills") + " can be created",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
	if strings.Contains(output, "skill_root=project") {
		t.Fatalf("stdout = %q, want no project skill roots without selected project manifest", output)
	}
	if strings.Contains(output, "target=pi skill_root") {
		t.Fatalf("stdout = %q, want only opencode skill roots", output)
	}
}

func TestRunDoctorReportsExplicitManifestProjectSkillRoot(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: Oracle review.\n---\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]
`)
	t.Setenv("HOME", homeDir)
	testkit.SetDefaultRootEnv(t, tempDir)
	testkit.WithWorkingDirectory(t, tempDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--manifest", manifestPath, "--target", "codex"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		"ok target=codex skill_root=project preferred",
		filepath.Join(tempDir, ".agents", "skills") + " can be created",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
}

func TestRunDoctorReportsImplicitCWDManifestProjectSkillRoot(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	testkit.WriteFile(t, tempDir, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: Oracle review.\n---\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]
`)
	t.Setenv("HOME", homeDir)
	testkit.SetDefaultRootEnv(t, tempDir)
	testkit.WithWorkingDirectory(t, tempDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--target", "codex"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		"ok target=codex skill_root=project preferred",
		filepath.Join(tempDir, ".agents", "skills") + " can be created",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
}
