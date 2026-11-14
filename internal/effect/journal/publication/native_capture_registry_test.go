package publication

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/journal/namespace/namespacetest"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/target"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

func TestNativeCaptureRegistrySelectedAbsenceIsNotMissingSelection(t *testing.T) {
	for _, scenario := range []struct{ name, path string }{
		{name: "ascii", path: "prompts/example.md"},
		{name: "unicode", path: "prompts/Caf\u00e9.md"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := wholeGroupFixtureAtPath(t, target.TargetPi, target.ScopeProject, "create", scenario.path, nil, []byte("after"))
			input, _ := captureOwnershipFixture(t, fixture)
			input.registry = nil
			if result, err := input.prepare(t.Context()); err == nil || result != nil {
				t.Fatal("project-only native capture treated no selected reader as empty state")
			}
			registryPath := filepath.Join(t.TempDir(), "ownership.json")
			root, destination, err := rootedpath.CaptureDestination(registryPath)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = root.Close() })
			input.registry, err = ownershipstore.BindRooted(root, destination, physicalwork.MaximumPhysicalPathDepth, input.budget)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := input.prepare(t.Context())
			if err != nil || len(prepared) != 1 || len(prepared[0].Bindings()) != 1 {
				t.Fatalf("selected rooted absence did not admit project binding acquisition: %v", err)
			}
			binding := prepared[0].Bindings()[0]
			if _, present := binding.Before().Get(); present || !binding.Owner().ExactEqual(input.owner) || binding.Destination() != fixture.namespace.Addresses()[0].AggregateRoot() {
				t.Fatal("selected empty registry did not acquire the document for its exact owner")
			}
			if _, err := os.Lstat(registryPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("capture preparation published a registry: %v", err)
			}
			if err := root.Close(); err != nil {
				t.Fatal(err)
			}
			if result, err := input.prepare(t.Context()); err == nil || result != nil {
				t.Fatal("failed selected registry read was replaced by empty state")
			}
			fixture.requireUnchangedHost(t)
		})
	}
}

func TestNativeCaptureRegistryChecksAcquisitionAgainstForeignClaims(t *testing.T) {
	fixture := wholeGroupFixture(t, target.TargetPi, target.ScopeGlobal, "record", []byte("before"), nil)
	input, reader := captureOwnershipFixture(t, fixture)
	input.before = durable.EmptySnapshot()
	before := reader.value.Claims()[0]
	foreign, err := ownership.NewActiveClaim(before.Address(), documentClaimsForTest(t).owner)
	if err != nil {
		t.Fatal(err)
	}
	reader.value, err = ownership.NewRegistry([]ownership.Claim{foreign})
	if err != nil {
		t.Fatal(err)
	}
	if prepared, err := input.prepare(t.Context()); err == nil || prepared != nil {
		t.Fatal("initial acquisition bypassed current foreign exclusion")
	}
	if claims := reader.value.Claims(); len(claims) != 1 || !claims[0].Equal(foreign) {
		t.Fatal("failed dry run changed the observed registry")
	}
	fixture.requireUnchangedHost(t)
}

