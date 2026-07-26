package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/test/testkit"
)

func TestHookAssetPublicCLILifecycleForCodexAndClaude(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	scriptContent := "#!/bin/sh\necho guard\n"
	assetHash := hookAssetHash(scriptContent, true)
	assetDestination := hookAssetDestination(tempDir, "guard", assetHash)

	testkit.WriteFile(t, tempDir, "hooks/guard.sh", scriptContent)
	testkit.WriteFile(t, tempDir, "daem.toml", hookAssetManifest("{hook_file:guard} --check"))

	runHookAssetCLI(t, []string{"lock", "--manifest", manifestPath}, 0)
	lockfileContent, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatalf("ReadFile lockfile returned error: %v", err)
	}
	for _, want := range []string{
		"[[locked.subject]]",
		`entity_id = "hook_asset:guard"`,
		`subject_id = "resource/hook_asset/guard"`,
		`source_id = "local:hooks/guard.sh?mode=vendor"`,
		`content_hash = "` + assetHash + `"`,
		`[locked.subject.exact_file_use]`,
		"executable = true",
		`permission_policy = "exact"`,
		"exact_permission_mode = 448",
	} {
		if !strings.Contains(string(lockfileContent), want) {
			t.Fatalf("lockfile = %q, want %q", lockfileContent, want)
		}
	}

	stdout, _ := runHookAssetCLI(t, []string{"apply", "--manifest", manifestPath, "--yes"}, 0)
	if strings.Contains(stdout, "hook.command.lookup_ambiguous") {
		t.Fatalf("stdout = %q, want no PATH lookup diagnostic for hook_file placeholder", stdout)
	}
	if !strings.Contains(stdout, "applied: 3 actions") {
		t.Fatalf("stdout = %q, want asset plus Codex/Claude hook aggregate actions", stdout)
	}
	testkit.AssertContainsInOrder(
		t, stdout,
		`create resource="hook_asset/guard"`,
		`create resource="hook/protect" target=codex`,
		`create resource="hook/protect" target=claude-code`,
	)
	testkit.AssertFileContent(t, assetDestination, scriptContent)
	assertFileMode(t, assetDestination, 0o700)
	for _, hostFile := range []string{".codex/hooks.json", ".claude/settings.json"} {
		content, err := os.ReadFile(filepath.Join(tempDir, hostFile))
		if err != nil {
			t.Fatalf("ReadFile %s returned error: %v", hostFile, err)
		}
		if !strings.Contains(string(content), assetDestination+" --check") ||
			strings.Contains(string(content), "{hook_file:guard}") {
			t.Fatalf("%s = %q, want rendered asset path and no placeholder", hostFile, content)
		}
	}
	state := loadHookAssetState(t, tempDir)
	assertHookAssetStateExactMode(t, state, hookAssetStatePath("guard", assetHash), 0o700)
	testkit.AssertManagedPathState(
		t, state, entity.KindHookAsset, "guard", []string{"claude-code", "codex"},
		"project", hookAssetStatePath("guard", assetHash), assetHash, "file",
	)

	statusStdout, _ := runHookAssetCLI(t, []string{"status", "--manifest", manifestPath}, 0)
	for _, want := range []string{
		`noop resource="hook_asset/guard"`,
		`noop resource="hook/protect" target=codex`,
		`noop resource="hook/protect" target=claude-code`,
	} {
		if !strings.Contains(statusStdout, want) {
			t.Fatalf("status stdout = %q, want %q", statusStdout, want)
		}
	}

	updatedScriptContent := "#!/bin/sh\necho updated guard\n"
	updatedAssetHash := hookAssetHash(updatedScriptContent, true)
	updatedAssetDestination := hookAssetDestination(tempDir, "guard", updatedAssetHash)
	testkit.WriteFile(t, tempDir, "hooks/guard.sh", updatedScriptContent)
	testkit.WriteHookManifestAndLock(t, tempDir, hookAssetManifest("{hook_file:guard} --check"))
	updateStdout, _ := runHookAssetCLI(t, []string{"apply", "--manifest", manifestPath, "--yes"}, 0)
	for _, want := range []string{
		`update resource="hook_asset/guard"`,
		`update resource="hook/protect" target=codex`,
		`update resource="hook/protect" target=claude-code`,
	} {
		if !strings.Contains(updateStdout, want) {
			t.Fatalf("update stdout = %q, want %q", updateStdout, want)
		}
	}
	testkit.AssertContainsInOrder(
		t, updateStdout,
		`update resource="hook_asset/guard"`,
		`update resource="hook/protect" target=codex`,
		`update resource="hook/protect" target=claude-code`,
	)
	testkit.AssertFileContent(t, updatedAssetDestination, updatedScriptContent)
	assertFileMode(t, updatedAssetDestination, 0o700)
	if _, err := os.Stat(assetDestination); !os.IsNotExist(err) {
		t.Fatalf("old asset destination stat err = %v, want retired path", err)
	}
	for _, hostFile := range []string{".codex/hooks.json", ".claude/settings.json"} {
		content, err := os.ReadFile(filepath.Join(tempDir, hostFile))
		if err != nil {
			t.Fatalf("ReadFile updated %s returned error: %v", hostFile, err)
		}
		if !strings.Contains(string(content), updatedAssetDestination+" --check") ||
			strings.Contains(string(content), assetDestination+" --check") {
			t.Fatalf("updated %s = %q, want only new asset path", hostFile, content)
		}
	}
	state = loadHookAssetState(t, tempDir)
	testkit.AssertManagedPathStateMissing(t, state, entity.KindHookAsset, "guard", "project", hookAssetStatePath("guard", assetHash))
	testkit.AssertManagedPathState(
		t, state, entity.KindHookAsset, "guard", []string{"claude-code", "codex"},
		"project", hookAssetStatePath("guard", updatedAssetHash), updatedAssetHash, "file",
	)

	testkit.WriteHookManifestAndLock(t, tempDir, hookAssetManifest("echo done"))
	removeStdout, _ := runHookAssetCLI(t, []string{"apply", "--manifest", manifestPath, "--yes"}, 0)
	for _, want := range []string{
		`delete resource="hook_asset/guard"`,
		`update resource="hook/protect" target=codex`,
		`update resource="hook/protect" target=claude-code`,
	} {
		if !strings.Contains(removeStdout, want) {
			t.Fatalf("remove stdout = %q, want %q", removeStdout, want)
		}
	}
	testkit.AssertContainsInOrder(
		t, removeStdout,
		`update resource="hook/protect" target=codex`,
		`update resource="hook/protect" target=claude-code`,
		`delete resource="hook_asset/guard"`,
	)
	if _, err := os.Stat(updatedAssetDestination); !os.IsNotExist(err) {
		t.Fatalf("asset destination stat err = %v, want deleted asset", err)
	}
	state = loadHookAssetState(t, tempDir)
	testkit.AssertManagedPathStateMissing(t, state, entity.KindHookAsset, "guard", "project", hookAssetStatePath("guard", updatedAssetHash))
}

