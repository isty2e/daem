package store

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
)

func TestDocumentRegistrySettlementPublishesBothFamiliesAndIsIdempotent(t *testing.T) {
	for _, lastMember := range []bool{false, true} {
		fixture := newDocumentSettlementFixture(t, lastMember)
		if !lastMember {
			if value, err := fixture.store.ConvergeRegistry(t.Context(), fixture.claims, fixture.bindings); err == nil || value.Equal(value) {
				t.Fatal("default writer was widened to retained missing document claims")
			}
			fixture.requireBytes(t, fixture.beforeBytes)
		}
		for attempt := 0; attempt != 2; attempt++ {
			before, err := os.Stat(fixture.registryPath)
			if err != nil {
				t.Fatal(err)
			}
			result, err := fixture.store.ConvergeForDocuments(t.Context(), fixture.claims, fixture.bindings, fixture.owner, fixture.missing, physicalwork.MaximumPhysicalPathDepth, fixture.budget)
			if err != nil || !result.Equal(fixture.target) {
				t.Fatalf("last=%t attempt=%d: joint convergence failed: %v", lastMember, attempt, err)
			}
			fixture.requireBytes(t, fixture.targetBytes)
			after, err := os.Stat(fixture.registryPath)
			if err != nil || attempt != 0 && !os.SameFile(before, after) {
				t.Fatalf("idempotent target rewrote the registry: %v", err)
			}
		}
	}
}

func TestDocumentRegistrySettlementFailuresPublishNeitherFamily(t *testing.T) {
	for _, fault := range []string{"third claim phase", "third binding", "ambient", "closed root", "foreign scope owner", "canceled", "budget", "invalid convergence"} {
		t.Run(fault, func(t *testing.T) {
			fixture := newDocumentSettlementFixture(t, fault == "ambient")
			ctx, owner := t.Context(), fixture.owner
			var budget rootedpath.PhysicalTraversalBudget = fixture.budget
			var expectedError error
			expected := fixture.beforeBytes
			claims := fixture.claims
			switch fault {
			case "third claim phase":
				current := fixture.before.Claims()
				var err error
				for index, claim := range current {
					if claim.OwnedBy(fixture.owner) {
						current[index], err = ownership.NewReservedClaim(claim.Address(), claim.Owner(), "other-operation")
						if err != nil {
							t.Fatal(err)
						}
						break
					}
				}
				third, err := ownership.NewRegistry(current, fixture.before.DocumentBindings()...)
				if err != nil {
					t.Fatal(err)
				}
				expected = writeDocumentRegistry(t, fixture.registryPath, third)
			case "third binding":
				wrongPath, err := mutation.ObservePersistedDirectoryEntryAuthority(filepath.Join(filepath.Dir(fixture.hostPath), "other.json"))
				if err != nil {
					t.Fatal(err)
				}
				current := fixture.before.DocumentBindings()
				for index, value := range current {
					if value.Owner().Equal(fixture.owner) {
						current[index], err = ownership.NewDocumentBinding(value.Owner(), value.Destination(), wrongPath.Exact())
						if err != nil {
							t.Fatal(err)
						}
					}
				}
				third, err := ownership.NewRegistry(fixture.before.Claims(), current...)
				if err != nil {
					t.Fatal(err)
				}
				expected = writeDocumentRegistry(t, fixture.registryPath, third)
			case "ambient":
				var err error
				fixture.store, err = New(fixture.registryPath)
				if err != nil {
					t.Fatal(err)
				}
			case "closed root":
				if err := fixture.root.Close(); err != nil {
					t.Fatal(err)
				}
			case "foreign scope owner":
				owner = fixture.foreign.Owner()
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				expectedError = context.Canceled
			case "budget":
				expectedError = errors.New("document settlement path budget exhausted")
				budget = documentRegistryFailBudget{err: expectedError}
			case "invalid convergence":
				claims = ownership.ClaimConvergence{}
				budget = &documentSettlementCountingBudget{}
			}
			result, err := fixture.store.ConvergeForDocuments(ctx, claims, fixture.bindings, owner, fixture.missing, physicalwork.MaximumPhysicalPathDepth, budget)
			if err == nil || result.Equal(result) {
				t.Fatalf("refused operation returned a successor: %v", err)
			}
			if expectedError != nil && !errors.Is(err, expectedError) {
				t.Fatalf("failure cause was discarded: %v", err)
			}
			if calls, counted := budget.(*documentSettlementCountingBudget); counted && calls.calls != 0 {
				t.Fatal("invalid convergence reached physical observation")
			}
			fixture.requireBytes(t, expected)
		})
	}
}

