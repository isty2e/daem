package cli_test

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	daempaths "github.com/isty2e/daem/internal/paths"
	recoverworkflow "github.com/isty2e/daem/internal/workflow/recover"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestClaudeGlobalHookRecoveryUsesTheOriginalProfile(t *testing.T) {
	for _, tc := range []struct {
		boundary string
		existing bool
	}{
		{"ordinary-before-state", false},
		{"ordinary-after-state", false},
		{"ordinary-before-state", true},
		{"host", false}, // Native keys and hooks in one settings.json.
	} {
		name := tc.boundary
		if tc.existing {
			name += "-update"
		}
		t.Run(name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			nativefixture.SetRecoveryEnvironment(t, root)
			workspace := filepath.Join(root, "workspace")
			profile := os.Getenv("CLAUDE_CONFIG_DIR")
			destination := filepath.Join(profile, "settings.json")
			manifest := filepath.Join(workspace, "daem.toml")
			fixture := nativefixture.RecoveryFixture{Manifest: manifest, Destination: destination}
			testkit.WriteFile(t, profile, "settings.json", `{"outside":true}`)
			if err := os.Chmod(destination, 0o644); err != nil {
				t.Fatal(err)
			}
			hook := claudeStopHook{"notify", "echo original"}
			var beforeHooks []claudeStopHook
			if tc.existing {
				writeClaudeHookKeyManifest(t, workspace, "global", "", hook)
				nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
				nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
				beforeHooks = []claudeStopHook{hook}
			}
			model := ""
			if tc.boundary == "host" {
				model = "example"
			}
			hook.command = "echo changed"
			writeClaudeHookKeyManifest(t, workspace, "global", model, hook)
			nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
			code, out, stderr := nativefixture.InvokeRecoveryCommand(t, fixture, tc.boundary, "apply", "--yes")
			if code != nativefixture.RecoveryExit {
				t.Fatalf("did not interrupt the apply at %s: %d %s %s", tc.boundary, code, out, stderr)
			}
			partial := testkit.ReadFile(t, destination)
			requireClaudeHookKeyValues(t, partial, model, hook)
			nativefixture.RequireSharedMode(t, destination, 0o644)
			if err := os.Chmod(destination, 0o740); err != nil {
				t.Fatal(err)
			}

			other := filepath.Join(root, "other-profile")
			t.Setenv("CLAUDE_CONFIG_DIR", other)
			const untouched = `{"outside":"other"}`
			testkit.WriteFile(t, other, "settings.json", untouched)
			if !tc.existing && tc.boundary != "ordinary-after-state" {
				code, out, stderr := testkit.RunCapturedCLI("recover", "--manifest", manifest, "--yes")
				if code == 0 {
					t.Fatalf("an unbound first write recovered under a different profile: %s %s", out, stderr)
				}
				testkit.AssertFileContent(t, destination, string(partial))
				t.Setenv("CLAUDE_CONFIG_DIR", profile)
			}
			plan, err := recoverworkflow.Plan(t.Context(), recoverworkflow.PlanInput{ManifestPath: manifest})
			if err != nil {
				t.Fatal(err)
			}
			defer plan.Close()
			if err := os.Chmod(destination, 0o640); err != nil {
				t.Fatal(err)
			}
			if _, err := recoverworkflow.Execute(t.Context(), plan, recoverworkflow.ExecuteOptions{}); err != nil {
				t.Fatalf("recovery refused the same settings file after chmod: %v", err)
			}
			nativefixture.RequireSharedMode(t, destination, 0o640)
			if tc.boundary == "ordinary-after-state" {
				testkit.AssertFileContent(t, destination, string(partial))
			} else {
				requireClaudeHookKeyValues(t, testkit.ReadFile(t, destination), "", beforeHooks...)
			}
			testkit.AssertFileContent(t, filepath.Join(other, "settings.json"), untouched)
			testkit.AssertNoRecoveryArtifacts(t, workspace)
		})
	}
}

