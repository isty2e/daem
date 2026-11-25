package cli_test

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestFreshClaudeGlobalHooksUseSelectedProfile(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	nativefixture.SetRecoveryEnvironment(t, root)
	workspace := filepath.Join(root, "workspace")
	manifest := filepath.Join(workspace, "daem.toml")
	hook := claudeStopHook{"notify", "echo done"}
	writeClaudeHookKeyManifest(t, workspace, "global", "", hook)

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	const personalSettings = `{"model":"personal"}`
	testkit.WriteFile(t, home, ".claude/settings.json", personalSettings)
	profile := os.Getenv("CLAUDE_CONFIG_DIR")
	selected := filepath.Join(profile, "settings.json")
	testkit.WriteFile(t, profile, "settings.json", `{"unmanaged":true}`)
	nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
	preview := nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--dry-run")
	if !strings.Contains(preview, selected) {
		t.Fatal("preview omitted the selected settings file:", preview)
	}
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
	requireClaudeHookKeyValues(t, testkit.ReadFile(t, selected), "", hook)

	otherProfile := filepath.Join(root, "other")
	t.Setenv("CLAUDE_CONFIG_DIR", otherProfile)
	const otherSettings = `{"unmanaged":"other"}`
	other := filepath.Join(otherProfile, "settings.json")
	testkit.WriteFile(t, otherProfile, "settings.json", otherSettings)
	hook.command = "echo updated"
	second := claudeStopHook{"other", "echo second"}
	writeClaudeHookKeyManifest(t, workspace, "global", "", hook, second)
	nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
	plan := clijson.DecodePlan(t, []byte(nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--dry-run", "--json")))
	diagnostic := clijson.FindPlanDiagnostic(t, plan, "claude_global_hook_settings", "ok", "hook/notify", "claude-code")
	if !strings.Contains(diagnostic.Detail, selected) {
		t.Fatalf("plan selected the wrong settings file: %s", diagnostic.Detail)
	}
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
	requireClaudeHookKeyValues(t, testkit.ReadFile(t, selected), "", hook, second)
	testkit.AssertFileContent(t, other, otherSettings)
	nativefixture.RequireLifecycleClean(t, nativefixture.RunLifecycleCommand(t, "status", "--manifest", manifest, "--check", "--json"))

	// Retiring the last hook frees the next installation to choose its own profile.
	writeClaudeHookKeyManifest(t, workspace, "global", "")
	nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
	retired := testkit.ReadFile(t, selected)
	requireClaudeHookKeyValues(t, retired, "")
	nativefixture.RequireSharedValues(t, configdocument.JSON, retired, `{"unmanaged":true}`, true)
	writeClaudeHookKeyManifest(t, workspace, "global", "", second)
	nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
	requireClaudeHookKeyValues(t, testkit.ReadFile(t, other), "", second)
	testkit.AssertFileContent(t, selected, string(retired))
	testkit.AssertFileContent(t, filepath.Join(home, ".claude", "settings.json"), personalSettings)
}

func TestHistoricalClaudeGlobalHooksKeepTheirLiteralHome(t *testing.T) {
	root := t.TempDir()
	nativefixture.SetRecoveryEnvironment(t, root)
	workspace := filepath.Join(root, "workspace")
	manifest := filepath.Join(workspace, "daem.toml")
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	personal := filepath.Join(homeDir, ".claude", "settings.json")
	workDir := os.Getenv("CLAUDE_CONFIG_DIR")
	work := filepath.Join(workDir, "settings.json")
	const other = `{"unmanaged":"work"}`
	testkit.WriteFile(t, workDir, "settings.json", other)
	hook := claudeStopHook{"notify", "echo original"}
	writeClaudeHookKeyManifest(t, workspace, "global", "", hook)
	nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
	testkit.WriteFile(t, filepath.Dir(personal), "settings.json", `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo original"}]}]}}`)
	testkit.WriteHookAggregateStateFromLock(t, workspace) // Pre-profile baseline with no saved settings location.
	testkit.WriteActiveOwnershipClaim(t, manifest, "~/.claude/settings.json", "/hooks")

	hook.command = "echo updated"
	second := claudeStopHook{"second", "echo second"}
	writeClaudeHookKeyManifest(t, workspace, "global", "", hook, second)
	nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
	installed := testkit.ReadFile(t, personal)
	requireClaudeHookKeyValues(t, installed, "", hook, second)
	testkit.AssertFileContent(t, work, other)

	writeClaudeHookKeyManifest(t, workspace, "global", "example", hook, second)
	nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
	if code, stdout, stderr := testkit.RunCapturedCLI("apply", "--manifest", manifest, "--yes"); code != 1 {
		t.Fatalf("mixed changes to two settings files were not refused: %d %s %s", code, stdout, stderr)
	}
	testkit.AssertFileContent(t, personal, string(installed))
	testkit.AssertFileContent(t, work, other)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Dir(personal))
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
	requireClaudeHookKeyValues(t, testkit.ReadFile(t, personal), "example", hook, second)
}

func TestClaudeHookAndKeyShareSettings(t *testing.T) {
	first := claudeStopHook{"first", "echo first"}
	second := claudeStopHook{"second", "echo second"}
	updated := claudeStopHook{"first", "echo updated"}
	type step struct {
		model string
		hooks []claudeStopHook
	}
	installBoth := step{"example", []claudeStopHook{first, second}}
	onlySecond := []claudeStopHook{second}
	for _, tc := range []struct {
		name            string
		existing        bool
		profileOverride bool
		steps           []step
	}{
		{"absent", false, true, []step{installBoth}},
		{"existing", true, false, []step{
			installBoth,
			{"example", []claudeStopHook{updated, second}},
			{"example", onlySecond},
		}},
		{"key first", true, true, []step{
			{"example", nil},
			{"example", onlySecond},
			{"changed", onlySecond},
			{"changed", nil},
			{"changed", onlySecond},
			{"", onlySecond},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			nativefixture.SetRecoveryEnvironment(t, root)
			claudeHome := os.Getenv("CLAUDE_CONFIG_DIR")
			if !tc.profileOverride {
				if err := os.Unsetenv("CLAUDE_CONFIG_DIR"); err != nil {
					t.Fatal(err)
				}
				claudeHome = filepath.Join(os.Getenv("HOME"), ".claude")
			}
			workspace := filepath.Join(root, "workspace")
			manifest := filepath.Join(workspace, "daem.toml")
			destination := filepath.Join(claudeHome, "settings.json")
			const outside = `{"unmanaged":42}`
			if tc.existing {
				testkit.WriteFile(t, claudeHome, "settings.json", outside)
			}

			for _, step := range tc.steps {
				t.Logf("applying model=%q hooks=%v", step.model, step.hooks)
				writeClaudeHookKeyManifest(t, workspace, "global", step.model, step.hooks...)
				nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
				nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--dry-run", "--verbose")
				nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes", "--verbose")

				content := testkit.ReadFile(t, destination)
				requireClaudeHookKeyValues(t, content, step.model, step.hooks...)
				nativefixture.RequireSharedValues(t, configdocument.JSON, content, outside, tc.existing)
				nativefixture.RequireLifecycleClean(t, nativefixture.RunLifecycleCommand(t, "status", "--manifest", manifest, "--check", "--json"))
				preview := nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--dry-run", "--json")
				plan := clijson.DecodePlan(t, []byte(preview))
				wantActions := len(step.hooks)
				if step.model != "" {
					wantActions++
				}
				if len(plan.Actions) != wantActions {
					t.Fatal("repeat preview lost a managed subject", preview)
				}
				for _, action := range plan.Actions {
					if action.Kind != "noop" {
						t.Fatal("repeat preview still plans changes", preview)
					}
				}
			}
		})
	}
}

func TestClaudeHookDriftBlocksAnUnrelatedKeyUpdate(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	nativefixture.SetRecoveryEnvironment(t, root)
	workspace := filepath.Join(root, "workspace")
	manifest := filepath.Join(workspace, "daem.toml")
	hooks := []claudeStopHook{{"first", "echo first"}, {"second", "echo second"}}
	writeClaudeHookKeyManifest(t, workspace, "project", "old", hooks...)
	nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")

	writeClaudeHookKeyManifest(t, workspace, "project", "new", hooks...)
	nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
	paths, err := daempaths.Resolve(manifest)
	if err != nil {
		t.Fatal(err)
	}
	fixture := nativefixture.RecoveryFixture{Paths: paths, Destination: filepath.Join(workspace, ".claude", "settings.json")}
	original := testkit.ReadFile(t, fixture.Destination)
	requireClaudeHookKeyValues(t, original, "old", hooks...)

	for _, tc := range []struct{ name, liveHooks string }{
		{"reordered", `{"Stop":[{"hooks":[{"type":"command","command":"echo second"}]},{"hooks":[{"type":"command","command":"echo first"}]}]}`},
		{"invalid shape", `null`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testkit.WriteFile(t, workspace, ".claude/settings.json", `{"model":"old","hooks":`+tc.liveHooks+`}`)
			before := nativefixture.SnapshotRecoveryEvidence(t, fixture)
			code, stdout, stderr := testkit.RunCapturedCLI("apply", "--manifest", manifest, "--yes", "--json")
			if code != 1 || !clijson.DecodeApplyResult(t, []byte(stdout)).HasErrors {
				t.Fatalf("apply did not refuse the changed hook projection: %d %s %s", code, stdout, stderr)
			}
			if !maps.Equal(before, nativefixture.SnapshotRecoveryEvidence(t, fixture)) {
				t.Fatal("refused key update changed host settings, state, ownership, or recovery files")
			}
		})
	}

	testkit.WriteFile(t, workspace, ".claude/settings.json", string(original))
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
	requireClaudeHookKeyValues(t, testkit.ReadFile(t, fixture.Destination), "new", hooks...)
}

