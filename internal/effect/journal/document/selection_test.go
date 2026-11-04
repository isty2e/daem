package document

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func TestDocumentSelectionFrontierReconstructsActualParentContract(t *testing.T) {
	parent := selectionNativeRow(t, piSelectionContract(t, ".pi/native.json", "node"), "1")
	child := selectionNativeRow(t, piSelectionContract(t, ".pi/native.json", "node", "leaf"), "2")
	outside := selectionNativeRow(t, piSelectionContract(t, ".pi/native.json", "outside"), "8")
	indexes := selectionIndexes(t, selectionState(t, parent, outside), selectionState(t, child, outside))
	selections, err := indexes.SelectedContracts(t.Context(), []topology.SubjectID{child.Subject(), parent.Subject()}, []aggregate.DocumentAddress{parent.Contribution().Contract().Address().Document()})
	if err != nil || len(selections) != 1 || len(selections[0].Contracts()) != 1 || !selections[0].Contracts()[0].Equal(parent.Contribution().Contract()) {
		t.Fatalf("actual parent frontier: selections=%d error=%v", len(selections), err)
	}

	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	codec, err := nativecodec.New(t.Context(), selections[0].CodecContractID(), limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	participant, err := compose.NewParticipant(codec, selections[0], configdocument.JSON, []output.KeyPath{parent.Contribution().Contract().Address().KeyPath()})
	if err != nil {
		t.Fatal(err)
	}
	mode := recovery.NewPermissionMode(0o600)
	comparison, err := NewSelectedComparison(t.Context(), ComparisonInput{
		Before:     aggregate.ExistingDocument([]byte(`{"node":1,"outside":8}`)),
		Expected:   aggregate.ExistingDocument([]byte(`{"node":{"leaf":2},"outside":8}`)),
		BeforeMode: mode, ExpectedMode: mode, Limits: limits,
	}, []compose.Participant{participant}, nil)
	if err != nil {
		t.Fatal(err)
	}
	current := aggregate.ExistingDocument([]byte(`{"node":{"leaf":2},"outside":99}`))
	restored, err := comparison.RestoreBefore(t.Context(), current, mode)
	if err != nil {
		t.Fatal(err)
	}
	var values struct {
		Node    int `json:"node"`
		Outside int `json:"outside"`
	}
	if err := json.Unmarshal(restored.Document().Content(), &values); err != nil || values.Node != 1 || values.Outside != 99 {
		t.Fatalf("resolved frontier rollback lost selected or foreign values: %+v, %v", values, err)
	}
}

func TestDocumentSelectionNarrowingDoesNotSelectWholeOrReleasedState(t *testing.T) {
	whole := nativeWholeFileJournalBaseline(t, target.TargetPi, target.ScopeProject)
	retained := selectionNativeRow(t, piSelectionContract(t, whole.Destination().String(), "model"), `"fast"`)
	other := selectionNativeRow(t, piSelectionContract(t, whole.Destination().String(), "other"), "true")
	before, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: []durable.ManagedPathState{whole}})
	if err != nil {
		t.Fatal(err)
	}
	indexes := selectionIndexes(t, before, selectionState(t, retained, other))
	aliases := []aggregate.DocumentAddress{retained.Contribution().Contract().Address().Document()}
	selected, err := indexes.SelectedContracts(t.Context(), []topology.SubjectID{retained.Subject()}, aliases)
	if err != nil || len(selected) != 1 || len(selected[0].Contracts()) != 1 || !selected[0].Contracts()[0].Equal(retained.Contribution().Contract()) {
		t.Fatalf("narrowing selection: groups=%d error=%v", len(selected), err)
	}
	if selected, err := indexes.SelectedContracts(t.Context(), nil, aliases); err != nil || len(selected) != 0 {
		t.Fatalf("release-only selection: groups=%d error=%v", len(selected), err)
	}
	if selected, err := indexes.SelectedContracts(t.Context(), []topology.SubjectID{whole.Subject()}, aliases); err == nil || selected != nil {
		t.Fatal("whole-file baseline became selected-key restoration authority")
	}
}

