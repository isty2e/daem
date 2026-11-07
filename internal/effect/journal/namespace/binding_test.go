package namespace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestWriterBindingReobservesAliasesBeforeExposingBorrowedRoot(t *testing.T) {
	root := nativeNamespaceRoot(t, t.TempDir())
	directory := filepath.Join(root.SelectedPath, ".pi")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"first.json", "second.json"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	alias := nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/settings.json")
	link := filepath.Join(directory, "settings.json")
	if err := os.Symlink("first.json", link); err != nil {
		t.Fatal(err)
	}
	reader := &nativeNamespaceReader{RootedReader: journalTestFilesystem()}
	budget := imageBudget(t)
	namespace, err := Observe(t.Context(), []aggregate.DocumentAddress{alias}, fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: root}), reader, budget)
	if err != nil {
		t.Fatal(err)
	}
	want, err := root.Root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	borrowed, bound, err := namespace.WriterBinding(t.Context(), budget)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := borrowed.Authority()
	if err != nil || !authority.Equal(want) || !bound.Root().Equal(want) {
		t.Fatalf("writer binding changed borrowed mount authority: %v", err)
	}
	path, err := bound.LexicalPath()
	if err != nil {
		t.Fatal(err)
	}
	actual, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.Stat(filepath.Join(directory, "first.json"))
	if err != nil || !os.SameFile(actual, expected) {
		t.Fatalf("writer binding did not select the observed physical file: %v", err)
	}
	for _, capability := range reader.capabilities {
		if capability.Validate() == nil {
			_ = capability.Close()
			t.Fatal("writer binding retained an observation capability")
		}
	}

	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("second.json", link); err != nil {
		t.Fatal(err)
	}
	borrowed, bound, err = namespace.WriterBinding(t.Context(), budget)
	if err == nil || borrowed != nil || bound.Validate() == nil {
		t.Fatal("changed alias exposed its cached borrowed route")
	}
	if _, err := root.Root.Authority(); err != nil {
		t.Fatalf("failed binding closed a caller-owned root: %v", err)
	}
	if borrowed, _, err := (DocumentNamespace{}).WriterBinding(t.Context(), budget); err == nil || borrowed != nil {
		t.Fatal("zero namespace exposed a borrowed root")
	}
}
