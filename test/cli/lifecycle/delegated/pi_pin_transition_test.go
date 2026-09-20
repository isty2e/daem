package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/subprocess"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestPiPinChangePublicCLIDisclosesRetainsAndResumes(t *testing.T) {
	for _, scenario := range []struct{ scope, failureFormat string }{
		{"project", "json"},
		{"global", "json"},
		{"project", "human"},
		{"global", "human"},
	} {
		t.Run(scenario.scope+"/"+scenario.failureFormat, func(t *testing.T) {
			scope := scenario.scope
			root := t.TempDir()
			testkit.SetDataRootEnv(t, root)
			t.Setenv("HOME", filepath.Join(root, "home"))
			t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(root, "pi-agent"))
			manifest := filepath.Join(root, "daem.toml")
			settings := filepath.Join(root, ".pi", "settings.json")
			if scope == "global" {
				settings = filepath.Join(root, "pi-agent", "settings.json")
			}
			before := "git:github.com/example/package@" + strings.Repeat("a", 40)
			after := "git:github.com/example/package@" + strings.Repeat("b", 40)
			declaration := fmt.Sprintf("version = 1\ntargets = [\"pi\"]\n[[extension]]\nid = \"tools-managed\"\ncarrier = \"pi-package\"\ntargets = [\"pi\"]\nscope = %q\nsource = { host_source = %q }\n", scope, before)
			testkit.WriteFile(t, root, "daem.toml", declaration)
			testkit.WriteFile(t, filepath.Dir(settings), filepath.Base(settings), fmt.Sprintf("{\"packages\":[%q]}", before))
			var stdout, stderr bytes.Buffer
			calls := 0
			publish := false
			executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{Runner: func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
				calls++
				want := []string{"install", after}
				if scope == "project" {
					want = append(want, "-l")
				}
				if request.Command != "pi" || !slices.Equal(request.Args, want) {
					t.Fatalf("unexpected native mutation %s %v", request.Command, request.Args)
				}
				if publish {
					testkit.WriteFile(t, filepath.Dir(settings), filepath.Base(settings), fmt.Sprintf("{\"packages\":[%q]}", after))
				}
				return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
			}})
			run := func(args ...string) int {
				stdout.Reset()
				stderr.Reset()
				args = append(args, "--manifest", manifest)
				code := testkit.RunVerboseCLIWithOptions(args, clipkg.RunOptions{Stdout: &stdout, Stderr: &stderr, ApplyExecuteOptions: applyworkflow.ExecuteOptions{HostRouteExecutor: executor}})

				// v0.2.3 advertised plan 12/apply 19 without pin-change disclosure.
				if slices.Contains(args, "--json") {
					switch {
					case args[0] == "status" || args[0] == "apply" && slices.Contains(args, "--dry-run"):
						payload := clijson.DecodePlan(t, stdout.Bytes())
						if payload.SchemaVersion <= 12 {
							t.Errorf("pin-capable plan uses released schema %d; want a version newer than 12", payload.SchemaVersion)
						}
					case args[0] == "apply":
						payload := clijson.DecodeApplyResult(t, stdout.Bytes())
						if payload.SchemaVersion <= 19 {
							t.Errorf("pin-capable apply result uses released schema %d; want a version newer than 19", payload.SchemaVersion)
						}
					}
				}

				return code
			}
			if code := run("lock"); code != 0 {
				t.Fatalf("initial lock=%d: %s", code, &stderr)
			}
			if code := run("apply", "--manage-existing", "--yes", "--json"); code != 0 {
				t.Fatalf("adoption=%d: %s %s", code, &stdout, &stderr)
			}
			if calls != 0 {
				t.Fatal("adoption invoked native host")
			}

			nextDeclaration := strings.Replace(declaration, before, after, 1)
			testkit.WriteFile(t, root, "daem.toml", nextDeclaration)
			if code := run("lock"); code != 0 {
				t.Fatalf("new pin lock=%d: %s", code, &stderr)
			}
			t.Run("initial change is not a retry", func(t *testing.T) {
				if code := run("apply", "--dry-run"); code != 0 || !strings.Contains(stdout.String(), before) || !strings.Contains(stdout.String(), after) || !strings.Contains(stdout.String(), "no automatic rollback") {
					t.Fatalf("pin disclosure=%d: %s %s", code, &stdout, &stderr)
				}
				assertNoPendingPinGuidance(t, stdout.String())
				if code := run("status"); code != 0 {
					t.Fatalf("initial status=%d: %s %s", code, &stdout, &stderr)
				}
				assertNoPendingPinGuidance(t, stdout.String())
			})
			t.Run("initial conflict is not a retry", func(t *testing.T) {
				defer testkit.WriteFile(t, filepath.Dir(settings), filepath.Base(settings), fmt.Sprintf("{\"packages\":[%q]}", before))
				testkit.WriteFile(t, filepath.Dir(settings), filepath.Base(settings), fmt.Sprintf("{\"packages\":[%q]}", after))
				for _, args := range [][]string{{"status", "--check"}, {"apply", "--dry-run"}} {
					if code := run(args...); code != 1 || !strings.Contains(stdout.String(), "pin_transition_conflict") {
						t.Fatalf("initial conflict %v=%d: %s %s", args, code, &stdout, &stderr)
					}
					assertNoPendingPinGuidance(t, stdout.String())
				}
			})
			if code := run("apply", "--dry-run", "--json"); code != 0 {
				t.Fatalf("pin JSON preview=%d: %s", code, &stderr)
			}
			assertPinChangeDisclosure(t, stdout.Bytes(), before, after, false)
			if code := run("status", "--json"); code != 0 {
				t.Fatalf("pin JSON status=%d: %s", code, &stderr)
			}
			assertPinChangeDisclosure(t, stdout.Bytes(), before, after, false)
			if calls != 0 {
				t.Fatal("preview invoked native host")
			}
			if code := run("apply"); code != 2 || calls != 0 {
				t.Fatalf("missing authorization=%d, calls=%d", code, calls)
			}
			failedArgs := []string{"apply", "--yes"}
			if scenario.failureFormat == "json" {
				failedArgs = append(failedArgs, "--json")
			}
			if code := run(failedArgs...); code != 1 || calls != 1 {
				t.Fatalf("false native success=%d, calls=%d: %s %s", code, calls, &stdout, &stderr)
			}
			if scenario.failureFormat == "human" {
				if !strings.Contains(stderr.String(), "next: inspect with daem status --manifest") {
					t.Fatalf("initial failure lost fresh inspection guidance: %s", &stderr)
				}
				assertNoPendingPinGuidance(t, stderr.String())
			}
			if scenario.failureFormat == "json" {
				assertPinChangeDisclosure(t, stdout.Bytes(), before, after, false)
			}
			t.Run("pending status advances past inspection", func(t *testing.T) {
				if code := run("status"); code != 0 || !strings.Contains(stdout.String(), "original pending target") || !strings.Contains(stdout.String(), "new authorization") {
					t.Fatalf("pending retry guidance=%d: %s %s", code, &stdout, &stderr)
				}
				if strings.Contains(stdout.String(), "next: inspect") {
					t.Fatalf("verbose status recommends itself: %s", &stdout)
				}
			})
			if calls != 1 {
				t.Fatal("pending status invoked the native host")
			}
			if code := run("refresh", "extension", "tools-managed", "--dry-run"); code != 1 || !strings.Contains(stdout.String()+stderr.String(), "pending pin change") {
				t.Fatalf("pending refresh=%d: %s %s", code, &stdout, &stderr)
			}
			if code := run("unmanage", "extension", "tools-managed", "--target", "pi", "--scope", scope, "--json"); code != 1 {
				t.Fatalf("pending unmanage=%d: %s %s", code, &stdout, &stderr)
			}

			for _, changed := range []struct {
				name, manifest  string
				missingSettings bool
			}{
				{name: "omitted", manifest: "version = 1\ntargets = [\"pi\"]\n"},
				{name: "omitted and absent", manifest: "version = 1\ntargets = [\"pi\"]\n", missingSettings: true},
				{name: "different target", manifest: strings.Replace(nextDeclaration, after, "git:github.com/example/package@"+strings.Repeat("c", 40), 1)},
				{name: "old target", manifest: declaration},
			} {
				t.Run("pending "+changed.name, func(t *testing.T) {
					defer func() {
						testkit.WriteFile(t, root, "daem.toml", nextDeclaration)
						testkit.WriteFile(t, filepath.Dir(settings), filepath.Base(settings), fmt.Sprintf("{\"packages\":[%q]}", before))
						if code := run("lock"); code != 0 {
							t.Errorf("restore pending target=%d: %s", code, &stderr)
						}
					}()
					testkit.WriteFile(t, root, "daem.toml", changed.manifest)
					if changed.missingSettings {
						testkit.WriteFile(t, filepath.Dir(settings), filepath.Base(settings), "{\"packages\":[]}")
					}
					if code := run("lock"); code != 0 {
						t.Fatalf("changed pending declaration lock=%d: %s", code, &stderr)
					}
					for _, args := range [][]string{{"status", "--check"}, {"apply", "--dry-run"}, {"apply", "--yes"}} {
						code := run(args...)
						text := stdout.String() + stderr.String()
						if code != 1 || strings.Count(text, "original pending target") != 1 || !strings.Contains(text, "new authorization") {
							t.Fatalf("pending %v=%d: %s", args, code, text)
						}
						if calls != 1 {
							t.Fatalf("pending refusal invoked Pi: calls=%d", calls)
						}
					}
				})
			}

			if code := run("apply", "--dry-run", "--json"); code != 0 {
				t.Fatalf("retry preview=%d: %s", code, &stderr)
			}
			assertPinChangeDisclosure(t, stdout.Bytes(), before, after, true)
			if code := run("apply", "--yes"); code != 1 || calls != 2 {
				t.Fatalf("failed retry=%d, calls=%d: %s %s", code, calls, &stdout, &stderr)
			}
			if !strings.Contains(stderr.String(), "next: inspect with daem status --manifest") {
				t.Fatalf("failed retry lost fresh inspection guidance: %s", &stderr)
			}
			assertNoPendingPinGuidance(t, stderr.String())

			publish = true
			if code := run("apply", "--yes", "--json"); code != 0 || calls != 3 {
				t.Fatalf("retry=%d, calls=%d: %s %s", code, calls, &stdout, &stderr)
			}
			assertPinChangeDisclosure(t, stdout.Bytes(), before, after, true)
			if code := run("apply", "--yes", "--json"); code != 0 || calls != 3 {
				t.Fatalf("settled apply=%d, calls=%d: %s %s", code, calls, &stdout, &stderr)
			}
			if code := run("status", "--check", "--json"); code != 0 {
				t.Fatalf("settled JSON status=%d: %s %s", code, &stdout, &stderr)
			}
			if code := run("status", "--check"); code != 0 {
				t.Fatalf("settled status=%d: %s %s", code, &stdout, &stderr)
			}
			assertNoPendingPinGuidance(t, stdout.String())
		})
	}
}

