package cli_test

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestMain(m *testing.M) {
	if isWorkspaceMutationCLIHelperInvocation() {
		os.Exit(m.Run())
	}
	if err := preserveGoBuildCaches(); err != nil {
		fmt.Fprintf(os.Stderr, "preserve Go build caches: %v\n", err)
		os.Exit(1)
	}
	os.Exit(testkit.RunWithIsolatedDefaultRoots(m))
}

func preserveGoBuildCaches() error {
	for _, name := range []string{"GOMODCACHE", "GOCACHE"} {
		if os.Getenv(name) != "" {
			continue
		}
		command := exec.Command("go", "env", name)
		output, err := command.Output()
		if err != nil {
			return fmt.Errorf("go env %s: %w", name, err)
		}
		value := strings.TrimSpace(string(output))
		if value == "" {
			return fmt.Errorf("go env %s returned an empty path", name)
		}
		if err := os.Setenv(name, value); err != nil {
			return fmt.Errorf("set %s: %w", name, err)
		}
	}
	return nil
}

func TestWorkspaceMutationCLIHelperInvocationRequiresExactProcessShape(t *testing.T) {
	for name, testCase := range map[string]struct {
		environment string
		arguments   []string
		want        bool
	}{
		"canonical helper":    {environment: "1", arguments: []string{"daem.test", workspaceMutationCLIHelperRunArg, "--", "apply"}, want: true},
		"stale environment":   {environment: "1", arguments: []string{"daem.test", "-test.run=TestRunApply", "--", "apply"}},
		"missing environment": {arguments: []string{"daem.test", workspaceMutationCLIHelperRunArg, "--", "apply"}},
		"missing separator":   {environment: "1", arguments: []string{"daem.test", workspaceMutationCLIHelperRunArg, "apply"}},
		"missing command":     {environment: "1", arguments: []string{"daem.test", workspaceMutationCLIHelperRunArg, "--"}},
		"empty command":       {environment: "1", arguments: []string{"daem.test", workspaceMutationCLIHelperRunArg, "--", ""}},
		"selector displaced":  {environment: "1", arguments: []string{"daem.test", "-test.v", workspaceMutationCLIHelperRunArg, "--", "apply"}},
	} {
		t.Run(name, func(t *testing.T) {
			if got := workspaceMutationCLIHelperInvocation(testCase.environment, testCase.arguments); got != testCase.want {
				t.Fatalf("workspaceMutationCLIHelperInvocation() = %t, want %t", got, testCase.want)
			}
		})
	}
}

func isWorkspaceMutationCLIHelperInvocation() bool {
	return workspaceMutationCLIHelperInvocation(os.Getenv(workspaceMutationCLIHelperEnv), os.Args)
}

func workspaceMutationCLIHelperInvocation(environment string, arguments []string) bool {
	return environment == "1" &&
		len(arguments) >= 4 &&
		arguments[1] == workspaceMutationCLIHelperRunArg &&
		arguments[2] == "--" &&
		arguments[3] != ""
}
