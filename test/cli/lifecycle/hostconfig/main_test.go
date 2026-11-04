package cli_test

import (
	"os"
	"testing"

	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestMain(m *testing.M) {
	os.Exit(nativefixture.RunPackage(m))
}
