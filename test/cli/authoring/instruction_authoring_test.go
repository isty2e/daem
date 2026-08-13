package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/realization/lockfile"
)

func TestRunAddInstructionDryRunPlansLocalInstructionWithoutWrites(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	original := "version = 1\ntargets = [\"codex\"]\n"
	sourcePath := filepath.Join(tempDir, "instructions", "AGENTS.md")
	testkit.WriteFile(t, tempDir, "daem.toml", original)
	testkit.WriteFile(t, filepath.Dir(sourcePath), filepath.Base(sourcePath), "Use concise answers.\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "instruction", "project", sourcePath,
		"--manifest", manifestPath,
		"--target", "codex",
		"--target", "claude-code",
		"--dry-run",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"add: instructions/project",
		"change: append instruction resource",
		"[instructions.\"project\"]",
		`source = "instructions/AGENTS.md"`,
		`targets = ["codex", "claude-code"]`,
		"lockfile: would write " + filepath.Join(tempDir, "daem.lock.toml"),
		"next: rerun this authoring command without --dry-run",
		"note: add updates the manifest and lockfile only; host files are written only by apply",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	if strings.Contains(stdout.String(), "manifest diff:") {
		t.Fatalf("stdout = %q, want concise dry-run without manifest diff", stdout.String())
	}
	testkit.AssertFileContent(t, manifestPath, original)
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.lock.toml"))
	testkit.AssertPathMissing(t, filepath.Join(tempDir, ".daem"))
}

func TestRunAddInstructionDryRunDiffShowsResultingManifestDelta(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	original := "version = 1\ntargets = [\"codex\"]\n"
	sourcePath := filepath.Join(tempDir, "AGENTS.md")
	testkit.WriteFile(t, tempDir, "daem.toml", original)
	testkit.WriteFile(t, tempDir, "AGENTS.md", "Project guidance.\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "instruction", "project", sourcePath,
		"--manifest", manifestPath,
		"--target", "codex",
		"--dry-run",
		"--diff",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"manifest diff:",
		"--- " + manifestPath,
		"+++ " + manifestPath,
		"+[instructions.\"project\"]",
		`+source = "AGENTS.md"`,
		`+targets = ["codex"]`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertFileContent(t, manifestPath, original)
}

func TestRunAddInstructionDryRunSupportsAdmittedInstructionTargetDefaults(t *testing.T) {
	tests := []struct {
		name         string
		resourceName string
		target       string
		sourceRel    string
		wantSource   func(string) string
	}{
		{
			name:         "opencode project",
			resourceName: "project",
			target:       "opencode",
			sourceRel:    "instructions/opencode-project.md",
			wantSource: func(_ string) string {
				return `source = "instructions/opencode-project.md"`
			},
		},
		{
			name:         "opencode global",
			resourceName: "global",
			target:       "opencode",
			sourceRel:    "instructions/opencode-global.md",
			wantSource: func(sourcePath string) string {
				return `source = "` + filepath.ToSlash(sourcePath) + `"`
			},
		},
		{
			name:         "pi project",
			resourceName: "project",
			target:       "pi",
			sourceRel:    "instructions/pi-project.md",
			wantSource: func(_ string) string {
				return `source = "instructions/pi-project.md"`
			},
		},
		{
			name:         "pi global",
			resourceName: "global",
			target:       "pi",
			sourceRel:    "instructions/pi-global.md",
			wantSource: func(sourcePath string) string {
				return `source = "` + filepath.ToSlash(sourcePath) + `"`
			},
		},
		{
			name:         "antigravity cli project",
			resourceName: "project",
			target:       "antigravity-cli",
			sourceRel:    "instructions/antigravity-project.md",
			wantSource: func(_ string) string {
				return `source = "instructions/antigravity-project.md"`
			},
		},
		{
			name:         "antigravity cli global",
			resourceName: "global",
			target:       "antigravity-cli",
			sourceRel:    "instructions/antigravity-global.md",
			wantSource: func(sourcePath string) string {
				return `source = "` + filepath.ToSlash(sourcePath) + `"`
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tempDir := t.TempDir()
			manifestPath := filepath.Join(tempDir, "daem.toml")
			sourcePath := filepath.Join(tempDir, filepath.FromSlash(test.sourceRel))
			original := "version = 1\ntargets = [\"" + test.target + "\"]\n"
			testkit.WriteFile(t, tempDir, "daem.toml", original)
			testkit.WriteFile(t, filepath.Dir(sourcePath), filepath.Base(sourcePath), "target instructions\n")

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI([]string{
				"add", "instruction", test.resourceName, sourcePath,
				"--manifest", manifestPath,
				"--target", test.target,
				"--dry-run",
			}, &stdout, &stderr)
			if exitCode != 0 {
				t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
			}
			for _, want := range []string{
				"add: instructions/" + test.resourceName,
				"change: append instruction resource",
				`targets = ["` + test.target + `"]`,
				test.wantSource(sourcePath),
				"lockfile: would write " + filepath.Join(tempDir, "daem.lock.toml"),
			} {
				if !strings.Contains(stdout.String(), want) {
					t.Fatalf("stdout = %q, want %q", stdout.String(), want)
				}
			}
			testkit.AssertFileContent(t, manifestPath, original)
			testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.lock.toml"))
		})
	}
}

func TestRunAddInstructionYesAddsGlobalSourceAsAbsolute(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	sourcePath := filepath.Join(tempDir, "instructions", "global.md")
	testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")
	testkit.WriteFile(t, filepath.Dir(sourcePath), filepath.Base(sourcePath), "Global guidance.\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "instruction", "global", sourcePath,
		"--manifest", manifestPath,
		"--target", "codex",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	locked, err := lockfile.Load(t.Context(), filepath.Join(tempDir, "daem.lock.toml"))
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(testkit.LockedInstructions(t, locked)) != 1 || testkit.LockedInstructions(t, locked)[0].Name != "global" {
		t.Fatalf("locked instructions = %#v, want global", testkit.LockedInstructions(t, locked))
	}
	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("Parse returned error: %v\n%s", err, content)
	}
	if len(config.Instructions()) != 1 || config.Instructions()[0].ID().Name() != "global" {
		t.Fatalf("instructions = %#v, want global instruction", config.Instructions())
	}
	testkit.AssertPathMissing(t, testkit.AuthoringTransactionDir(filepath.Join(tempDir, ".daem")))
}

func TestRunAddInstructionYesWritesAntigravityProjectManifestAndLockWithoutHostOutput(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	sourcePath := filepath.Join(tempDir, "instructions", "antigravity.md")
	testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"antigravity-cli\"]\n")
	testkit.WriteFile(t, filepath.Dir(sourcePath), filepath.Base(sourcePath), "Antigravity CLI guidance.\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "instruction", "project", sourcePath,
		"--manifest", manifestPath,
		"--target", "antigravity-cli",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"added: instructions/project",
		"change: append instruction resource",
		"lockfile: wrote " + filepath.Join(tempDir, "daem.lock.toml"),
		"note: add updates the manifest and lockfile only; host files are written only by apply",
	} {
		testkit.AssertOutputLine(t, stdout.String(), want)
	}

	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("Parse returned error: %v\n%s", err, content)
	}
	if len(config.Instructions()) != 1 || len(config.Instructions()[0].Targets()) != 1 || config.Instructions()[0].Targets()[0] != "antigravity-cli" {
		t.Fatalf("instructions = %#v, want antigravity-cli project instruction", config.Instructions())
	}
	locked, err := lockfile.Load(t.Context(), filepath.Join(tempDir, "daem.lock.toml"))
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(testkit.LockedInstructions(t, locked)) != 1 || testkit.LockedInstructions(t, locked)[0].Name != "project" {
		t.Fatalf("locked instructions = %#v, want project", testkit.LockedInstructions(t, locked))
	}
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "AGENTS.md"))
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "GEMINI.md"))
	testkit.AssertPathMissing(t, testkit.AuthoringTransactionDir(filepath.Join(tempDir, ".daem")))
}

