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

func TestRunDoctorExplicitManifestDefaultsToManifestContextTargets(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "skills/review/SKILL.md", "---\nname: review\ndescription: Review code.\n---\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill]]
name = "review"
source = { path = "skills/review", mode = "vendor" }
targets = ["claude-code"]
`)
	t.Setenv("HOME", homeDir)
	testkit.SetDefaultRootEnv(t, tempDir)
	testkit.WithWorkingDirectory(t, tempDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		"ok target=codex capability=skill",
		"ok target=claude-code capability=skill",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
	for _, unwanted := range []string{
		"target=codex capability=instructions",
		"target=claude-code capability=instructions",
		"capability=hook",
		"target=opencode capability=instructions",
		"target=pi capability=instructions",
	} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("stdout = %q, want no %q", output, unwanted)
		}
	}
}

func TestRunDoctorExplicitManifestDefaultsToManifestResourceKinds(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, filepath.Join("skills", "review", "SKILL.md"), "---\nname: review\ndescription: Review code.\n---\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["opencode"]

[[skill]]
name = "review"
source = { path = "skills/review", mode = "vendor" }
targets = ["opencode"]
`)
	t.Setenv("HOME", homeDir)
	testkit.SetDefaultRootEnv(t, tempDir)
	testkit.WithWorkingDirectory(t, tempDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		"ok target=opencode capability=skill",
		"ok target=opencode skill_root=global preferred",
		"ok target=opencode skill=review compatibility",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
	for _, unwanted := range []string{
		"target=opencode capability=instructions",
		"target=opencode capability=hook",
		"target=opencode config_file",
		"target=codex",
		"target=claude-code",
		"target=pi",
	} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("stdout = %q, want no %q", output, unwanted)
		}
	}
}

func TestRunDoctorReportsSkillCompatibilityDiagnostics(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, filepath.Join(".agents", "skills", "shared-review", "SKILL.md"), "---\nname: shared-review\ndescription: Review code.\nallowed-tools: Bash(git status *)\n---\n")
	testkit.WriteFile(t, tempDir, filepath.Join("skills", "claude-minimal", "SKILL.md"), "---\nname: claude-minimal\n---\n")
	testkit.WriteFile(t, tempDir, filepath.Join("skills", "bad-open", "SKILL.md"), "---\nname: other\nallowed-tools: Bash(git status *)\n---\n")
	if err := os.MkdirAll(filepath.Join(tempDir, "skills", "missing-skill"), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex", "claude-code", "opencode"]

[[skill]]
name = "shared-review"
source = { path = ".agents/skills/shared-review", mode = "vendor" }
targets = ["codex", "opencode"]

[[skill]]
name = "claude-minimal"
source = { path = "skills/claude-minimal", mode = "vendor" }
targets = ["claude-code"]

[[skill]]
name = "bad-open"
source = { path = "skills/bad-open", mode = "vendor" }
targets = ["opencode"]

[[skill]]
name = "missing-skill"
source = { path = "skills/missing-skill", mode = "vendor" }
targets = ["codex"]
`)
	t.Setenv("HOME", homeDir)
	testkit.SetDefaultRootEnv(t, tempDir)
	testkit.WithWorkingDirectory(t, tempDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		`ok target=codex skill=shared-review compatibility`,
		`warn target=opencode skill=shared-review compatibility`,
		`field \"allowed-tools\" is not recognized`,
		`warn target=claude-code skill=claude-minimal compatibility`,
		`description is recommended`,
		`error target=opencode skill=bad-open compatibility`,
		`description is required`,
		`name \"other\" must match skill name \"bad-open\"`,
		`error target=codex skill=missing-skill compatibility`,
		`missing SKILL.md`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
}

func TestRunDoctorReportsMissingSkillSourceAsWarning(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill]]
name = "missing"
source = { path = "skills/missing", mode = "vendor" }
targets = ["codex"]
`)
	t.Setenv("HOME", homeDir)
	testkit.SetDefaultRootEnv(t, tempDir)
	testkit.WithWorkingDirectory(t, tempDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `warn target=codex skill=missing compatibility detail="local skill source skills/missing is missing`) {
		t.Fatalf("stdout = %q, want missing-source warning", stdout.String())
	}
}

func TestRunDoctorExplicitManifestTargetCanNameTargetOutsideManifest(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")
	t.Setenv("HOME", homeDir)
	testkit.SetDefaultRootEnv(t, tempDir)
	testkit.WithWorkingDirectory(t, tempDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--manifest", manifestPath, "--target", "pi"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "ok target=pi capability=skill") {
		t.Fatalf("stdout = %q, want pi diagnostics", output)
	}
	if strings.Contains(output, "target=codex capability=instructions") {
		t.Fatalf("stdout = %q, want explicit pi target only", output)
	}
}

func TestRunDoctorAllTargetsOverridesExplicitManifestDefault(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")
	t.Setenv("HOME", homeDir)
	testkit.SetDefaultRootEnv(t, tempDir)
	testkit.WithWorkingDirectory(t, tempDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--manifest", manifestPath, "--all-targets"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	for _, target := range []string{"codex", "claude-code", "opencode", "pi"} {
		token := "target=" + target + " capability=instructions"
		if !strings.Contains(stdout.String(), token) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), token)
		}
	}
}

func TestRunDoctorRejectsAllTargetsWithExplicitTarget(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--target", "codex", "--all-targets"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr.String(), "--all-targets cannot be combined with --target") {
		t.Fatalf("stderr = %q, want all-targets conflict", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunDoctorReportsBlockedSkillRootAsError(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	if err := os.MkdirAll(homeDir, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, ".agents"), []byte("not a directory\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
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
	output := stdout.String()
	if !strings.Contains(output, "error target=codex skill_root=global preferred") {
		t.Fatalf("stdout = %q, want skill root error", output)
	}
	if !strings.Contains(output, ".agents") {
		t.Fatalf("stdout = %q, want blocked .agents path", output)
	}
}

func TestRunDoctorDeduplicatesTargetCapabilityMatrix(t *testing.T) {
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

	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--target", "opencode", "--target", "opencode"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if count := strings.Count(stdout.String(), "target=opencode capability=instructions"); count != 1 {
		t.Fatalf("opencode instruction capability count = %d, stdout = %q", count, stdout.String())
	}
}