func TestDocumentRegistryCommitReadRejectsInterveningReplacement(t *testing.T) {
	fixture := newDocumentSettlementFixture(t, false)
	observations, err := newDocumentPathAuthorityObservationSession(t.Context(), fixture.owner, fixture.missing, physicalwork.MaximumPhysicalPathDepth, fixture.budget)
	if err != nil {
		t.Fatal(err)
	}
	current, identity, exists, capability, err := fixture.store.loadRootedForCommit(t.Context(), fixture.claims.ExpectedRemovals(), observations)
	if err != nil || !exists || capability == nil || !current.Equal(fixture.before) {
		t.Fatalf("rooted commit read lost its snapshot: %v", err)
	}
	consumed := false
	defer func() {
		if !consumed {
			_ = capability.Close()
		}
	}()
	intervening := append(append([]byte(nil), fixture.beforeBytes...), '\n')
	if err := os.WriteFile(fixture.registryPath, intervening, 0o600); err != nil {
		t.Fatal(err)
	}
	request, err := storagecommit.NewRootedFileReplacement(capability, fixture.targetBytes, 0o600, identity)
	if err != nil {
		t.Fatal(err)
	}
	consumed = true
	if err := storagecommit.CommitFile(t.Context(), request); err == nil {
		t.Fatal("snapshot identity accepted an intervening metadata write")
	}
	fixture.requireBytes(t, intervening)
}

