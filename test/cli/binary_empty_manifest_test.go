package cli_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestBuiltCLIEmptyManifestStatusAndApplyDryRun(t *testing.T) {
	buildRoot := t.TempDir()
	binaryPath := filepath.Join(buildRoot, "daem")
	build := exec.Command("go", "build", "-mod=readonly", "-o", binaryPath, "./cmd/daem")
	build.Dir = testkit.RepositoryRoot(t)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build daem: %v\n%s", err, output)
	}

	scenarios := []struct {
		name        string
		args        []string
		humanMarker string
		jsonCommand string
	}{
		{
			name:        "status human",
			args:        []string{"status", "--manifest", "daem.toml"},
			humanMarker: "status: 0 resources (up to date=0 changes=0)",
		},
		{
			name:        "status JSON",
			args:        []string{"status", "--manifest", "daem.toml", "--json"},
			jsonCommand: "status",
		},
		{
			name:        "apply dry-run human",
			args:        []string{"apply", "--manifest", "daem.toml", "--dry-run"},
			humanMarker: "dry-run: 0 resources (up to date=0 changes=0)",
		},
		{
			name:        "apply dry-run JSON",
			args:        []string{"apply", "--manifest", "daem.toml", "--dry-run", "--json"},
			jsonCommand: "apply",
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			workspace := t.TempDir()
			testkit.SetDefaultRootEnv(t, workspace)
			testkit.WriteFile(t, workspace, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")

			runBuiltCLIForSetup(t, binaryPath, workspace, "lock", "--manifest", "daem.toml")
			resolved, err := paths.Resolve(filepath.Join(workspace, "daem.toml"))
			if err != nil {
				t.Fatal(err)
			}
			writeRetiredEmptyArtifact(t, resolved.StatefilePath, `{"version":7,"managed_paths":[],"managed_aggregate_contributions":[],"pending_carrier_installs":[],"pending_carrier_removals":[],"managed_carrier_claims":[],"delegate_attempts":[],"host_route_attempts":[]}`)
			writeRetiredEmptyArtifact(t, resolved.OwnershipRegistryPath, `{"version":1,"claims":[]}`)
			writeRetiredEmptyArtifact(t, resolved.CarrierClaimRegistryPath, `{"version":1,"claims":[]}`)
			before := testkit.HashDirectory(t, workspace)

			stdout, stderr, exitCode := runBuiltCLI(t, binaryPath, workspace, scenario.args...)
			if exitCode != 0 || stderr != "" {
				t.Fatalf(
					"args=%q exitCode=%d stdout=%q stderr=%q",
					scenario.args,
					exitCode,
					stdout,
					stderr,
				)
			}
			if after := testkit.HashDirectory(t, workspace); after != before {
				t.Fatalf("args=%q changed workspace hash from %q to %q", scenario.args, before, after)
			}

			if scenario.humanMarker != "" {
				if !strings.Contains(stdout, scenario.humanMarker) {
					t.Fatalf("stdout = %q, want %q", stdout, scenario.humanMarker)
				}
				return
			}
			payload := clijson.DecodePlan(t, []byte(stdout))
			if payload.Command != scenario.jsonCommand ||
				payload.ActionCount != 0 ||
				len(payload.Actions) != 0 ||
				payload.HasErrors {
				t.Fatalf("payload = %#v, want clean zero-action %s plan", payload, scenario.jsonCommand)
			}
		})
	}
}

func writeRetiredEmptyArtifact(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runBuiltCLIForSetup(t *testing.T, binaryPath string, workDir string, args ...string) {
	t.Helper()
	stdout, stderr, exitCode := runBuiltCLI(t, binaryPath, workDir, args...)
	if exitCode != 0 || stderr != "" {
		t.Fatalf("setup args=%q exitCode=%d stdout=%q stderr=%q", args, exitCode, stdout, stderr)
	}
}

func runBuiltCLI(t *testing.T, binaryPath string, workDir string, args ...string) (string, string, int) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := exec.Command(binaryPath, args...)
	command.Dir = workDir
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if err == nil {
		return stdout.String(), stderr.String(), 0
	}
	exitError, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("run built daem args=%q: %v", args, err)
	}
	return stdout.String(), stderr.String(), exitError.ExitCode()
}