func TestRunAddInstructionDryRunFailsForMissingLocalSource(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	original := "version = 1\ntargets = [\"codex\"]\n"
	testkit.WriteFile(t, tempDir, "daem.toml", original)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "instruction", "project", filepath.Join(tempDir, "instructions", "missing.git"),
		"--manifest", manifestPath,
		"--target", "codex",
		"--dry-run",
	}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), `add failed: lock prospective manifest: resolve instructions "project" source`) {
		t.Fatalf("stderr = %q, want lock preflight diagnostic", stderr.String())
	}
	if strings.Contains(stderr.String(), "next: run daem init") {
		t.Fatalf("stderr = %q, want no init hint for source lock failure", stderr.String())
	}
	testkit.AssertFileContent(t, manifestPath, original)
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.lock.toml"))
}

func TestRunAddInstructionYesWritesAntigravityGlobalManifestAndLockWithoutHostOutput(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	manifestPath := filepath.Join(tempDir, "daem.toml")
	sourcePath := filepath.Join(tempDir, "instructions", "global.md")
	testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"antigravity-cli\"]\n")
	testkit.WriteFile(t, filepath.Dir(sourcePath), filepath.Base(sourcePath), "global guidance\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "instruction", "global", sourcePath,
		"--manifest", manifestPath,
		"--target", "antigravity-cli",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"added: instructions/global",
		"change: append instruction resource",
		"lockfile: wrote " + filepath.Join(tempDir, "daem.lock.toml"),
		"note: add updates the manifest and lockfile only; host files are written only by apply",
	} {
		testkit.AssertOutputLine(t, stdout.String(), want)
	}

	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("Parse returned error: %v\n%s", err, content)
	}
	if len(config.Instructions()) != 1 ||
		config.Instructions()[0].ID().Name() != "global" ||
		config.Instructions()[0].Scope() != "global" ||
		len(config.Instructions()[0].Targets()) != 1 ||
		config.Instructions()[0].Targets()[0] != "antigravity-cli" {
		t.Fatalf("instructions = %#v, want antigravity-cli global instruction", config.Instructions())
	}
	locked, err := lockfile.Load(t.Context(), filepath.Join(tempDir, "daem.lock.toml"))
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(testkit.LockedInstructions(t, locked)) != 1 || testkit.LockedInstructions(t, locked)[0].Name != "global" {
		t.Fatalf("locked instructions = %#v, want global", testkit.LockedInstructions(t, locked))
	}
	testkit.AssertPathMissing(t, filepath.Join(homeDir, ".gemini", "GEMINI.md"))
	testkit.AssertPathMissing(t, testkit.AuthoringTransactionDir(filepath.Join(tempDir, ".daem")))
}

