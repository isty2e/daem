//go:build darwin || linux

package namespace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/protection"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/test/ownershiptest"
)

func TestCaptureProtectedDocumentFollowsOnlyTheRecordedSourceReferent(t *testing.T) {
	fixture := ownershiptest.PermissionRecovery(t, ".pi/settings.json")
	alias := fixture.Documents[0]
	actual := filepath.Join(filepath.Dir(alias), "actual.json")
	if err := os.Rename(alias, actual); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("actual.json", alias); err != nil {
		t.Fatal(err)
	}
	observed, err := mutation.ObservePersistedDirectoryEntryAuthority(actual)
	if err != nil {
		t.Fatal(err)
	}
	before := fixture.Protection.Documents()[0]
	binding, err := ownership.NewDocumentBinding(fixture.Owner, before.Bindings()[0].Destination(), observed.Exact())
	if err != nil {
		t.Fatal(err)
	}
	document, err := ownership.NewProtectedDocument([]ownership.DocumentBinding{binding}, before.UserID(), before.GroupID(), before.ModeCap())
	if err != nil {
		t.Fatal(err)
	}
	resolution := NewResolution(fixture.Paths, fixture.Owner)
	root, destination, err := resolution.CaptureDocument(t.Context(), document, physicalwork.NewCaptureBudget(nil))
	if err != nil {
		t.Fatal("same-source leaf alias was not admitted for permission capture", err)
	}
	selected, err := destination.LexicalPath()
	if err != nil {
		t.Fatal(err)
	}
	left, err := os.Stat(selected)
	if err != nil {
		t.Fatal(err)
	}
	right, err := os.Stat(actual)
	if err != nil || !os.SameFile(left, right) {
		t.Fatal("permission capture did not retain the recorded referent", err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(filepath.Dir(alias), "other.json"), []byte("other"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("other.json", alias); err != nil {
		t.Fatal(err)
	}
	var failure *protection.Failure
	if root, _, err := resolution.CaptureDocument(t.Context(), document, physicalwork.NewCaptureBudget(nil)); !errors.As(err, &failure) || failure.Kind() != protection.DocumentNamespaceUnqualified {
		if root != nil {
			root.Close()
		}
		t.Fatal("permission capture did not identify its changed source namespace", err)
	}
}
