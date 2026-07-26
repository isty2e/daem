package cli_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestRunAddExtensionDryRunPlansManifestAndLockOnly(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	original := "version = 1\ntargets = [\"claude-code\"]\n"
	testkit.WriteFile(t, tempDir, "daem.toml", original)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "extension", "context7-managed", "context7@market",
		"--manifest", manifestPath,
		"--dry-run",
		"--diff",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"add: extension/context7-managed",
		"change: append extension resource",
		"[[extension]]",
		`id = "context7-managed"`,
		`carrier = "claude-code-plugin"`,
		`targets = ["claude-code"]`,
		`scope = "project"`,
		`source = { marketplace = "context7@market" }`,
		"manifest diff:",
		"lockfile: would write " + lockfilePath,
		"note: add updates the manifest and lockfile only; carrier lifecycle changes require a separately admitted host route",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	assertNoCarrierSuccessClaims(t, stdout.String())
	testkit.AssertFileContent(t, manifestPath, original)
	testkit.AssertPathMissing(t, lockfilePath)
}

func TestRunAddExtensionYesWritesManifestAndLockOnly(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"claude-code\"]\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "extension", "context7-managed", "context7@market",
		"--manifest", manifestPath,
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"added: extension/context7-managed",
		"change: append extension resource",
		"lockfile: wrote " + lockfilePath,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	assertNoCarrierSuccessClaims(t, stdout.String())
	normalized, err := declarationmanifest.Decode(testkit.ReadFile(t, manifestPath))
	if err != nil {
		t.Fatalf("declarationmanifest.Decode returned error: %v", err)
	}
	if len(normalized.Extensions()) != 1 ||
		normalized.Extensions()[0].ID().Name() != "context7-managed" ||
		normalized.Extensions()[0].Source().Ref() != "context7@market" {
		t.Fatalf("Extensions = %#v, want context7-managed marketplace context7@market", normalized.Extensions())
	}
	assertCLIClaudeExtensionLockedSubject(t, lockfilePath)
}

func TestRunAddExtensionYesUnderGlobalDefaultsLocksExplicitProjectScope(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", `version = 1
targets = ["claude-code"]

[defaults]
scope = "global"
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "extension", "context7-managed", "context7@market",
		"--manifest", manifestPath,
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	normalized, err := declarationmanifest.Decode(testkit.ReadFile(t, manifestPath))
	if err != nil {
		t.Fatalf("declarationmanifest.Decode returned error: %v", err)
	}
	if len(normalized.Extensions()) != 1 ||
		normalized.Extensions()[0].ID().Name() != "context7-managed" ||
		string(normalized.Extensions()[0].Scope()) != "project" {
		t.Fatalf("Extensions = %#v, want explicit project extension under global defaults", normalized.Extensions())
	}
	manifestContent := testkit.ReadFile(t, manifestPath)
	if !strings.Contains(string(manifestContent), `scope = "project"`) {
		t.Fatalf("manifest = %q, want explicit project scope", manifestContent)
	}
	assertCLIClaudeExtensionLockedSubject(t, lockfilePath)
}

func TestRunAddExtensionJSONUsesCanonicalMarketplaceSelector(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"claude-code\"]\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "extension", "context7", "context7@market",
		"--manifest", manifestPath,
		"--dry-run",
		"--json",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	payload := clijson.DecodeManifestAuthoring(t, stdout.Bytes())
	if payload.Command != "add" || payload.Mode != "dry-run" || payload.Operation != "add" {
		t.Fatalf("payload header = %#v", payload)
	}
	if len(payload.Changes) != 1 ||
		payload.Changes[0].ResourceID != "extension/context7" ||
		payload.Changes[0].Resource.Kind != "extension" ||
		payload.Changes[0].Resource.Name != "context7" ||
		payload.Changes[0].ChangeKind != "append extension resource" {
		t.Fatalf("changes = %#v", payload.Changes)
	}
	if !strings.Contains(payload.Changes[0].ManifestBlock, `source = { marketplace = "context7@market" }`) {
		t.Fatalf("manifest_block = %q, want canonical marketplace selector", payload.Changes[0].ManifestBlock)
	}
	testkit.AssertFileContent(t, manifestPath, "version = 1\ntargets = [\"claude-code\"]\n")
}

