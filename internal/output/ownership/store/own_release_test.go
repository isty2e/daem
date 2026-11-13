package store

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output/ownership"
)

func storedOwnRelease(t *testing.T, binding ownership.DocumentBinding, before []ownership.Claim, remove []ownership.ManagedAddress, retain bool) ownership.OwnDocumentRelease {
	t.Helper()
	scope, err := ownership.NewDocumentScope([]pathauthority.Exact{binding.PathAuthority()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	value, err := ownership.PresentDocumentBinding(binding)
	if err != nil {
		t.Fatal(err)
	}
	release, err := ownership.NewOwnDocumentRelease(ownership.OwnDocumentReleaseInput{
		Owner: binding.Owner(), Scope: scope,
		BeforeClaims: before, RemovalAddresses: remove, BindingDestination: binding.Destination(), BindingBefore: value, RetainBinding: retain,
	})
	if err != nil {
		t.Fatal(err)
	}
	return release
}

func writeOwnReleaseRegistry(t *testing.T, store Store, registry ownership.Registry, located bool, references ...pathauthority.Reference) {
	t.Helper()
	var content []byte
	var err error
	if located {
		image, imageErr := NewRegistryImage(registry, references)
		if imageErr != nil {
			t.Fatal(imageErr)
		}
		content, err = MarshalImage(image)
	} else {
		content, err = Marshal(registry)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(), content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestStoreOwnReleaseDoesNotReobserveHostOrOwnerLocators(t *testing.T) {
	for _, located := range []bool{false, true} {
		root := canonicalTestRoot(t)
		host := filepath.Join(root, "host.json")
		if err := os.WriteFile(host, []byte("original-host"), 0o600); err != nil {
			t.Fatal(err)
		}
		own := testActiveClaim(t, root, "own", host, "/selected")
		retained := testActiveClaim(t, root, "own", host, "/retained")
		foreign := testActiveClaim(t, root, "foreign", host, "/foreign")
		binding := storedDocumentBinding(t, own, ".pi/settings.json")
		foreignBinding := storedDocumentBinding(t, foreign, ".pi/settings.json")
		registry, err := ownership.NewRegistry([]ownership.Claim{own, retained, foreign}, binding, foreignBinding)
		if err != nil {
			t.Fatal(err)
		}
		store := mustStore(t, filepath.Join(root, "claims.json"))
		pool := append(storeClaimReferences(t, own), storeClaimReferences(t, foreign)...)
		writeOwnReleaseRegistry(t, store, registry, located, pool...)
		release := storedOwnRelease(t, binding, []ownership.Claim{own, retained}, []ownership.ManagedAddress{own.Address()}, true)
		if err := os.Remove(host); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(host, 0o700); err != nil {
			t.Fatal(err)
		}
		result, err := store.ConvergeOwnReleaseAfterCommit(t.Context(), release)
		want, _ := ownership.NewRegistry([]ownership.Claim{retained, foreign}, binding, foreignBinding)
		if err != nil || !result.Equal(want) {
			t.Fatal("own release required a current host file or discarded survivors", located, err)
		}
		imageBefore, err := os.ReadFile(store.Path())
		if err != nil {
			t.Fatal(err)
		}
		actual, err := store.LoadProtectionRegistry(t.Context())
		if err != nil || !actual.Equal(want) {
			t.Fatal("release persisted a different own/foreign fragment", err)
		}
		if _, err := store.ConvergeOwnReleaseAfterCommit(t.Context(), release); err != nil {
			t.Fatal(err)
		}
		imageAfter, err := os.ReadFile(store.Path())
		if err != nil || !bytes.Equal(imageBefore, imageAfter) {
			t.Fatal("idempotent release rewrote metadata", err)
		}
		if info, err := os.Stat(host); err != nil || !info.IsDir() {
			t.Fatal("metadata release touched the replacement host entry", err)
		}
		document, err := decodeRegistryDocument(imageAfter)
		if err != nil || (document.references.Validate() == nil) != located {
			t.Fatal("release changed the loaded physical Registry generation", err)
		}
	}
}

func TestRootedStoreOwnReleaseKeepsANewForeignOwnerAndItsReferences(t *testing.T) {
	root := canonicalTestRoot(t)
	host := filepath.Join(root, "host.json")
	old := testActiveClaim(t, root, "old", host, "/selected")
	foreign := testActiveClaim(t, root, "new", host, "")
	oldBinding := storedDocumentBinding(t, old, ".pi/settings.json")
	foreignBinding := storedDocumentBinding(t, foreign, ".pi/settings.json")
	registry, err := ownership.NewRegistry([]ownership.Claim{foreign}, oldBinding, foreignBinding)
	if err != nil {
		t.Fatal(err)
	}
	store := mustStore(t, filepath.Join(root, "claims.json"))
	pool := append(storeClaimReferences(t, old), storeClaimReferences(t, foreign)...)
	writeOwnReleaseRegistry(t, store, registry, true, pool...)
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	captured, destination, err := rootedpath.CaptureDestinationBounded(store.Path(), physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := captured.Close(); err != nil {
			t.Error(err)
		}
	}()
	rooted, err := NewRooted(captured, destination, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	release := storedOwnRelease(t, oldBinding, []ownership.Claim{old}, []ownership.ManagedAddress{old.Address()}, false)
	result, err := rooted.ConvergeOwnReleaseAfterCommit(t.Context(), release)
	want, _ := ownership.NewRegistry([]ownership.Claim{foreign}, foreignBinding)
	if err != nil || !result.Equal(want) {
		t.Fatal("rooted release reacquired or deleted foreign ownership", err)
	}
	content, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	image, err := UnmarshalImage(content)
	if err != nil || !image.Registry().Equal(want) {
		t.Fatal("located successor lost foreign metadata", err)
	}
	for _, reference := range storeClaimReferences(t, foreign) {
		actual, present := image.Reference(reference.Identity())
		if !present || !actual.Equal(reference) {
			t.Fatal("release replaced a surviving owner's recorded locator")
		}
	}
	if _, present := image.Reference(old.Owner().StatefileAuthority()); present {
		t.Fatal("removed owner reference survived without any member")
	}
}

func TestStoreOwnReleaseRefusesProtectionAndOwnDriftWithoutPublishing(t *testing.T) {
	root := canonicalTestRoot(t)
	host := filepath.Join(root, "host.json")
	own := testActiveClaim(t, root, "own", host, "/selected")
	extra := testActiveClaim(t, root, "own", host, "/unexpected")
	binding := storedDocumentBinding(t, own, ".pi/settings.json")
	release := storedOwnRelease(t, binding, []ownership.Claim{own}, []ownership.ManagedAddress{own.Address()}, false)
	protection := storedPendingProtection(t, binding)
	protected, err := ownership.NewRegistryWithProtections([]ownership.Claim{own}, []ownership.DocumentBinding{binding}, []ownership.PendingProtection{protection})
	if err != nil {
		t.Fatal(err)
	}
	drift, err := ownership.NewRegistry([]ownership.Claim{own, extra}, binding)
	if err != nil {
		t.Fatal(err)
	}
	for _, registry := range []ownership.Registry{protected, drift} {
		store := mustStore(t, filepath.Join(root, "claims.json"))
		writeOwnReleaseRegistry(t, store, registry, true, storeClaimReferences(t, own)...)
		before, err := os.ReadFile(store.Path())
		if err != nil {
			t.Fatal(err)
		}
		if result, err := store.ConvergeOwnReleaseAfterCommit(t.Context(), release); err == nil || result.Equal(result) {
			t.Fatal("refused release exposed a partial successor", err)
		}
		after, err := os.ReadFile(store.Path())
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("refused release changed registry bytes", err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	store := mustStore(t, filepath.Join(root, "claims.json"))
	if _, err := store.ConvergeOwnReleaseAfterCommit(ctx, release); !errors.Is(err, context.Canceled) {
		t.Fatal("release lost cancellation", err)
	}
}
