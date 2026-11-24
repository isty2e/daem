package cli_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestClaudeHookAndKeyShareSettings(t *testing.T) {
	for _, initial := range []string{"absent", "existing"} {
		t.Run(initial, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			nativefixture.SetRecoveryEnvironment(t, root)
			claudeHome := filepath.Join(root, "home", ".claude")
			t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)
			workspace := filepath.Join(root, "workspace")
			manifest := filepath.Join(workspace, "daem.toml")
			destination := filepath.Join(claudeHome, "settings.json")
			const outside = `{"unmanaged":42}`
			if initial == "existing" {
				testkit.WriteFile(t, claudeHome, "settings.json", outside)
			}

			const first = `{"hooks":[{"type":"command","command":"echo first"}]}`
			const second = `{"hooks":[{"type":"command","command":"echo second"}]}`
			const updated = `{"hooks":[{"type":"command","command":"echo updated"}]}`
			for _, step := range []struct {
				hooks    []claudeStopHook
				wantStop string
			}{
				{[]claudeStopHook{{"first", "echo first"}, {"second", "echo second"}}, first + "," + second},
				{[]claudeStopHook{{"first", "echo updated"}, {"second", "echo second"}}, updated + "," + second},
				{[]claudeStopHook{{"second", "echo second"}}, second},
			} {
				writeClaudeHookKeyManifest(t, workspace, "global", step.hooks...)
				nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
				nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--dry-run", "--verbose")
				nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes", "--verbose")

				content := testkit.ReadFile(t, destination)
				nativefixture.RequireSharedValues(t, configdocument.JSON, content, `{"model":"example","hooks":{"Stop":[`+step.wantStop+`]}}`, true)
				nativefixture.RequireSharedValues(t, configdocument.JSON, content, outside, initial == "existing")
				nativefixture.RequireLifecycleClean(t, nativefixture.RunLifecycleCommand(t, "status", "--manifest", manifest, "--check", "--json"))
				preview := nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--dry-run", "--json")
				plan := clijson.DecodePlan(t, []byte(preview))
				if len(plan.Actions) != 1+len(step.hooks) {
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

type claudeStopHook struct{ name, command string }

func writeClaudeHookKeyManifest(t *testing.T, workspace, scope string, hooks ...claudeStopHook) {
	t.Helper()
	source := "./preferences.json"
	if scope == "global" {
		source = filepath.Join(workspace, "preferences.json")
	}
	manifest := fmt.Sprintf(`version = 1
targets = ["claude-code"]

[[agent_config]]
id = "preferences"
scope = %q
path = "settings.json"
source = %q
`, scope, source)
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
	testkit.WriteFile(t, workspace, "preferences.json", `{"model":"example"}`)
}