func assertNoPendingPinGuidance(t *testing.T, output string) {
	t.Helper()
	for _, advice := range []string{"original pending target", "retry only after"} {
		if strings.Contains(output, advice) {
			t.Errorf("non-pending state includes %q: %s", advice, output)
		}
	}
}

func assertPinChangeDisclosure(t *testing.T, data []byte, before, after string, resume bool) {
	t.Helper()
	var payload struct {
		Relations []struct {
			Kind string `json:"kind"`
			Pin  struct {
				From       string   `json:"from_source"`
				To         string   `json:"to_source"`
				Resume     bool     `json:"resume"`
				Provenance string   `json:"previous_provenance"`
				Effects    []string `json:"effect_classes"`
				NonClaims  []string `json:"non_claims"`
			} `json:"pin_change"`
		} `json:"relation_actions"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Relations) != 1 {
		t.Fatalf("relation actions=%d", len(payload.Relations))
	}
	action := payload.Relations[0]
	if action.Kind != "change_pin" || action.Pin.From != before || action.Pin.To != after || action.Pin.Resume != resume || action.Pin.Provenance != "explicitly_adopted_observed" {
		t.Fatalf("pin disclosure=%+v", action)
	}
	if !slices.Contains(action.Pin.Effects, "git_checkout_reset_and_clean") ||
		!slices.Contains(action.Pin.Effects, "dependency_install") ||
		!slices.Contains(action.Pin.NonClaims, "automatic_host_rollback") {
		t.Fatalf("missing pin effect disclosure: %+v", action.Pin)
	}
}