type claudeStopHook struct{ name, command string }

func requireClaudeHookKeyValues(t *testing.T, content []byte, model string, hooks ...claudeStopHook) {
	t.Helper()
	nativefixture.RequireSharedValues(t, configdocument.JSON, content, fmt.Sprintf(`{"model":%q}`, model), model != "")
	if len(hooks) == 0 {
		nativefixture.RequireSharedValues(t, configdocument.JSON, content, `{"hooks":{}}`, false)
		return
	}
	var groups []string
	for _, hook := range hooks {
		groups = append(groups, fmt.Sprintf(`{"hooks":[{"type":"command","command":%q}]}`, hook.command))
	}
	nativefixture.RequireSharedValues(t, configdocument.JSON, content, `{"hooks":{"Stop":[`+strings.Join(groups, ",")+`]}}`, true)
}

func writeClaudeHookKeyManifest(t *testing.T, workspace, scope, model string, hooks ...claudeStopHook) {
	t.Helper()
	manifest := `version = 1
targets = ["claude-code"]
`
	if model != "" {
		source := "./preferences.json"
		if scope == "global" {
			source = filepath.Join(workspace, "preferences.json")
		}
		manifest += fmt.Sprintf(`
[[agent_config]]
id = "preferences"
scope = %q
path = "settings.json"
source = %q
`, scope, source)
		testkit.WriteFile(t, workspace, "preferences.json", fmt.Sprintf(`{"model":%q}`, model))
	}
	for _, hook := range hooks {
		manifest += fmt.Sprintf(`
[[hook]]
name = %q
scope = %q
event = "Stop"
command = %q
`, hook.name, scope, hook.command)
	}
	testkit.WriteFile(t, workspace, "daem.toml", manifest)
}
