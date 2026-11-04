package journal

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/target"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func TestNativeBindingMembershipRetainsUnchangedSiblingsAndRetiresLastMember(t *testing.T) {
	fixture := documentClaimsForTest(t)
	first := membershipNativeRow(t, fixture.alias, "native-json-v1", "selected")
	unchanged := membershipNativeRow(t, fixture.alias, "native-json-v1", "unchanged")
	value := membershipBindingValue(t, fixture)
	retain := membershipIntent(t, fixture, value, value)
	retire := membershipIntent(t, fixture, value, ownership.NoDocumentBinding())
	acquire := membershipIntent(t, fixture, ownership.NoDocumentBinding(), value)
	indexes := selectionIndexes(t, selectionState(t, first, unchanged), selectionState(t, unchanged))
	aliases := []aggregate.DocumentAddress{fixture.alias}
	if err := indexes.validateNativeBindingMembership(t.Context(), fixture.owner, aliases, []ownership.DocumentBindingIntent{retain}); err != nil {
		t.Fatalf("unchanged sibling lost its binding: %v", err)
	}
	for _, intents := range [][]ownership.DocumentBindingIntent{nil, {retire}, {acquire}} {
		if err := indexes.validateNativeBindingMembership(t.Context(), fixture.owner, aliases, intents); err == nil {
			t.Fatal("remaining native membership accepted missing history or premature retirement")
		}
	}
	indexes = selectionIndexes(t, selectionState(t, first), durable.EmptySnapshot())
	if err := indexes.validateNativeBindingMembership(t.Context(), fixture.owner, aliases, []ownership.DocumentBindingIntent{retire}); err != nil {
		t.Fatalf("last-native retirement: %v", err)
	}
	if err := indexes.validateNativeBindingMembership(t.Context(), fixture.owner, aliases, []ownership.DocumentBindingIntent{retain}); err == nil {
		t.Fatal("last-native retirement left a binding behind")
	}
}

func TestNativeBindingMembershipCountsWholeAndKeyedStateAcrossModeChanges(t *testing.T) {
	fixture := documentClaimsForTest(t)
	whole := nativeWholeFileJournalBaseline(t, target.TargetPi, target.ScopeGlobal)
	contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeGlobal, whole.Destination(), documentKeyFootprint(t, "selected"), "native-json-v1")
	if err != nil {
		t.Fatal(err)
	}
	key := selectionNativeRow(t, contract, "1")
	fixture.alias = contract.Address().Document()
	wholeState, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: []durable.ManagedPathState{whole}})
	if err != nil {
		t.Fatal(err)
	}
	value := membershipBindingValue(t, fixture)
	retain := membershipIntent(t, fixture, value, value)
	retire := membershipIntent(t, fixture, value, ownership.NoDocumentBinding())
	for _, snapshots := range [][2]durable.Snapshot{
		{wholeState, selectionState(t, key)},
		{selectionState(t, key), wholeState},
		{wholeState, wholeState},
	} {
		indexes := selectionIndexes(t, snapshots[0], snapshots[1])
		if err := indexes.validateNativeBindingMembership(t.Context(), fixture.owner, []aggregate.DocumentAddress{fixture.alias}, []ownership.DocumentBindingIntent{retain}); err != nil {
			t.Fatalf("whole/key native membership did not retain its exact binding: %v", err)
		}
		if err := indexes.validateNativeBindingMembership(t.Context(), fixture.owner, []aggregate.DocumentAddress{fixture.alias}, []ownership.DocumentBindingIntent{retire}); err == nil {
			t.Fatal("whole/key mode change was mistaken for native retirement")
		}
	}
	indexes := selectionIndexes(t, wholeState, durable.EmptySnapshot())
	if err := indexes.validateNativeBindingMembership(t.Context(), fixture.owner, []aggregate.DocumentAddress{fixture.alias}, []ownership.DocumentBindingIntent{retire}); err != nil {
		t.Fatalf("whole-file last-native retirement: %v", err)
	}
}

