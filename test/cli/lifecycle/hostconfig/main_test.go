package cli_test

import (
	"os"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestMain(m *testing.M) {
	os.Exit(testkit.RunWithIsolatedDefaultRoots(m))
}
