package store

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/output/ownership"
)

func TestStoreConvergesDocumentShapeAndBindingsThroughJointWriter(t *testing.T) {
	root := canonicalTestRoot(t)
	path := filepath.Join(root, "config.json")
	parent := testActiveClaim(t, root, "owner", path, "/a")
	child := testActiveClaim(t, root, "owner", path, "/a/b")
	foreign := testActiveClaim(t, root, "foreign", filepath.Join(root, "foreign.json"), "")
	binding := storedDocumentBinding(t, parent, "@pi-agent/settings.json")
	staleBinding := storedDocumentBinding(t, foreign, "@pi-agent/settings.json")
	bindings := mustStoreBindingConvergence(t, storeBindingIntent(t, binding, false, true))
	change, err := ownership.NewDocumentClaimChange([]ownership.Claim{child}, []ownership.Claim{parent})
	if err != nil {
		t.Fatal(err)
	}
	base, err := ownership.NewClaimConvergence(nil)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := base.WithDocumentChanges([]ownership.DocumentClaimChange{change})
	if err != nil {
		t.Fatal(err)
	}
	for _, stale := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "stale binding"}[stale], func(t *testing.T) {
			store := mustStore(t, filepath.Join(t.TempDir(), "claims.json"))
			var initialBindings []ownership.DocumentBinding
			if stale {
				// Keep the logical owner/key but change its exact referent.
				staleBinding, err = ownership.NewDocumentBinding(binding.Owner(), binding.Destination(), foreign.Address().PathAuthority())
				if err != nil {
					t.Fatal(err)
				}
				initialBindings = []ownership.DocumentBinding{staleBinding}
			}
			initial := seedConvergenceRegistry(t, store, []ownership.Claim{child, foreign}, initialBindings...)
			before, err := os.ReadFile(store.Path())
			if err != nil {
				t.Fatal(err)
			}
			identity, err := os.Stat(store.Path())
			if err != nil {
				t.Fatal(err)
			}
			result, err := store.ConvergeRegistry(t.Context(), claims, bindings)
			if stale {
				if err == nil {
					t.Fatal("stale second family was accepted after shape convergence")
				}
				assertUnusableRegistry(t, result)
				after, readErr := os.ReadFile(store.Path())
				info, statErr := os.Stat(store.Path())
				loaded, loadErr := store.Load(t.Context())
				if readErr != nil || statErr != nil || loadErr != nil || !bytes.Equal(before, after) || !os.SameFile(identity, info) || !loaded.Equal(initial) {
					t.Fatalf("stale binding published a shape-only successor: %v, %v, %v", readErr, statErr, loadErr)
				}
				return
			}
			want, wantErr := ownership.NewRegistry([]ownership.Claim{parent, foreign}, binding)
			if err != nil || wantErr != nil || !result.Equal(want) {
				t.Fatalf("joint shape/binding successor differs: %v, %v", err, wantErr)
			}
			loaded, err := store.Load(t.Context())
			if err != nil || !loaded.Equal(want) {
				t.Fatalf("durable shape/binding successor differs: %v", err)
			}
			identity, err = os.Stat(store.Path())
			if err != nil {
				t.Fatal(err)
			}
			if again, err := store.ConvergeRegistry(t.Context(), claims, bindings); err != nil || !again.Equal(want) {
				t.Fatalf("joint shape retry differs: %v", err)
			}
			info, err := os.Stat(store.Path())
			if err != nil || !os.SameFile(identity, info) {
				t.Fatalf("idempotent shape convergence republished metadata: %v", err)
			}
		})
	}
}
