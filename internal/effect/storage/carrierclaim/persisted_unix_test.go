//go:build darwin || linux

package carrierclaim

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

func TestNewCarrierRegistryRequiresAndRecordsOwnerReference(t *testing.T) {
	for _, qualified := range []bool{false, true} {
		t.Run(fmt.Sprint(qualified), func(t *testing.T) {
			root := t.TempDir()
			store, err := New(filepath.Join(root, "claims.json"))
			if err != nil {
				t.Fatal(err)
			}
			claim := testGlobalClaim(t, "alpha", "alpha@official", filepath.Join(root, "owner"))
			var references []pathauthority.Reference
			if qualified {
				references = []pathauthority.Reference{observedCarrierReference(t, claim.Owner().StatefileKey())}
			}
			_, err = store.Upsert(t.Context(), claim, references...)
			if !qualified {
				if err == nil {
					t.Fatal("new carrier registry accepted an unlocated owner")
				}
				if _, err := os.Stat(store.path); !os.IsNotExist(err) {
					t.Fatal("refused carrier addition created a registry", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			image := readLocatedCarrier(t, store.path)
			if len(image.Registry().Claims()) != 1 || len(image.References()) != 1 {
				t.Fatal("new carrier registry did not retain its owner reference")
			}
		})
	}
}

func TestLocatedCarrierStoreQualifiesAdditionsAndRetainsThenPrunesOwners(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "lookup")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	first := testGlobalClaim(t, "alpha", "alpha@official", filepath.Join(root, "first"))
	second := testGlobalClaim(t, "beta", "beta@official", filepath.Join(root, "second"))
	initial, err := durablecarrier.NewGlobalCarrierClaims([]durablecarrier.ManagedCarrierClaim{first})
	if err != nil {
		t.Fatal(err)
	}
	firstReference := observedCarrierReference(t, first.Owner().StatefileKey())
	relative, err := filepath.Rel(root, firstReference.Locator())
	if err != nil {
		t.Fatal(err)
	}
	aliasReference, err := pathauthority.NewReference(firstReference.Identity(), filepath.Join(alias, relative))
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(filepath.Join(root, "claims.json"))
	if err != nil {
		t.Fatal(err)
	}
	before := writeLocatedCarrier(t, store.path, initial, []pathauthority.Reference{aliasReference})
	secondReference := observedCarrierReference(t, second.Owner().StatefileKey())
	wrong, err := pathauthority.NewReference(secondReference.Identity(), firstReference.Locator())
	if err != nil {
		t.Fatal(err)
	}
	for _, references := range [][]pathauthority.Reference{nil, {wrong}, {secondReference, wrong}, {secondReference, observedCarrierReference(t, filepath.Join(root, "extra.json"))}} {
		if got, err := store.UpsertAllIfCurrent(t.Context(), initial, []durablecarrier.ManagedCarrierClaim{second}, references...); err == nil || len(got.Claims()) != 0 {
			t.Fatal("unqualified owner addition produced a successor", err)
		}
		actual, err := os.ReadFile(store.path)
		if err != nil || !bytes.Equal(actual, before) {
			t.Fatal("refused addition changed registry bytes", err)
		}
	}
	expectedNext, _, err := initial.WithClaims([]durablecarrier.ManagedCarrierClaim{second})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := store.PrepareRewrite(t.Context(), initial, expectedNext, secondReference, firstReference)
	if err != nil {
		t.Fatal(err)
	}
	preparedImage, err := UnmarshalImage(prepared)
	if err != nil || !preparedImage.Registry().Equal(expectedNext) || len(preparedImage.References()) != 2 {
		t.Fatal("prepared carrier rewrite lost its located image", err)
	}
	actual, err := os.ReadFile(store.path)
	if err != nil || !bytes.Equal(actual, before) {
		t.Fatal("preparing a rewrite published its bytes", err)
	}
	var stale mutation.StaleSnapshotError
	if content, err := store.PrepareRewrite(t.Context(), expectedNext, expectedNext, secondReference); !errors.As(err, &stale) || content != nil {
		t.Fatal("stale carrier rewrite returned bytes", err)
	}
	next, err := store.UpsertAllIfCurrent(t.Context(), initial, []durablecarrier.ManagedCarrierClaim{second}, secondReference, firstReference)
	if err != nil {
		t.Fatal("qualified owner addition failed", err)
	}
	image := readLocatedCarrier(t, store.path)
	retained, present := image.Reference(firstReference.Identity())
	if !present || !retained.Equal(aliasReference) || len(image.References()) != 2 {
		t.Fatal("ordinary write replaced a recorded locator or lost an owner")
	}
	beforeNoOp, err := os.Stat(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertAllIfCurrent(t.Context(), next, []durablecarrier.ManagedCarrierClaim{second}); err != nil {
		t.Fatal(err)
	}
	afterNoOp, err := os.Stat(store.path)
	if err != nil || !os.SameFile(beforeNoOp, afterNoOp) {
		t.Fatal("idempotent carrier update rewrote its image", err)
	}
	if _, err := store.Remove(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	image = readLocatedCarrier(t, store.path)
	if len(image.References()) != 1 || !image.References()[0].Equal(secondReference) {
		t.Fatal("removal did not prune exactly the departed owner")
	}
	writeLocatedCarrier(t, store.path, image.Registry(), []pathauthority.Reference{wrong})
	if _, err := store.Load(t.Context()); err == nil {
		t.Fatal("located owner validation fell back to its key")
	}
	writeLocatedCarrier(t, store.path, image.Registry(), image.References())
	if _, err := store.RetireAllIfCurrent(t.Context(), image.Registry(), []durablecarrier.ManagedCarrierClaim{second}); err != nil {
		t.Fatal(err)
	}
	if empty := readLocatedCarrier(t, store.path); len(empty.References()) != 0 || len(empty.Registry().Claims()) != 0 {
		t.Fatal("last-owner retirement downgraded the image or retained evidence")
	}
}

func TestLookupEvidencePreservesObservedLegacyCarrierFormat(t *testing.T) {
	root := t.TempDir()
	store, err := New(filepath.Join(root, "claims.json"))
	if err != nil {
		t.Fatal(err)
	}
	content, err := Marshal(durablecarrier.EmptyGlobalCarrierClaims())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	claim := testGlobalClaim(t, "alpha", "alpha@official", root)
	next, err := store.Upsert(t.Context(), claim, observedCarrierReference(t, claim.Owner().StatefileKey()))
	if err != nil {
		t.Fatal(err)
	}
	content, err = os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := DecodeRegistryFacts(content)
	if err != nil || !legacy.Equal(next) {
		t.Fatal("optional evidence changed the observed legacy carrier format", err)
	}
}

func TestLocatedCarrierPinTransitionPreservesReferencesAcrossFailureAndRetry(t *testing.T) {
	root := t.TempDir()
	claim, _ := testPiPinClaim(t, root, "tools", strings.Repeat("a", 40))
	_, nextContract := testPiPinClaim(t, root, "tools", strings.Repeat("b", 40))
	transition, err := claim.PinTransitionTo(nextContract)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := durablecarrier.NewGlobalCarrierClaims([]durablecarrier.ManagedCarrierClaim{claim})
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(filepath.Join(root, "claims.json"))
	if err != nil {
		t.Fatal(err)
	}
	reference := observedCarrierReference(t, claim.Owner().StatefileKey())
	writeLocatedCarrier(t, store.path, registry, []pathauthority.Reference{reference})
	reserved, err := store.ReservePinTransitionIfCurrent(t.Context(), registry, transition)
	if err != nil {
		t.Fatal(err)
	}
	image := readLocatedCarrier(t, store.path)
	if !image.Registry().Equal(reserved) || !image.References()[0].Equal(reference) {
		t.Fatal("pin reservation lost owner lookup evidence")
	}
	relation := transition.Identity().ExpectedRelation()
	row, err := observerelation.NewRow(observerelation.RowSpec{SubjectKey: string(relation.SubjectKey()), HasManagedInstanceKey: true, ManagedInstanceKey: string(relation.ManagedInstanceKey())})
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := observerelation.NewInventory(observerelation.InventorySpec{Availability: observerelation.InventorySupported, Freshness: observerelation.EvidenceFresh, Rows: []observerelation.Row{row}})
	if err != nil {
		t.Fatal(err)
	}
	correlation := observerelation.Correlate(relation, inventory)
	fault := errors.New("carrier update refused")
	broken := store
	broken.commitFile = func(context.Context, storagecommit.FileCommit) error { return fault }
	if _, err := broken.CompletePinTransitionIfCurrent(t.Context(), reserved, transition, correlation); !errors.Is(err, fault) {
		t.Fatal("failed completion lost its cause", err)
	}
	if image = readLocatedCarrier(t, store.path); !image.Registry().Equal(reserved) || !image.References()[0].Equal(reference) {
		t.Fatal("failed completion discarded its reservation or references")
	}
	completed, err := store.CompletePinTransitionIfCurrent(t.Context(), reserved, transition, correlation)
	if err != nil {
		t.Fatal(err)
	}
	image = readLocatedCarrier(t, store.path)
	if !image.Registry().Equal(completed) || !image.References()[0].Equal(reference) || !completed.Claims()[0].MatchesLockedRecord(nextContract) {
		t.Fatal("completed pin transition changed its owner evidence")
	}
}

func writeLocatedCarrier(t *testing.T, path string, registry durablecarrier.GlobalCarrierClaims, references []pathauthority.Reference) []byte {
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

func readLocatedCarrier(t *testing.T, path string) RegistryImage {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	image, err := UnmarshalImage(content)
	if err != nil {
		t.Fatal("normal carrier update did not preserve a located image", err)
	}
	return image
}
