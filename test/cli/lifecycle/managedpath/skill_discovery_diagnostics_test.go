package cli_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/doctorenv"
)

const retainedSkillDiscoveryCode = "skill_discovery_duplicate_retained"

func TestRetainedSkillDiscoveryIsDiagnosedWithoutMutation(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	manifestPath := filepath.Join(root, "daem.toml")
	selectedPath := filepath.Join(root, ".opencode", "skills", "review")
	retainedPath := filepath.Join(root, ".agents", "skills", "review")
	t.Setenv("HOME", home)
	testkit.SetDefaultRootEnv(t, root)
	doctorenv.WithFakeGit(t, "git version test")

	testkit.WriteFile(t, root, "skills/review/SKILL.md", "---\nname: review\ndescription: review changes\n---\n")
	testkit.WriteFile(t, root, ".agents/skills/review/SKILL.md", "---\nname: review\ndescription: retained copy\n---\n")
	testkit.WriteFile(t, root, "daem.toml", `
version = 1
targets = ["opencode"]

[[skill]]
name = "review"
source = { path = "skills/review", mode = "vendor" }
targets = ["opencode"]
`)
	runSkillDiscoveryCLI(t, 0, "lock", "--manifest", manifestPath)
	retainedBefore, err := os.ReadFile(filepath.Join(retainedPath, "SKILL.md"))
	if err != nil {
		t.Fatalf("read retained skill: %v", err)
	}

	doctorOutput := runSkillDiscoveryCLI(
		t,
		0,
		"doctor",
		"--manifest",
		manifestPath,
		"--target",
		"opencode",
	)
	for _, want := range []string{
		retainedSkillDiscoveryCode,
		"target=opencode",
		"scope=project",
		"skill=review",
		"will be retained",
	} {
		if !strings.Contains(doctorOutput, want) {
			t.Fatalf("doctor output = %q, want %q", doctorOutput, want)
		}
	}

	for _, command := range []string{"status", "apply"} {
		args := []string{command, "--manifest", manifestPath, "--target", "opencode", "--json"}
		if command == "apply" {
			args = append(args, "--dry-run")
		}
		output := runSkillDiscoveryCLI(t, 0, args...)
		payload := clijson.DecodePlan(t, []byte(output))
		diagnostic := clijson.FindPlanDiagnostic(
			t,
			payload,
			retainedSkillDiscoveryCode,
			"warn",
			"skill/review",
			"opencode",
		)
		if diagnostic.Scope != "project" ||
			!strings.Contains(diagnostic.Detail, selectedPath) ||
			!strings.Contains(diagnostic.Detail, retainedPath) ||
			!strings.Contains(diagnostic.Detail, "will be retained") ||
			!strings.Contains(diagnostic.NextStep, "manually") {
			t.Fatalf("%s diagnostic = %#v, want full bounded remediation context", command, diagnostic)
		}
	}

	if _, err := os.Stat(selectedPath); !os.IsNotExist(err) {
		t.Fatalf("diagnostic commands created selected path %q or stat failed: %v", selectedPath, err)
	}
	retainedAfter, err := os.ReadFile(filepath.Join(retainedPath, "SKILL.md"))
	if err != nil {
		t.Fatalf("read retained skill after diagnostics: %v", err)
	}
	if !bytes.Equal(retainedAfter, retainedBefore) {
		t.Fatal("diagnostic commands modified the retained discovery entry")
	}
}

func TestManagedSkillRelocationIsNotMisclassifiedAsRetainedDuplicate(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	oldPath := filepath.Join(root, ".opencode", "skills", "review")
	newPath := filepath.Join(root, ".agents", "skills", "review")
	testkit.SetDefaultRootEnv(t, root)
	doctorenv.WithFakeGit(t, "git version test")
	testkit.WriteFile(t, root, "skills/review/SKILL.md", "---\nname: review\ndescription: review changes\n---\n")
	writeOpenCodeDiscoveryManifest(t, root, "")
	runSkillDiscoveryCLI(t, 0, "lock", "--manifest", manifestPath)
	runSkillDiscoveryCLI(t, 0, "apply", "--manifest", manifestPath, "--yes")
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("initial managed skill is missing: %v", err)
	}

	writeOpenCodeDiscoveryManifest(t, root, ".agents/skills")
	runSkillDiscoveryCLI(t, 0, "lock", "--manifest", manifestPath)

	doctorOutput := runSkillDiscoveryCLI(
		t,
		0,
		"doctor",
		"--manifest",
		manifestPath,
		"--target",
		"opencode",
	)
	if strings.Contains(doctorOutput, retainedSkillDiscoveryCode) {
		t.Fatalf("doctor misclassified managed relocation source: %q", doctorOutput)
	}

	applyOutput := runSkillDiscoveryCLI(
		t,
		0,
		"apply",
		"--manifest",
		manifestPath,
		"--target",
		"opencode",
		"--dry-run",
		"--json",
	)
	payload := clijson.DecodePlan(t, []byte(applyOutput))
	for _, diagnostic := range payload.Diagnostics {
		if diagnostic.Code == retainedSkillDiscoveryCode {
			t.Fatalf("apply dry-run misclassified planned relocation source: %#v", diagnostic)
		}
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("dry-run removed relocation source: %v", err)
	}
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run created relocation destination or stat failed: %v", err)
	}
}

func writeOpenCodeDiscoveryManifest(t *testing.T, root string, installTo string) {
	t.Helper()
	content := `
version = 1
targets = ["opencode"]

[[skill]]
name = "review"
source = { path = "skills/review", mode = "vendor" }
targets = ["opencode"]
	`
	if installTo != "" {
		content += fmt.Sprintf("\n[skill.target.opencode]\ninstall_to = %q\n", installTo)
	}
	testkit.WriteFile(t, root, "daem.toml", content)
}

func runSkillDiscoveryCLI(t *testing.T, expectedExit int, args ...string) string {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr)
	if exitCode != expectedExit || stderr.Len() != 0 {
		t.Fatalf(
			"%v exit=%d stderr=%q stdout=%q, want exit=%d and empty stderr",
			args,
			exitCode,
			stderr.String(),
			stdout.String(),
			expectedExit,
		)
	}
	return stdout.String()
}