func TestNativeCaptureRegistryMCPDoesNotKeepNativeBindingAlive(t *testing.T) {
	fixture := wholeGroupFixture(t, target.TargetCodex, target.ScopeProject, "record", []byte("before"), nil)
	input, reader := captureOwnershipFixture(t, fixture)
	placement, found := aggregate.MCPPlacementForID(aggregate.MCPPlacementCodexProject)
	if !found {
		t.Fatal("Codex project MCP placement missing")
	}
	canonical, err := mcpcodec.CanonicalCodexProjectMCPServerEntry(mcpcodec.MCPNoEnvServerProjection{
		ServerID: "search", Command: "node", AdapterContract: aggregate.CodexProjectMCPStdioCommandV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	contribution, err := placement.Contribution("search", string(canonical))
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologymcp.ProjectionSubject(target.TargetCodex, target.ScopeProject, "search")
	if err != nil {
		t.Fatal(err)
	}
	mcpRow, err := durable.NewManagedAggregateState(subject, contribution)
	if err != nil {
		t.Fatal(err)
	}
	alias := contribution.Contract().Address().Document()
	input.namespaces[0], err = journalnamespace.Observe(t.Context(), []aggregate.DocumentAddress{alias}, fixture.roots, namespacetest.Reservations(t), fixture.reader, input.budget)
	if err != nil {
		t.Fatal(err)
	}
	native := membershipNativeRow(t, alias, "native-toml-v1", "model")
	input.before, input.after = selectionState(t, native, mcpRow), selectionState(t, mcpRow)
	exact, known := input.namespaces[0].Observation().Exact()
	if !known {
		// The binding is an existing baseline, so create the actual document and
		// observe it again rather than fabricating exact authority.
		destination := filepath.Join(namespacetest.LegacyRoot(t, fixture.roots, output.RootProject).SelectedPath, filepath.FromSlash(alias.AggregateRoot().RelativePath()))
		requireWholeGroupWrite(t, destination, []byte("model=1\n"), 0o644)
		input.namespaces[0], err = journalnamespace.Observe(t.Context(), []aggregate.DocumentAddress{alias}, fixture.roots, namespacetest.Reservations(t), fixture.reader, input.budget)
		if err != nil {
			t.Fatal(err)
		}
		exact, known = input.namespaces[0].Observation().Exact()
	}
	if !known {
		t.Fatal("existing MCP document has no exact authority")
	}
	binding, err := ownership.NewDocumentBinding(input.owner, alias.AggregateRoot(), exact)
	if err != nil {
		t.Fatal(err)
	}
	reader.value, err = ownership.NewRegistry(native17FixtureClaims(t, input.owner, input.namespaces[0], input.before), binding)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := input.prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	after, known := prepared[0].Bindings()[0].KnownAfter()
	if _, present := after.Get(); !known || present {
		t.Fatal("MCP-only successor retained native binding")
	}
	convergence, err := ownership.NewDocumentBindingConvergence(prepared[0].Bindings())
	if err != nil {
		t.Fatal(err)
	}
	successor, changed, err := convergence.Apply(reader.value)
	if err != nil || !changed {
		t.Fatal("native binding retirement did not produce its actual successor", err)
	}

	input.before = input.after
	input.registry = nil
	if result, err := input.prepare(t.Context()); err == nil || result != nil {
		t.Fatal("MCP-only observation treated a missing reader as selected empty state")
	}
	input.registry, reader.value = reader, successor
	loads := reader.loads
	prepared, err = input.prepare(t.Context())
	if err != nil || len(prepared) != 1 || len(prepared[0].Bindings()) != 0 || prepared[0].Claims() != nil {
		t.Fatalf("project MCP-only work required native registry metadata: %v", err)
	}
	if reader.loads != loads+1 || !reader.value.Equal(successor) {
		t.Fatal("MCP-only preparation did not preserve its single selected registry observation")
	}
}

func TestNativeCaptureRegistryRetainsCompleteGlobalClaimProvenance(t *testing.T) {
	fixture := wholeGroupFixture(t, target.TargetPi, target.ScopeGlobal, "record", []byte("before"), nil)
	input, reader := captureOwnershipFixture(t, fixture)
	stored := reader.value.Claims()[0]
	aliasOwner, err := stateauthority.New(input.owner.StatefileAuthority(), filepath.Join(t.TempDir(), "new-diagnostic-manifest.toml"))
	if err != nil {
		t.Fatal(err)
	}
	input.owner = aliasOwner
	prepared, err := input.prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if after := prepared[0].Claims().After(); len(after) != 1 || !after[0].Equal(stored) || !after[0].Address().Footprint().Equal(output.WholePathFootprint()) {
		t.Fatal("retained global claim was reconstructed instead of preserved")
	}
}
