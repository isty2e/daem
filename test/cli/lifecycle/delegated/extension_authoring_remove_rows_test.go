package cli_test

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/execcheck"
)

func TestRunRemoveExtensionWritesEveryAdmittedCarrierRowByID(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		target      string
		carrier     string
		scope       string
		sourceField string
		sourceRef   string
		command     string
	}{
		{name: "claude project", id: "claude-project", target: "claude-code", carrier: "claude-code-plugin", scope: "project", sourceField: "marketplace", sourceRef: "plugin@market", command: "claude"},
		{name: "claude global", id: "claude-global", target: "claude-code", carrier: "claude-code-plugin", scope: "global", sourceField: "marketplace", sourceRef: "plugin@market", command: "claude"},
		{name: "codex global", id: "codex", target: "codex", carrier: "codex-plugin", scope: "global", sourceField: "marketplace", sourceRef: "plugin@market", command: "codex"},
		{name: "opencode project", id: "opencode-project", target: "opencode", carrier: "opencode-plugin", scope: "project", sourceField: "host_source", sourceRef: "@acme/plugin", command: "opencode"},
		{name: "opencode global", id: "opencode-global", target: "opencode", carrier: "opencode-plugin", scope: "global", sourceField: "host_source", sourceRef: "@acme/plugin", command: "opencode"},
		{name: "pi project", id: "pi-project", target: "pi", carrier: "pi-package", scope: "project", sourceField: "host_source", sourceRef: "github:acme/plugin", command: "pi"},
		{name: "pi global", id: "pi-global", target: "pi", carrier: "pi-package", scope: "global", sourceField: "host_source", sourceRef: "github:acme/plugin", command: "pi"},
		{name: "antigravity global", id: "antigravity", target: "antigravity-cli", carrier: "antigravity-cli-plugin", scope: "global", sourceField: "host_source", sourceRef: "plugin@publisher", command: "agy"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			manifestPath := filepath.Join(root, "daem.toml")
			lockfilePath := filepath.Join(root, "daem.lock.toml")
			manifest := extensionAuthoringRowManifest(test.id, test.target, test.carrier, test.scope, test.sourceField, test.sourceRef)
			testkit.WriteFile(t, root, "daem.toml", manifest)
			runExtensionAuthoringLock(t, manifestPath, lockfilePath)
			canary := execcheck.New(t, test.command)

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI([]string{
				"remove", "extension", test.id,
				"--manifest", manifestPath,
				"--json",
			}, &stdout, &stderr)
			if exitCode != 0 || stderr.Len() != 0 {
				t.Fatalf("remove exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
			if strings.Contains(stdout.String(), "next_steps") {
				t.Fatalf("stdout = %q, did not want human next steps in JSON", stdout.String())
			}
			execcheck.AssertClean(t, canary, "remove "+test.name)

			normalized, err := declarationmanifest.Decode(testkit.ReadFile(t, manifestPath))
			if err != nil {
				t.Fatalf("declarationmanifest.Decode returned error: %v", err)
			}
			if len(normalized.Extensions()) != 0 {
				t.Fatalf("extensions = %#v, want removed", normalized.Extensions())
			}
			locked, err := lockfile.Load(lockfilePath)
			if err != nil {
				t.Fatalf("lockfile.Load returned error: %v", err)
			}
			if len(locked.Locked.Subjects()) != 0 {
				t.Fatalf("locked subjects = %#v, want none", locked.Locked.Subjects())
			}

			stdout.Reset()
			stderr.Reset()
			exitCode = testkit.RunVerboseCLI([]string{
				"apply",
				"--manifest", manifestPath,
				"--yes",
				"--json",
			}, &stdout, &stderr)
			if exitCode != 0 || stderr.Len() != 0 {
				t.Fatalf("apply after removal exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
			payload := clijson.DecodeApplyResult(t, stdout.Bytes())
			if payload.HasErrors || payload.ActionCount != 0 || len(payload.Actions) != 0 ||
				len(payload.RelationActions) != 0 || len(payload.HostRouteAttempts) != 0 {
				t.Fatalf("apply after removal payload = %#v, want no carrier or destructive work", payload)
			}
			execcheck.AssertClean(t, canary, "apply after removal "+test.name)
		})
	}
}

func TestRunRemoveExtensionMatchesManualOmissionAfterLock(t *testing.T) {
	cliRoot := t.TempDir()
	manualRoot := t.TempDir()
	cliManifestPath := filepath.Join(cliRoot, "daem.toml")
	cliLockfilePath := filepath.Join(cliRoot, "daem.lock.toml")
	manualManifestPath := filepath.Join(manualRoot, "daem.toml")
	manualLockfilePath := filepath.Join(manualRoot, "daem.lock.toml")
	initial := extensionAuthoringRowManifest(
		"formatter",
		"opencode",
		"opencode-plugin",
		"project",
		"host_source",
		"@acme/formatter",
	)
	testkit.WriteFile(t, cliRoot, "daem.toml", initial)
	testkit.WriteFile(t, manualRoot, "daem.toml", initial)
	runExtensionAuthoringLock(t, cliManifestPath, cliLockfilePath)
	runExtensionAuthoringLock(t, manualManifestPath, manualLockfilePath)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"remove", "extension", "formatter",
		"--manifest", cliManifestPath,
	}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("remove exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}

	manualOmission := "version = 1\ntargets = [\"opencode\"]\n"
	testkit.WriteFile(t, manualRoot, "daem.toml", manualOmission)
	runExtensionAuthoringLock(t, manualManifestPath, manualLockfilePath)

	cliLockfile := testkit.ReadFile(t, cliLockfilePath)
	manualLockfile := testkit.ReadFile(t, manualLockfilePath)
	if !bytes.Equal(cliLockfile, manualLockfile) {
		t.Fatalf(
			"remove and manual omission lockfiles differ:\nremove:\n%s\nmanual omission:\n%s",
			cliLockfile,
			manualLockfile,
		)
	}
	for _, result := range []struct {
		label string
		path  string
	}{
		{label: "remove", path: cliManifestPath},
		{label: "manual omission", path: manualManifestPath},
	} {
		normalized, err := declarationmanifest.Decode(testkit.ReadFile(t, result.path))
		if err != nil {
			t.Fatalf("%s declarationmanifest.Decode returned error: %v", result.label, err)
		}
		if len(normalized.Extensions()) != 0 {
			t.Fatalf("%s extensions = %#v, want desired absence", result.label, normalized.Extensions())
		}
	}
}

func TestRunRemoveExtensionFiltersAreAssertionsWithoutDefaults(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantExit int
		want     string
	}{
		{
			name:     "matching target only",
			args:     []string{"--target", "opencode"},
			wantExit: 0,
		},
		{
			name:     "matching scope only",
			args:     []string{"--scope", "project"},
			wantExit: 0,
		},
		{
			name:     "repeated matching target normalizes once",
			args:     []string{"--target", "opencode", "--target", "opencode"},
			wantExit: 0,
		},
		{
			name:     "repeated matching scope normalizes once",
			args:     []string{"--scope", "project", "--scope", "project"},
			wantExit: 0,
		},
		{
			name:     "mismatching target",
			args:     []string{"--target", "pi"},
			wantExit: 1,
			want:     `extension resource "formatter" not found`,
		},
		{
			name:     "mismatching scope",
			args:     []string{"--scope", "global"},
			wantExit: 1,
			want:     `extension resource "formatter" not found`,
		},
		{
			name:     "multiple targets",
			args:     []string{"--target", "opencode", "--target", "pi"},
			wantExit: 1,
			want:     "extension removal accepts at most one --target filter",
		},
		{
			name:     "host scope vocabulary",
			args:     []string{"--scope", "user"},
			wantExit: 2,
			want:     `unknown scope "user"`,
		},
		{
			name:     "source filter is not admitted",
			args:     []string{"--host-source", "@acme/formatter"},
			wantExit: 2,
			want:     "flag provided but not defined: -host-source",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			manifestPath := filepath.Join(root, "daem.toml")
			lockfilePath := filepath.Join(root, "daem.lock.toml")
			manifest := extensionAuthoringRowManifest("formatter", "opencode", "opencode-plugin", "project", "host_source", "@acme/formatter")
			testkit.WriteFile(t, root, "daem.toml", manifest)
			runExtensionAuthoringLock(t, manifestPath, lockfilePath)

			args := []string{"remove", "extension", "formatter", "--manifest", manifestPath}
			args = append(args, test.args...)
			args = append(args, "--dry-run", "--diff")
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr)
			if exitCode != test.wantExit {
				t.Fatalf("exitCode=%d stdout=%q stderr=%q, want %d", exitCode, stdout.String(), stderr.String(), test.wantExit)
			}
			if test.want != "" && !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), test.want)
			}
			if test.wantExit == 0 && (!strings.Contains(stdout.String(), "manifest diff:") || !strings.Contains(stdout.String(), "remove extension resource")) {
				t.Fatalf("stdout = %q, want dry-run diff", stdout.String())
			}
			testkit.AssertFileContent(t, manifestPath, manifest)
		})
	}
}

