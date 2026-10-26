package readiness

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

type nativePlanningRegistryReader struct {
	ownershipmutation.RegistryReader
	reads int
}

func (reader *nativePlanningRegistryReader) Load(ctx context.Context) (ownership.Registry, error) {
	reader.reads++
	return reader.RegistryReader.Load(ctx)
}

func (reader *nativePlanningRegistryReader) LoadForClaimRemovals(ctx context.Context, claims []ownership.Claim, depth int, budget rootedpath.PhysicalTraversalBudget) (ownership.Registry, error) {
	reader.reads++
	return reader.RegistryReader.LoadForClaimRemovals(ctx, claims, depth, budget)
}

func (reader *nativePlanningRegistryReader) LoadForDocuments(ctx context.Context, owner stateauthority.Authority, missing []pathauthority.Provisional, depth int, budget rootedpath.PhysicalTraversalBudget) (ownership.Registry, error) {
	reader.reads++
	return reader.RegistryReader.(ownershipmutation.DocumentRegistryReader).LoadForDocuments(ctx, owner, missing, depth, budget)
}

type nativePlanningFixture struct {
	nativePreparationFixture
	reader *nativePlanningRegistryReader
}

func newNativePlanningFixture(t *testing.T, scope target.Scope, path string) nativePlanningFixture {
	t.Helper()
	base := newNativePreparationFixtureAt(t, scope, `{"a":1}`, path)
	narrowing, err := PrepareNativeSources(t.Context(), base.input)
	if err != nil {
		t.Fatal(err)
	}
	state, err := execute.PrepareNativeApplyState(t.Context(), execute.NativeApplyStateInput{Before: base.observation.Before, Narrowings: narrowing.Narrowings(), StateCodec: base.codec})
	if err != nil || len(state.AfterState().ManagedAggregates()) != 1 {
		t.Fatal("key baseline was not produced by actual narrowing preparation", err)
	}
	fixture := nativePlanningFixture{nativePreparationFixture: base}
	fixture.persistBefore(t, state.AfterState(), nil)
	requireNativePreparationFile(t, fixture.sourcePath, fixture.original)
	fixture.config, err = hostconfig.New(hostconfig.Spec{Name: "settings", Source: fixture.config.Source(), Target: fixture.config.Target(), Scope: scope, Path: path, Ownership: hostconfig.OwnershipDocument})
	if err != nil {
		t.Fatal(err)
	}
	fixture.input.Environment = nativePreparationEnvironment(t, fixture.config)
	fixture.refreshEpoch(t)
	return fixture
}

func (fixture *nativePlanningFixture) persistBefore(t *testing.T, before durable.Snapshot, extra []ownership.Claim) {
	t.Helper()
	observed, err := mutation.ObserveDirectoryEntryAuthority(fixture.hostPath)
	if err != nil {
		t.Fatal(err)
	}
	exact, known := observed.Exact()
	if !known {
		t.Fatal("fixture persistence requires an existing exact document")
	}
	claims := append([]ownership.Claim(nil), extra...)
	for _, row := range before.ManagedAggregates() {
		if row.Contribution().Scope() != target.ScopeGlobal {
			continue
		}
		address, err := ownership.NewManagedAddress(exact, row.Contribution().Footprint())
		if err != nil {
			t.Fatal(err)
		}
		claim, err := ownership.NewActiveClaim(address, fixture.observation.Owner)
		if err != nil {
			t.Fatal(err)
		}
		claims = append(claims, claim)
	}
	var bindings []ownership.DocumentBinding
	seen := make(map[output.Destination]struct{})
	for _, row := range before.ManagedAggregates() {
		if _, native := aggregate.HostConfigSyntaxForCodec(row.Contribution().CodecContractID()); !native {
			continue
		}
		destination := row.Contribution().AggregateRoot()
		if _, duplicate := seen[destination]; duplicate {
			continue
		}
		seen[destination] = struct{}{}
		binding, err := ownership.NewDocumentBinding(fixture.observation.Owner, destination, exact)
		if err != nil {
			t.Fatal(err)
		}
		bindings = append(bindings, binding)
	}
	registry, err := ownership.NewRegistry(claims, bindings...)
	if err != nil {
		t.Fatal(err)
	}
	content, err := fixture.codec.Encode(before)
	if err != nil {
		t.Fatal(err)
	}
	requireNativePreparationFile(t, fixture.paths.StatefilePath, content)
	fixture.observation.Before, err = statefile.LoadOptionalWithOptions(t.Context(), fixture.paths.StatefilePath, statefile.ReadOptions{Codec: fixture.codec, PhysicalPathBudget: fixture.observation.Budget})
	if err != nil {
		t.Fatal(err)
	}
	content, err = ownershipstore.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	requireNativePreparationFile(t, fixture.registryPath, content)
	if fixture.reader == nil {
		fixture.reader = &nativePlanningRegistryReader{RegistryReader: fixture.observation.Registry}
		fixture.observation.Registry = fixture.reader
	}
	fixture.observe(t)
}

func (fixture *nativePlanningFixture) observe(t *testing.T) {
	t.Helper()
	var err error
	fixture.input.Documents, err = journal.ObserveNativeDocuments(t.Context(), fixture.observation)
	if err != nil {
		t.Fatal(err)
	}
}

func (fixture nativePlanningFixture) adoption(t *testing.T, consent bool) NativeWholeAdoptionInput {
	t.Helper()
	source, err := fixture.input.SourceEpoch.NativeSource(t.Context(), fixture.config)
	if err != nil {
		t.Fatal(err)
	}
	return NativeWholeAdoptionInput{Source: source, Documents: fixture.input.Documents, ManageUnmanagedMatches: consent}
}

func (fixture nativePlanningFixture) foreignClaim(t *testing.T, reserved bool) ownership.Claim {
	t.Helper()
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	if err := os.WriteFile(statePath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := mutation.ObservePersistedDirectoryEntryAuthority(statePath)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(state.Exact(), filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	current, err := fixture.input.Documents.Ownership(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	whole, err := current.Document(t.Context(), fixture.observation.Aliases[0])
	if err != nil {
		t.Fatal(err)
	}
	address, err := ownership.NewManagedAddress(whole.ComparisonNamespace(), nativePlanningKey(t, "z"))
	if err != nil {
		t.Fatal(err)
	}
	if reserved {
		claim, err := ownership.NewReservedClaim(address, owner, "interrupted")
		if err != nil {
			t.Fatal(err)
		}
		return claim
	}
	claim, err := ownership.NewActiveClaim(address, owner)
	if err != nil {
		t.Fatal(err)
	}
	return claim
}
