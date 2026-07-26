package cli_test

import (
	"bytes"
	"context"
	"path/filepath"
	"slices"
	"testing"

	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/subprocess"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestOpenCodeCarrierPublicCLIApplyYesObservesExactGlobalRelation(t *testing.T) {
	tempDir := t.TempDir()
	testkit.SetDefaultRootEnv(t, tempDir)
	manifestPath := filepath.Join(tempDir, "daem.toml")
	source := "@acme/opencode-formatter"
	testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"opencode\"]\n")
	runExtensionAuthoringCLI(
		t,
		"add", "extension", "formatter-managed", source,
		"--manifest", manifestPath,
		"--target", "opencode",
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
			testkit.WriteFile(
				t,
				tempDir,
				"config/opencode/opencode.json",
				`{"plugin":["`+source+`"]}`,
			)
			testkit.WriteFile(
				t,
				tempDir,
				"config/opencode/tui.jsonc",
				`{"plugin":[["`+source+`",{"enabled":true}],],}`,
			)
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
	dryRun := clijson.DecodePlan(t, stdout.Bytes())
	assertCLIHostRouteDryRunDisclosure(t, dryRun.RelationActions, hostRouteDisclosureExpectation{
		namespace: "opencode.plugin-carrier",
		name:      "formatter-managed",
		target:    "opencode",
		scope:     "global",
		routeID:   openCodePluginRoute(t).RouteID(),
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
		requests[0].Command != "opencode" ||
		!slices.Equal(requests[0].Args, []string{"plugin", source, "--global"}) ||
		requests[0].WorkDir != tempDir {
		t.Fatalf("host route requests = %#v, want one OpenCode global install", requests)
	}
	applyPayload := clijson.DecodeApplyResult(t, stdout.Bytes())
	assertCLIHostSourceHostRouteAttemptJSON(t, applyPayload.HostRouteAttempts, hostSourceHostRouteAttemptExpectation{
		namespace:   "opencode.plugin-carrier",
		name:        "formatter-managed",
		target:      "opencode",
		scope:       "global",
		routeID:     openCodePluginRoute(t).RouteID(),
		resultClass: "attempted_observed_present",
		reason:      "observed_present",
	})
	if claims := loadCLIGlobalCarrierClaims(t, manifestPath); len(claims) != 1 {
		t.Fatalf("global OpenCode claims = %#v, want one", claims)
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
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("repeat apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if len(requests) != 1 {
		t.Fatalf("repeat apply requests = %#v, want no second install", requests)
	}
	retry := clijson.DecodeApplyResult(t, stdout.Bytes())
	if len(retry.HostRouteAttempts) != 0 {
		t.Fatalf("repeat apply host route attempts = %#v, want none", retry.HostRouteAttempts)
	}
}