func TestNativeBindingMembershipAdmitsOnlyFirstNativeAcquisition(t *testing.T) {
	fixture := documentClaimsForTest(t)
	row := membershipNativeRow(t, fixture.alias, "native-json-v1", "selected")
	value := membershipBindingValue(t, fixture)
	exact := membershipIntent(t, fixture, ownership.NoDocumentBinding(), value)
	pending, err := ownership.NewProvisionalDocumentBindingIntent(fixture.owner, fixture.alias.AggregateRoot(), fixture.provisional)
	if err != nil {
		t.Fatal(err)
	}
	aliases := []aggregate.DocumentAddress{fixture.alias}
	for _, intent := range []ownership.DocumentBindingIntent{exact, pending} {
		indexes := selectionIndexes(t, durable.EmptySnapshot(), selectionState(t, row))
		if err := indexes.validateNativeBindingMembership(t.Context(), fixture.owner, aliases, []ownership.DocumentBindingIntent{intent}); err != nil {
			t.Fatalf("first-native acquisition: %v", err)
		}
		for _, state := range []durable.Snapshot{durable.EmptySnapshot(), selectionState(t, row)} {
			indexes = selectionIndexes(t, state, state)
			if err := indexes.validateNativeBindingMembership(t.Context(), fixture.owner, aliases, []ownership.DocumentBindingIntent{intent}); err == nil {
				t.Fatal("state-neutral operation acquired new native binding evidence")
			}
		}
	}
}

