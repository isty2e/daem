package cli_test

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestSuccessfulApplyRetainsEveryOutputResult(t *testing.T) {
	for _, kind := range []string{"skill", "hook"} {
		for _, verbose := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/verbose=%t", kind, verbose), func(t *testing.T) {
				root := t.TempDir()
				home := filepath.Join(root, "home")
				t.Setenv("HOME", home)
				for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "PI_CODING_AGENT_DIR"} {
					t.Setenv(key, filepath.Join(home, key))
				}
				testkit.WithWorkingDirectory(t, root)
				manifest := filepath.Join(root, "daem.toml")
				selected := "codex"
				if kind == "hook" {
					selected = "claude-code"
					testkit.WriteFile(t, root, ".claude/settings.json", `{"env":{"RESULT_POLICY_FIXTURE":"keep"}}`)
				}

				writeDesired := func(revision int, present bool) {
					var content strings.Builder
					fmt.Fprintf(&content, "version = 1\ntargets = [%q]\n", selected)
					if present {
						for index := range 5 {
							name := fmt.Sprintf("entry-%d", index)
							if kind == "skill" {
								fmt.Fprintf(&content, "\n[[skill]]\nname = %q\nsource = { path = %q, mode = \"vendor\" }\nscope = \"project\"\n", name, "skills/"+name)
								testkit.WriteFile(t, root, "skills/"+name+"/SKILL.md", fmt.Sprintf("---\nname: %s\ndescription: fixture\n---\nrevision %d\n", name, revision))
							} else {
								fmt.Fprintf(&content, "\n[[hook]]\nname = %q\nevent = \"PostToolUse\"\ncommand = %q\nscope = \"project\"\n", name, fmt.Sprintf("must-not-run-%s-%d", name, revision))
							}
						}
					}
					testkit.WriteFile(t, root, "daem.toml", content.String())
				}

				for _, phase := range []struct {
					name     string
					revision int
					present  bool
				}{
					{name: "create", revision: 1, present: true},
					{name: "update", revision: 2, present: true},
					{name: "remove"},
				} {
					writeDesired(phase.revision, phase.present)
					runCountUnitCLI(t, manifest, "lock")
					args := []string{"apply", "--yes"}
					if verbose {
						args = append(args, "--verbose")
					}
					output := string(runCountUnitCLI(t, manifest, args...))
					heading := "output results: 5 output entries\n"
					start := strings.Index(output, heading)
					if start < 0 || !strings.Contains(output[:start], "applied: 5 actions\n") {
						t.Fatalf("%s missing complete result/action counts: %s", phase.name, output)
					}
					results := output[start:]
					for index := range 5 {
						identity := fmt.Sprintf("resource=\"%s/entry-%d\"", kind, index)
						if count := strings.Count(results, identity); count != 1 {
							t.Fatalf("%s result %s occurs %d times, want once: %s", phase.name, identity, count, results)
						}
						if kind == "skill" {
							path := filepath.Join(root, ".agents", "skills", fmt.Sprintf("entry-%d", index), "SKILL.md")
							if phase.present {
								testkit.AssertFileContent(t, path, fmt.Sprintf("---\nname: entry-%d\ndescription: fixture\n---\nrevision %d\n", index, phase.revision))
							} else {
								testkit.AssertPathMissing(t, filepath.Dir(path))
							}
						}
					}

					if kind == "hook" {
						var configuration struct {
							Env   map[string]string `json:"env"`
							Hooks map[string][]struct {
								Hooks []struct {
									Command string `json:"command"`
								} `json:"hooks"`
							} `json:"hooks"`
						}
						if err := json.Unmarshal(testkit.ReadFile(t, filepath.Join(root, ".claude", "settings.json")), &configuration); err != nil {
							t.Fatal(err)
						}
						commands := make(map[string]int)
						for _, entries := range configuration.Hooks {
							for _, entry := range entries {
								for _, hook := range entry.Hooks {
									commands[hook.Command]++
								}
							}
						}
						want := 0
						if phase.present {
							want = 5
							for index := range 5 {
								if commands[fmt.Sprintf("must-not-run-entry-%d-%d", index, phase.revision)] != 1 {
									t.Fatalf("%s lost an applied hook: %#v", phase.name, commands)
								}
							}
						}
						if len(commands) != want || configuration.Env["RESULT_POLICY_FIXTURE"] != "keep" {
							t.Fatalf("%s changed unrelated config or retained wrong hooks: %#v", phase.name, configuration)
						}
					}
					testkit.AssertNoRecoveryArtifacts(t, root)

					if phase.name == "update" {
						unchanged := string(runCountUnitCLI(t, manifest, args...))
						if !strings.Contains(unchanged, "applied: 0 actions\n") || strings.Contains(unchanged, "output results:") {
							t.Fatalf("unchanged apply emitted individual result rows: %s", unchanged)
						}
					}
				}
			})
		}
	}
}
