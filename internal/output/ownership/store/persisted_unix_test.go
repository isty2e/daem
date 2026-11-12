//go:build darwin || linux

package store

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/output/ownership"
)

func TestLocatedStoreConvergenceRetainsLookupEvidenceAndPrunesDepartedRoles(t *testing.T) {
	root := canonicalTestRoot(t)
	alias := filepath.Join(root, "lookup")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	retiring := testActiveClaim(t, root, "owner", filepath.Join(root, "old.json"), "")
	foreign := testActiveClaim(t, root, "foreign", filepath.Join(root, "foreign.json"), "")
	added := testActiveClaim(t, root, "owner", filepath.Join(root, "new.json"), "")
	binding := storedDocumentBinding(t, retiring, "old.json")
	before, err := ownership.NewRegistry([]ownership.Claim{retiring, foreign}, binding)
	if err != nil {
		t.Fatal(err)
	}
	ownerReference := mustImageReference(t, retiring.Owner().StatefileAuthority(), filepath.Join(alias, "owner", ".daem", "state.json"))
	references := []pathauthority.Reference{
		ownerReference,
		mustImageReference(t, retiring.Address().PathAuthority(), filepath.Join(alias, "old.json")),
		observedStoreReference(t, foreign.Address().Path()),
		observedStoreReference(t, foreign.Owner().StatefileKey()),
	}
	store := mustStore(t, filepath.Join(root, "claims.json"))
	writeLocatedOwnership(t, store.Path(), before, references)
	if loaded, err := store.Load(t.Context()); err != nil || !loaded.Equal(before) {
		t.Fatal("located read lost qualified facts", err)
	}
	claims, err := ownership.NewClaimConvergence([]ownership.ClaimChange{
		mustStoreClaimChange(t, retiring.Address(), storeClaimValue(t, retiring), ownership.NoClaim()),
		mustStoreClaimChange(t, added.Address(), ownership.NoClaim(), storeClaimValue(t, added)),
	})
	if err != nil {
		t.Fatal(err)
	}
	bindings := mustStoreBindingConvergence(t, storeBindingIntent(t, binding, true, false))
	got, err := store.ConvergeRegistry(t.Context(), claims, bindings,
		observedStoreReference(t, added.Address().Path()), observedStoreReference(t, added.Owner().StatefileKey()))
	if err != nil || len(got.Claims()) != 2 || len(got.DocumentBindings()) != 0 {
		t.Fatal("located joint convergence failed", err)
	}
	image := readLocatedOwnership(t, store.Path())
	retained, present := image.Reference(ownerReference.Identity())
	if !present || !retained.Equal(ownerReference) {
		t.Fatal("fresh selected spelling replaced retained owner lookup evidence")
	}
	if _, present := image.Reference(retiring.Address().PathAuthority()); present {
		t.Fatal("departed claim and binding retained their reference")
	}
	if _, err := store.RemoveClaim(t.Context(), foreign); err != nil {
		t.Fatal(err)
	}
	image = readLocatedOwnership(t, store.Path())
	if len(image.References()) != 2 || len(image.Registry().Claims()) != 1 {
		t.Fatal("claim removal did not prune the departed foreign owner")
	}
	beforeBytes, _ := os.ReadFile(store.Path())
	identityBefore, err := os.Stat(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	value := storeClaimValue(t, added)
	if _, err := store.Apply(t.Context(), added.Address(), value, value); err != nil {
		t.Fatal(err)
	}
	identityAfter, _ := os.Stat(store.Path())
	afterBytes, _ := os.ReadFile(store.Path())
	if !os.SameFile(identityBefore, identityAfter) || !bytes.Equal(beforeBytes, afterBytes) {
		t.Fatal("idempotent located update rewrote bytes or entry identity")
	}
}

func TestLocatedStoreRefusesUnqualifiedAdditionsBeforeChangingTheRegistry(t *testing.T) {
	root := canonicalTestRoot(t)
	store := mustStore(t, filepath.Join(root, "claims.json"))
	before := writeLocatedOwnership(t, store.Path(), ownership.EmptyRegistry(), nil)
	claim := testActiveClaim(t, root, "owner", filepath.Join(root, "document.json"), "")
	documentReference := observedStoreReference(t, claim.Address().Path())
	ownerReference := observedStoreReference(t, claim.Owner().StatefileKey())
	wrong := mustImageReference(t, documentReference.Identity(), filepath.Join(root, "other.json"))
	for _, test := range []struct {
		name string
		refs []pathauthority.Reference
	}{
		{name: "missing"},
		{name: "wrong namespace", refs: []pathauthority.Reference{wrong, ownerReference}},
		{name: "conflicting", refs: []pathauthority.Reference{documentReference, wrong, ownerReference}},
		{name: "unrelated", refs: []pathauthority.Reference{documentReference, ownerReference, observedStoreReference(t, filepath.Join(root, "extra.json"))}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if result, err := store.Apply(t.Context(), claim.Address(), ownership.NoClaim(), storeClaimValue(t, claim), test.refs...); err == nil || result.Equal(result) {
				t.Fatal("unqualified addition returned a usable successor", err)
			}
			actual, err := os.ReadFile(store.Path())
			if err != nil || !bytes.Equal(actual, before) {
				t.Fatal("refused addition changed the registry", err)
			}
		})
	}
	next, err := ownership.NewRegistry([]ownership.Claim{claim})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := store.PrepareRewrite(t.Context(), ownership.EmptyRegistry(), next, documentReference, ownerReference)
	if err != nil {
		t.Fatal(err)
	}
	preparedImage, err := UnmarshalImage(prepared)
	if err != nil || !preparedImage.Registry().Equal(next) || len(preparedImage.References()) != 2 {
		t.Fatal("prepared FileSet rewrite lost located facts or evidence", err)
	}
	actual, err := os.ReadFile(store.Path())
	if err != nil || !bytes.Equal(actual, before) {
		t.Fatal("preparing a rewrite published it", err)
	}
	var stale mutation.StaleSnapshotError
	if content, err := store.PrepareRewrite(t.Context(), next, next, documentReference, ownerReference); !errors.As(err, &stale) || content != nil {
		t.Fatal("stale FileSet preparation returned bytes", err)
	}
	if _, err := store.Apply(t.Context(), claim.Address(), ownership.NoClaim(), storeClaimValue(t, claim), documentReference, ownerReference); err != nil {
		t.Fatal("qualified addition was refused", err)
	}
	image := readLocatedOwnership(t, store.Path())
	if len(image.References()) != 2 || len(image.Registry().Claims()) != 1 {
		t.Fatal("qualified addition lost its complete image")
	}

	writeLocatedOwnership(t, store.Path(), image.Registry(), []pathauthority.Reference{wrong, ownerReference})
	if _, err := store.Load(t.Context()); err == nil {
		t.Fatal("normal located guard used the key instead of its recorded locator")
	}
}

