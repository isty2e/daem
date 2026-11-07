package publication

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/namespace/namespacetest"
	"github.com/isty2e/daem/internal/output"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	journalobservation "github.com/isty2e/daem/internal/effect/journal/observation"
	"github.com/isty2e/daem/internal/effect/mutation"

	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output/ownership"

	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

type nativeInitialRegistryReader struct {
	store     ownershipmutation.RegistryStore
	loads     int
	ambient   int
	qualified int
	budget    rootedpath.PhysicalTraversalBudget
	afterRead func()
}

func (reader *nativeInitialRegistryReader) Load(ctx context.Context) (ownership.Registry, error) {
	reader.ambient++
	return reader.store.Load(ctx)
}

func (reader *nativeInitialRegistryReader) LoadForClaimRemovals(ctx context.Context, removals []ownership.Claim, depth int, budget rootedpath.PhysicalTraversalBudget) (ownership.Registry, error) {
	reader.loads++
	reader.budget = budget
	value, err := reader.store.LoadForClaimRemovals(ctx, removals, depth, budget)
	if reader.afterRead != nil {
		reader.afterRead()
	}
	return value, err
}

func (reader *nativeInitialRegistryReader) LoadForDocuments(ctx context.Context, owner stateauthority.Authority, missing []pathauthority.Provisional, removals []ownership.Claim, depth int, budget rootedpath.PhysicalTraversalBudget) (ownership.Registry, error) {
	reader.loads++
	reader.qualified++
	reader.budget = budget
	qualified, supported := reader.store.(ownershipmutation.DocumentRegistryReader)
	if !supported {
		return ownership.Registry{}, errors.New("selected reader cannot qualify missing history")
	}
	value, err := qualified.LoadForDocuments(ctx, owner, missing, removals, depth, budget)
	if reader.afterRead != nil {
		reader.afterRead()
	}
	return value, err
}

