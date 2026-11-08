//go:build darwin || linux

package removaloperation

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/execute/controlwitness"
	"github.com/isty2e/daem/internal/effect/execute/mutationnamespace"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/hostpath"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func testFilesystem() storagecommit.Adapter { return storagecommit.Adapter{} }

func newProjectRootForMutationTest(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create project root: %v", err)
	}
	return root
}

func projectMutationDestinationForTest(t *testing.T, root, relative string) (*Operation, mutationnamespace.Destination) {
	t.Helper()
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	phase, err := physicalwork.NewTraversalPhase(budget)
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := mutationnamespace.New(func(destination output.Destination, _ rootedpath.PhysicalTraversalBudget) (string, error) {
		return hostpath.NewResolver(root).Resolve(destination)
	}, phase)
	if err != nil {
		t.Fatal(err)
	}
	captured, err := rootedpath.CaptureRootBounded(root, physicalwork.MaximumPhysicalPathDepth, phase)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = captured.Close() })
	if err := namespace.BindProject(captured, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := namespace.Close(); err != nil {
			t.Error(err)
		}
	})
	operation := New(namespace, testFilesystem(), controlwitness.New(testFilesystem()), budget, phase)
	if err := operation.PrepareDemands(emptyRemovalDemandsForTest(), budget); err != nil {
		t.Fatal(err)
	}
	destination, err := namespace.Resolve(target.ScopeProject, outputtest.Parse(t, relative))
	if err != nil {
		t.Fatal(err)
	}
	return operation, destination
}

func writeMutationTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create parent for %q: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

func assertMutationTestFile(t *testing.T, path string, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	if string(content) != want {
		t.Fatalf("content at %q = %q, want %q", path, content, want)
	}
}
