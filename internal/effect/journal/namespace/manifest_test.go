package namespace

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/test/outputtest"
)

func TestManifestAuthorityClosesOnlyItsOwnedRoot(t *testing.T) {
	selected := nativeNamespaceRoot(t, t.TempDir())
	destination := outputtest.Parse(t, "AGENTS.md")
	borrowed, err := CaptureManifestAuthority(selected.SelectedPath, selected.Root, nil)
	if err != nil {
		t.Fatal(err)
	}
	owned, err := CaptureManifestAuthority(selected.SelectedPath, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if borrowed.Provenance() != owned.Provenance() {
		t.Fatal("independent capture changed the same manifest incarnation")
	}
	if err := borrowed.Close(); err != nil {
		t.Fatal(err)
	}
	capability, err := borrowed.Acquire(destination)
	if err != nil {
		t.Fatal("borrowed close released the containing root", err)
	}
	if err := capability.Close(); err != nil {
		t.Fatal(err)
	}
	if err := owned.Close(); err != nil {
		t.Fatal(err)
	}
	if capability, err := owned.Acquire(destination); err == nil || capability != nil {
		t.Fatal("closed owned manifest supplied a capability", err)
	}
	if err := owned.Close(); err != nil {
		t.Fatal("repeated owned Close failed", err)
	}
	if _, err := selected.Root.Authority(); err != nil {
		t.Fatal("owned manifest close released an independent root", err)
	}
}

func TestManifestRecaptureRetainsItsAcquisitionAllocation(t *testing.T) {
	selected := nativeNamespaceRoot(t, t.TempDir())
	borrowed, err := CaptureManifestAuthority(selected.SelectedPath, selected.Root, nil)
	if err != nil {
		t.Fatal(err)
	}
	budget := physicalwork.NewCaptureBudget(nil)
	owned, err := RecaptureManifestAuthority(selected.SelectedPath, borrowed.Provenance(), budget)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := owned.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := selected.Root.Close(); err != nil {
		t.Fatal(err)
	}
	destination := outputtest.Parse(t, "AGENTS.md")
	capability, err := owned.Acquire(destination)
	if err != nil {
		t.Fatal("independently recaptured manifest borrowed the old descriptor", err)
	}
	if err := capability.Close(); err != nil {
		t.Fatal(err)
	}
	for budget.AdmitPathComponents(1) == nil {
	}
	if capability, err := owned.Acquire(destination); err == nil || capability != nil {
		t.Fatal("recaptured manifest escaped its exhausted allocation", err)
	}
	for _, replacement := range []*physicalwork.Budget{nil, physicalwork.NewCaptureBudget(nil)} {
		if capability, err := owned.AcquireBounded(destination, replacement); err == nil || capability != nil {
			t.Fatal("recaptured manifest substituted its original allocation", err)
		}
	}
}
