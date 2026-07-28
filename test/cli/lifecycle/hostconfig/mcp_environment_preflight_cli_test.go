package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	"github.com/isty2e/daem/internal/subprocess"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/execcheck"
)

func TestMCPPublicCLIApplyNeverPrintsOrPersistsResolvedEnvironmentValue(t *testing.T) {
	for _, test := range []struct {
		name       string
		outputArgs []string
	}{
		{name: "human"},
		{name: "verbose", outputArgs: []string{"--verbose"}},
		{name: "json", outputArgs: []string{"--json"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			const (
				sourceName = "DAEM_TEST_SECRET_ENV_REF"
				secret     = "super-secret-value"
			)
			t.Setenv(sourceName, secret)
			project := newMCPCLIProject(t)
			spec := mcpManifestSpec{
				Command: "secret-boundary-daem-test",
				Args:    []string{"--serve", "context7"},
				Env:     map[string]string{"API_TOKEN": sourceName},
			}
			writeMCPManifest(t, project.root, spec)
			runMCPLock(t, project)

			args := []string{
				"apply",
				"--manifest", project.manifestPath,
				"--target", "claude-code",
				"--yes",
			}
			args = append(args, test.outputArgs...)
			delegateRuns := 0
			exitCode, stdout, stderr := runMCPCLIWithOptions(t, args, clipkg.RunOptions{
				ApplyExecuteOptions: applyworkflow.ExecuteOptions{
					DelegateExecutor: delegate.NewExecutor(delegate.Options{
						Runner: func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
							delegateRuns++
							if !slices.Contains(request.Env, "API_TOKEN="+secret) {
								t.Fatalf("delegate env = %#v, want launch-time API_TOKEN value", request.Env)
							}
							return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
						},
					}),
				},
			})
			if exitCode != 0 || stderr != "" || delegateRuns != 1 {
				t.Fatalf(
					"apply exitCode=%d delegateRuns=%d stdout=%q stderr=%q",
					exitCode,
					delegateRuns,
					stdout,
					stderr,
				)
			}
			assertMCPDelegateNoSecretLeak(t, test.name+" stdout", stdout)
			for _, path := range []string{
				project.lockfilePath,
				filepath.Join(project.root, ".mcp.json"),
				filepath.Join(project.root, ".daem", "state.json"),
			} {
				content, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read %s: %v", path, err)
				}
				assertMCPDelegateNoSecretLeak(t, path, string(content))
			}
			recoveryPath := filepath.Join(project.root, ".daem", "recovery")
			entries, err := os.ReadDir(recoveryPath)
			if err != nil && !os.IsNotExist(err) {
				t.Fatalf("read recovery directory: %v", err)
			}
			if len(entries) != 0 {
				t.Fatalf("recovery entries = %#v, want no persisted transaction artifacts", entries)
			}
		})
	}
}

func TestMCPPublicCLIApplyDelegatedRouteMissingEnvDoesNotLaunchRunner(t *testing.T) {
	missingEnvName := "DAEM_TEST_MISSING_ENV_REF"
	unsetEnvForMCPDelegateTest(t, missingEnvName)
	canary := execcheck.New(t, "must-not-run-daem-test")
	project := newMCPCLIProject(t)
	spec := mcpManifestSpec{
		Command: "must-not-run-daem-test",
		Args:    []string{"--serve", "context7"},
		Env:     map[string]string{"API_TOKEN": missingEnvName},
	}
	writeMCPManifest(t, project.root, spec)
	runMCPLock(t, project)

	exitCode, stdout, stderr := runMCPCLI(
		t,
		"apply",
		"--manifest",
		project.manifestPath,
		"--target",
		"claude-code",
		"--yes",
		"--json",
	)
	if exitCode != 1 || stderr != "" {
		t.Fatalf("missing-env apply exitCode=%d stdout=%q stderr=%q, want preflight failure JSON only", exitCode, stdout, stderr)
	}
	payload := clijson.DecodeApplyResult(t, []byte(stdout))
	if !payload.HasErrors || len(payload.Errors) != 1 ||
		!strings.Contains(payload.Errors[0].Message, missingEnvName) {
		t.Fatalf("payload errors = %#v, want missing environment source", payload.Errors)
	}
	if payload.ActionCount != 0 || len(payload.Actions) != 1 ||
		len(payload.DelegateActions) != 1 || len(payload.DelegateAttempts) != 0 {
		t.Fatalf(
			"payload = %#v, want disclosed plan but no committed action or delegate attempt",
			payload,
		)
	}
	assertMCPDelegateActionDisclosure(t, payload.DelegateActions[0], "scheduled", "allow", true, "plain", spec)
	execcheck.AssertClean(t, canary, "missing env attempt")
	for _, path := range []string{
		filepath.Join(project.root, ".mcp.json"),
		filepath.Join(project.root, ".daem", "state.json"),
		filepath.Join(project.root, ".daem", "recovery"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s stat error = %v, want absent after preflight failure", path, err)
		}
	}
}
