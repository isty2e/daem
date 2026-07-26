package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"

	clipresent "github.com/isty2e/daem/internal/cli/present"
)

func TestRunAddSkillYesJSONWritesManifestWithoutHumanOutput(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")
	testkit.WriteFile(t, filepath.Join(tempDir, "skills", "oracle"), "SKILL.md", "---\nname: oracle\ndescription: Oracle\n---\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "skill", filepath.Join(tempDir, "skills", "oracle"),
		"--manifest", manifestPath,
		"--target", "codex",
		"--json",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if strings.Contains(stdout.String(), "added:") {
		t.Fatalf("stdout = %q, want JSON only", stdout.String())
	}

	payload := clijson.DecodeManifestAuthoring(t, stdout.Bytes())
	if payload.SchemaVersion != 2 || payload.Command != "add" || payload.Mode != "write" || payload.Operation != "add" {
		t.Fatalf("payload header = %#v", payload)
	}
	if payload.ManifestPath != manifestPath || payload.ResourceCount != 1 || payload.ChangeCount != 1 || payload.HasErrors {
		t.Fatalf("payload summary = %#v", payload)
	}
	if payload.Lockfile == nil || payload.Lockfile.Path != filepath.Join(tempDir, "daem.lock.toml") || payload.Lockfile.Status != "written" {
		t.Fatalf("lockfile = %#v", payload.Lockfile)
	}
	change := payload.Changes[0]
	if change.ResourceID != "skill/oracle" || change.Resource.Kind != "skill" || change.Resource.Name != "oracle" || change.ChangeKind != "append skill resource" {
		t.Fatalf("change = %#v", change)
	}
	if !strings.Contains(change.ManifestBlock, `name = "oracle"`) {
		t.Fatalf("manifest_block = %q, want oracle block", change.ManifestBlock)
	}
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if !strings.Contains(string(content), `name = "oracle"`) {
		t.Fatalf("manifest = %s, want oracle skill", content)
	}
}

func TestRunRemoveSkillYesJSONWritesManifestWithoutHumanOutput(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { git = "https://github.com/owner/repo.git", path = "skills/oracle", ref = "main" }
targets = ["codex"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"remove", "skill", "oracle",
		"--manifest", manifestPath,
		"--json",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if strings.Contains(stdout.String(), "removed:") {
		t.Fatalf("stdout = %q, want JSON only", stdout.String())
	}

	payload := clijson.DecodeManifestAuthoring(t, stdout.Bytes())
	if payload.Command != "remove" || payload.Mode != "write" || payload.Operation != "remove" {
		t.Fatalf("payload header = %#v", payload)
	}
	if len(payload.Changes) != 1 || payload.Changes[0].ResourceID != "skill/oracle" || payload.Changes[0].ChangeKind != "remove skill resource" {
		t.Fatalf("changes = %#v", payload.Changes)
	}
	if payload.Lockfile == nil || payload.Lockfile.Path != lockfilePath || payload.Lockfile.Status != "written" {
		t.Fatalf("lockfile = %#v", payload.Lockfile)
	}
	testkit.AssertFileContent(t, manifestPath, "\nversion = 1\ntargets = [\"codex\"]\n\n")
}

func TestRunAddInstructionDryRunJSONSupportsAdmittedInstructionTarget(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	sourcePath := filepath.Join(tempDir, "instructions", "pi.md")
	testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"pi\"]\n")
	testkit.WriteFile(t, filepath.Dir(sourcePath), filepath.Base(sourcePath), "Pi guidance.\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "instruction", "project", sourcePath,
		"--manifest", manifestPath,
		"--target", "pi",
		"--dry-run",
		"--json",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if strings.Contains(stdout.String(), "add:") {
		t.Fatalf("stdout = %q, want JSON only", stdout.String())
	}

	payload := clijson.DecodeManifestAuthoring(t, stdout.Bytes())
	if payload.Command != "add" || payload.Mode != "dry-run" || payload.Operation != "add" {
		t.Fatalf("payload header = %#v", payload)
	}
	if payload.Lockfile == nil || payload.Lockfile.Path != filepath.Join(tempDir, "daem.lock.toml") || payload.Lockfile.Status != "would_write" {
		t.Fatalf("lockfile = %#v", payload.Lockfile)
	}
	if len(payload.Changes) != 1 {
		t.Fatalf("changes = %#v, want one change", payload.Changes)
	}
	change := payload.Changes[0]
	if change.ResourceID != "instructions/project" || change.Resource.Kind != "instructions" || change.Resource.Name != "project" || change.ChangeKind != "append instruction resource" {
		t.Fatalf("change = %#v", change)
	}
	for _, want := range []string{
		`source = "instructions/pi.md"`,
		`targets = ["pi"]`,
	} {
		if !strings.Contains(change.ManifestBlock, want) {
			t.Fatalf("manifest_block = %q, want %q", change.ManifestBlock, want)
		}
	}
	testkit.AssertFileContent(t, manifestPath, "version = 1\ntargets = [\"pi\"]\n")
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.lock.toml"))
}

func TestRunImportDryRunJSONEmitsPlanWithoutWrites(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, tempDir, "AGENTS.md", "project instructions\n")
	outputPath := filepath.Join(tempDir, "daem.imported.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--manifest", outputPath, "--dry-run", "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if strings.Contains(stdout.String(), "import:") || strings.Contains(stdout.String(), "summary:") {
		t.Fatalf("stdout = %q, want JSON only", stdout.String())
	}

	payload := clijson.DecodeManifestAuthoring(t, stdout.Bytes())
	if payload.Command != "import" || payload.Mode != "dry-run" || payload.ManifestPath != outputPath {
		t.Fatalf("payload header = %#v", payload)
	}
	if payload.SourceDir != filepath.Join(tempDir, "daem.imported.d") || payload.ResourceCount != 1 || payload.ChangeCount != 1 || payload.HasErrors {
		t.Fatalf("payload summary = %#v", payload)
	}
	change := payload.Changes[0]
	if change.ResourceID != "instructions/codex_project" || change.Target != "codex" || change.Scope != "project" || change.LivePath != "AGENTS.md" {
		t.Fatalf("change = %#v", change)
	}
	if len(payload.Summary) != 1 || payload.Summary[0].Instructions != 1 {
		t.Fatalf("summary = %#v", payload.Summary)
	}
	testkit.AssertPathMissing(t, outputPath)
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.imported.d"))
}

func TestRunImportDryRunJSONIncludesInstructionRenderToProvenance(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, tempDir, "GEMINI.md", "gemini guidance\n")
	outputPath := filepath.Join(tempDir, "daem.imported.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "antigravity-cli", "--manifest", outputPath, "--dry-run", "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}

	payload := clijson.DecodeManifestAuthoring(t, stdout.Bytes())
	if payload.Command != "import" || payload.Mode != "dry-run" || payload.ResourceCount != 1 {
		t.Fatalf("payload = %#v", payload)
	}
	change := payload.Changes[0]
	if change.ResourceID != "instructions/antigravity_cli_project_gemini" ||
		change.Target != "antigravity-cli" ||
		change.Scope != "project" ||
		change.LivePath != "GEMINI.md" ||
		change.RenderTo != "GEMINI.md" {
		t.Fatalf("change = %#v, want render_to provenance", change)
	}
	if !importJSONSkippedContains(payload.Skipped, "AGENTS.md", "missing") {
		t.Fatalf("skipped = %#v, want missing default AGENTS.md", payload.Skipped)
	}
	testkit.AssertPathMissing(t, outputPath)
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.imported.d"))
}

