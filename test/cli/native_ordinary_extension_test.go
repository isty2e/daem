package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/subprocess"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
)

func TestNativeOrdinaryApplyWithCurrentExtension(t *testing.T) {
	fixture := nativePresentationFixture(t, "model='native-test'\n", "foreign=true\n")
	workspace := filepath.Dir(fixture.manifest)
	const source = "git:github.com/acme/pi-tools@v1"
	testkit.WriteFile(t, workspace, filepath.Base(fixture.manifest), "version=1\ntargets=['codex','pi']\n")
	runNativeLifecycleCommand(t, "lock", "--manifest", fixture.manifest)
	runNativeLifecycleCommand(t, "add", "extension", "tools", source, "--manifest", fixture.manifest, "--target", "pi", "--scope", "project")
	runNativeLifecycleCommand(t, "lock", "--manifest", fixture.manifest)
	routes := 0
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
		routes++
		if request.Command != "pi" || !reflect.DeepEqual(request.Args, []string{"install", source, "-l"}) {
			t.Fatalf("unexpected host route: %#v", request)
		}
		testkit.WriteFile(t, workspace, ".pi/settings.json", `{"packages":["`+source+`"]}`)
		return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
	}})
	run := func(mode string, jsonOutput bool) (int, string, string) {
		t.Helper()
		args := []string{"apply", "--manifest", fixture.manifest, mode}
		if jsonOutput {
			args = append(args, "--json")
		}
		var stdout, stderr bytes.Buffer
		code := testkit.RunVerboseCLIWithOptions(args, clipkg.RunOptions{Stdout: &stdout, Stderr: &stderr, ApplyExecuteOptions: applyworkflow.ExecuteOptions{HostRouteExecutor: executor}})
		return code, stdout.String(), stderr.String()
	}
	if code, stdout, stderr := run("--yes", false); code != 0 || routes != 1 {
		t.Fatalf("extension-only baseline failed: %d %s %s routes=%d", code, stdout, stderr, routes)
	}
	manifest := string(testkit.ReadFile(t, fixture.manifest)) + "\n[instructions.project]\nsource='./instructions.md'\n[[host_config]]\nid='preferences'\ntargets=['codex']\npath='config.toml'\nsource='./preferences.toml'\nownership='keys'\n"
	testkit.WriteFile(t, workspace, filepath.Base(fixture.manifest), manifest)
	for _, content := range []string{"first\n", "second\n"} {
		testkit.WriteFile(t, workspace, "instructions.md", content)
		runNativeLifecycleCommand(t, "lock", "--manifest", fixture.manifest)
		for _, mode := range []string{"--dry-run", "--yes"} {
			if code, stdout, stderr := run(mode, true); code != 0 || !json.Valid([]byte(stdout)) || routes != 1 {
				t.Fatalf("current extension blocked mixed Apply or was rerun: %d %s %s routes=%d", code, stdout, stderr, routes)
			}
		}
		if !bytes.Contains(testkit.ReadFile(t, filepath.Join(workspace, "AGENTS.md")), []byte(content)) {
			t.Fatal("current extension prevented the selected instruction update")
		}
		runNativeLifecycleCommand(t, "status", "--manifest", fixture.manifest, "--check", "--json")
	}
	if err := os.Remove(filepath.Join(workspace, ".pi", "settings.json")); err != nil {
		t.Fatal(err)
	}
	before := snapshotNativeRecoveryPaths(t, fixture.destination, filepath.Join(workspace, "AGENTS.md"), filepath.Join(workspace, ".daem"))
	for _, mode := range []string{"--dry-run", "--yes"} {
		code, stdout, stderr := run(mode, false)
		if code == 0 || routes != 1 || !strings.Contains(stdout+stderr, "extension or host-registration changes") {
			t.Fatalf("changed extension escaped the unsupported-route boundary: %d %s %s routes=%d", code, stdout, stderr, routes)
		}
		if mode == "--dry-run" && (!strings.Contains(stdout, "not executable") || !strings.Contains(stderr, "apply preview failed:")) {
			t.Fatalf("refused preview lost its phase or affected plan: %s %s", stdout, stderr)
		}
		if !reflect.DeepEqual(before, snapshotNativeRecoveryPaths(t, fixture.destination, filepath.Join(workspace, "AGENTS.md"), filepath.Join(workspace, ".daem"))) {
			t.Fatal("unsupported-route refusal changed the direct operation")
		}
	}
}