func TestNativeBindingMembershipUsesOwnerKeyAndAllDeclaredDestinations(t *testing.T) {
	fixture := documentClaimsForTest(t)
	other := fixture
	destination, err := output.Parse("@pi-agent/other.json")
	if err != nil {
		t.Fatal(err)
	}
	contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeGlobal, destination, documentKeyFootprint(t, "selected"), "native-json-v1")
	if err != nil {
		t.Fatal(err)
	}
	other.alias = contract.Address().Document()
	firstRow := membershipNativeRow(t, fixture.alias, "native-json-v1", "selected")
	otherRow := membershipNativeRow(t, other.alias, "native-json-v1", "other")
	otherID, err := entity.New(entity.KindHostConfig, "other-declaration")
	if err != nil {
		t.Fatal(err)
	}
	otherSubject, err := topologyprojection.HostConfigKeySubject(otherID, other.alias.Target(), other.alias.Scope(), otherRow.Contribution().Contract().Address().KeyPath())
	if err != nil {
		t.Fatal(err)
	}
	otherRow, err = durable.NewManagedAggregateState(otherSubject, otherRow.Contribution())
	if err != nil {
		t.Fatal(err)
	}
	state := selectionState(t, firstRow, otherRow)
	indexes := selectionIndexes(t, state, state)
	firstValue, otherValue := membershipBindingValue(t, fixture), membershipBindingValue(t, other)
	firstIntent := membershipIntent(t, fixture, firstValue, firstValue)
	otherIntent := membershipIntent(t, other, otherValue, otherValue)
	aliases := []aggregate.DocumentAddress{other.alias, fixture.alias}
	intents := []ownership.DocumentBindingIntent{firstIntent, otherIntent}
	if err := indexes.validateNativeBindingMembership(t.Context(), fixture.owner, aliases, intents); err != nil {
		t.Fatalf("same physical target with distinct logical bindings: %v", err)
	}
	for _, bad := range [][]ownership.DocumentBindingIntent{{firstIntent}, {firstIntent, firstIntent}, {firstIntent, {}}} {
		if err := indexes.validateNativeBindingMembership(t.Context(), fixture.owner, aliases, bad); err == nil {
			t.Fatal("missing, duplicate or later-invalid binding intent was accepted")
		}
	}
	if err := indexes.validateNativeBindingMembership(t.Context(), fixture.owner, []aggregate.DocumentAddress{fixture.alias}, []ownership.DocumentBindingIntent{firstIntent}); err != nil {
		t.Fatalf("untouched destination required a synthetic group: %v", err)
	}
	if err := indexes.validateNativeBindingMembership(t.Context(), fixture.owner, []aggregate.DocumentAddress{fixture.alias}, intents); err == nil {
		t.Fatal("binding intent without its declared logical alias was accepted")
	}
	aliasOwner, err := stateauthority.New(fixture.owner.StatefileAuthority(), filepath.Join(t.TempDir(), "diagnostic-alias.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := indexes.validateNativeBindingMembership(t.Context(), aliasOwner, aliases, intents); err != nil || !firstIntent.Owner().ExactEqual(fixture.owner) {
		t.Fatalf("diagnostic provenance changed ownership or was refreshed: %v", err)
	}
	foreign, err := stateauthority.New(pathtest.Exact(filepath.Join(t.TempDir(), "foreign-state.json")), fixture.owner.ManifestPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := indexes.validateNativeBindingMembership(t.Context(), foreign, aliases, intents); err == nil {
		t.Fatal("foreign state-owner binding was accepted")
	}
}

func TestNativeBindingMembershipDoesNotCountDedicatedMCPRows(t *testing.T) {
	fixture := documentClaimsForTest(t)
	placement, found := aggregate.MCPPlacementForID(aggregate.MCPPlacementCodexProject)
	if !found {
		t.Fatal("Codex MCP placement is unavailable")
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
	fixture.alias = contribution.Contract().Address().Document()
	native := membershipNativeRow(t, fixture.alias, "native-toml-v1", "model")
	value := membershipBindingValue(t, fixture)
	retire := membershipIntent(t, fixture, value, ownership.NoDocumentBinding())
	indexes := selectionIndexes(t, selectionState(t, native, mcpRow), selectionState(t, mcpRow))
	if err := indexes.validateNativeBindingMembership(t.Context(), fixture.owner, []aggregate.DocumentAddress{fixture.alias}, []ownership.DocumentBindingIntent{retire}); err != nil {
		t.Fatalf("dedicated MCP member prevented last-native retirement: %v", err)
	}
	indexes = selectionIndexes(t, selectionState(t, mcpRow), selectionState(t, mcpRow))
	if err := indexes.validateNativeBindingMembership(t.Context(), fixture.owner, []aggregate.DocumentAddress{fixture.alias}, nil); err != nil {
		t.Fatalf("MCP-only state demanded a native binding: %v", err)
	}
	if err := indexes.validateNativeBindingMembership(t.Context(), fixture.owner, []aggregate.DocumentAddress{fixture.alias}, []ownership.DocumentBindingIntent{retire}); err == nil {
		t.Fatal("MCP-only state authorized native binding retirement")
	}
}

func TestNativeBindingMembershipEmptyAndCanceledOperations(t *testing.T) {
	fixture := documentClaimsForTest(t)
	indexes := selectionIndexes(t, durable.EmptySnapshot(), durable.EmptySnapshot())
	if err := indexes.validateNativeBindingMembership(t.Context(), fixture.owner, nil, nil); err != nil {
		t.Fatalf("ordinary-only operation required native group evidence: %v", err)
	}
	if err := indexes.validateNativeBindingMembership(t.Context(), stateauthority.Authority{}, nil, nil); err == nil {
		t.Fatal("uninitialized operation owner was accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := indexes.validateNativeBindingMembership(ctx, fixture.owner, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled binding join: %v", err)
	}
}

func membershipNativeRow(t *testing.T, alias aggregate.DocumentAddress, codec aggregate.CodecContractID, parts ...string) durable.ManagedAggregateState {
	t.Helper()
	contract, err := aggregate.NewHostConfigProjectionContract(alias.Target(), alias.Scope(), alias.AggregateRoot(), documentKeyFootprint(t, parts...), codec)
	if err != nil {
		t.Fatal(err)
	}
	return selectionNativeRow(t, contract, "1")
}

func membershipBindingValue(t *testing.T, fixture documentClaimFixture) ownership.DocumentBindingValue {
	t.Helper()
	binding, err := ownership.NewDocumentBinding(fixture.owner, fixture.alias.AggregateRoot(), fixture.exact)
	if err != nil {
		t.Fatal(err)
	}
	value, err := ownership.PresentDocumentBinding(binding)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func membershipIntent(t *testing.T, fixture documentClaimFixture, before, after ownership.DocumentBindingValue) ownership.DocumentBindingIntent {
	t.Helper()
	intent, err := ownership.NewExactDocumentBindingIntent(fixture.owner, fixture.alias.AggregateRoot(), before, after)
	if err != nil {
		t.Fatal(err)
	}
	return intent
}