func TestFinalClaudeHookRetirementRecoveryUsesTheOriginalProfile(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	nativefixture.SetRecoveryEnvironment(t, root)
	workspace := filepath.Join(root, "workspace")
	manifest := filepath.Join(workspace, "daem.toml")
	profile := os.Getenv("CLAUDE_CONFIG_DIR")
	destination := filepath.Join(profile, "settings.json")
	testkit.WriteFile(t, profile, "settings.json", `{"outside":true}`)
	writeClaudeHookKeyManifest(t, workspace, "global", "", claudeStopHook{"notify", "echo retiring"})
	nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
	writeClaudeHookKeyManifest(t, workspace, "global", "")
	nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)

	other := filepath.Join(root, "other-profile")
	const untouched = `{"outside":"other"}`
	testkit.WriteFile(t, other, "settings.json", untouched)
	t.Setenv("CLAUDE_CONFIG_DIR", other)
	fixture := nativefixture.RecoveryFixture{Manifest: manifest, Destination: destination}
	code, out, stderr := nativefixture.InvokeRecoveryCommand(t, fixture, "ordinary-after-state", "apply", "--yes")
	if code != nativefixture.RecoveryExit {
		t.Fatalf("retirement did not reach the state boundary: %d %s %s", code, out, stderr)
	}
	retired := testkit.ReadFile(t, destination)
	requireClaudeHookKeyValues(t, retired, "")
	code, _, stderr = testkit.RunCapturedCLI("recover", "--manifest", manifest, "--yes")
	if code == 0 || !strings.Contains(stderr, "CLAUDE_CONFIG_DIR") || !strings.Contains(stderr, profile) {
		t.Fatalf("wrong-profile recovery did not explain how to retry: %d %s", code, stderr)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", profile)
	nativefixture.RunLifecycleCommand(t, "recover", "--manifest", manifest, "--yes")
	testkit.AssertFileContent(t, destination, string(retired))
	testkit.AssertFileContent(t, filepath.Join(other, "settings.json"), untouched)
	testkit.AssertNoRecoveryArtifacts(t, workspace)
}

func TestClaudeHookRecoveryAcrossDocumentCreationAndDeletion(t *testing.T) {
	for _, tc := range []struct {
		name, boundary string
		retire, addKey bool
	}{
		{"undo creation after chmod", "ordinary-before-state", false, false},
		{"undo final removal", "ordinary-before-state", true, false},
		{"undo final removal and separate key installation", "before-state", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			nativefixture.SetRecoveryEnvironment(t, root)
			workspace := filepath.Join(root, "workspace")
			manifest := filepath.Join(workspace, "daem.toml")
			destination := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "settings.json")
			hook := claudeStopHook{"notify", "echo notify"}
			writeClaudeHookKeyManifest(t, workspace, "global", "", hook)
			nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
			if tc.retire {
				nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
				if err := os.Chmod(destination, 0o644); err != nil {
					t.Fatal(err)
				}
				if tc.addKey {
					writeClaudeHookKeyManifest(t, workspace, "project", "example")
				} else {
					writeClaudeHookKeyManifest(t, workspace, "global", "")
				}
				nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
			}
			fixture := nativefixture.RecoveryFixture{Manifest: manifest, Destination: destination}
			code, out, stderr := nativefixture.InvokeRecoveryCommand(t, fixture, tc.boundary, "apply", "--yes")
			if code != nativefixture.RecoveryExit {
				t.Fatalf("apply did not reach state boundary: %d %s %s", code, out, stderr)
			}
			if tc.retire {
				if _, err := os.Lstat(destination); !os.IsNotExist(err) {
					t.Fatalf("last hook did not remove the empty settings file: %v", err)
				}
			} else if err := os.Chmod(destination, 0o744); err != nil {
				t.Fatal(err)
			}
			keyDocument := filepath.Join(workspace, ".claude", "settings.json")
			if tc.addKey {
				requireClaudeHookKeyValues(t, testkit.ReadFile(t, keyDocument), "example")
			}
			nativefixture.RunLifecycleCommand(t, "recover", "--manifest", manifest, "--yes")
			if tc.retire {
				requireClaudeHookKeyValues(t, testkit.ReadFile(t, destination), "", hook)
				nativefixture.RequireSharedMode(t, destination, 0o644)
			} else if _, err := os.Lstat(destination); !os.IsNotExist(err) {
				t.Fatalf("rollback did not undo document creation: %v", err)
			}
			if tc.addKey {
				if _, err := os.Lstat(keyDocument); !os.IsNotExist(err) {
					t.Fatalf("rollback did not remove the new key document: %v", err)
				}
			}
			testkit.AssertNoRecoveryArtifacts(t, workspace)
		})
	}
}