func TestHookAssetPublicCLIManageExistingRequiresExactMode(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	scriptContent := "#!/bin/sh\necho guard\n"
	assetHash := hookAssetHash(scriptContent, true)
	assetDestination := hookAssetDestination(tempDir, "guard", assetHash)

	testkit.WriteFile(t, tempDir, "hooks/guard.sh", scriptContent)
	testkit.WriteFile(t, tempDir, "daem.toml", hookAssetManifest("{hook_file:guard} --check"))
	runHookAssetCLI(t, []string{"lock", "--manifest", manifestPath}, 0)
	testkit.WriteFile(t, tempDir, hookAssetStatePath("guard", assetHash), scriptContent)
	if err := os.Chmod(assetDestination, 0o755); err != nil {
		t.Fatalf("Chmod returned error: %v", err)
	}

	stdout, stderr := runHookAssetCLI(t, []string{"apply", "--manifest", manifestPath, "--manage-existing", "--yes"}, 1)
	if !strings.Contains(stdout+stderr, "unmanaged_output_exists") ||
		!strings.Contains(stdout+stderr, "different file mode") {
		t.Fatalf("stdout=%q stderr=%q, want mode mismatch unmanaged diagnostic", stdout, stderr)
	}
	testkit.AssertPathMissing(t, filepath.Join(tempDir, ".codex/hooks.json"))
	testkit.AssertPathMissing(t, filepath.Join(tempDir, ".daem/state.json"))
	assertFileMode(t, assetDestination, 0o755)

	if err := os.Chmod(assetDestination, 0o700); err != nil {
		t.Fatalf("Chmod returned error: %v", err)
	}
	stdout, _ = runHookAssetCLI(t, []string{"apply", "--manifest", manifestPath, "--manage-existing", "--yes"}, 0)
	if !strings.Contains(stdout, `record resource="hook_asset/guard"`) ||
		!strings.Contains(stdout, "reason=managed_existing") {
		t.Fatalf("stdout = %q, want managed_existing record for exact content/mode asset", stdout)
	}
	state := loadHookAssetState(t, tempDir)
	testkit.AssertManagedPathState(
		t, state, entity.KindHookAsset, "guard", []string{"claude-code", "codex"},
		"project", hookAssetStatePath("guard", assetHash), assetHash, "file",
	)
}

