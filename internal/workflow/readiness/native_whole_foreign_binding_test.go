package readiness

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/test/outputtest"
)

func TestNativeWholeAdoptionRefusesForeignProjectBindingWithoutClaim(t *testing.T) {
	fixture := newNativePlanningFixture(t, target.ScopeProject, "extension-settings/test.json")
	binding := nativeForeignProjectBinding(t, fixture, outputtest.Parse(t, "shared/peer.json"), fixture.hostPath)
	registry, err := fixture.observation.Registry.Load(t.Context())
	if err != nil || len(registry.Claims()) != 0 {
		t.Fatal("project fixture unexpectedly depends on an exclusive claim", err)
	}
	registry, err = ownership.NewRegistry(registry.Claims(), append(registry.DocumentBindings(), binding)...)
	if err != nil {
		t.Fatal(err)
	}
	content, err := ownershipstore.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	requireNativePreparationFile(t, fixture.registryPath, content)
	fixture.observe(t)
	stateBefore, err := os.ReadFile(fixture.paths.StatefilePath)
	if err != nil {
		t.Fatal(err)
	}
	reads, budget := fixture.reader.reads, fixture.observation.Budget.RemainingBytes()
	plan, err := PrepareNativeWholeAdoption(t.Context(), fixture.adoption(t, true))
	if err != nil || plan.Validate() != nil || !plan.Decision().IsBlocked() || len(plan.RetiredSubjects()) != 0 {
		t.Fatal("whole adoption ignored a foreign project-key participant without an exclusive claim", err)
	}
	hostAfter, hostErr := os.ReadFile(fixture.hostPath)
	registryAfter, registryErr := os.ReadFile(fixture.registryPath)
	stateAfter, stateErr := os.ReadFile(fixture.paths.StatefilePath)
	if hostErr != nil || registryErr != nil || stateErr != nil || !bytes.Equal(hostAfter, fixture.original) || !bytes.Equal(registryAfter, content) || !bytes.Equal(stateAfter, stateBefore) || fixture.reader.reads != reads || fixture.observation.Budget.RemainingBytes() != budget || fixture.observation.Budget.RequireCapturePending() != nil {
		t.Fatal("read-only participant refusal changed files, reread Registry or changed allocation")
	}
}

func nativeForeignProjectBinding(t *testing.T, fixture nativePlanningFixture, destination output.Destination, physical string) ownership.DocumentBinding {
	t.Helper()
	contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeProject, destination, nativePlanningKey(t, "released"), fixture.observation.Before.ManagedAggregates()[0].Contribution().CodecContractID())
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := aggregatecodec.ForOperation(t.Context(), []aggregate.CodecContractID{contract.CodecContractID()}, narrowingLimits, narrowingLimits)
	if err != nil {
		t.Fatal(err)
	}
	codec, _ := catalog.Lookup(contract.CodecContractID())
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	document, err := os.ReadFile(physical)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, failure := codec.Read(aggregate.ExistingDocument(document), selection)
	if failure != nil || !snapshot.States()[0].Present() {
		t.Fatal("foreign baseline is not a real present key", failure)
	}
	body, err := aggregate.NewManagedContribution(contract, snapshot.States()[0].CanonicalProjection())
	if err != nil {
		t.Fatal(err)
	}
	id, err := entity.New(entity.KindHostConfig, "foreign-settings")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.HostConfigKeySubject(id, target.TargetPi, target.ScopeProject, contract.Address().KeyPath())
	if err != nil {
		t.Fatal(err)
	}
	row, err := durable.NewManagedAggregateState(subject, body)
	if err != nil {
		t.Fatal(err)
	}
	before, err := durable.NewSnapshot(durable.SnapshotInput{ManagedAggregates: []durable.ManagedAggregateState{row}})
	if err != nil {
		t.Fatal(err)
	}
	content, err := fixture.codec.Encode(before)
	if err != nil {
		t.Fatal(err)
	}
	root := fixture.observation.Roots.Manifest.SelectedPath
	statePath := filepath.Join(root, "foreign-state.json")
	requireNativePreparationFile(t, statePath, content)
	state, err := mutation.ObservePersistedDirectoryEntryAuthority(statePath)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(state.Exact(), filepath.Join(root, "foreign.toml"))
	if err != nil {
		t.Fatal(err)
	}
	aliasPath := filepath.Join(root, destination.RelativePath())
	if err := os.MkdirAll(filepath.Dir(aliasPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if aliasPath != physical {
		if err := os.Symlink(physical, aliasPath); err != nil {
			t.Fatal(err)
		}
	}
	namespace, err := journal.ObserveNativeDocumentNamespace(t.Context(), []aggregate.DocumentAddress{contract.Address().Document()}, fixture.observation.Roots, fixture.observation.Filesystem, fixture.observation.Budget)
	if err != nil {
		t.Fatal(err)
	}
	exact, present := namespace.Observation().Exact()
	if !present {
		t.Fatal("foreign baseline did not resolve to an exact document")
	}
	binding, err := ownership.NewDocumentBinding(owner, destination, exact)
	if err != nil {
		t.Fatal(err)
	}
	return binding
}
