package recovery_test

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
	"github.com/isty2e/daem/internal/target"
)

func TestPartialDocumentPhaseIgnoresNativeHistoricalRootAcrossGrammars(t *testing.T) {
	for _, syntax := range []configdocument.Syntax{configdocument.JSON, configdocument.JSONC, configdocument.JSONLineComments, configdocument.TOML} {
		t.Run(syntax.String(), func(t *testing.T) {
			codec := partialNativeCodec(t, syntax)
			selection := partialNativeSelection(t, codec, ".codex/example.data", partialKey(t, "a"))
			expected, retained, fresh := `{"a":true}`, `{"outside":1}`, `{"outside":2,"a":true}`
			if syntax == configdocument.TOML {
				expected, retained, fresh = "a=true\n", "# fresh\noutside=1\n", "outside=2\na=true\n"
			} else if syntax != configdocument.JSON {
				retained = "// fresh\n" + retained
			}
			before := partialState(t, false, 0, partialRead(t, codec, selection, aggregate.AbsentDocument()))
			after := partialState(t, true, 0o640, partialRead(t, codec, selection, aggregate.ExistingDocument([]byte(expected))))
			for _, test := range []struct {
				text         string
				wantBefore   bool
				wantExpected bool
			}{
				{text: retained, wantBefore: true},
				{text: fresh, wantExpected: true},
			} {
				current := partialState(t, true, 0o640, partialRead(t, codec, selection, aggregate.ExistingDocument([]byte(test.text))))
				match, err := recovery.MatchPartialDocumentStates(before, after, current)
				if err != nil || !match.Recoverable() || match.MatchesBefore() != test.wantBefore || match.MatchesExpected() != test.wantExpected {
					t.Fatalf("native selected phase was tied to historical root presence: %+v, %v", match, err)
				}
			}
		})
	}
}

func TestPartialDocumentRemovalRetainsForeignRootButNotThirdMetadata(t *testing.T) {
	codec := partialNativeCodec(t, configdocument.JSON)
	selection := partialNativeSelection(t, codec, ".codex/example.data", partialKey(t, "a"))
	before := partialState(t, true, 0o600, partialRead(t, codec, selection, aggregate.ExistingDocument([]byte(`{"a":true}`))))
	after := partialState(t, false, 0, partialRead(t, codec, selection, aggregate.AbsentDocument()))
	foreign := partialRead(t, codec, selection, aggregate.ExistingDocument([]byte(`{"foreign":"keep"}`)))
	current := partialState(t, true, 0o600, foreign)
	match, err := recovery.MatchPartialDocumentStates(before, after, current)
	if err != nil || !match.MatchesExpected() || match.MatchesBefore() || !match.Recoverable() {
		t.Fatalf("selected removal did not permit the retained foreign root: %+v, %v", match, err)
	}
	thirdMode := partialState(t, true, 0o640, foreign)
	match, err = recovery.MatchPartialDocumentStates(before, after, thirdMode)
	if err != nil {
		t.Fatal(err)
	}
	assertPartialBlocked(t, match)
}

func TestPartialDocumentMetadataEnvelopeAndExplicitZeroMode(t *testing.T) {
	absent := partialState(t, false, 0)
	present := partialState(t, true, 0)
	for _, test := range []struct {
		name                      string
		before, expected, current recovery.PartialDocumentState
		blocked                   bool
		invalid                   bool
	}{
		{name: "constructed absent empty", before: absent, expected: absent, current: absent},
		{name: "explicit zero mode", before: present, expected: present, current: present},
		{name: "new root outside both absent", before: absent, expected: absent, current: present, blocked: true},
		{name: "lost root outside both present", before: present, expected: present, current: absent, blocked: true},
		{name: "before metadata after creation", before: absent, expected: present, current: absent},
		{name: "expected metadata after creation", before: absent, expected: present, current: present},
		{name: "mode changed by partial mutation", before: present, expected: partialState(t, true, 0o600), current: present, invalid: true},
		{name: "zero state", before: absent, expected: absent, invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			match, err := recovery.MatchPartialDocumentStates(test.before, test.expected, test.current)
			if (err != nil) != test.invalid {
				t.Fatalf("partial metadata validity differs: %v", err)
			}
			if test.blocked || test.invalid {
				assertPartialBlocked(t, match)
			} else if !match.MatchesBefore() || !match.MatchesExpected() || !match.Recoverable() {
				t.Fatal("empty semantic product did not retain both phases within its metadata envelope")
			}
		})
	}
}

