package nativefixture

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

const invocationProbeEnv = "DAEM_NATIVE_FIXTURE_INVOCATION_PROBE"

func TestMain(m *testing.M) {
	if os.Getenv(invocationProbeEnv) == "without-main" {
		os.Exit(testkit.RunWithIsolatedDefaultRoots(m))
	}
	os.Exit(RunPackage(m))
}

func TestRecoveryInvocationRejectsMissingExecutionContext(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		diagnostic string
	}{
		{"without-main", "native recovery invocation requires nativefixture.RunPackage in TestMain"},
		{"without-worker", "native recovery helper was unavailable"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestRecoveryInvocationProbe$")
			command.Env = append(os.Environ(), invocationProbeEnv+"="+scenario.name)
			output, err := command.CombinedOutput()
			if err == nil || !strings.Contains(string(output), scenario.diagnostic) {
				t.Fatalf("missing fixture context was not rejected: error=%v output=%s", err, output)
			}
		})
	}
}

func TestRecoveryInvocationProbe(t *testing.T) {
	if os.Getenv(invocationProbeEnv) == "" {
		return
	}
	fixture := RecoveryFixture{Manifest: filepath.Join(t.TempDir(), "daem.toml")}
	_, _, _ = InvokeRecoveryCommand(t, fixture, "none", "apply", "--yes")
	// Neither a zero nor a nonzero apparent CLI result is valid without a worker.
	os.Exit(0)
}
