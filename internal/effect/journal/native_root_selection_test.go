package journal

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestNativeMissingRootSelectionRetainsOriginalAnchorWithoutCreatingIt(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(base, "new", "agent")
	budget := physicalwork.NewCaptureBudget(nil)
	root, err := CaptureNativeDocumentRoot(t.Context(), selected, budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Root.Close() })
	alias := nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/settings.json")
	roots := NativeDocumentRoots{PiAgent: root}
	namespace, err := ObserveNativeDocumentNamespace(t.Context(), []aggregate.DocumentAddress{alias}, roots, journalTestFilesystem(), budget)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(base, "new")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("observation created selected root scaffolding", err)
	}
	authority, err := root.Root.AuthorityBounded(budget)
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := authority.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(selected, 0o700); err != nil {
		t.Fatal(err)
	}
	reopened, err := captureRecordedNativeRoot(t.Context(), selected, persistedNativeRoot(provenance), budget)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Root.Close()
	current, err := ObserveNativeDocumentNamespace(t.Context(), []aggregate.DocumentAddress{alias}, NativeDocumentRoots{PiAgent: reopened}, journalTestFilesystem(), budget)
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := persistedNativeObservation(namespace.observation)
	if err != nil {
		t.Fatal(err)
	}
	if err := matchRetainedNativeObservation(recorded, current.observation); err != nil {
		t.Fatal("created root lost its admitted original namespace", err)
	}
	if current.writer.Root().PhysicalRoot() != namespace.writer.Root().PhysicalRoot() {
		t.Fatal("recovery replaced the original anchor with the created directory")
	}
}

func TestNativeMissingRootSelectionDoesNotGrantSiblingWriters(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(base, "agent")
	budget := physicalwork.NewCaptureBudget(nil)
	root, err := CaptureNativeDocumentRoot(t.Context(), selected, budget)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Root.Close()
	outside := filepath.Join(base, "outside.json")
	if err := os.WriteFile(outside, []byte("{\"outside\":true}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(selected, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(selected, "settings.json")); err != nil {
		t.Fatal(err)
	}
	alias := nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/settings.json")
	namespace, err := ObserveNativeDocumentNamespace(t.Context(), []aggregate.DocumentAddress{alias}, NativeDocumentRoots{PiAgent: root}, journalTestFilesystem(), budget)
	if err == nil || namespace.writerRoot != nil {
		t.Fatal("a missing-root anchor granted an unselected sibling writer")
	}
	actual, err := os.ReadFile(outside)
	if err != nil || string(actual) != "{\"outside\":true}\n" {
		t.Fatal("refusal changed the sibling", err)
	}
}

func TestNativeRecordedExistingRootCannotBecomeAnAncestorFallback(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(base, "agent")
	if err := os.Mkdir(selected, 0o700); err != nil {
		t.Fatal(err)
	}
	budget := physicalwork.NewCaptureBudget(nil)
	root, err := CaptureNativeDocumentRoot(t.Context(), selected, budget)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Root.Close()
	authority, err := root.Root.AuthorityBounded(budget)
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := authority.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(selected, selected+"-previous"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(selected, 0o700); err != nil {
		t.Fatal(err)
	}
	current, err := captureRecordedNativeRoot(t.Context(), selected, persistedNativeRoot(provenance), budget)
	if err == nil {
		_ = current.Root.Close()
		t.Fatal("an existing root incarnation change was disguised as first-root creation")
	}
}