func TestClaudeHookAndKeyRecoverTogether(t *testing.T) {
	for _, tc := range []struct {
		name, boundary string
		update         bool
	}{
		{"host", "host", false},
		{"state", "state", false},
		{"update with retained hook", "host", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			nativefixture.SetRecoveryEnvironment(t, root)
			workspace := filepath.Join(root, "workspace")
			manifest := filepath.Join(workspace, "daem.toml")
			paths, err := daempaths.Resolve(manifest)
			if err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(workspace, ".claude", "settings.json")
			testkit.WriteFile(t, workspace, ".claude/settings.json", `{"outside":"before"}`)
			fixture := nativefixture.RecoveryFixture{Manifest: manifest, Destination: destination, Paths: paths}
			var beforeModel string
			var beforeHooks []claudeStopHook
			afterHooks := []claudeStopHook{{"first", "echo first"}}
			if tc.update {
				second := claudeStopHook{"second", "echo retained"}
				beforeModel = "original"
				beforeHooks = []claudeStopHook{{"first", "echo original"}, second}
				afterHooks = append(afterHooks, second)
				writeClaudeHookKeyManifest(t, workspace, "project", beforeModel, beforeHooks...)
				nativefixture.RunRecoveryCommand(t, fixture, "none", "lock")
				nativefixture.RunRecoveryCommand(t, fixture, "none", "apply", "--yes")
				requireClaudeHookKeyValues(t, testkit.ReadFile(t, destination), beforeModel, beforeHooks...)
			}

			writeClaudeHookKeyManifest(t, workspace, "project", "example", afterHooks...)
			nativefixture.RunRecoveryCommand(t, fixture, "none", "lock")
			code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture, tc.boundary, "apply", "--yes", "--verbose")
			if code != nativefixture.RecoveryExit || !strings.Contains(stderr, nativefixture.RecoveryReceipt(destination)) {
				t.Fatalf("apply did not reach %s publication: %d %s %s", tc.boundary, code, stdout, stderr)
			}
			nativefixture.RequireRecoveryJournalState(t, fixture, tc.boundary)

			after := testkit.ReadFile(t, destination)
			requireClaudeHookKeyValues(t, after, "example", afterHooks...)
			later := strings.Replace(string(after), `"before"`, `"later"`, 1)
			if tc.boundary == "host" {
				third := strings.Replace(later, afterHooks[len(afterHooks)-1].command, "echo third", 1)
				testkit.WriteFile(t, workspace, ".claude/settings.json", third)
				before := nativefixture.SnapshotRecoveryEvidence(t, fixture)
				code, out, _ := nativefixture.InvokeRecoveryCommand(t, fixture, "none", "recover", "--yes", "--json")
				if code == 0 || clijson.DecodeRecover(t, []byte(out)).Classification != "blocked" || !maps.Equal(before, nativefixture.SnapshotRecoveryEvidence(t, fixture)) {
					t.Fatal("recovery did not preserve the third-party hook edit and its journal", out)
				}
			}

			testkit.WriteFile(t, workspace, ".claude/settings.json", later)
			nativefixture.RunRecoveryCommand(t, fixture, "none", "recover", "--yes")
			testkit.AssertNoRecoveryArtifacts(t, workspace)
			result := testkit.ReadFile(t, destination)
			nativefixture.RequireSharedValues(t, configdocument.JSON, result, `{"outside":"later"}`, true)
			if tc.boundary == "host" {
				requireClaudeHookKeyValues(t, result, beforeModel, beforeHooks...)
			} else if !bytes.Equal(result, []byte(later)) {
				t.Fatal("finalization rewrote the committed host settings")
			}
		})
	}
}
