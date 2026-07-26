package cli_test

import (
	"bytes"
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/subprocess"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestPiPackageCarrierPublicCLIApplyYesObservesExactProjectRelation(t *testing.T) {
	tempDir := t.TempDir()
	testkit.SetDataRootEnv(t, tempDir)
	manifestPath := filepath.Join(tempDir, "daem.toml")
	settingsPath := filepath.Join(tempDir, ".pi", "settings.json")
	source := "git:github.com/acme/pi-tools@v1"
	testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"pi\"]\n")
	runExtensionAuthoringCLI(
		t,
		"add", "extension", "tools-managed", source,
		"--manifest", manifestPath,
		"--target", "pi",
		"--scope", "project",
	)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr); exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("lock exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}

	var requests []subprocess.CommandRequest
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			requests = append(requests, request)
			testkit.WriteFile(t, tempDir, ".pi/settings.json", `{"packages":["`+source+`"]}`)
			return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
		},
	})

	stdout.Reset()
	stderr.Reset()
	exitCode := testkit.RunVerboseCLIWithOptions(
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
		t.Fatalf("dry-run exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if len(requests) != 0 {
		t.Fatalf("dry-run host route requests = %#v, want none", requests)
	}
	testkit.AssertPathMissing(t, settingsPath)

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
		requests[0].Command != "pi" ||
		!slices.Equal(requests[0].Args, []string{"install", source, "-l"}) ||
		requests[0].WorkDir != tempDir {
		t.Fatalf("host route requests = %#v, want exact Pi project install", requests)
	}
	applyPayload := clijson.DecodeApplyResult(t, stdout.Bytes())
	assertCLIHostSourceHostRouteAttemptJSON(t, applyPayload.HostRouteAttempts, hostSourceHostRouteAttemptExpectation{
		namespace:   "pi.package-carrier",
		name:        "tools-managed",
		target:      "pi",
		scope:       "project",
		routeID:     piPackageRoute(t).RouteID(),
		resultClass: "attempted_observed_present",
		reason:      "observed_present",
	})
	assertCLIHostRouteAttemptObservedPresentCommandSuccessJSON(t, applyPayload.HostRouteAttempts[0])
	assertNoCarrierInstallConvergenceClaims(t, stdout.String())

	state, err := statefile.Load(t.Context(), filepath.Join(tempDir, ".daem", "state.json"))
	if err != nil {
		t.Fatalf("load statefile: %v", err)
	}
	if claims := state.ManagedCarrierClaims(); len(claims) != 1 {
		t.Fatalf("managed carrier claims = %#v, want one exact Pi project claim", claims)
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
		t.Fatalf("converged apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if len(requests) != 1 {
		t.Fatalf("converged apply host route requests = %#v, want no repeated install", requests)
	}
}
