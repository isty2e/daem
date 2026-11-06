package statefileoperation

import (
	"errors"
	"os"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

func hasRootedPathFailureKind(err error, kind rootedpath.FailureKind) bool {
	var failure *rootedpath.Failure
	return errors.As(err, &failure) && failure.Kind() == kind
}

func assertHostFileContent(t *testing.T, path string, want string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read host file %q: %v", path, err)
	}
	if string(content) != want {
		t.Fatalf("host file %q = %q, want %q", path, content, want)
	}
}

func assertHostMissing(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("host path %q exists or stat failed unexpectedly: %v", path, err)
	}
}