func TestRunRemoveExtensionRejectsDuplicateIDAsInvalidManifest(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	manifest := extensionAuthoringRowManifest("duplicate", "claude-code", "claude-code-plugin", "project", "marketplace", "plugin@market") + `
[[extension]]
id = "duplicate"
carrier = "opencode-plugin"
targets = ["opencode"]
scope = "project"
source = { host_source = "@acme/plugin" }
`
	testkit.WriteFile(t, root, "daem.toml", manifest)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"remove", "extension", "duplicate", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 || !strings.Contains(stderr.String(), `duplicate extension id "duplicate"`) {
		t.Fatalf("exitCode=%d stdout=%q stderr=%q, want duplicate-id manifest error", exitCode, stdout.String(), stderr.String())
	}
	testkit.AssertFileContent(t, manifestPath, manifest)
}

func TestRunRemoveExtensionLeavesFilesUnchangedWhenProspectiveLockFails(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	lockfilePath := filepath.Join(root, "daem.lock.toml")
	originalManifest := extensionAuthoringRowManifest("formatter", "opencode", "opencode-plugin", "project", "host_source", "@acme/formatter") + `
[instructions.project]
source = "missing.md"
targets = ["opencode"]
`
	originalLockfile := "version = 1\n# keep me\n"
	testkit.WriteFile(t, root, "daem.toml", originalManifest)
	testkit.WriteFile(t, root, "daem.lock.toml", originalLockfile)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"remove", "extension", "formatter",
		"--manifest", manifestPath,
	}, &stdout, &stderr)
	if exitCode != 1 || !strings.Contains(stderr.String(), "remove failed: lock prospective manifest") {
		t.Fatalf("exitCode=%d stdout=%q stderr=%q, want prospective lock failure", exitCode, stdout.String(), stderr.String())
	}
	testkit.AssertFileContent(t, manifestPath, originalManifest)
	testkit.AssertFileContent(t, lockfilePath, originalLockfile)
}

func extensionAuthoringRowManifest(id string, target string, carrier string, scope string, sourceField string, sourceRef string) string {
	return fmt.Sprintf(`version = 1
targets = [%q]

[[extension]]
id = %q
carrier = %q
targets = [%q]
scope = %q
source = { %s = %q }
`, target, id, carrier, target, scope, sourceField, sourceRef)
}

func runExtensionAuthoringLock(t *testing.T, manifestPath string, lockfilePath string) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("lock exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
}
