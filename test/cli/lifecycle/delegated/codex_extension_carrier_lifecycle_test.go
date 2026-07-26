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

func TestCodexExtensionCarrierPublicCLIApplyYesDelegatesAdmittedHostRoute(t *testing.T) {
	tempDir := t.TempDir()
	codexHome := filepath.Join(tempDir, "codex-home")
	if err := os.Mkdir(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", codexHome)
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")
	runExtensionAuthoringCLI(
		t,
		"add", "extension", "documents-managed", "documents@openai-primary-runtime",
		"--manifest", manifestPath,
		"--target", "codex",
		"--scope", "global",
	)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("lock write exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	var requests []subprocess.CommandRequest
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			requests = append(requests, request)
			if err := os.WriteFile(
				filepath.Join(codexHome, "config.toml"),
				[]byte("[plugins.\"documents@openai-primary-runtime\"]\n"),
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
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("apply --dry-run --json exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if len(requests) != 0 {
		t.Fatalf("dry-run host route requests = %#v, want none", requests)
	}
	dryRunPayload := clijson.DecodePlan(t, stdout.Bytes())
	if len(dryRunPayload.HostRouteAttempts) != 0 {
		t.Fatalf("dry-run host_route_attempts = %#v, want none", dryRunPayload.HostRouteAttempts)
	}
	assertCLIHostRouteDryRunDisclosure(t, dryRunPayload.RelationActions, hostRouteDisclosureExpectation{
		namespace: "codex.plugin-carrier",
		name:      "documents-managed",
		target:    "codex",
		scope:     "global",
		routeID:   codexPluginRoute(t).RouteID(),
		attempt:   false,
	})
	assertNoCarrierInstallConvergenceClaims(t, stdout.String())

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
		t.Fatalf("apply --yes --json exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if len(requests) != 1 ||
		requests[0].Command != "codex" ||
		!slices.Equal(requests[0].Args, []string{"plugin", "add", "documents@openai-primary-runtime", "--json"}) ||
		requests[0].WorkDir != tempDir {
		t.Fatalf("host route requests = %#v, want one Codex plugin add request", requests)
	}
	applyPayload := clijson.DecodeApplyResult(t, stdout.Bytes())
	assertCLICodexHostRouteAttemptJSON(t, applyPayload.HostRouteAttempts, "attempted_observed_present", "observed_present")
	assertCLIHostRouteAttemptObservedPresentCommandSuccessJSON(t, applyPayload.HostRouteAttempts[0])
	if len(applyPayload.DelegateAttempts) != 0 {
		t.Fatalf("delegate_attempts = %#v, want none for carrier host route", applyPayload.DelegateAttempts)
	}
	assertNoCarrierInstallConvergenceClaims(t, stdout.String())

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath, "--json"}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("status --json exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	statusPayload := clijson.DecodePlan(t, stdout.Bytes())
	assertCLICodexHostRouteAttemptJSON(t, statusPayload.HostRouteAttempts, "attempted_observed_present", "observed_present")
	assertCLIHostRouteConvergedDisclosure(t, statusPayload.RelationActions, hostRouteDisclosureExpectation{
		namespace: "codex.plugin-carrier",
		name:      "documents-managed",
		target:    "codex",
		scope:     "global",
		routeID:   codexPluginRoute(t).RouteID(),
	})
	assertNoCarrierInstallConvergenceClaims(t, stdout.String())

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
		t.Fatalf("repeat apply exitCode=%d requests=%#v stdout=%q stderr=%q, want no second host attempt", exitCode, requests, stdout.String(), stderr.String())
	}
	retryPayload := clijson.DecodeApplyResult(t, stdout.Bytes())
	if len(retryPayload.HostRouteAttempts) != 0 {
		t.Fatalf("repeat apply host_route_attempts = %#v, want none for converged relation", retryPayload.HostRouteAttempts)
	}
	assertNoCarrierInstallConvergenceClaims(t, stdout.String())
}
