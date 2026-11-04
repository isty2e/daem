//go:build darwin || linux

package store

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output/ownership"
)

func TestRootedProtectionStoreSharesPhysicalBudgetAndRetainsCapability(t *testing.T) {
	root := canonicalTestRoot(t)
	store := mustStore(t, filepath.Join(root, "claims.json"))
	claim := testActiveClaim(t, root, "owner", filepath.Join(root, "settings.json"), "/model")
	binding := storedDocumentBinding(t, claim, "@pi-agent/settings.json")
	seedConvergenceRegistry(t, store, nil, binding)
	protection := storedPendingProtection(t, binding)
	arm, _ := ownership.NewArmProtection(protection)
	captured, destination, err := rootedpath.CaptureDestination(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer captured.Close()
	budget := &recordingPhysicalTraversalBudget{remaining: 1 << 25}
	rooted, err := NewRooted(captured, destination, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	beforeArm := budget.admitted
	if _, err := rooted.TransitionProtection(t.Context(), protection.Owner(), arm); err != nil {
		t.Fatalf("rooted protection publication failed: %v", err)
	}
	if budget.admitted <= beforeArm {
		t.Fatal("rooted protection publication bypassed the shared path budget")
	}
	beforeRead := budget.admitted
	rows, err := rooted.LoadPendingProtections(t.Context())
	if err != nil || len(rows) != 1 || !rows[0].Equal(protection) {
		t.Fatalf("rooted protection was not discoverable: %v", err)
	}
	if budget.admitted <= beforeRead {
		t.Fatal("rooted protection read bypassed the shared path budget")
	}
	before, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	budget.remaining = 0
	if _, err := rooted.LoadPendingProtections(t.Context()); err == nil {
		t.Fatal("safety discovery bypassed exhausted path capacity")
	}
	clear, _ := ownership.NewClearProtection(protection)
	if _, err := rooted.TransitionProtection(t.Context(), protection.Owner(), clear); err == nil {
		t.Fatal("protection settlement bypassed exhausted path capacity")
	}
	after, err := os.ReadFile(store.Path())
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("under-budget settlement modified the registry: %v", err)
	}
	budget.remaining = 1 << 25
	if _, err := rooted.TransitionProtection(t.Context(), protection.Owner(), clear); err != nil {
		t.Fatalf("failed attempt consumed or leaked retained root authority: %v", err)
	}
}

func TestRootedProtectionPublicationConsumesActualPayloadWork(t *testing.T) {
	for _, clear := range []bool{false, true} {
		for _, allowance := range []string{"read-entry", "read-bytes", "write-entry", "write-bytes", "exact"} {
			t.Run(fmt.Sprintf("clear=%t/%s", clear, allowance), func(t *testing.T) {
				root := canonicalTestRoot(t)
				store := mustStore(t, filepath.Join(root, "claims.json"))
				claim := testActiveClaim(t, root, "owner", filepath.Join(root, "settings.json"), "/model")
				binding := storedDocumentBinding(t, claim, "@pi-agent/settings.json")
				seedConvergenceRegistry(t, store, nil, binding)
				protection := storedPendingProtection(t, binding)
				transition, err := ownership.NewArmProtection(protection)
				if err != nil {
					t.Fatal(err)
				}
				if clear {
					if _, err := store.TransitionProtection(t.Context(), protection.Owner(), transition); err != nil {
						t.Fatal(err)
					}
					transition, err = ownership.NewClearProtection(protection)
					if err != nil {
						t.Fatal(err)
					}
				}
				current, err := store.LoadProtectionRegistry(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				expected, changed, err := current.ApplyProtection(protection.Owner(), transition)
				if err != nil || !changed {
					t.Fatal("fixture has no actual protection transition", err)
				}
				after, err := encode(expected)
				if err != nil {
					t.Fatal(err)
				}
				before := mustReadRegistryBytes(t, store.Path())
				entries := 2
				payloadBytes := int64(len(before) + len(after))
				switch allowance {
				case "read-entry":
					entries = 0
				case "read-bytes":
					payloadBytes = int64(len(before) - 1)
				case "write-entry":
					entries = 1
				case "write-bytes":
					payloadBytes--
				}
				budget := physicalwork.NewCaptureBudget(nil)
				if err := budget.AdmitPhysicalWork(0, physicalwork.MaximumPhysicalEntries-entries, physicalwork.MaximumPhysicalBytes-payloadBytes); err != nil {
					t.Fatal(err)
				}
				captured, destination, err := rootedpath.CaptureDestination(store.Path())
				if err != nil {
					t.Fatal(err)
				}
				defer captured.Close()
				rooted, err := NewRooted(captured, destination, physicalwork.MaximumPhysicalPathDepth, budget)
				if err != nil {
					t.Fatal(err)
				}
				actual, err := rooted.TransitionProtection(t.Context(), protection.Owner(), transition)
				if allowance != "exact" {
					if err == nil || !bytes.Equal(before, mustReadRegistryBytes(t, store.Path())) {
						t.Fatal("unadmitted protection work changed the registry", err)
					}
					return
				}
				if err != nil || !actual.Equal(expected) || !bytes.Equal(after, mustReadRegistryBytes(t, store.Path())) {
					t.Fatal("exact payload allowance did not publish the complete transition", err)
				}
				if budget.RemainingEntries() != 0 || budget.RemainingBytes() != 0 {
					t.Fatal("protection publication bypassed or overcharged its payload allowance")
				}
			})
		}
	}
}
