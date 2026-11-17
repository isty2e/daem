package recoveryoperation

import (
	"os"
	"testing"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

func nativeNamespaceRoot(t *testing.T, path string) journalnamespace.RootSelection {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := rootedpath.CaptureRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return journalnamespace.RootSelection{SelectedPath: path, Root: root}
}
