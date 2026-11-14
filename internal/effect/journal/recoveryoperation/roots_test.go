package recoveryoperation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

func TestRecoveryRootsOwnOnlyTheirIndependentCaptures(t *testing.T) {
	profile, journal, borrowed := rootFixture(t)
	owned, err := profile.OpenRoots(t.Context(), journal)
	if err != nil {
		t.Fatal(err)
	}
	selection := owned.Selection()
	manifest, supplied := selection.Manifest()
	if !supplied || manifest.Root == nil || manifest.Root == borrowed {
		t.Fatal("disk recovery borrowed the containing operation's capture")
	}
	qualified, err := selection.Qualify(t.Context(), profile.Budget)
	if err != nil {
		t.Fatal(err)
	}
	grant, supplied := qualified.Manifest()
	reference, err := journal.ManifestRootReference()
	if err != nil || !supplied || grant.RootReference() != reference || reference.LocatorPolicy() != "selected-project-v1" {
		t.Fatal("recovery did not retain its recorded manifest locator", reference, err)
	}
	if err := owned.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := manifest.Root.Authority(); err == nil {
		t.Fatal("recovery root remained live after its owner closed")
	}
	if _, err := borrowed.Authority(); err != nil {
		t.Fatal("recovery closed an independently supplied root", err)
	}
	if err := owned.Close(); err != nil {
		t.Fatal("repeated close failed", err)
	}
	closed, closedSupplied := owned.Selection().Manifest()
	absent, absentSupplied := (*OwnedRoots)(nil).Selection().Manifest()
	if closedSupplied || absentSupplied || closed.Root != nil || absent.Root != nil {
		t.Fatal("closed or absent root ownership supplied a selected root")
	}
}

func TestRecoveryRootFailureReturnsNoOwnedPrefix(t *testing.T) {
	profile, journal, _ := rootFixture(t)
	profile.Manifest = "relative-root"
	if owned, err := profile.OpenRoots(t.Context(), journal); err == nil || owned != nil {
		t.Fatal("noncanonical root supplied recovery custody", err)
	}
	profile, journal, _ = rootFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if owned, err := profile.OpenRoots(ctx, journal); !errors.Is(err, context.Canceled) || owned != nil {
		t.Fatal("cancelled root selection exposed an owned prefix", err)
	}
	if owned, err := profile.OpenRoots(nil, journal); err == nil || owned != nil {
		t.Fatal("missing context supplied recovery custody", err)
	}
	if owned, err := profile.OpenRoots(t.Context(), journalrecord.Native{}); err == nil || owned != nil {
		t.Fatal("uninitialized record supplied selected recovery roots", err)
	}
}

func rootFixture(t *testing.T) (Profile, journalrecord.Native, *rootedpath.CapturedRoot) {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(path, "state.json")
	if err := os.WriteFile(statePath, []byte("state"), 0o600); err != nil {
		t.Fatal(err)
	}
	observed, err := mutation.ObservePersistedDirectoryEntryAuthority(statePath)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(observed.Exact(), filepath.Join(path, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := rootedpath.CaptureRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	authority, err := root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := authority.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	journal, err := journalrecord.NewNativeCapture("recovery-root-custody", time.Unix(0, 0), owner, provenance, durable.EmptySnapshot(), durable.EmptySnapshot(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return Profile{Manifest: path, ManifestPath: owner.ManifestPath(), Budget: physicalwork.NewCaptureBudget(nil)}, journal, root
}