func TestPartialDocumentMixedKeysAndThirdValueBlockTheCompleteMatch(t *testing.T) {
	codec := partialNativeCodec(t, configdocument.JSON)
	selection := partialNativeSelection(t, codec, ".codex/example.data", partialKey(t, "a"), partialKey(t, "b"))
	read := func(text string) aggregate.Snapshot {
		return partialRead(t, codec, selection, aggregate.ExistingDocument([]byte(text)))
	}
	before := partialState(t, true, 0o600, read(`{"a":0,"b":0}`))
	expected := partialState(t, true, 0o600, read(`{"a":1,"b":1}`))
	mixed := partialState(t, true, 0o600, read(`{"b":1,"a":0,"outside":true}`))
	match, err := recovery.MatchPartialDocumentStates(before, expected, mixed)
	if err != nil || !match.Recoverable() || match.MatchesBefore() || match.MatchesExpected() {
		t.Fatalf("mixed selected phases did not remain selectively recoverable: %+v, %v", match, err)
	}
	third := partialState(t, true, 0o600, read(`{"a":2,"b":1}`))
	match, err = recovery.MatchPartialDocumentStates(before, expected, third)
	if err != nil {
		t.Fatal(err)
	}
	assertPartialBlocked(t, match)

	parentChild := partialNativeSelection(t, codec, ".codex/example.data", partialKey(t, "a"), partialKey(t, "a", "b"))
	blocked := partialRead(t, codec, parentChild, aggregate.ExistingDocument([]byte(`{"a":"original"}`)))
	blockedChild := false
	for _, state := range blocked.States() {
		if state.Contract().Address().KeyPath() == partialKey(t, "a", "b") {
			_, blockedChild = state.BlockedBy()
		}
	}
	if !blockedChild {
		t.Fatal("real native read did not retain the child traversal blocker")
	}
	changed := partialRead(t, codec, parentChild, aggregate.ExistingDocument([]byte(`{"a":{"b":true}}`)))
	match, err = recovery.MatchPartialDocumentStates(partialState(t, true, 0o600, blocked), partialState(t, true, 0o600, changed), partialState(t, true, 0o600, blocked))
	if err != nil || !match.MatchesBefore() || match.MatchesExpected() || !match.Recoverable() {
		t.Fatalf("selected blocker correlation was lost: %+v, %v", match, err)
	}
}

func TestPartialDocumentMatchingPreservesRealMCPParentFacts(t *testing.T) {
	native := partialNativeCodec(t, configdocument.TOML)
	nativeSelection := partialNativeSelection(t, native, ".codex/config.toml", partialKey(t, "model"))
	placement, found := aggregate.MCPPlacementForID(aggregate.MCPPlacementCodexProject)
	if !found {
		t.Fatal("Codex MCP placement unavailable")
	}
	mcp, found := mcpcodec.For(placement.CodecContractID())
	if !found {
		t.Fatal("Codex MCP codec unavailable")
	}
	contract, err := placement.ProjectionContract("search")
	if err != nil {
		t.Fatal(err)
	}
	mcpSelection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	original := aggregate.ExistingDocument([]byte("model='review'\n"))
	beforeNative := partialRead(t, native, nativeSelection, original)
	beforeMCP := partialRead(t, mcp, mcpSelection, original)
	before := partialState(t, true, 0o600, beforeNative, beforeMCP)
	if match, err := recovery.MatchPartialDocumentStates(before, before, partialState(t, true, 0o600, beforeMCP, beforeNative)); err != nil || !match.MatchesBefore() || !match.MatchesExpected() {
		t.Fatalf("member ordering changed the same observation: %v", err)
	}

	currentDocument := aggregate.ExistingDocument([]byte("model='review'\n[mcp_servers]\n"))
	currentMCP := partialRead(t, mcp, mcpSelection, currentDocument)
	oldServer, _ := beforeMCP.State(contract)
	newServer, _ := currentMCP.State(contract)
	if oldServer.Present() || newServer.Present() || oldServer.ParentPresent() || !newServer.ParentPresent() {
		t.Fatal("fixture does not distinguish an absent server's MCP section presence")
	}
	current := partialState(t, true, 0o600, partialRead(t, native, nativeSelection, currentDocument), currentMCP)
	match, err := recovery.MatchPartialDocumentStates(before, before, current)
	if err != nil {
		t.Fatal(err)
	}
	assertPartialBlocked(t, match)
}

