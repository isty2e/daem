package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/subprocess"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestPiManagedRelationDefaultPreviewDoesNotImplyRemoval(t *testing.T) {
	root := t.TempDir()
	testkit.SetDataRootEnv(t, root)
	manifest := filepath.Join(root, "daem.toml")
	settings := filepath.Join(root, ".pi", "settings.json")
	state := filepath.Join(root, ".daem", "state.json")
	source := "git:github.com/acme/pi-tools@v1"
	testkit.WriteFile(t, root, "daem.toml", `version = 1
targets = ["pi"]
[[extension]]
id = "tools-managed"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "git:github.com/acme/pi-tools@v1" }
`)
	testkit.WriteFile(t, root, ".pi/settings.json", `{"packages":["`+source+`"]}`)

	var stdout, stderr bytes.Buffer
	requests := 0
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			requests++
			t.Errorf("preview/adoption unexpectedly invoked a host route: %#v", request)
			return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 1}
		},
	})
	run := func(args ...string) int {
		stdout.Reset()
		stderr.Reset()
		return testkit.RunCLIWithOptions(append(args, "--manifest", manifest), clipkg.RunOptions{
			Stdout: &stdout,
			Stderr: &stderr,
			ApplyExecuteOptions: applyworkflow.ExecuteOptions{
				HostRouteExecutor: executor,
			},
		})
	}
	for _, args := range [][]string{{"lock"}, {"apply", "--manage-existing", "--yes", "--json"}} {
		if code := run(args...); code != 0 || stderr.Len() != 0 {
			t.Fatalf("setup %v exited %d: %s %s", args, code, &stdout, &stderr)
		}
	}

	before := map[string][]byte{}
	for _, path := range []string{settings, state} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = content
	}

	for _, args := range [][]string{{"status"}, {"apply", "--dry-run"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			if code := run(append(args, "--json")...); code != 0 || stderr.Len() != 0 {
				t.Fatalf("JSON preview exited %d: %s %s", code, &stdout, &stderr)
			}
			payload := clijson.DecodePlan(t, stdout.Bytes())
			if len(payload.RelationActions) != 1 {
				t.Fatalf("relation_actions = %#v, want one", payload.RelationActions)
			}
			action := payload.RelationActions[0]
			if action.Kind != "no_op" || action.Execution != "no_mutation" || action.InvokesHostRoute || action.BlocksOrdinaryApply {
				t.Fatalf("relation_action = %#v, want no-op without host execution or block", action)
			}

			if code := run(args...); code != 0 || stderr.Len() != 0 {
				t.Fatalf("default preview exited %d: %s %s", code, &stdout, &stderr)
			}
			text := stdout.String()
			for _, required := range []string{
				"relation actions: 1 subjects\n",
				"no change planned for extension relation",
				`subject="host_relation/pi.package-carrier/tools-managed" target=pi scope=project`,
			} {
				if !strings.Contains(text, required) {
					t.Errorf("default preview = %q, want %q", text, required)
				}
			}
			if strings.Contains(text, "remove managed extension binding") || strings.Contains(text, "install extension through host") {
				t.Errorf("default preview invents a host effect: %q", text)
			}
		})
	}

	if requests != 0 {
		t.Fatalf("host route requests = %d, want none", requests)
	}
	for path, content := range before {
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(after, content) {
			t.Errorf("preview changed %s", path)
		}
	}
}
