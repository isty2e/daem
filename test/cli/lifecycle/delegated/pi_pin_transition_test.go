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
)

func TestPiPinChangePublicCLIDisclosesRetainsAndResumes(t *testing.T) {
	for _, scope := range []string{"project", "global"} {
		t.Run(scope, func(t *testing.T) {
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
				return testkit.RunVerboseCLIWithOptions(args, clipkg.RunOptions{Stdout: &stdout, Stderr: &stderr, ApplyExecuteOptions: applyworkflow.ExecuteOptions{HostRouteExecutor: executor}})
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

			testkit.WriteFile(t, root, "daem.toml", strings.Replace(declaration, before, after, 1))
			if code := run("lock"); code != 0 {
				t.Fatalf("new pin lock=%d: %s", code, &stderr)
			}
			if code := run("apply", "--dry-run"); code != 0 || !strings.Contains(stdout.String(), before) || !strings.Contains(stdout.String(), after) || !strings.Contains(stdout.String(), "no automatic rollback") {
				t.Fatalf("pin disclosure=%d: %s %s", code, &stdout, &stderr)
			}
			if code := run("apply", "--dry-run", "--json"); code != 0 {
				t.Fatalf("pin JSON preview=%d: %s", code, &stderr)
			}
			assertPinChangeDisclosure(t, stdout.Bytes(), before, after, false)
			if calls != 0 {
				t.Fatal("preview invoked native host")
			}
			if code := run("apply"); code != 2 || calls != 0 {
				t.Fatalf("missing authorization=%d, calls=%d", code, calls)
			}
			if code := run("apply", "--yes", "--json"); code != 1 || calls != 1 {
				t.Fatalf("false native success=%d, calls=%d: %s %s", code, calls, &stdout, &stderr)
			}
			if code := run("status"); code != 0 || !strings.Contains(stdout.String(), "original pending target") || !strings.Contains(stdout.String(), "new authorization") {
				t.Fatalf("pending retry guidance=%d: %s %s", code, &stdout, &stderr)
			}
			if calls != 1 {
				t.Fatal("pending status invoked the native host")
			}
			if code := run("refresh", "extension", "tools-managed", "--dry-run"); code != 1 || !strings.Contains(stdout.String()+stderr.String(), "pending pin change") {
				t.Fatalf("pending refresh=%d: %s %s", code, &stdout, &stderr)
			}
			if code := run("unmanage", "extension", "tools-managed", "--target", "pi", "--scope", scope, "--json"); code != 1 {
				t.Fatalf("pending unmanage=%d: %s %s", code, &stdout, &stderr)
			}

			if code := run("apply", "--dry-run", "--json"); code != 0 {
				t.Fatalf("retry preview=%d: %s", code, &stderr)
			}
			assertPinChangeDisclosure(t, stdout.Bytes(), before, after, true)
			publish = true
			if code := run("apply", "--yes", "--json"); code != 0 || calls != 2 {
				t.Fatalf("retry=%d, calls=%d: %s %s", code, calls, &stdout, &stderr)
			}
			if code := run("apply", "--yes", "--json"); code != 0 || calls != 2 {
				t.Fatalf("settled apply=%d, calls=%d: %s %s", code, calls, &stdout, &stderr)
			}
		})
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