func TestHookAssetPublicCLIRejectsInvalidReferencesBeforeMutation(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		manifest string
		want     string
	}{
		{
			name: "missing asset declaration",
			manifest: `
version = 1
targets = ["codex"]

[[hook]]
name = "protect"
event = "Stop"
command = "{hook_file:guard} --check"
targets = ["codex"]
timeout = 5
`,
			want: `hook asset "guard" is not declared`,
		},
		{
			name: "cross scope reference",
			manifest: `
version = 1
targets = ["codex"]

[hook_asset.guard]
source = "hooks/guard.sh"
kind = "file"
scope = "project"
executable = true

[[hook]]
name = "protect"
event = "Stop"
command = "{hook_file:guard} --check"
targets = ["codex"]
scope = "global"
timeout = 5
`,
			want: `hook asset "guard" scope "project" does not match hook scope "global"`,
		},
		{
			name: "malformed path like id",
			manifest: `
version = 1
targets = ["codex"]

[hook_asset.guard]
source = "hooks/guard.sh"
kind = "file"
executable = true

[[hook]]
name = "protect"
event = "Stop"
command = "{hook_file:bad/path} --check"
targets = ["codex"]
timeout = 5
`,
			want: `malformed hook asset placeholder`,
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			tempDir := t.TempDir()
			manifestPath := filepath.Join(tempDir, "daem.toml")
			testkit.WriteFile(t, tempDir, "hooks/guard.sh", "#!/bin/sh\necho guard\n")
			testkit.WriteFile(t, tempDir, "daem.toml", scenario.manifest)
			stdout, stderr := runHookAssetCLI(t, []string{"lock", "--manifest", manifestPath}, 1)
			if !strings.Contains(stdout+stderr, scenario.want) {
				t.Fatalf("stdout=%q stderr=%q, want %q", stdout, stderr, scenario.want)
			}
			testkit.AssertPathMissing(t, filepath.Join(tempDir, ".codex/hooks.json"))
			testkit.AssertPathMissing(t, filepath.Join(tempDir, ".daem/state.json"))
			testkit.AssertNoRecoveryArtifacts(t, tempDir)
		})
	}
}

func TestHookAssetPublicCLIImportDoesNotInferAssetsFromCommandText(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	scriptPath := filepath.Join(tempDir, "hooks", "guard.sh")
	testkit.WriteFile(t, tempDir, "hooks/guard.sh", "#!/bin/sh\necho guard\n")
	testkit.WriteFile(t, tempDir, ".codex/hooks.json", `{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "`+filepath.ToSlash(scriptPath)+` --check"
          }
        ]
      }
    ]
  }
}
`)

	outputPath := filepath.Join(tempDir, "daem.imported.toml")
	stdout, _ := runHookAssetCLI(t, []string{"import", "--target", "codex", "--manifest", outputPath, "--dry-run", "--diff"}, 0)
	if strings.Contains(stdout, "[hook_asset.") || strings.Contains(stdout, "hook_file:") {
		t.Fatalf("stdout = %q, want import to preserve command text without inferring hook_asset", stdout)
	}
	if !strings.Contains(stdout, filepath.ToSlash(scriptPath)+" --check") {
		t.Fatalf("stdout = %q, want imported hook command text", stdout)
	}
	testkit.AssertPathMissing(t, outputPath)
}

func hookAssetManifest(command string) string {
	return `
version = 1
targets = ["codex", "claude-code"]

[hook_asset.guard]
source = "hooks/guard.sh"
kind = "file"
executable = true

[[hook]]
name = "protect"
event = "Stop"
command = "` + command + `"
targets = ["codex", "claude-code"]
timeout = 5
`
}

func hookAssetHash(content string, executable bool) string {
	return string(artifact.HashFileContentWithExecutable([]byte(content), executable))
}

func hookAssetDestination(root string, name string, contentHash string) string {
	return filepath.Join(root, hookAssetStatePath(name, contentHash))
}

func hookAssetStatePath(name string, contentHash string) string {
	return filepath.Join(".daem", "hook-assets", name, strings.Replace(contentHash, "sha256:", "sha256-", 1), "asset")
}

func loadHookAssetState(t *testing.T, root string) durable.Snapshot {
	t.Helper()

	state, err := statefile.Load(t.Context(), filepath.Join(root, ".daem", "state.json"))
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	return state
}

func assertHookAssetStateExactMode(
	t *testing.T,
	state durable.Snapshot,
	path string,
	want os.FileMode,
) {
	t.Helper()

	for _, managedPath := range state.ManagedPaths() {
		if string(managedPath.Destination()) != path {
			continue
		}
		if managedPath.PermissionPolicy() != realization.PathPermissionsExact || managedPath.FileMode() != want {
			t.Fatalf(
				"hook asset state exact mode = %04o policy %q, want %04o",
				managedPath.FileMode(),
				managedPath.PermissionPolicy(),
				want,
			)
		}
		return
	}
	t.Fatal("hook asset managed-path state is missing")
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat %q returned error: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode %q = %s, want %s", path, got, want)
	}
}

func runHookAssetCLI(t *testing.T, args []string, wantExit int) (string, string) {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr)
	if exitCode != wantExit {
		t.Fatalf("RunCLI(%v) exitCode = %d, want %d; stdout = %q stderr = %q", args, exitCode, wantExit, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String()
}