func TestLookupEvidenceDoesNotSelectTheLegacyOwnershipOutputFormat(t *testing.T) {
	for _, seeded := range []bool{false, true} {
		root := canonicalTestRoot(t)
		store := mustStore(t, filepath.Join(root, "claims.json"))
		if seeded {
			content, err := Marshal(ownership.EmptyRegistry())
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(store.Path(), content, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		claim := testActiveClaim(t, root, "owner", filepath.Join(root, "document.json"), "")
		next, err := store.Apply(t.Context(), claim.Address(), ownership.NoClaim(), storeClaimValue(t, claim),
			observedStoreReference(t, claim.Owner().StatefileKey()), observedStoreReference(t, claim.Address().Path()))
		if err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(store.Path())
		if err != nil {
			t.Fatal(err)
		}
		legacy, err := DecodeRegistryFacts(content)
		if err != nil || !legacy.Equal(next) {
			t.Fatal("optional evidence changed the default format without cohort conversion", err)
		}
	}
}

func TestRootedLocatedStoreRetainsImageAcrossProtectionAndRemoval(t *testing.T) {
	root := canonicalTestRoot(t)
	path := filepath.Join(root, "claims.json")
	writeLocatedOwnership(t, path, ownership.EmptyRegistry(), nil)
	captured, destination, err := rootedpath.CaptureDestination(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = captured.Close() })
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewRooted(captured, destination, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	claim := testActiveClaim(t, root, "owner", filepath.Join(root, "protected.json"), "")
	binding := storedDocumentBinding(t, claim, "protected.json")
	claims, err := ownership.NewClaimConvergence(nil)
	if err != nil {
		t.Fatal(err)
	}
	addBinding := mustStoreBindingConvergence(t, storeBindingIntent(t, binding, false, true))
	if _, err := store.ConvergeRegistry(t.Context(), claims, addBinding,
		observedStoreReference(t, claim.Owner().StatefileKey()), observedStoreReference(t, claim.Address().Path())); err != nil {
		t.Fatal(err)
	}
	protection := storedPendingProtection(t, binding)
	arm, _ := ownership.NewArmProtection(protection)
	if _, err := store.TransitionProtection(t.Context(), claim.Owner(), arm); err != nil {
		t.Fatal(err)
	}
	image := readLocatedOwnership(t, path)
	if len(image.References()) != 2 || len(image.Registry().PendingProtections()) != 1 {
		t.Fatal("protection-only image lost its reference cohort")
	}
	assertProtectionNoOp(t, store, claim.Owner(), arm, image.Registry())
	clear, _ := ownership.NewClearProtection(protection)
	if _, err := store.TransitionProtection(t.Context(), claim.Owner(), clear); err != nil {
		t.Fatal(err)
	}
	if len(readLocatedOwnership(t, path).References()) != 2 {
		t.Fatal("cleared protection pruned still-required binding references")
	}
	removeBinding := mustStoreBindingConvergence(t, storeBindingIntent(t, binding, true, false))
	if _, err := store.ConvergeRegistry(t.Context(), claims, removeBinding); err != nil {
		t.Fatal(err)
	}
	if len(readLocatedOwnership(t, path).References()) != 0 {
		t.Fatal("retired binding retained orphan references")
	}
	if _, err := store.Apply(t.Context(), claim.Address(), ownership.NoClaim(), storeClaimValue(t, claim),
		observedStoreReference(t, claim.Owner().StatefileKey()), observedStoreReference(t, claim.Address().Path())); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RemoveClaim(t.Context(), claim); err != nil {
		t.Fatal(err)
	}
	if len(readLocatedOwnership(t, path).References()) != 0 {
		t.Fatal("rooted removal retained departed references")
	}
}

func TestStaleLocatedSnapshotCannotReplaceANewerReferenceCohort(t *testing.T) {
	root := canonicalTestRoot(t)
	store := mustStore(t, filepath.Join(root, "claims.json"))
	writeLocatedOwnership(t, store.Path(), ownership.EmptyRegistry(), nil)
	observations := newPathAuthorityObservationSession()
	before, identity, exists, _, err := store.loadForUpdate(t.Context(), nil, observations)
	if err != nil || !exists {
		t.Fatal(err)
	}
	claim := testActiveClaim(t, root, "owner", filepath.Join(root, "document.json"), "")
	refs := []pathauthority.Reference{observedStoreReference(t, claim.Address().Path()), observedStoreReference(t, claim.Owner().StatefileKey())}
	if _, err := store.Apply(t.Context(), claim.Address(), ownership.NoClaim(), storeClaimValue(t, claim), refs...); err != nil {
		t.Fatal(err)
	}
	staleContent, err := before.encodeSuccessor(t.Context(), ownership.EmptyRegistry(), nil, observations)
	if err != nil {
		t.Fatal(err)
	}
	request, err := storagecommit.NewFileReplacement(store.Path(), staleContent, 0o600, identity)
	if err != nil {
		t.Fatal(err)
	}
	if err := storagecommit.CommitFile(t.Context(), request); err == nil {
		t.Fatal("stale document replaced a newer reference cohort")
	}
	if len(readLocatedOwnership(t, store.Path()).References()) != 2 {
		t.Fatal("stale commit erased qualified references")
	}
}

func TestLocatedDocumentSettlementRetainsTheMissingComparisonException(t *testing.T) {
	fixture := newDocumentSettlementFixture(t, false)
	var references []pathauthority.Reference
	for _, identity := range RequiredReferenceIdentities(fixture.before) {
		if fixture.missing[0].MatchesMissingExact(identity) {
			references = append(references, mustImageReference(t, identity, fixture.hostPath))
		} else {
			references = append(references, observedStoreReference(t, identity.Key()))
		}
	}
	before := writeLocatedOwnership(t, fixture.registryPath, fixture.before, references)
	if _, err := fixture.store.ConvergeRegistry(t.Context(), fixture.claims, fixture.bindings); err == nil {
		t.Fatal("located input widened the ordinary missing-document guard")
	}
	content, err := os.ReadFile(fixture.registryPath)
	if err != nil || !bytes.Equal(content, before) {
		t.Fatal("refused ordinary settlement changed its located image", err)
	}
	for attempt := 0; attempt != 2; attempt++ {
		got, err := fixture.store.ConvergeForDocuments(t.Context(), fixture.claims, fixture.bindings, fixture.owner, fixture.missing, physicalwork.MaximumPhysicalPathDepth, fixture.budget)
		if err != nil || !got.Equal(fixture.target) {
			t.Fatal("located document comparison could not settle", err)
		}
		image := readLocatedOwnership(t, fixture.registryPath)
		if !image.Registry().Equal(fixture.target) || len(image.References()) != len(references) {
			t.Fatal("document settlement lost its retained cohort")
		}
		for _, reference := range references {
			retained, present := image.Reference(reference.Identity())
			if !present || !retained.Equal(reference) {
				t.Fatal("document settlement replaced retained lookup evidence")
			}
		}
	}
}

func observedStoreReference(t *testing.T, path string) pathauthority.Reference {
	t.Helper()
	observed, err := mutation.ObservePersistedDirectoryEntryAuthority(path)
	if err != nil {
		t.Fatal(err)
	}
	return observed.Reference()
}

func writeLocatedOwnership(t *testing.T, path string, registry ownership.Registry, references []pathauthority.Reference) []byte {
	t.Helper()
	image, err := NewRegistryImage(registry, references)
	if err != nil {
		t.Fatal(err)
	}
	content, err := MarshalImage(image)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return content
}

func readLocatedOwnership(t *testing.T, path string) RegistryImage {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	image, err := UnmarshalImage(content)
	if err != nil {
		t.Fatal("normal update did not preserve a located image", err)
	}
	return image
}