func TestPartialDocumentConstructionAndSelectionRefusalsAreAllOrNothing(t *testing.T) {
	codec := partialNativeCodec(t, configdocument.JSON)
	selection := partialNativeSelection(t, codec, ".codex/example.data", partialKey(t, "a"))
	document := aggregate.ExistingDocument([]byte(`{"a":0,"b":0}`))
	snapshot := partialRead(t, codec, selection, document)
	mode, invalidMode := recovery.PermissionMode(0o640), recovery.PermissionMode(0o1000)
	for _, input := range []struct {
		exists bool
		mode   *recovery.PermissionMode
		states []aggregate.Snapshot
	}{
		{exists: true},
		{mode: &mode},
		{exists: true, mode: &invalidMode},
		{exists: true, mode: &mode, states: []aggregate.Snapshot{{}}},
		{exists: true, mode: &mode, states: []aggregate.Snapshot{snapshot, snapshot}},
		{states: []aggregate.Snapshot{snapshot}},
	} {
		state, err := recovery.NewPartialDocumentState(input.exists, input.mode, input.states)
		if err == nil {
			t.Fatal("contradictory partial document input was accepted")
		}
		match, err := recovery.MatchPartialDocumentStates(state, state, state)
		if err == nil {
			t.Fatal("failed construction returned usable partial evidence")
		}
		assertPartialBlocked(t, match)
	}

	inputs := []aggregate.Snapshot{snapshot}
	before, err := recovery.NewPartialDocumentState(true, &mode, inputs)
	if err != nil {
		t.Fatal(err)
	}
	inputs[0] = aggregate.Snapshot{}
	mode = 0
	actual := partialState(t, true, 0o640, snapshot)
	if match, err := recovery.MatchPartialDocumentStates(before, before, actual); err != nil || !match.MatchesBefore() || !match.MatchesExpected() {
		t.Fatalf("caller mutation changed immutable observed facts: %v", err)
	}
	otherSelection := partialNativeSelection(t, codec, ".codex/example.data", partialKey(t, "b"))
	other := partialRead(t, codec, otherSelection, document)
	wrongCodec := partialNativeCodec(t, configdocument.JSONC)
	wrongContract := partialRead(t, wrongCodec, partialNativeSelection(t, wrongCodec, ".codex/example.data", partialKey(t, "a")), document)
	for _, incompatible := range []recovery.PartialDocumentState{
		partialState(t, true, 0o640),
		partialState(t, true, 0o640, other),
		partialState(t, true, 0o640, snapshot, other),
		partialState(t, true, 0o640, wrongContract),
	} {
		for _, sides := range [][3]recovery.PartialDocumentState{{before, actual, incompatible}, {before, incompatible, actual}} {
			match, err := recovery.MatchPartialDocumentStates(sides[0], sides[1], sides[2])
			if err == nil {
				t.Fatal("incomplete/foreign/changed contract selection was accepted")
			}
			assertPartialBlocked(t, match)
		}
	}
}

func TestPartialDocumentRootMetadataQueryRejectsUninitializedEvidence(t *testing.T) {
	absent, err := recovery.NewPartialDocumentState(false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	zero := recovery.PartialDocumentState{}
	if zero.SameRootMetadata(absent) || absent.SameRootMetadata(zero) {
		t.Fatal("uninitialized evidence matched an observed absent root")
	}
	mode := recovery.PermissionMode(0)
	present, err := recovery.NewPartialDocumentState(true, &mode, nil)
	if err != nil {
		t.Fatal(err)
	}
	if present.SameRootMetadata(absent) || absent.SameRootMetadata(present) || !present.SameRootMetadata(present) || !absent.SameRootMetadata(absent) {
		t.Fatal("root metadata query collapsed absence and exact0000 presence")
	}
}

func partialNativeCodec(t *testing.T, syntax configdocument.Syntax) aggregate.Codec {
	t.Helper()
	id, found := aggregate.HostConfigCodecForSyntax(syntax.String())
	if !found {
		t.Fatal("fixture native grammar unavailable")
	}
	limits := configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64}
	codec, err := nativecodec.New(t.Context(), id, limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	return codec
}

func partialNativeSelection(t *testing.T, codec aggregate.Codec, root string, keys ...output.KeyPath) aggregate.Selection {
	t.Helper()
	destination, err := output.Parse(root)
	if err != nil {
		t.Fatal(err)
	}
	contracts := make([]aggregate.ProjectionContract, 0, len(keys))
	for _, key := range keys {
		footprint, err := output.NewKeyFootprint(key)
		if err != nil {
			t.Fatal(err)
		}
		contract, err := aggregate.NewHostConfigProjectionContract(target.TargetCodex, target.ScopeProject, destination, footprint, codec.ContractID())
		if err != nil {
			t.Fatal(err)
		}
		contracts = append(contracts, contract)
	}
	selection, err := aggregate.NewSelection(contracts)
	if err != nil {
		t.Fatal(err)
	}
	return selection
}

func partialKey(t *testing.T, parts ...string) output.KeyPath {
	t.Helper()
	key, err := output.NewKeyPath(parts...)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func partialRead(t *testing.T, codec aggregate.Codec, selection aggregate.Selection, document aggregate.Document) aggregate.Snapshot {
	t.Helper()
	snapshot, failure := codec.Read(document, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	return snapshot
}

func partialState(t *testing.T, exists bool, mode recovery.PermissionMode, snapshots ...aggregate.Snapshot) recovery.PartialDocumentState {
	t.Helper()
	var pointer *recovery.PermissionMode
	if exists {
		pointer = &mode
	}
	state, err := recovery.NewPartialDocumentState(exists, pointer, snapshots)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func assertPartialBlocked(t *testing.T, match recovery.PartialDocumentMatch) {
	t.Helper()
	if match.MatchesBefore() || match.MatchesExpected() || match.Recoverable() {
		t.Fatal("refusal/third state left a positive partial-document match")
	}
}