func TestDocumentRegistrySettlementKeepsRetainedRootAfterAliasRetarget(t *testing.T) {
	fixture := newDocumentSettlementFixture(t, false)
	alias := filepath.Join(t.TempDir(), "registry-root")
	if err := os.Symlink(filepath.Dir(fixture.registryPath), alias); err != nil {
		t.Skipf("directory alias is unavailable: %v", err)
	}
	root, destination, err := rootedpath.CaptureDestination(filepath.Join(alias, "ownership.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	selected, err := NewRooted(root, destination, physicalwork.MaximumPhysicalPathDepth, fixture.budget)
	if err != nil {
		t.Fatal(err)
	}
	otherRoot := t.TempDir()
	otherPath := filepath.Join(otherRoot, "ownership.json")
	if err := os.WriteFile(otherPath, fixture.beforeBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(otherRoot, alias); err != nil {
		t.Fatal(err)
	}
	result, err := selected.ConvergeForDocuments(t.Context(), fixture.claims, fixture.bindings, fixture.owner, fixture.missing, physicalwork.MaximumPhysicalPathDepth, fixture.budget)
	if err != nil || !result.Equal(fixture.target) {
		t.Fatalf("retained-root convergence failed: %v", err)
	}
	fixture.requireBytes(t, fixture.targetBytes)
	actual, err := os.ReadFile(otherPath)
	if err != nil || !bytes.Equal(actual, fixture.beforeBytes) {
		t.Fatalf("retargeted alias received a registry write: %v", err)
	}
}

type documentSettlementFixture struct {
	store                    Store
	root                     *rootedpath.CapturedRoot
	owner                    stateauthority.Authority
	missing                  []pathauthority.Provisional
	before, target           ownership.Registry
	claims                   ownership.ClaimConvergence
	bindings                 ownership.DocumentBindingConvergence
	foreign                  ownership.Claim
	registryPath, hostPath   string
	beforeBytes, targetBytes []byte
	budget                   *physicalwork.Budget
}

func newDocumentSettlementFixture(t *testing.T, lastMember bool) documentSettlementFixture {
	t.Helper()
	base := t.TempDir()
	host := filepath.Join(base, "Caf\u00e9.json")
	if err := os.WriteFile(host, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	exact, err := mutation.ObservePersistedDirectoryEntryAuthority(host)
	if err != nil {
		t.Fatal(err)
	}
	var owners []stateauthority.Authority
	for _, name := range []string{"owner", "foreign"} {
		state, err := mutation.ObservePersistedDirectoryEntryAuthority(filepath.Join(base, name+"-state.json"))
		if err != nil {
			t.Fatal(err)
		}
		owner, err := stateauthority.New(state.Exact(), filepath.Join(base, name+".toml"))
		if err != nil {
			t.Fatal(err)
		}
		owners = append(owners, owner)
	}
	var before []ownership.Claim
	for _, name := range []string{"removed", "retained"} {
		key, err := output.NewKeyPath(name)
		if err != nil {
			t.Fatal(err)
		}
		footprint, err := output.NewKeyFootprint(key)
		if err != nil {
			t.Fatal(err)
		}
		address, err := ownership.NewManagedAddress(exact.Exact(), footprint)
		if err != nil {
			t.Fatal(err)
		}
		claim, err := ownership.NewActiveClaim(address, owners[0])
		if err != nil {
			t.Fatal(err)
		}
		before = append(before, claim)
	}
	other, err := mutation.ObservePersistedDirectoryEntryAuthority(filepath.Join(base, "unrelated.json"))
	if err != nil {
		t.Fatal(err)
	}
	address, err := ownership.NewManagedAddress(other.Exact(), output.WholePathFootprint())
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := ownership.NewActiveClaim(address, owners[1])
	if err != nil {
		t.Fatal(err)
	}
	destination, err := output.Parse("@pi-agent/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	binding, err := ownership.NewDocumentBinding(owners[0], destination, exact.Exact())
	if err != nil {
		t.Fatal(err)
	}
	foreignBinding, err := ownership.NewDocumentBinding(owners[1], destination, other.Exact())
	if err != nil {
		t.Fatal(err)
	}
	present, err := ownership.PresentDocumentBinding(binding)
	if err != nil {
		t.Fatal(err)
	}
	after, bindingAfter := []ownership.Claim{before[1]}, present
	if lastMember {
		after, bindingAfter = nil, ownership.NoDocumentBinding()
	}
	transition, err := ownershipmutation.NewDocumentClaimTransition(before, after, "settle")
	if err != nil {
		t.Fatal(err)
	}
	change, err := transition.Finalization()
	if err != nil {
		t.Fatal(err)
	}
	convergence, err := ownership.NewClaimConvergence(nil)
	if err != nil {
		t.Fatal(err)
	}
	convergence, err = convergence.WithDocumentChanges([]ownership.DocumentClaimChange{change})
	if err != nil {
		t.Fatal(err)
	}
	intent, err := ownership.NewExactDocumentBindingIntent(owners[0], destination, present, bindingAfter)
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := ownership.NewDocumentBindingConvergence([]ownership.DocumentBindingIntent{intent})
	if err != nil {
		t.Fatal(err)
	}
	current, err := ownership.NewRegistry(append(append([]ownership.Claim(nil), before...), foreign), binding, foreignBinding)
	if err != nil {
		t.Fatal(err)
	}
	targetBindings := []ownership.DocumentBinding{binding, foreignBinding}
	if lastMember {
		targetBindings = []ownership.DocumentBinding{foreignBinding}
	}
	target, err := ownership.NewRegistry(append(append([]ownership.Claim(nil), after...), foreign), targetBindings...)
	if err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(base, "ownership.json")
	content := writeDocumentRegistry(t, registryPath, current)
	targetBytes, err := Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	root, selected, err := rootedpath.CaptureDestination(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	budget, err := physicalwork.NewBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewRooted(root, selected, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(host); err != nil {
		t.Fatal(err)
	}
	observed, err := mutation.ObserveDirectoryEntryAuthority(host)
	if err != nil {
		t.Fatal(err)
	}
	missing, provisional := observed.Provisional()
	if !provisional {
		t.Skip("filesystem supplies exact authority for the absent Unicode document")
	}
	return documentSettlementFixture{store: store, root: root, owner: owners[0], missing: []pathauthority.Provisional{missing}, before: current, target: target, claims: convergence, bindings: bindings, foreign: foreign, registryPath: registryPath, hostPath: host, beforeBytes: content, targetBytes: targetBytes, budget: budget}
}

func writeDocumentRegistry(t *testing.T, path string, registry ownership.Registry) []byte {
	t.Helper()
	content, err := Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return content
}

func (fixture documentSettlementFixture) requireBytes(t *testing.T, expected []byte) {
	t.Helper()
	actual, err := os.ReadFile(fixture.registryPath)
	if err != nil || !bytes.Equal(actual, expected) {
		t.Fatalf("registry differs from the expected complete phase: %v", err)
	}
	if _, err := os.Lstat(fixture.hostPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("metadata convergence changed the absent host: %v", err)
	}
}

type documentSettlementCountingBudget struct{ calls int }

func (budget *documentSettlementCountingBudget) AdmitPathComponents(int) error {
	budget.calls++
	return nil
}