func TestRunAddExtensionRejectsMissingOrBareMarketplaceSelector(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		source   string
		wantExit int
		want     string
	}{
		{name: "missing", wantExit: 2, want: "missing extension source"},
		{name: "bare", source: "context7", wantExit: 1, want: "PLUGIN@MARKETPLACE"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			tempDir := t.TempDir()
			manifestPath := filepath.Join(tempDir, "daem.toml")
			original := "version = 1\ntargets = [\"claude-code\"]\n"
			testkit.WriteFile(t, tempDir, "daem.toml", original)
			args := []string{"add", "extension", "context7"}
			if scenario.source != "" {
				args = append(args, scenario.source)
			}
			args = append(args, "--manifest", manifestPath, "--dry-run")
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr)
			if exitCode != scenario.wantExit || !strings.Contains(stderr.String(), scenario.want) {
				t.Fatalf("exitCode=%d stdout=%q stderr=%q, want %d/%q", exitCode, stdout.String(), stderr.String(), scenario.wantExit, scenario.want)
			}
			testkit.AssertFileContent(t, manifestPath, original)
		})
	}
}

func TestRunAddExtensionGlobalDryRunPlansManifestAndLockOnly(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	original := "version = 1\ntargets = [\"claude-code\"]\n"
	testkit.WriteFile(t, tempDir, "daem.toml", original)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "extension", "context7-global", "context7@market",
		"--manifest", manifestPath,
		"--scope", "global",
		"--dry-run",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"add: extension/context7-global",
		"change: append extension resource",
		`scope = "global"`,
		`source = { marketplace = "context7@market" }`,
		"lockfile: would write " + lockfilePath,
		"note: add updates the manifest and lockfile only; carrier lifecycle changes require a separately admitted host route",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	assertNoCarrierSuccessClaims(t, stdout.String())
	testkit.AssertFileContent(t, manifestPath, original)
	testkit.AssertPathMissing(t, lockfilePath)
}

func TestRunAddExtensionGlobalYesWritesManifestAndLockOnly(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"claude-code\"]\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "extension", "context7-global", "context7@market",
		"--manifest", manifestPath,
		"--scope", "global",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"added: extension/context7-global",
		"change: append extension resource",
		"lockfile: wrote " + lockfilePath,
		"note: add updates the manifest and lockfile only; carrier lifecycle changes require a separately admitted host route",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	assertNoCarrierSuccessClaims(t, stdout.String())
	manifestContent := testkit.ReadFile(t, manifestPath)
	if !strings.Contains(string(manifestContent), `scope = "global"`) ||
		strings.Contains(string(manifestContent), `scope = "user"`) {
		t.Fatalf("manifest = %q, want public global scope without host user scope", manifestContent)
	}
	normalized, err := declarationmanifest.Decode(manifestContent)
	if err != nil {
		t.Fatalf("declarationmanifest.Decode returned error: %v", err)
	}
	if len(normalized.Extensions()) != 1 ||
		normalized.Extensions()[0].ID().Name() != "context7-global" ||
		normalized.Extensions()[0].Source().Ref() != "context7@market" ||
		string(normalized.Extensions()[0].Scope()) != "global" {
		t.Fatalf("Extensions = %#v, want context7-global marketplace context7@market global", normalized.Extensions())
	}
	assertCLIClaudeExtensionLockedSubjectWithScope(t, lockfilePath, "context7-global", "global")
	lockfileContent := testkit.ReadFile(t, lockfilePath)
	if strings.Contains(string(lockfileContent), `scope = "user"`) {
		t.Fatalf("lockfile = %q, want no host user scope", lockfileContent)
	}
}

func TestRunAddExtensionRejectsUnsupportedTargetAndScope(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantExit int
		want     string
	}{
		{
			name:     "codex omitted global scope",
			args:     []string{"add", "extension", "context7", "context7@market", "--target", "codex", "--dry-run"},
			wantExit: 1,
			want:     "--scope global is required for --target codex",
		},
		{
			name:     "antigravity omitted global scope",
			args:     []string{"add", "extension", "guidance", "guidance@publisher", "--target", "antigravity-cli", "--dry-run"},
			wantExit: 1,
			want:     "--scope global is required for --target antigravity-cli",
		},
		{
			name:     "user scope",
			args:     []string{"add", "extension", "context7", "context7@market", "--scope", "user", "--dry-run"},
			wantExit: 2,
			want:     `unknown scope "user"`,
		},
		{
			name:     "local scope",
			args:     []string{"add", "extension", "context7", "context7@market", "--scope", "local", "--dry-run"},
			wantExit: 2,
			want:     `unknown scope "local"`,
		},
		{
			name:     "managed scope",
			args:     []string{"add", "extension", "context7", "context7@market", "--scope", "managed", "--dry-run"},
			wantExit: 2,
			want:     `unknown scope "managed"`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tempDir := t.TempDir()
			manifestPath := filepath.Join(tempDir, "daem.toml")
			testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"claude-code\"]\n")
			args := append([]string{}, test.args...)
			args = append(args, "--manifest", manifestPath)
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr)
			if exitCode != test.wantExit {
				t.Fatalf("exitCode = %d, want %d; stdout=%q stderr=%q", exitCode, test.wantExit, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), test.want)
			}
		})
	}
}

