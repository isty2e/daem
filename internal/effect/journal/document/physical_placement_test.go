package document

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

func TestPhysicalPlacementPartitionsOnlyTheTransferredAlias(t *testing.T) {
	carrier, _, _ := carrierSelection(t)
	address := carrier.DocumentAddress()
	bar := documentClaimsForTest(t).alias
	groups := [][]aggregate.DocumentAddress{{address, bar}, {address}}
	transfer, err := NewCarrierTransfer(address, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	placement, err := ResolvePhysicalPlacement(t.Context(), groups, []CarrierTransfer{transfer})
	if err != nil {
		t.Fatal(err)
	}
	if index, found := placement.BeforeGroup(address); !found || index != 0 {
		t.Fatal("source lost its before membership")
	}
	if index, found := placement.AfterGroup(address); !found || index != 1 {
		t.Fatal("target lost its after membership")
	}
	if before, found := placement.BeforeGroup(bar); !found || before != 0 {
		t.Fatal("transfer moved an unrelated before alias")
	}
	if after, found := placement.AfterGroup(bar); !found || after != 0 || !slices.Equal(placement.AfterAliases(0), []aggregate.DocumentAddress{bar}) {
		t.Fatal("transfer moved an unrelated after alias")
	}
	coalesced, err := ResolvePhysicalPlacement(t.Context(), groups[:1], nil)
	if err != nil || !slices.Equal(coalesced.BeforeAliases(0), coalesced.AfterAliases(0)) || len(coalesced.AfterAliases(0)) != 2 {
		t.Fatal("one physical document did not retain both aliases without a transfer", err)
	}
	groups[0][0] = aggregate.DocumentAddress{}
	borrowed := placement.BeforeAliases(0)
	borrowed[0] = aggregate.DocumentAddress{}
	if !slices.Equal(placement.BeforeAliases(0), []aggregate.DocumentAddress{address, bar}) {
		t.Fatal("placement borrowed mutable caller or result slices")
	}
}

func TestPhysicalPlacementRefusesUnpairedAndAmbiguousTransfers(t *testing.T) {
	selection, _, _ := carrierSelection(t)
	address := selection.DocumentAddress()
	bar := documentClaimsForTest(t).alias
	transfer, err := NewCarrierTransfer(address, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []struct {
		groups    [][]aggregate.DocumentAddress
		transfers []CarrierTransfer
	}{
		{[][]aggregate.DocumentAddress{{address}, {address}}, nil},
		{[][]aggregate.DocumentAddress{{address}}, []CarrierTransfer{transfer}},
		{[][]aggregate.DocumentAddress{{address}, {address}, {address}}, []CarrierTransfer{transfer}},
		{[][]aggregate.DocumentAddress{{address}, {bar}, {address}}, []CarrierTransfer{transfer}},
		{[][]aggregate.DocumentAddress{{bar}, {address}, {address}}, []CarrierTransfer{transfer}},
		{[][]aggregate.DocumentAddress{{address}, {address}}, []CarrierTransfer{transfer, transfer}},
		{[][]aggregate.DocumentAddress{{address}, {address}}, []CarrierTransfer{{}}},
	} {
		placement, err := ResolvePhysicalPlacement(t.Context(), invalid.groups, invalid.transfers)
		_, partial := placement.BeforeGroup(address)
		if err == nil || partial {
			t.Fatal("invalid carrier placement returned usable partial membership", err)
		}
	}
	if _, err := NewCarrierTransfer(address, 0, 0); err == nil {
		t.Fatal("coalesced document was represented as a two-group transfer")
	}
	if _, err := NewCarrierTransfer(documentClaimsForTest(t).alias, 0, 1); err == nil {
		t.Fatal("carrier transfer admitted a different host or explicit native file")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ResolvePhysicalPlacement(ctx, [][]aggregate.DocumentAddress{{address}}, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("placement lost cancellation", err)
	}
}

func TestCarrierStateReconstructionUsesTheCorrectExpectedPhysicalGroup(t *testing.T) {
	selection, row, codecs := carrierSelection(t)
	address := selection.DocumentAddress()
	fixture := documentClaimsForTest(t)
	inputs := []StateGroupInput{
		NewStateGroupInput([]aggregate.DocumentAddress{address}, []topology.SubjectID{row.Subject()}, carrierComparison(t, selection, `{"theme":"old","mcpServers":{"search":{"type":"stdio","command":"node"}}}`, `{"theme":"old","mcpServers":{}}`), nil),
		NewStateGroupInput([]aggregate.DocumentAddress{address}, []topology.SubjectID{row.Subject()}, carrierComparison(t, selection, `{"theme":"new","mcpServers":{}}`, `{"theme":"new","mcpServers":{"search":{"type":"stdio","command":"node"}}}`), nil),
	}
	state := selectionState(t, row)
	beforeOnly := selectionIndexes(t, state, durable.EmptySnapshot())
	addresses, err := AliasSet(t.Context(), []aggregate.DocumentAddress{address})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := beforeOnly.SelectedContracts(t.Context(), []topology.SubjectID{row.Subject()}, []aggregate.DocumentAddress{address}); err != nil {
		t.Fatal("before-phase positive control could not resolve its own subject", err)
	}
	if _, err := beforeOnly.selectedContractsAt(t.Context(), []topology.SubjectID{row.Subject()}, nil, addresses); err == nil {
		t.Fatal("after-only target borrowed its subject from the source phase")
	}
	for source := range 2 {
		targetIndex := 1 - source
		transfer, err := NewCarrierTransfer(address, source, targetIndex)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ReconstructStateGroups(t.Context(), fixture.owner, state, state, inputs); err == nil {
			t.Fatal("ordinary reconstruction admitted duplicate physical membership")
		}
		fresh := slices.Clone(inputs)
		fresh[source].selectedSubjects = nil
		comparison := fresh[source].comparison
		fresh[source].comparison = stateGroupComparison(t, string(comparison.BeforeDocument().Content()), string(comparison.ExpectedDocument().Content()))
		if _, err := ReconstructStateGroups(t.Context(), fixture.owner, durable.EmptySnapshot(), state, fresh, transfer); err == nil {
			t.Fatal("fresh management invented an owned before carrier")
		}
		groups, err := ReconstructStateGroups(t.Context(), fixture.owner, state, state, inputs, transfer)
		if err != nil {
			t.Fatal(err)
		}
		if len(groups[source].AfterAliases()) != 0 || len(groups[targetIndex].BeforeAliases()) != 0 {
			t.Fatal("carrier membership was inferred from the global row in both groups")
		}
		index, err := IndexSnapshot(t.Context(), state)
		if err != nil {
			t.Fatal(err)
		}
		if err := index.ValidateExpectedBaselines(t.Context(), groups, codecs, nil); err != nil {
			t.Fatal("after baseline was checked against the retired source", err)
		}
		retained := slices.Clone(inputs)
		original := inputs[source].comparison.BeforeDocument()
		retained[source].comparison = carrierComparison(t, selection, string(original.Content()), string(original.Content()))
		if _, err := ReconstructStateGroups(t.Context(), fixture.owner, state, state, retained, transfer); err == nil {
			t.Fatal("carrier transfer kept an owned before projection at its source")
		}
		removed := slices.Clone(inputs)
		removed[source].comparison = carrierComparisonDocuments(t, selection, original, aggregate.AbsentDocument())
		if _, err := ReconstructStateGroups(t.Context(), fixture.owner, state, state, removed, transfer); err == nil {
			t.Fatal("projection-only migration acquired whole source-document deletion")
		}
		retirement := slices.Clone(inputs)
		retirement[targetIndex].selectedSubjects = nil
		comparison = retirement[targetIndex].comparison
		retirement[targetIndex].comparison = stateGroupComparison(t, string(comparison.BeforeDocument().Content()), string(comparison.BeforeDocument().Content()))
		if _, err := ReconstructStateGroups(t.Context(), fixture.owner, state, durable.EmptySnapshot(), retirement, transfer); err == nil {
			t.Fatal("last-member retirement acquired a new carrier target")
		}
		if _, err := ReconstructStateGroups(t.Context(), fixture.owner, state, durable.EmptySnapshot(), []StateGroupInput{inputs[source]}); err != nil {
			t.Fatal("last-member retirement required a transfer target", err)
		}
		missing := slices.Clone(inputs)
		missing[source].comparison = carrierComparisonDocuments(t, selection, aggregate.AbsentDocument(), aggregate.AbsentDocument())
		if _, err := ReconstructStateGroups(t.Context(), fixture.owner, state, state, missing, transfer); err != nil {
			t.Fatal("already-missing source could not retire its MCP metadata", err)
		}
		missing[source].comparison = carrierComparisonDocuments(t, selection, aggregate.AbsentDocument(), aggregate.ExistingDocument([]byte(`{}`)))
		if _, err := ReconstructStateGroups(t.Context(), fixture.owner, state, state, missing, transfer); err == nil {
			t.Fatal("source-only retirement created an empty old carrier")
		}
		for _, omitted := range []int{source, targetIndex} {
			missing := slices.Clone(inputs)
			missing[omitted].selectedSubjects = nil
			comparison := missing[omitted].comparison
			missing[omitted].comparison = stateGroupComparison(t, string(comparison.BeforeDocument().Content()), string(comparison.ExpectedDocument().Content()))
			if _, err := ReconstructStateGroups(t.Context(), fixture.owner, state, state, missing, transfer); err == nil {
				t.Fatal("carrier transfer omitted a source or target MCP frontier")
			}
		}
		groups[targetIndex].comparison = inputs[source].comparison
		if err := index.ValidateExpectedBaselines(t.Context(), groups, codecs, nil); err == nil {
			t.Fatal("target without its managed after value passed baseline validation")
		}
		slices.Reverse(inputs)
	}
}

func TestCarrierTransferKeepsAnotherCurrentContributorsSourceCreation(t *testing.T) {
	selection, row, codecs := carrierSelection(t)
	fixture := documentClaimsForTest(t)
	bar := membershipNativeRow(t, fixture.alias, "native-json-v1", "model")
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	codecs, err := aggregatecodec.ForOperation(t.Context(), []aggregate.CodecContractID{selection.CodecContractID(), bar.Contribution().Contract().CodecContractID()}, limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	barSelection, err := aggregate.NewSelection([]aggregate.ProjectionContract{bar.Contribution().Contract()})
	if err != nil {
		t.Fatal(err)
	}
	barDocument := aggregate.ExistingDocument([]byte(`{"model":1}`))
	barCodec, _ := codecs.Lookup(barSelection.CodecContractID())
	barSnapshot, failure := barCodec.Read(barDocument, barSelection)
	if failure != nil {
		t.Fatal(failure)
	}
	barValue, _ := barSnapshot.State(bar.Contribution().Contract())
	bar = selectionNativeRow(t, bar.Contribution().Contract(), barValue.CanonicalProjection())
	value := membershipBindingValue(t, fixture)
	source := NewStateGroupInput(
		[]aggregate.DocumentAddress{selection.DocumentAddress(), fixture.alias},
		[]topology.SubjectID{row.Subject(), bar.Subject()},
		carrierComparisonDocuments(t, selection, aggregate.AbsentDocument(), barDocument, barSelection),
		[]ownership.DocumentBindingIntent{membershipIntent(t, fixture, value, value)},
	)
	targetInput := NewStateGroupInput(
		[]aggregate.DocumentAddress{selection.DocumentAddress()}, []topology.SubjectID{row.Subject()},
		carrierComparison(t, selection, `{}`, `{"mcpServers":{"search":{"type":"stdio","command":"node"}}}`), nil,
	)
	transfer, err := NewCarrierTransfer(selection.DocumentAddress(), 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	state := selectionState(t, row, bar)
	groups, err := ReconstructStateGroups(t.Context(), fixture.owner, state, state, []StateGroupInput{source, targetInput}, transfer)
	if err != nil {
		t.Fatal("MCP retirement blocked the other current contributor's repair", err)
	}
	index, err := IndexSnapshot(t.Context(), state)
	if err != nil {
		t.Fatal(err)
	}
	if err := index.ValidateExpectedBaselines(t.Context(), groups, codecs, nil); err != nil {
		t.Fatal("mixed source creation lost a managed after value", err)
	}
	unselected := source
	unselected.selectedSubjects = []topology.SubjectID{row.Subject()}
	unselected.comparison = carrierComparisonDocuments(t, selection, aggregate.AbsentDocument(), aggregate.ExistingDocument([]byte(`{}`)))
	if _, err := ReconstructStateGroups(t.Context(), fixture.owner, state, state, []StateGroupInput{unselected, targetInput}, transfer); err == nil {
		t.Fatal("unselected unchanged membership authorized empty source creation")
	}
	source.bindingIntents = []ownership.DocumentBindingIntent{membershipIntent(t, fixture, value, ownership.NoDocumentBinding())}
	source.comparison = carrierComparisonDocuments(t, selection, aggregate.AbsentDocument(), aggregate.ExistingDocument([]byte(`{}`)), barSelection)
	if _, err := ReconstructStateGroups(t.Context(), fixture.owner, state, selectionState(t, row), []StateGroupInput{source, targetInput}, transfer); err == nil {
		t.Fatal("an empty after alias without current members authorized source creation")
	}
}

func TestUnpairedCarrierRetirementDoesNotCreateOrDeleteItsFile(t *testing.T) {
	selection, row, _ := carrierSelection(t)
	fixture := documentClaimsForTest(t)
	address := selection.DocumentAddress()
	binding, err := configuration.OwnedMCPSourceBinding(configuration.AliasInput{Target: address.Target(), Scope: address.Scope(), Destination: address.AggregateRoot()})
	if err != nil {
		t.Fatal(err)
	}
	route, err := NewAliasRoute(address, binding)
	if err != nil {
		t.Fatal(err)
	}
	beforeState, afterState := selectionState(t, row), selectionState(t)
	for _, test := range []struct {
		name          string
		before, after aggregate.Document
		refuse        bool
	}{
		{"existing projection retirement", aggregate.ExistingDocument([]byte(`{"mcpServers":{"search":{"type":"stdio","command":"node"}}}`)), aggregate.ExistingDocument([]byte(`{}`)), false},
		{"already missing", aggregate.AbsentDocument(), aggregate.AbsentDocument(), false},
		{"whole file deletion", aggregate.ExistingDocument([]byte(`{"mcpServers":{"search":{"type":"stdio","command":"node"}}}`)), aggregate.AbsentDocument(), true},
		{"empty source creation", aggregate.AbsentDocument(), aggregate.ExistingDocument([]byte(`{}`)), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			input, err := NewBoundStateGroupInput(t.Context(), []AliasRoute{route}, []topology.SubjectID{row.Subject()}, carrierComparisonDocuments(t, selection, test.before, test.after), nil)
			if err != nil {
				t.Fatal(err)
			}
			groups, err := ReconstructStateGroups(t.Context(), fixture.owner, beforeState, afterState, []StateGroupInput{input})
			if (err != nil) != test.refuse || test.refuse && groups != nil {
				t.Fatal("unpaired source root transition violated projection retirement", err)
			}
		})
	}
}

func carrierSelection(t *testing.T) (aggregate.Selection, durable.ManagedAggregateState, aggregate.CodecCatalog) {
	t.Helper()
	placement, ok := aggregate.ImplementedMCPPlacement(target.TargetClaudeCode, target.ScopeGlobal)
	if !ok {
		t.Fatal("Claude global MCP placement is unavailable")
	}
	contract, err := placement.ProjectionContract("search")
	if err != nil {
		t.Fatal(err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	codecs := aggregatecodec.Catalog()
	codec, _ := codecs.Lookup(selection.CodecContractID())
	snapshot, failure := codec.Read(aggregate.ExistingDocument([]byte(`{"mcpServers":{"search":{"type":"stdio","command":"node"}}}`)), selection)
	if failure != nil {
		t.Fatal(failure)
	}
	value, _ := snapshot.State(contract)
	contribution, err := placement.Contribution("search", value.CanonicalProjection())
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologymcp.ProjectionSubject(placement.Target(), placement.Scope(), "search")
	if err != nil {
		t.Fatal(err)
	}
	row, err := durable.NewManagedAggregateState(subject, contribution)
	if err != nil {
		t.Fatal(err)
	}
	return selection, row, codecs
}

func carrierComparison(t *testing.T, selection aggregate.Selection, before, after string) Comparison {
	t.Helper()
	return carrierComparisonDocuments(t, selection, aggregate.ExistingDocument([]byte(before)), aggregate.ExistingDocument([]byte(after)))
}

func carrierComparisonDocuments(t *testing.T, selection aggregate.Selection, before, after aggregate.Document, peers ...aggregate.Selection) Comparison {
	t.Helper()
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	selections := append([]aggregate.Selection{selection}, peers...)
	var required []aggregate.CodecContractID
	for _, selected := range selections {
		required = append(required, selected.CodecContractID())
	}
	codecs, err := aggregatecodec.ForOperation(t.Context(), required, limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	var participants []compose.Participant
	for _, selected := range selections {
		participant, err := Participant(selected, codecs)
		if err != nil {
			t.Fatal(err)
		}
		participants = append(participants, participant)
	}
	var beforeMode, afterMode *recovery.PermissionMode
	if before.Exists() {
		beforeMode = recovery.NewPermissionMode(0o600)
	}
	if after.Exists() {
		afterMode = recovery.NewPermissionMode(0o600)
	}
	comparison, err := NewSelectedComparison(t.Context(), ComparisonInput{
		Before: before, Expected: after, BeforeMode: beforeMode, ExpectedMode: afterMode, Limits: limits,
	}, participants, nil)
	if err != nil {
		t.Fatal(err)
	}
	return comparison
}
