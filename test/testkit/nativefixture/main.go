package nativefixture

import (
	"fmt"
	"os"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

const RecoveryHelperEnv = "GO_WANT_NATIVE_KEY_RECOVERY_HELPER"

const helperUnavailableExit = 125

var packageMainInstalled bool

// RunPackage installs the native subprocess contract and isolated package roots.
func RunPackage(m *testing.M) int {
	packageMainInstalled = true
	helperRequested := os.Getenv(RecoveryHelperEnv) == "1"
	exitCode := testkit.RunWithIsolatedDefaultRoots(m)
	if helperRequested {
		// A real helper terminates with its CLI result or intended interruption.
		// Returning from M.Run means no worker ran or its test failed prematurely.
		fmt.Fprintln(os.Stderr, "native recovery helper did not complete its CLI invocation")
		return helperUnavailableExit
	}
	return exitCode
}
