package scale

import (
	"os"
	"testing"
)

const environmentVariable = "DAEM_TEST_SCALE"

// Require skips resource-scaling evidence outside the explicit scale lane.
func Require(t testing.TB) {
	t.Helper()
	if os.Getenv(environmentVariable) != "1" {
		t.Skip("resource-scaling evidence runs in tools/test.sh scale")
	}
}
