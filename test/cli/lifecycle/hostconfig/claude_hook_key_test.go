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
		name     string
		existing bool
		steps    []step
	}{
		{"absent", false, []step{installBoth}},
		{"existing", true, []step{
			installBoth,
			{"example", []claudeStopHook{updated, second}},
			{"example", onlySecond},
		}},
		{"key first", true, []step{
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
			if err := os.Unsetenv("CLAUDE_CONFIG_DIR"); err != nil {
				t.Fatal(err)
			}
			home, err := os.UserHomeDir()
			if err != nil {
				t.Fatal(err)
			}
			claudeHome := filepath.Join(home, ".claude")
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