func TestRunAddExtensionRejectsUnsupportedSourceFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "git source",
			args: []string{"add", "extension", "context7", "--git", "https://github.com/acme/context7", "--dry-run"},
		},
		{
			name: "github source",
			args: []string{"add", "extension", "context7", "--github", "acme/context7", "--dry-run"},
		},
		{
			name: "url source",
			args: []string{"add", "extension", "context7", "--url", "https://example.invalid/context7", "--dry-run"},
		},
		{
			name: "local path source",
			args: []string{"add", "extension", "context7", "--path", "./plugins/context7", "--dry-run"},
		},
		{
			name: "npm source",
			args: []string{"add", "extension", "context7", "--npm", "@acme/context7", "--dry-run"},
		},
		{
			name: "package source",
			args: []string{"add", "extension", "context7", "--package", "@acme/context7", "--dry-run"},
		},
		{
			name: "source selector",
			args: []string{"add", "extension", "context7", "--source", "marketplace:context7", "--dry-run"},
		},
		{
			name: "raw host config",
			args: []string{"add", "extension", "context7", "--host-config", `{"plugin":"context7"}`, "--dry-run"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tempDir := t.TempDir()
			manifestPath := filepath.Join(tempDir, "daem.toml")
			original := "version = 1\ntargets = [\"claude-code\"]\n"
			testkit.WriteFile(t, tempDir, "daem.toml", original)
			args := append([]string{}, test.args...)
			args = append(args, "--manifest", manifestPath)

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr)
			if exitCode != 2 {
				t.Fatalf("exitCode = %d, want 2; stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
			if stderr.Len() == 0 {
				t.Fatalf("stderr empty, want rejected non-canonical source flag")
			}
			testkit.AssertFileContent(t, manifestPath, original)
		})
	}
}

func TestRunAddExtensionRejectsMalformedMarketplace(t *testing.T) {
	tests := []struct {
		name        string
		marketplace string
		want        string
	}{
		{
			name:        "leading whitespace",
			marketplace: " context7",
			want:        "extension source must not contain leading or trailing whitespace",
		},
		{
			name:        "control character",
			marketplace: "context7\nnext",
			want:        "extension source must not contain control characters",
		},
		{
			name:        "option-looking value",
			marketplace: "--not-a-plugin",
			want:        "extension source must not begin with '-' because host CLIs may parse it as an option",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tempDir := t.TempDir()
			manifestPath := filepath.Join(tempDir, "daem.toml")
			original := "version = 1\ntargets = [\"claude-code\"]\n"
			testkit.WriteFile(t, tempDir, "daem.toml", original)
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			args := []string{
				"add", "extension", "context7-managed", test.marketplace,
				"--manifest", manifestPath,
				"--dry-run",
			}
			if strings.HasPrefix(test.marketplace, "-") {
				args = []string{
					"add", "extension", "--manifest", manifestPath, "--dry-run",
					"--", "context7-managed", test.marketplace,
				}
			}
			exitCode := testkit.RunCLI(args, &stdout, &stderr)
			if exitCode != 1 {
				t.Fatalf("exitCode = %d, want 1; stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), test.want)
			}
			testkit.AssertFileContent(t, manifestPath, original)
		})
	}
}

func TestRunAddExtensionYesLeavesFilesUnchangedWhenProspectiveLockFails(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	originalManifest := `version = 1
targets = ["claude-code"]

[instructions.project]
source = "missing.md"
targets = ["claude-code"]
`
	originalLockfile := "version = 1\n# keep me\n"
	testkit.WriteFile(t, tempDir, "daem.toml", originalManifest)
	testkit.WriteFile(t, tempDir, "daem.lock.toml", originalLockfile)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "extension", "context7-managed", "context7@market",
		"--manifest", manifestPath,
	}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "add failed: lock prospective manifest") {
		t.Fatalf("stderr = %q, want prospective lock failure", stderr.String())
	}
	testkit.AssertFileContent(t, manifestPath, originalManifest)
	testkit.AssertFileContent(t, lockfilePath, originalLockfile)
}
