package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/subprocess"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestAntigravityCarrierPublicCLIApplyConvergesFromHostPair(t *testing.T) {
	tempDir := t.TempDir()
	testkit.SetDataRootEnv(t, tempDir)
	home := filepath.Join(tempDir, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"antigravity-cli\"]\n")
	runExtensionAuthoringCLI(
		t,
		"add", "extension", "guidance-managed", "modern-web-guidance@google",
		"--manifest", manifestPath,
		"--target", "antigravity-cli",
		"--scope", "global",
	)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(
		[]string{"lock", "--manifest", manifestPath},
		&stdout,
		&stderr,
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("lock write exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}

	var requests []subprocess.CommandRequest
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			requests = append(requests, request)
			configRoot := filepath.Join(home, ".gemini", "config")
			pluginRoot := filepath.Join(configRoot, "plugins", "modern-web-guidance")
			if err := os.MkdirAll(pluginRoot, 0o700); err != nil {
				return subprocess.CommandResult{Started: true, Err: err}
			}
			if err := os.WriteFile(
				filepath.Join(configRoot, "import_manifest.json"),
				[]byte(`{"imports":[{"name":"modern-web-guidance","source":"antigravity"}]}`),
				0o600,
			); err != nil {
				return subprocess.CommandResult{Started: true, Err: err}
			}
			if err := os.WriteFile(
				filepath.Join(pluginRoot, "plugin.json"),
				[]byte(`{"name":"modern-web-guidance"}`),
				0o600,
			); err != nil {
				return subprocess.CommandResult{Started: true, Err: err}
			}
			return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
		},
	})

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLIWithOptions(
		[]string{"apply", "--manifest", manifestPath, "--dry-run", "--json"},
		clipkg.RunOptions{
			Stdout: &stdout,
			Stderr: &stderr,
			ApplyExecuteOptions: applyworkflow.ExecuteOptions{
				HostRouteExecutor: executor,
			},
		},
	)
	if exitCode != 0 || stderr.Len() != 0 || len(requests) != 0 {
		t.Fatalf(
			"dry-run exitCode=%d requests=%#v stdout=%q stderr=%q",
			exitCode,
			requests,
			stdout.String(),
			stderr.String(),
		)
	}
	dryRunPayload := clijson.DecodePlan(t, stdout.Bytes())
	assertCLIHostRouteDryRunDisclosure(t, dryRunPayload.RelationActions, hostRouteDisclosureExpectation{
		namespace: "antigravity-cli.plugin-carrier",
		name:      "guidance-managed",
		target:    "antigravity-cli",
		scope:     "global",
		routeID:   antigravityCLIPluginRoute(t).RouteID(),
		attempt:   false,
	})

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLIWithOptions(
		[]string{"apply", "--manifest", manifestPath, "--yes", "--json"},
		clipkg.RunOptions{
			Stdout: &stdout,
			Stderr: &stderr,
			ApplyExecuteOptions: applyworkflow.ExecuteOptions{
				HostRouteExecutor: executor,
			},
		},
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if len(requests) != 1 ||
		requests[0].Command != "agy" ||
		!slices.Equal(requests[0].Args, []string{"plugin", "install", "modern-web-guidance@google"}) ||
		requests[0].WorkDir != tempDir {
		t.Fatalf("host route requests = %#v, want one exact Antigravity install", requests)
	}
	applyPayload := clijson.DecodeApplyResult(t, stdout.Bytes())
	assertCLIHostSourceHostRouteAttemptJSON(t, applyPayload.HostRouteAttempts, hostSourceHostRouteAttemptExpectation{
		namespace:   "antigravity-cli.plugin-carrier",
		name:        "guidance-managed",
		target:      "antigravity-cli",
		scope:       "global",
		routeID:     antigravityCLIPluginRoute(t).RouteID(),
		resultClass: "attempted_observed_present",
		reason:      "observed_present",
	})
	assertCLIHostRouteAttemptObservedPresentCommandSuccessJSON(t, applyPayload.HostRouteAttempts[0])
	if claims := loadCLIGlobalCarrierClaims(t, manifestPath); len(claims) != 1 {
		t.Fatalf("Antigravity claims = %#v, want one", claims)
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLIWithOptions(
		[]string{"apply", "--manifest", manifestPath, "--yes", "--json"},
		clipkg.RunOptions{
			Stdout: &stdout,
			Stderr: &stderr,
			ApplyExecuteOptions: applyworkflow.ExecuteOptions{
				HostRouteExecutor: executor,
			},
		},
	)
	if exitCode != 0 || stderr.Len() != 0 || len(requests) != 1 {
		t.Fatalf(
			"repeat apply exitCode=%d requests=%#v stdout=%q stderr=%q",
			exitCode,
			requests,
			stdout.String(),
			stderr.String(),
		)
	}
	retryPayload := clijson.DecodeApplyResult(t, stdout.Bytes())
	if len(retryPayload.HostRouteAttempts) != 0 {
		t.Fatalf("repeat apply attempts = %#v, want none", retryPayload.HostRouteAttempts)
	}
}