func nativeInitialFixture(t *testing.T, scope target.Scope, action string) (wholeGroupTestFixture, journalobservation.Input, *nativeInitialRegistryReader, string) {
	t.Helper()
	fixture := wholeGroupFixture(t, target.TargetPi, scope, action, []byte("before"), []byte("after"))
	legacy, actual := captureOwnershipFixture(t, fixture)
	statefile, err := mutation.ObservePersistedDirectoryEntryAuthority(legacy.owner.StatefileKey())
	if err != nil {
		t.Fatal(err)
	}
	legacy.owner, err = legacy.owner.WithStatefile(statefile.Exact())
	if err != nil {
		t.Fatal(err)
	}
	claims, bindings := actual.value.Claims(), actual.value.DocumentBindings()
	for index, claim := range claims {
		claims[index], err = ownership.NewActiveClaim(claim.Address(), legacy.owner)
		if err != nil {
			t.Fatal(err)
		}
	}
	for index, binding := range bindings {
		bindings[index], err = ownership.NewDocumentBinding(legacy.owner, binding.Destination(), binding.PathAuthority())
		if err != nil {
			t.Fatal(err)
		}
	}
	actual.value, err = ownership.NewRegistry(claims, bindings...)
	if err != nil {
		t.Fatal(err)
	}
	budget := physicalwork.NewCaptureBudget(nil)
	registryPath := filepath.Join(t.TempDir(), "ownership.json")
	if action != "create" {
		content, err := ownershipstore.Marshal(actual.value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(registryPath, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root, destination, err := rootedpath.CaptureDestination(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	store, err := ownershipstore.BindRooted(root, destination, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	reader := &nativeInitialRegistryReader{store: store}
	input := journalobservation.Input{
		Owner: legacy.owner, Before: legacy.before, Aliases: fixture.namespace.Addresses(),
		Protection: namespacetest.Reservations(t),
		Roots:      fixture.roots, Filesystem: fixture.reader, Registry: reader,
		Limits: legacy.limits, Budget: budget,
	}
	return fixture, input, reader, registryPath
}

func TestNativeInitialObservationRetainsActualFactsBeforeAfterState(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, action := range []string{"create", "record", "remove"} {
			t.Run(string(scope)+"/"+action, func(t *testing.T) {
				fixture, input, reader, registryPath := nativeInitialFixture(t, scope, action)
				batch, err := ObserveNativeDocuments(t.Context(), input)
				if err != nil || (batch.Evidence().Validate() != nil) || reader.loads != 1 || reader.ambient != 0 || reader.budget != input.Budget {
					t.Fatalf("actual initial read failed: loads=%d error=%v", reader.loads, err)
				}
				document, mode, found := batch.Evidence().Document(input.Aliases[0])
				if !found || !document.Equal(fixture.original) || document.Exists() != (mode != nil) {
					t.Fatal("initial observation lost actual original facts")
				}
				if _, _, found := batch.Evidence().Document(aggregate.DocumentAddress{}); found {
					t.Fatal("unobserved alias received fallback document facts")
				}
				if mode != nil {
					*mode = 0
					_, retained, _ := batch.Evidence().Document(input.Aliases[0])
					if retained == nil || *retained == 0 {
						t.Fatal("caller changed retained original mode")
					}
				}
				remaining := input.Budget.RemainingBytes()
				// A later reader failure cannot replace the already observed Registry.
				reader.store = ownershipstore.Store{}
				prepared, err := batch.prepareOwnership(t.Context(), fixture.envelope.after, fixture.input.OperationID())
				if err != nil || len(prepared) != 1 || reader.loads != 1 || prepared[0].Budget() != input.Budget {
					t.Fatal("continuation reread Registry or lost shared allocation", err)
				}
				if input.Budget.RemainingBytes() != remaining-int64(len(fixture.original.Content())) || input.Budget.RequireCapturePending() != nil || input.Budget.AdmitObservation() == nil {
					t.Fatal("continuation reset work or opened removal admission")
				}
				if scope == target.ScopeProject && prepared[0].Claims() != nil || len(prepared[0].Bindings()) != 1 {
					t.Fatal("project binding was lost or treated as an exclusive claim")
				}
				fixture.requireUnchangedHost(t)
				for _, capability := range fixture.reader.capabilities {
					if capability.Validate() == nil {
						t.Fatal("initial observation leaked a namespace read capability")
					}
				}
				if _, err := namespacetest.LegacyRoot(t, input.Roots, output.RootProject).Root.Authority(); err != nil {
					t.Fatal("observation closed its borrowed root", err)
				}
				if action == "create" {
					if _, err := os.Lstat(registryPath); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("initial observation published an absent Registry", err)
					}
				}
			})
		}
	}
}

func TestNativeInitialObservationCoalescesActualAliases(t *testing.T) {
	fixture, input, reader, _ := nativeInitialFixture(t, target.ScopeGlobal, "record")
	projectPath := filepath.ToSlash(filepath.Join("global", input.Aliases[0].AggregateRoot().RelativePath()))
	alias, err := aggregate.NewDocumentAddress(target.TargetPi, target.ScopeProject, outputtest.Parse(t, projectPath))
	if err != nil {
		t.Fatal(err)
	}
	input.Aliases = append(input.Aliases, alias)
	batch, err := ObserveNativeDocuments(t.Context(), input)
	if err != nil || len(batch.Evidence().Namespaces()) != 1 || fixture.reader.imageReads != 1 || reader.loads != 1 {
		t.Fatal("logical aliases were not coalesced before the original read", err)
	}
	for _, alias := range input.Aliases {
		if document, _, found := batch.Evidence().Document(alias); !found || !document.Equal(fixture.original) {
			t.Fatal("coalesced alias lost its actual original")
		}
	}
	slices.Reverse(input.Aliases)
	reordered, err := ObserveNativeDocuments(t.Context(), input)
	if err != nil || len(reordered.Evidence().Namespaces()) != 1 || !slices.Equal(reordered.Evidence().Namespaces()[0].Addresses(), batch.Evidence().Namespaces()[0].Addresses()) {
		t.Fatal("alias order changed canonical physical grouping", err)
	}
}
