//go:build windows

package commit

import (
	"os"
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

func TestWindowsRootedObservationChargesOnlyRootOpen(t *testing.T) {
	rootPath := t.TempDir()
	directoryPath := filepath.Join(rootPath, "observed", "state")
	if err := os.MkdirAll(directoryPath, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, payload := range map[string]string{"first.json": "payload", "second.json": "content"} {
		request, err := NewFileCreate(filepath.Join(directoryPath, name), []byte(payload), 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if err := CommitFile(t.Context(), request); err != nil {
			t.Fatal(err)
		}
	}

	budget := &windowsAccountingRecordingBudget{}
	root, err := rootedpath.CaptureRootNoFollow(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	authority, err := root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := rootedpath.NewRelativeDestination("observed/state")
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
	defer capability.Close()

	openCharges := budget.pathComponents
	limits, err := mutationfs.NewTreeTraversalLimits(2, 2, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateRootedDirectoryTree(t.Context(), capability, limits); err != nil {
		t.Fatalf("rooted tree validation = %v, want success", err)
	}
	if openCharges == 0 {
		t.Fatal("root open charged no path components")
	}
	if budget.entries != 0 || budget.bytes != 0 {
		t.Fatalf("observation charged entries=%d bytes=%d against the capability budget", budget.entries, budget.bytes)
	}
	if budget.pathComponents != openCharges {
		t.Fatalf(
			"observation path charges = %d after root open, want no additional path work",
			budget.pathComponents-openCharges,
		)
	}
}

func TestWindowsRootedFileReadChargesOnlyRootOpen(t *testing.T) {
	rootPath := t.TempDir()
	filePath := filepath.Join(rootPath, "observed.json")
	payload := []byte("payload")
	request, err := NewFileCreate(filePath, payload, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := CommitFile(t.Context(), request); err != nil {
		t.Fatal(err)
	}

	budget := &windowsAccountingRecordingBudget{}
	root, err := rootedpath.CaptureRootNoFollow(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	authority, err := root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := rootedpath.NewRelativeDestination("observed.json")
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
	defer capability.Close()

	openCharges := budget.pathComponents
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
	if budget.pathComponents != openCharges {
		t.Fatalf("rooted read charged %d extra path components", budget.pathComponents-openCharges)
	}
}
