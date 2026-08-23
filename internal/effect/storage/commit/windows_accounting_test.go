//go:build windows

package commit

import (
	"path/filepath"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

type windowsAccountingRecordingBudget struct {
	pathComponents int
	entries        int
	bytes          int64
}

func (budget *windowsAccountingRecordingBudget) AdmitPathComponents(count int) error {
	budget.pathComponents += count
	return nil
}

func (budget *windowsAccountingRecordingBudget) AdmitPhysicalWork(pathComponents int, entries int, bytes int64) error {
	budget.pathComponents += pathComponents
	budget.entries += entries
	budget.bytes += bytes
	return nil
}

func acquireWindowsAccountingCapability(
	t *testing.T,
	rootPath string,
	relativePath string,
	budget *windowsAccountingRecordingBudget,
) rootedpath.CommitCapability {
	t.Helper()
	root, err := rootedpath.CaptureRootNoFollow(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	authority, err := root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := rootedpath.NewRelativeDestination(relativePath)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := authority.Bind(relative)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := root.AcquireBounded(destination, 64, budget)
	if err != nil {
		t.Fatal(err)
	}
	return capability
}

func calibrateWindowsRootOpenCharge(t *testing.T, rootPath string, relativePath string) int {
	t.Helper()
	budget := &windowsAccountingRecordingBudget{}
	capability := acquireWindowsAccountingCapability(t, rootPath, relativePath, budget)
	before := budget.pathComponents
	rootFile, err := capability.OpenRootDirectory()
	if err != nil {
		_ = capability.Close()
		t.Fatal(err)
	}
	charge := budget.pathComponents - before
	if err := rootFile.Close(); err != nil {
		_ = capability.Close()
		t.Fatal(err)
	}
	if err := capability.Close(); err != nil {
		t.Fatal(err)
	}
	if charge == 0 {
		t.Fatal("root open charged no path components")
	}
	return charge
}

func TestWindowsRootedObservationChargesOnlyRootOpen(t *testing.T) {
	rootPath := t.TempDir()
	directoryPath := filepath.Join(rootPath, "observed", "state")

	var preparation AncestorCleanup
	if err := preparation.PrepareParent(t.Context(), filepath.Join(directoryPath, "placeholder")); err != nil {
		t.Fatalf("prepare canonical observed directory: %v", err)
	}
	preparation.Close()
	for name, payload := range map[string]string{"first.json": "payload", "second.json": "content"} {
		request, err := NewFileCreate(filepath.Join(directoryPath, name), []byte(payload), 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if err := CommitFile(t.Context(), request); err != nil {
			t.Fatal(err)
		}
	}

	perOpen := calibrateWindowsRootOpenCharge(t, rootPath, "observed/state")
	budget := &windowsAccountingRecordingBudget{}
	capability := acquireWindowsAccountingCapability(t, rootPath, "observed/state", budget)
	defer capability.Close()

	limits, err := mutationfs.NewTreeTraversalLimits(2, 2, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateRootedDirectoryTree(t.Context(), capability, limits); err != nil {
		t.Fatalf("rooted tree validation = %v, want success", err)
	}
	if budget.entries != 0 || budget.bytes != 0 {
		t.Fatalf("observation charged entries=%d bytes=%d against the capability budget", budget.entries, budget.bytes)
	}
	if budget.pathComponents%perOpen != 0 || budget.pathComponents == 0 {
		t.Fatalf(
			"observation path charges = %d, want a whole number of root opens (%d components each)",
			budget.pathComponents,
			perOpen,
		)
	}
}

func TestWindowsRootedFileReadChargesOnlyRootOpen(t *testing.T) {
	rootPath := t.TempDir()
	payload := []byte("payload")
	request, err := NewFileCreate(filepath.Join(rootPath, "observed.json"), payload, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := CommitFile(t.Context(), request); err != nil {
		t.Fatal(err)
	}

	perOpen := calibrateWindowsRootOpenCharge(t, rootPath, "observed.json")
	budget := &windowsAccountingRecordingBudget{}
	capability := acquireWindowsAccountingCapability(t, rootPath, "observed.json", budget)
	defer capability.Close()

	content, _, _, err := ReadRootedRegularFileUpTo(t.Context(), capability, 1024)
	if err != nil {
		t.Fatalf("rooted read = %v, want success", err)
	}
	if string(content) != string(payload) {
		t.Fatalf("rooted read content = %q, want %q", content, payload)
	}
	if budget.entries != 0 || budget.bytes != 0 {
		t.Fatalf("rooted read charged entries=%d bytes=%d against the capability budget", budget.entries, budget.bytes)
	}
	if budget.pathComponents%perOpen != 0 || budget.pathComponents == 0 {
		t.Fatalf(
			"rooted read path charges = %d, want a whole number of root opens (%d components each)",
			budget.pathComponents,
			perOpen,
		)
	}
}