func TestRunAddInstructionYesRejectsInvalidExistingRenderToBeforeManifestWrite(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	original := `
version = 1
targets = ["codex", "opencode"]

[instructions.project]
source = "instructions/project.md"
targets = ["codex"]

[instructions.project.target.codex]
render_to = "CLAUDE.md"
`
	testkit.WriteFile(t, tempDir, "daem.toml", original)
	testkit.WriteFile(t, tempDir, "instructions/project.md", "shared guidance\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "instruction", "project", filepath.Join(tempDir, "instructions", "project.md"),
		"--manifest", manifestPath,
		"--target", "opencode",
	}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	for _, want := range []string{
		`add failed: lock prospective manifest: instructions "project" target "codex"`,
		`render_to "CLAUDE.md" for target "codex" scope "project"`,
		`not an admitted instruction placement destination`,
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
	testkit.AssertFileContent(t, manifestPath, original)
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.lock.toml"))
	testkit.AssertPathMissing(t, testkit.AuthoringTransactionDir(filepath.Join(tempDir, ".daem")))
}

func TestRunRemoveInstructionYesUpdatesManifestAndLockfile(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	hostInstructionPath := filepath.Join(tempDir, "AGENTS.md")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[instructions."project"]
source = "instructions/AGENTS.md"
targets = ["codex"]
`)
	testkit.WriteFile(t, tempDir, "daem.lock.toml", "version = 5\n\n[locked]\n")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "host stays\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"remove", "instruction", "project", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"removed: instructions/project",
		"change: remove instruction resource",
		"lockfile: wrote " + lockfilePath,
		"next: run daem apply --manifest " + manifestPath + " --dry-run",
		"note: remove updates the manifest and lockfile only; host files are deleted only when apply reconciles managed state",
	} {
		testkit.AssertOutputLine(t, stdout.String(), want)
	}
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("Parse returned error: %v\n%s", err, content)
	}
	if len(config.Instructions()) != 0 {
		t.Fatalf("instructions = %#v, want none", config.Instructions())
	}
	locked, err := lockfile.Load(t.Context(), lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(testkit.LockedInstructions(t, locked)) != 0 || len(testkit.LockedSkills(t, locked)) != 0 || len(testkit.LockedHooks(t, locked)) != 0 {
		t.Fatalf("locked = %#v, want no resources", locked.Locked)
	}
	testkit.AssertFileContent(t, hostInstructionPath, "host stays\n")
}

func TestRunRemoveInstructionYesRemovesLastAdmittedNewTargetResourceOnly(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/opencode.md", "OpenCode guidance.\n")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "host stays\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["opencode"]

[instructions.project]
source = "instructions/opencode.md"
targets = ["opencode"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"remove", "instruction", "project",
		"--manifest", manifestPath,
		"--target", "opencode",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	testkit.AssertOutputLine(t, stdout.String(), "removed: instructions/project")
	testkit.AssertOutputLine(t, stdout.String(), "change: remove instruction resource")

	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("Parse returned error: %v\n%s", err, content)
	}
	if len(config.Instructions()) != 0 {
		t.Fatalf("instructions = %#v, want none", config.Instructions())
	}
	locked, err := lockfile.Load(t.Context(), lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(testkit.LockedInstructions(t, locked)) != 0 {
		t.Fatalf("locked instructions = %#v, want none", testkit.LockedInstructions(t, locked))
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "host stays\n")
}

func TestRunRemoveInstructionDryRunDiffShowsResultingManifestDelta(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	original := `
version = 1
targets = ["codex"]

[instructions."project"]
source = "AGENTS.md"
targets = ["codex"]
`
	testkit.WriteFile(t, tempDir, "daem.toml", original)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"remove", "instruction", "project", "--manifest", manifestPath, "--dry-run", "--diff"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"manifest diff:",
		"--- " + manifestPath,
		"+++ " + manifestPath,
		`-[instructions."project"]`,
		`-source = "AGENTS.md"`,
		`-targets = ["codex"]`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertFileContent(t, manifestPath, original)
}

func TestRunRemoveInstructionDryRunRemovesHyphenTargetRenderingTable(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	original := `
version = 1
targets = ["codex", "antigravity-cli"]

[instructions.project]
source = "instructions/project.md"
targets = ["codex", "antigravity-cli"]

[instructions.project.target.antigravity-cli]
render_to = "GEMINI.md"
`
	testkit.WriteFile(t, tempDir, "daem.toml", original)
	testkit.WriteFile(t, tempDir, "instructions/project.md", "Shared guidance.\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"remove", "instruction", "project",
		"--manifest", manifestPath,
		"--target", "antigravity-cli",
		"--dry-run",
		"--diff",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"change: update instruction targets",
		`-targets = ["codex", "antigravity-cli"]`,
		`+targets = ["codex"]`,
		`-[instructions.project.target.antigravity-cli]`,
		`-render_to = "GEMINI.md"`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertFileContent(t, manifestPath, original)
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.lock.toml"))
}
