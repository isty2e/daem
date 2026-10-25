package store

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
)

func TestDocumentRegistryReadKeepsStoredMissingHistoryScopedAndReadOnly(t *testing.T) {
	base := t.TempDir()
	host := filepath.Join(base, "Caf\u00e9.json")
	if err := os.WriteFile(host, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	observed, err := mutation.ObservePersistedDirectoryEntryAuthority(host)
	if err != nil {
		t.Fatal(err)
	}
	state, err := mutation.ObservePersistedDirectoryEntryAuthority(filepath.Join(base, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(state.Exact(), filepath.Join(base, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	address, err := ownership.NewManagedAddress(observed.Exact(), output.WholePathFootprint())
	if err != nil {
		t.Fatal(err)
	}
	claim, err := ownership.NewActiveClaim(address, owner)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := ownership.NewRegistry([]ownership.Claim{claim})
	if err != nil {
		t.Fatal(err)
	}
	content, err := Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(base, "ownership.json")
	if err := os.WriteFile(registryPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	root, destination, err := rootedpath.CaptureDestination(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	budget, err := recovery.NewPhysicalWorkBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewRooted(root, destination, recovery.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(host); err != nil {
		t.Fatal(err)
	}
	current, err := mutation.ObserveDirectoryEntryAuthority(host)
	if err != nil {
		t.Fatal(err)
	}
	missing, provisional := current.Provisional()
	if !provisional {
		t.Skip("filesystem supplies exact authority for the absent Unicode document")
	}
	for _, fault := range []string{"none", "default read", "foreign owner", "unrequested document", "wrong witness", "canceled", "budget failure", "missing scope"} {
		t.Run(fault, func(t *testing.T) {
			ctx := t.Context()
			selectedOwner, namespaces := owner, []pathauthority.Provisional{missing}
			var selectedBudget rootedpath.PhysicalTraversalBudget = budget
			var expectedError error
			switch fault {
			case "foreign owner":
				other, err := mutation.ObservePersistedDirectoryEntryAuthority(filepath.Join(base, "other-state.json"))
				if err != nil {
					t.Fatal(err)
				}
				selectedOwner, err = stateauthority.New(other.Exact(), owner.ManifestPath())
				if err != nil {
					t.Fatal(err)
				}
			case "unrequested document":
				other, err := mutation.ObserveDirectoryEntryAuthority(filepath.Join(base, "Other-Caf\u00e9.json"))
				if err != nil {
					t.Fatal(err)
				}
				namespaces[0], _ = other.Provisional()
			case "wrong witness":
				wrongCandidate := pathtest.DarwinCaseSensitive(missing.CandidateKey())
				wrongNamespace := pathtest.DarwinCaseSensitive(missing.Namespace().Key())
				if wrongCandidate.Witness() == missing.CandidateWitness() {
					t.Skip("current filesystem already has the selected case-sensitive witness")
				}
				namespaces[0], err = pathauthority.NewProvisional(wrongCandidate.Key(), wrongCandidate.Witness(), wrongNamespace.Key(), wrongNamespace.Witness())
				if err != nil {
					t.Fatal(err)
				}

			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				expectedError = context.Canceled
			case "budget failure":
				expectedError = errors.New("document registry path budget exhausted")
				selectedBudget = documentRegistryFailBudget{err: expectedError}
			case "missing scope":
				namespaces = nil
			}
			var loaded ownership.Registry
			var readErr error
			if fault == "default read" {
				loaded, readErr = store.Load(ctx)
			} else {
				loaded, readErr = store.LoadForDocuments(ctx, selectedOwner, namespaces, recovery.MaximumPhysicalPathDepth, selectedBudget)
			}
			if fault == "none" {
				if readErr != nil || !loaded.Equal(registry) {
					t.Fatalf("scoped comparison lost the actual stored claim: %v", readErr)
				}
			} else if readErr == nil || loaded.Equal(loaded) {
				t.Fatalf("invalid comparison returned usable registry: %v", readErr)
			}
			if expectedError != nil && !errors.Is(readErr, expectedError) {
				t.Fatalf("read discarded its failure cause: %v", readErr)
			}
			actual, err := os.ReadFile(registryPath)
			if err != nil || !bytes.Equal(actual, content) {
				t.Fatalf("comparison read published registry changes: %v", err)
			}
			if _, err := os.Lstat(host); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("comparison read created the absent document: %v", err)
			}
		})
	}
}

type documentRegistryFailBudget struct{ err error }

func (budget documentRegistryFailBudget) AdmitPathComponents(int) error { return budget.err }
