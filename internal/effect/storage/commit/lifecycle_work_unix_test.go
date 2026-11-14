//go:build darwin || linux

package commit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

// Charging only full root validations is a lower bound on the operation's
// physical path work, independent of its pre-admitted storage projection.
type rootValidationWorkCapability struct {
	rootedpath.CommitCapability
	rootWork int
	budget   *physicalwork.Budget
}

func (capability *rootValidationWorkCapability) Validate() error {
	if capability.budget != nil {
		if err := capability.budget.AdmitPhysicalWork(capability.rootWork, 0, 0); err != nil {
			return err
		}
	}
	return capability.CommitCapability.Validate()
}

func (capability *rootValidationWorkCapability) ValidateDirectoryHandle(handle uintptr) error {
	if capability.budget != nil {
		if err := capability.budget.AdmitPhysicalWork(capability.rootWork, 0, 0); err != nil {
			return err
		}
	}
	return capability.CommitCapability.ValidateDirectoryHandle(handle)
}

func (capability *rootValidationWorkCapability) OpenRootDirectory() (*os.File, error) {
	if capability.budget != nil {
		if err := capability.budget.AdmitPhysicalWork(capability.rootWork, 0, 0); err != nil {
			return nil, err
		}
	}
	return capability.CommitCapability.OpenRootDirectory()
}

func TestRootedEntryRenameFitsProjectedWorkThroughNestedParents(t *testing.T) {
	rootPath := filepath.Join(canonicalTempDir(t), "captured", "nested", "root")
	relative, err := rootedpath.NewRelativeDestination(strings.Repeat("parent/", 16) + "active")
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(rootPath, filepath.FromSlash(relative.Path()))
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(source, "payload"), "retained", 0o600)

	root := captureRootForCommitTest(t, rootPath)
	authority, err := root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	destination, err := authority.Bind(relative)
	if err != nil {
		t.Fatal(err)
	}
	work, err := MaximumRootedEntryRenamePathWork(destination)
	if err != nil {
		t.Fatal(err)
	}
	inner, err := root.Acquire(destination)
	if err != nil {
		t.Fatal(err)
	}
	rootWork, err := destination.RootValidationWork()
	if err != nil {
		t.Fatal(err)
	}
	capability := &rootValidationWorkCapability{CommitCapability: inner, rootWork: rootWork}
	t.Cleanup(func() { _ = capability.Close() })
	expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewRootedEntryRename(capability, ".retained", expected)
	if err != nil {
		t.Fatal(err)
	}
	budget := physicalwork.NewCaptureBudget(nil)
	if err := budget.AdmitPhysicalWork(mutationfs.MaximumPhysicalPathComponentVisits-work, 0, 0); err != nil {
		t.Fatal(err)
	}
	capability.budget = budget

	outcome, err := CommitRootedEntryRename(t.Context(), request)
	if err != nil {
		t.Fatalf("rename exceeded its projected path work %d: %v", work, err)
	}
	assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeComplete)
	if _, err := os.Lstat(source); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source remains after rename: %v", err)
	}
	assertFile(t, filepath.Join(filepath.Dir(source), ".retained", "payload"), "retained", 0o600)
}