func TestRunImportYesJSONWritesManifestAndSources(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, tempDir, "AGENTS.md", "project instructions\n")
	outputPath := filepath.Join(tempDir, "daem.imported.toml")
	sourcePath := filepath.Join(tempDir, "daem.imported.d", "instructions", "codex-project.md")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--manifest", outputPath, "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	payload := clijson.DecodeManifestAuthoring(t, stdout.Bytes())
	if payload.Command != "import" || payload.Mode != "write" || payload.ChangeCount != 1 {
		t.Fatalf("payload = %#v", payload)
	}
	testkit.AssertFileContent(t, sourcePath, "project instructions\n")
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if !strings.Contains(string(content), "codex_project") {
		t.Fatalf("manifest = %s, want imported instruction", content)
	}
}

func TestRunImportMergeConflictJSONPreservesTypedResult(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		mode string
	}{
		{name: "dry-run", args: []string{"--dry-run"}, mode: "dry-run"},
		{name: "write", mode: "write"},
	} {
		t.Run(test.name, func(t *testing.T) {
			tempDir := t.TempDir()
			testkit.WithWorkingDirectory(t, tempDir)
			outputPath := filepath.Join(tempDir, "daem.toml")
			testkit.WriteFile(t, tempDir, "AGENTS.md", "project instructions\n")
			testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.codex_project]
source = "instructions/other.md"
targets = ["codex"]
scope = "project"
`)
			original, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatalf("ReadFile returned error: %v", err)
			}

			args := []string{"import", "--target", "codex", "--manifest", outputPath, "--merge", "--json"}
			args = append(args, test.args...)
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr)
			if exitCode != 1 {
				t.Fatalf("exitCode = %d, want 1, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty for typed JSON result", stderr.String())
			}
			if strings.Contains(stdout.String(), "merge resource=") {
				t.Fatalf("stdout = %q, want JSON only", stdout.String())
			}
			if strings.Contains(stdout.String(), `"changes": null`) {
				t.Fatalf("stdout = %q, want changes array instead of null", stdout.String())
			}
			payload := clijson.DecodeManifestAuthoring(t, stdout.Bytes())
			if payload.SchemaVersion != 2 || payload.Command != "import" || payload.Mode != test.mode {
				t.Fatalf("payload header = %#v", payload)
			}
			if !payload.HasErrors || len(payload.MergeResults) != 1 {
				t.Fatalf("payload = %#v, want merge conflict", payload)
			}
			if payload.MergeResults[0].ResourceID != "instructions/codex_project" || payload.MergeResults[0].Status != "conflict" {
				t.Fatalf("merge_results = %#v", payload.MergeResults)
			}
			testkit.AssertFileContent(t, outputPath, string(original))
			testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.d"))
		})
	}
}

func TestRunImportPreResultJSONFailureUsesStderrOnly(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "unknown", "--json"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty before a typed result exists", stdout.String())
	}
	if !strings.Contains(stderr.String(), "unknown target") {
		t.Fatalf("stderr = %q, want target diagnostic", stderr.String())
	}
}

func TestRunManifestAuthoringJSONAndDiffAreMutuallyExclusive(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "add",
			args: []string{"add", "skill", "owner/repo", "--path", "skills/oracle", "--ref", "main", "--dry-run", "--json", "--diff"},
			want: "add failed: --json and --diff are mutually exclusive",
		},
		{
			name: "remove",
			args: []string{"remove", "skill", "oracle", "--dry-run", "--json", "--diff"},
			want: "remove failed: --json and --diff are mutually exclusive",
		},
		{
			name: "import",
			args: []string{"import", "--target", "codex", "--dry-run", "--json", "--diff"},
			want: "import failed: --json and --diff are mutually exclusive",
		},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI(scenario.args, &stdout, &stderr)
			if exitCode != 2 {
				t.Fatalf("exitCode = %d, want 2", exitCode)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), scenario.want) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), scenario.want)
			}
		})
	}
}

func importJSONSkippedContains(skipped []clipresent.ImportAuthoringJSONSkipped, livePath string, reason string) bool {
	for _, item := range skipped {
		if item.LivePath == livePath && item.Reason == reason {
			return true
		}
	}
	return false
}
