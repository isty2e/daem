package cli_test

import (
	"encoding/json"
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
	const logical = "~/.claude/settings.json"
	if len(plan.Actions) != 2 {
		t.Fatalf("expected both hooks in the plan: %+v", plan.Actions)
	}
	for _, action := range plan.Actions {
		if action.Destination != logical || action.Projection == nil || action.Projection.ConfigPath != logical || action.ResolvedPath != selected {
			t.Fatalf("incorrect hook locations in plan: %+v", action)
		}
	}
	applied := clijson.DecodeApplyResult(t, []byte(nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes", "--json")))
	if len(applied.Actions) != len(plan.Actions) {
		t.Fatalf("apply reported %d hook actions after previewing %d", len(applied.Actions), len(plan.Actions))
	}
	for _, action := range applied.Actions {
		if action.ResolvedPath != selected {
			t.Fatalf("apply reported a different settings file than its preview: %+v", action)
		}
	}
	requireClaudeHookKeyValues(t, testkit.ReadFile(t, selected), "", hook, second)
	testkit.AssertFileContent(t, other, otherSettings)
	nativefixture.RequireLifecycleClean(t, nativefixture.RunLifecycleCommand(t, "status", "--manifest", manifest, "--check", "--json"))
	var inventory struct {
		Managed []struct {
			Path         string `json:"path"`
			ResolvedPath string `json:"resolved_path"`
		} `json:"managed"`
	}
	if err := json.Unmarshal([]byte(nativefixture.RunLifecycleCommand(t, "list", "outputs", "--manifest", manifest, "--json")), &inventory); err != nil || len(inventory.Managed) != 2 {
		t.Fatalf("expected two installed hooks: %+v, %v", inventory, err)
	}
	for _, row := range inventory.Managed {
		if row.Path != logical || row.ResolvedPath != selected {
			t.Fatalf("incorrect hook locations in inventory: %+v", row)
		}
	}
	t.Run("another manifest sees the owner conflict in the same file", func(t *testing.T) {
		t.Setenv("CLAUDE_CONFIG_DIR", profile)
		workspace := filepath.Join(root, "second-workspace")
		manifest := filepath.Join(workspace, "daem.toml")
		writeClaudeHookKeyManifest(t, workspace, "global", "", hook)
		nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
		inventory := nativefixture.RunLifecycleCommand(t, "list", "outputs", "--manifest", manifest)
		if !strings.Contains(inventory, "ownership_conflict") || !strings.Contains(inventory, logical) || !strings.Contains(inventory, selected) {
			t.Fatalf("blocked hook did not identify its settings file: %s", inventory)
		}
	})

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
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
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
	for _, invocation := range []string{"apply --yes", "apply --dry-run", "status", "list outputs"} {
		command, _, _ := strings.Cut(invocation, " --")
		for _, machine := range []bool{false, true} {
			args := append(strings.Fields(invocation), "--manifest", manifest)
			if machine {
				args = append(args, "--json")
			}
			code, stdout, stderr := testkit.RunCapturedCLI(args...)
			message, guidance := stderr, stderr
			if machine {
				result := clijson.DecodeFailure(t, stdout, command, "operation")
				message, guidance = result.Error.Message, fmt.Sprint(result.Guidance)
			}
			if code != 1 || !strings.Contains(message, personal) || !strings.Contains(message, work) || !strings.Contains(guidance, "CLAUDE_CONFIG_DIR") || !strings.Contains(guidance, filepath.Dir(personal)) {
				t.Fatalf("%v: missing profile mismatch or retry instruction: %d %s %s", args, code, message, guidance)
			}
		}
	}
	testkit.AssertFileContent(t, personal, string(installed))
	testkit.AssertFileContent(t, work, other)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Dir(personal))
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
	requireClaudeHookKeyValues(t, testkit.ReadFile(t, personal), "example", hook, second)
}

func TestClaudeHookProfileGuidanceRequiresAnOwnedFile(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	nativefixture.SetRecoveryEnvironment(t, root)
	workspace := filepath.Join(root, "workspace")
	manifest := filepath.Join(workspace, "daem.toml")
	profile := os.Getenv("CLAUDE_CONFIG_DIR")
	hook := claudeStopHook{"notify", "echo original"}
	writeClaudeHookKeyManifest(t, workspace, "global", "", hook)
	nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
	settings := filepath.Join(profile, "settings.json")
	installed := string(testkit.ReadFile(t, settings))

	writeClaudeHookKeyManifest(t, workspace, "global", "example", hook)
	nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
	other := filepath.Join(root, "new profile") // Its settings file does not exist yet.
	t.Setenv("CLAUDE_CONFIG_DIR", other)
	code, _, stderr := testkit.RunCapturedCLI("apply", "--dry-run", "--manifest", manifest)
	if code != 1 || !strings.Contains(stderr, settings) || !strings.Contains(stderr, filepath.Join(other, "settings.json")) || !strings.Contains(stderr, "CLAUDE_CONFIG_DIR") {
		t.Fatalf("missing profile guidance: %d %s", code, stderr)
	}

	state := filepath.Join(workspace, ".daem", "state.json")
	original := testkit.ReadFile(t, state)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(original, &fields); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(root, "unowned", "settings.json")
	testkit.WriteFile(t, filepath.Dir(foreign), "settings.json", installed)
	fields["claude_hook_settings"], err = json.Marshal(foreign)
	if err != nil {
		t.Fatal(err)
	}
	altered, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	testkit.WriteFile(t, filepath.Dir(state), "state.json", string(altered))
	code, _, stderr = testkit.RunCapturedCLI("status", "--manifest", manifest)
	if code != 1 || strings.Contains(stderr, "CLAUDE_CONFIG_DIR") || !strings.Contains(stderr, "ownership_claim_missing") {
		t.Fatalf("an unowned path must produce an ownership failure, not retry guidance: %d %s", code, stderr)
	}
	testkit.AssertFileContent(t, foreign, installed)
	testkit.AssertFileContent(t, settings, installed)

	testkit.WriteFile(t, filepath.Dir(state), "state.json", string(original))
	t.Run("another spelling of the owned file", func(t *testing.T) {
		alias := filepath.Join(root, "work alias")
		if err := os.Symlink(profile, alias); err != nil {
			t.Skipf("directory symlink unavailable: %v", err)
		}
		t.Setenv("CLAUDE_CONFIG_DIR", alias)
		nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
		requireClaudeHookKeyValues(t, testkit.ReadFile(t, settings), "example", hook)
	})
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
			mode := os.FileMode(0o600)
			if tc.existing {
				testkit.WriteFile(t, claudeHome, "settings.json", outside)
				mode = 0o644
				if err := os.Chmod(destination, mode); err != nil {
					t.Fatal(err)
				}
			}

			for i, step := range tc.steps {
				if i == 1 {
					mode = 0o444
					if err := os.Chmod(destination, mode); err != nil {
						t.Fatal(err)
					}
				}
				t.Logf("applying model=%q hooks=%v", step.model, step.hooks)
				writeClaudeHookKeyManifest(t, workspace, "global", step.model, step.hooks...)
				nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
				nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--dry-run", "--verbose")
				nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes", "--verbose")

				content := testkit.ReadFile(t, destination)
				nativefixture.RequireSharedMode(t, destination, mode)
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