func TestDocumentSelectionDestinationChangeResolvesEachRetireCreateSide(t *testing.T) {
	before := selectionNativeRow(t, piSelectionContract(t, ".pi/old.json", "model"), `"before"`)
	after := selectionNativeRow(t, piSelectionContract(t, ".pi/new.json", "model"), `"after"`)
	if before.Subject() != after.Subject() {
		t.Fatal("fixture does not preserve the stable key subject across destination change")
	}
	indexes := selectionIndexes(t, selectionState(t, before), selectionState(t, after))
	for _, row := range []durable.ManagedAggregateState{before, after} {
		contract := row.Contribution().Contract()
		selected, err := indexes.SelectedContracts(t.Context(), []topology.SubjectID{row.Subject()}, []aggregate.DocumentAddress{contract.Address().Document()})
		if err != nil || len(selected) != 1 || len(selected[0].Contracts()) != 1 || !selected[0].Contracts()[0].Equal(contract) {
			t.Fatalf("retire/create address did not resolve its actual side: groups=%d error=%v", len(selected), err)
		}
	}
	if selected, err := indexes.SelectedContracts(t.Context(), []topology.SubjectID{before.Subject()}, []aggregate.DocumentAddress{
		before.Contribution().Contract().Address().Document(), after.Contribution().Contract().Address().Document(),
	}); err == nil || selected != nil {
		t.Fatal("one group silently retargeted an in-group subject contract")
	}
}

func TestDocumentSelectionRejectsInvalidRefsWithoutPartialOutput(t *testing.T) {
	row := selectionNativeRow(t, piSelectionContract(t, ".pi/native.json", "model"), `"fast"`)
	missing := selectionNativeRow(t, piSelectionContract(t, ".pi/native.json", "missing"), "true")
	alias := row.Contribution().Contract().Address().Document()
	other := piSelectionContract(t, ".pi/other.json", "model").Address().Document()
	wrongNamespace, err := topology.NewSubjectID(topology.SubjectProjection, "other-placement", row.Subject().Key())
	if err != nil {
		t.Fatal(err)
	}
	indexes := selectionIndexes(t, selectionState(t, row), selectionState(t, row))
	for _, test := range []struct {
		name    string
		refs    []topology.SubjectID
		aliases []aggregate.DocumentAddress
	}{
		{name: "missing after a valid ref", refs: []topology.SubjectID{row.Subject(), missing.Subject()}, aliases: []aggregate.DocumentAddress{alias}},
		{name: "duplicate", refs: []topology.SubjectID{row.Subject(), row.Subject()}, aliases: []aggregate.DocumentAddress{alias}},
		{name: "wrong full namespace", refs: []topology.SubjectID{wrongNamespace}, aliases: []aggregate.DocumentAddress{alias}},
		{name: "wrong document", refs: []topology.SubjectID{row.Subject()}, aliases: []aggregate.DocumentAddress{other}},
		{name: "zero ref", refs: []topology.SubjectID{{}}, aliases: []aggregate.DocumentAddress{alias}},
		{name: "missing alias", refs: []topology.SubjectID{row.Subject()}},
		{name: "duplicate alias", refs: []topology.SubjectID{row.Subject()}, aliases: []aggregate.DocumentAddress{alias, alias}},
		{name: "zero alias", refs: []topology.SubjectID{row.Subject()}, aliases: []aggregate.DocumentAddress{{}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if selected, err := indexes.SelectedContracts(t.Context(), test.refs, test.aliases); err == nil || selected != nil {
				t.Fatalf("invalid selection returned partial groups=%d error=%v", len(selected), err)
			}
		})
	}

	changedContract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeProject, row.Contribution().Contract().Address().Document().AggregateRoot(), row.Contribution().Contract().Address().Footprint(), "native-jsonc-v1")
	if err != nil {
		t.Fatal(err)
	}
	changed := selectionNativeRow(t, changedContract, `"fast"`)
	indexes = selectionIndexes(t, selectionState(t, row), selectionState(t, changed))
	if selected, err := indexes.SelectedContracts(t.Context(), []topology.SubjectID{row.Subject()}, []aggregate.DocumentAddress{alias}); err == nil || selected != nil {
		t.Fatal("selected grammar changed silently within one document")
	}
}

