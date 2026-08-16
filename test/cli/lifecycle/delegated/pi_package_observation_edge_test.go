package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/subprocess"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestPiPackageCarrierBlocksPreexistingUnmanagedRelation(t *testing.T) {
	tests := []struct {
		name           string
		expected       string
		observed       string
		expectedReason string
	}{
		{
			name:           "exact source",
			expected:       "npm:@acme/pi-tools@1.0.0",
			observed:       "npm:@acme/pi-tools@1.0.0",
			expectedReason: "present_unclaimed",
		},
		{
			name:           "equivalent package different version",
			expected:       "npm:@acme/pi-tools@1.0.0",
			observed:       "npm:@acme/pi-tools@2.0.0",
			expectedReason: "unkeyed_same_subject",
		},
		{
			name:           "equivalent git different transport and ref",
			expected:       "git:github.com/acme/pi-tools@v1",
			observed:       "https://github.com/acme/pi-tools.git@v2",
			expectedReason: "unkeyed_same_subject",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := writePiPackageCarrierFixture(t, test.expected)
			testkit.WriteFile(
				t,
				fixture.root,
				".pi/settings.json",
				`{"packages":[`+quotedJSONForCLI(t, test.observed)+`]}`,
			)

			var requests []subprocess.CommandRequest
			executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
				Runner: func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
					requests = append(requests, request)
					return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
				},
			})
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLIWithOptions(
				[]string{"apply", "--manifest", fixture.manifestPath, "--yes", "--json"},
				clipkg.RunOptions{
					Stdout: &stdout,
					Stderr: &stderr,
					ApplyExecuteOptions: applyworkflow.ExecuteOptions{
						HostRouteExecutor: executor,
					},
				},
			)

			if exitCode != 1 || stderr.Len() != 0 {
				t.Fatalf("apply exitCode=%d stdout=%q stderr=%q, want JSON failure", exitCode, stdout.String(), stderr.String())
			}
			if len(requests) != 0 {
				t.Fatalf("blocked apply invoked host route: %#v", requests)
			}
			payload := clijson.DecodeApplyResult(t, stdout.Bytes())
			clijson.RequireApplyFailure(
				t,
				payload,
				applyworkflow.FailureReasonRelationActionBlocked,
				applyworkflow.FailurePhasePreflight,
				applyworkflow.FailureOutcomeRefused,
			)
			if len(payload.RelationActions) != 1 ||
				payload.RelationActions[0].Reason != test.expectedReason {
				t.Fatalf("relation actions = %#v, want reason %s", payload.RelationActions, test.expectedReason)
			}
			assertNoCarrierInstallConvergenceClaims(t, stdout.String())
			assertNoPiManagedClaims(t, fixture.root)
		})
	}
}

func TestPiPackageCarrierDoesNotClaimEquivalentPostAttemptRelation(t *testing.T) {
	expected := "npm:@acme/pi-tools@1.0.0"
	fixture := writePiPackageCarrierFixture(t, expected)

	var requests []subprocess.CommandRequest
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			requests = append(requests, request)
			testkit.WriteFile(
				t,
				fixture.root,
				".pi/settings.json",
				`{"packages":["npm:@acme/pi-tools@2.0.0"]}`,
			)
			return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
		},
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLIWithOptions(
		[]string{"apply", "--manifest", fixture.manifestPath, "--yes", "--json"},
		clipkg.RunOptions{
			Stdout: &stdout,
			Stderr: &stderr,
			ApplyExecuteOptions: applyworkflow.ExecuteOptions{
				HostRouteExecutor: executor,
			},
		},
	)

	if exitCode != 1 || stderr.Len() != 0 {
		t.Fatalf("apply exitCode=%d stdout=%q stderr=%q, want JSON failure", exitCode, stdout.String(), stderr.String())
	}
	if len(requests) != 1 {
		t.Fatalf("host route requests = %#v, want one attempted install", requests)
	}
	payload := clijson.DecodeApplyResult(t, stdout.Bytes())
	clijson.RequireApplyFailure(
		t,
		payload,
		applyworkflow.FailureReasonHostRouteAttemptFailed,
		applyworkflow.FailurePhaseExecution,
		applyworkflow.FailureOutcomeIncomplete,
	)
	if len(payload.HostRouteAttempts) != 1 {
		t.Fatalf("host route attempts = %#v, want one failed post-observation diagnostic", payload.HostRouteAttempts)
	}
	if strings.Contains(stdout.String(), "attempted_observed_present") {
		t.Fatalf("apply output granted observed-present authority to equivalent source: %s", stdout.String())
	}
	assertNoCarrierInstallConvergenceClaims(t, stdout.String())
	assertNoPiManagedClaims(t, fixture.root)
}

type piPackageCarrierFixture struct {
	root         string
	manifestPath string
}

func writePiPackageCarrierFixture(t *testing.T, source string) piPackageCarrierFixture {
	t.Helper()
	root := t.TempDir()
	testkit.SetDataRootEnv(t, root)
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(root, "pi-global"))
	manifestPath := filepath.Join(root, "daem.toml")
	testkit.WriteFile(t, root, "daem.toml", "version = 1\ntargets = [\"pi\"]\n")
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
	return piPackageCarrierFixture{root: root, manifestPath: manifestPath}
}

func assertNoPiManagedClaims(t *testing.T, root string) {
	t.Helper()
	statePath := filepath.Join(root, ".daem", "state.json")
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		return
	} else if err != nil {
		t.Fatalf("stat statefile: %v", err)
	}
	state, err := statefile.Load(t.Context(), statePath)
	if err != nil {
		t.Fatalf("load statefile: %v", err)
	}
	if claims := state.ManagedCarrierClaims(); len(claims) != 0 {
		t.Fatalf("managed carrier claims = %#v, want none", claims)
	}
}

func quotedJSONForCLI(t *testing.T, value string) string {
	t.Helper()
	return strconv.Quote(value)
}