func TestDocumentSelectionIsStableAcrossInputOrderingAndPreservesLiteralKeys(t *testing.T) {
	first := selectionNativeRow(t, piSelectionContract(t, ".pi/a.json", "a", "b"), "1")
	literal := selectionNativeRow(t, piSelectionContract(t, ".pi/a.json", "a/b"), "2")
	control := selectionNativeRow(t, piSelectionContract(t, ".pi/b.json", "line\nkey"), "3")
	rows := []durable.ManagedAggregateState{first, literal, control}
	refs := []topology.SubjectID{control.Subject(), literal.Subject(), first.Subject()}
	aliases := []aggregate.DocumentAddress{control.Contribution().Contract().Address().Document(), first.Contribution().Contract().Address().Document()}
	indexes := selectionIndexes(t, selectionState(t, rows...), selectionState(t, rows...))
	selected, err := indexes.SelectedContracts(t.Context(), refs, aliases)
	if err != nil || len(selected) != 2 {
		t.Fatalf("literal and nested keys collapsed: groups=%d error=%v", len(selected), err)
	}
	contracts := append(selected[0].Contracts(), selected[1].Contracts()...)
	if len(contracts) != 3 || !slices.ContainsFunc(contracts, first.Contribution().Contract().Equal) || !slices.ContainsFunc(contracts, literal.Contribution().Contract().Equal) || !slices.ContainsFunc(contracts, control.Contribution().Contract().Equal) {
		t.Fatal("literal, nested or control-bearing key identity was lost")
	}
	slices.Reverse(rows)
	slices.Reverse(refs)
	slices.Reverse(aliases)
	indexes = selectionIndexes(t, selectionState(t, rows...), selectionState(t, rows...))
	reordered, err := indexes.SelectedContracts(t.Context(), refs, aliases)
	if err != nil || len(reordered) != len(selected) {
		t.Fatalf("reordered selection: %v", err)
	}
	for index := range selected {
		if !slices.EqualFunc(selected[index].Contracts(), reordered[index].Contracts(), aggregate.ProjectionContract.Equal) {
			t.Fatal("snapshot/ref/alias ordering changed the resolved contracts")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := IndexSnapshots(ctx, selectionState(t, rows...), durable.EmptySnapshot()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled indexing: %v", err)
	}
	if selected, err := indexes.SelectedContracts(ctx, refs, aliases); !errors.Is(err, context.Canceled) || selected != nil {
		t.Fatalf("canceled selection returned groups=%d error=%v", len(selected), err)
	}
}

func TestDocumentSelectionReconstructsNativeAndMCPCodecsIndependently(t *testing.T) {
	placement, found := aggregate.MCPPlacementForID(aggregate.MCPPlacementCodexProject)
	if !found {
		t.Fatal("Codex MCP placement is unavailable")
	}
	mcpContract, err := placement.ProjectionContract("search")
	if err != nil {
		t.Fatal(err)
	}
	mcp, found := mcpcodec.For(placement.CodecContractID())
	if !found {
		t.Fatal("Codex MCP codec is unavailable")
	}
	subject, err := topologymcp.ProjectionSubject(target.TargetCodex, target.ScopeProject, "search")
	if err != nil {
		t.Fatal(err)
	}
	mcpSelection, err := aggregate.NewSelection([]aggregate.ProjectionContract{mcpContract})
	if err != nil {
		t.Fatal(err)
	}
	mcpRow := func(content string) durable.ManagedAggregateState {
		baseline, failure := mcp.Read(aggregate.ExistingDocument([]byte(content)), mcpSelection)
		if failure != nil {
			t.Fatal(failure)
		}
		contribution, err := aggregate.NewManagedContribution(mcpContract, baseline.States()[0].CanonicalProjection())
		if err != nil {
			t.Fatal(err)
		}
		row, err := durable.NewManagedAggregateState(subject, contribution)
		if err != nil {
			t.Fatal(err)
		}
		return row
	}
	key, err := output.NewKeyPath("model")
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	nativeContract, err := aggregate.NewHostConfigProjectionContract(target.TargetCodex, target.ScopeProject, mcpContract.Address().Document().AggregateRoot(), footprint, "native-toml-v1")
	if err != nil {
		t.Fatal(err)
	}
	before := "model='old'\n[mcp_servers.search]\ncommand='node'\n"
	after := "model='fast'\n[mcp_servers.search]\ncommand='python'\n"
	nativeBefore := selectionNativeRow(t, nativeContract, `"old"`)
	nativeAfter := selectionNativeRow(t, nativeContract, `"fast"`)
	indexes := selectionIndexes(t, selectionState(t, nativeBefore, mcpRow(before)), selectionState(t, nativeAfter, mcpRow(after)))
	selections, err := indexes.SelectedContracts(t.Context(), []topology.SubjectID{subject, nativeBefore.Subject()}, []aggregate.DocumentAddress{mcpContract.Address().Document()})
	if err != nil || len(selections) != 2 {
		t.Fatalf("native/MCP codec partition: groups=%d error=%v", len(selections), err)
	}
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	native, err := nativecodec.New(t.Context(), "native-toml-v1", limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := aggregate.NewCodecCatalog([]aggregate.Codec{mcp, native})
	if err != nil {
		t.Fatal(err)
	}
	var participants []compose.Participant
	for _, selection := range selections {
		codec, found := catalog.Lookup(selection.CodecContractID())
		if !found {
			t.Fatal("resolved codec was lost")
		}
		participant, err := compose.NewParticipant(codec, selection, configdocument.TOML, []output.KeyPath{selection.Contracts()[0].Address().KeyPath()})
		if err != nil {
			t.Fatal(err)
		}
		participants = append(participants, participant)
	}
	mode := recovery.NewPermissionMode(0o600)
	comparison, err := NewSelectedComparison(t.Context(), ComparisonInput{
		Before: aggregate.ExistingDocument([]byte(before)), Expected: aggregate.ExistingDocument([]byte(after)),
		BeforeMode: mode, ExpectedMode: mode, Limits: limits,
	}, participants, nil)
	if err != nil {
		t.Fatal(err)
	}
	mixed := aggregate.ExistingDocument([]byte("model='fast'\n[mcp_servers.search]\ncommand='node'\n"))
	match, err := comparison.Match(t.Context(), mixed, mode)
	if err != nil || !match.Recoverable() || match.MatchesBefore() || match.MatchesExpected() {
		t.Fatalf("resolved mixed-codec state was collapsed into one phase: %+v, %v", match, err)
	}
}

func piSelectionContract(t *testing.T, destination string, parts ...string) aggregate.ProjectionContract {
	t.Helper()
	root, err := output.Parse(destination)
	if err != nil {
		t.Fatal(err)
	}
	key, err := output.NewKeyPath(parts...)
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeProject, root, footprint, "native-json-v1")
	if err != nil {
		t.Fatal(err)
	}
	return contract
}

func selectionNativeRow(t *testing.T, contract aggregate.ProjectionContract, value string) durable.ManagedAggregateState {
	t.Helper()
	id, err := entity.New(entity.KindHostConfig, "native")
	if err != nil {
		t.Fatal(err)
	}
	address := contract.Address()
	subject, err := topologyprojection.HostConfigKeySubject(id, address.Document().Target(), address.Document().Scope(), address.KeyPath())
	if err != nil {
		t.Fatal(err)
	}
	contribution, err := aggregate.NewManagedContribution(contract, value)
	if err != nil {
		t.Fatal(err)
	}
	row, err := durable.NewManagedAggregateState(subject, contribution)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func selectionState(t *testing.T, rows ...durable.ManagedAggregateState) durable.Snapshot {
	t.Helper()
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{ManagedAggregates: rows})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func selectionIndexes(t *testing.T, before, after durable.Snapshot) SnapshotIndexes {
	t.Helper()
	indexes, err := IndexSnapshots(t.Context(), before, after)
	if err != nil {
		t.Fatal(err)
	}
	return indexes
}
