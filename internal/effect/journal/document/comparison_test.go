package document_test

import (
	"context"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/target"
)

func TestDocumentComparisonBindsWholeBytesAndExactMetadata(t *testing.T) {
	BeforeMode, expectedMode, thirdMode := recovery.PermissionMode(0o644), recovery.PermissionMode(0o600), recovery.PermissionMode(0o640)
	input := comparisonInput("opaque original", "opaque expected", &BeforeMode, &expectedMode)
	comparison, err := document.NewWholeComparison(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		document aggregate.Document
		mode     *recovery.PermissionMode
		before   bool
		expected bool
	}{
		{"before", input.Before, &BeforeMode, true, false},
		{"expected", input.Expected, &expectedMode, false, true},
		{"third bytes", aggregate.ExistingDocument([]byte("unrecorded")), &expectedMode, false, false},
		{"third mode", input.Expected, &thirdMode, false, false},
		{"mixed bytes and mode", input.Expected, &BeforeMode, false, false},
		{"missing", aggregate.AbsentDocument(), nil, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			match, err := comparison.Match(t.Context(), test.document, test.mode)
			if err != nil || match.MatchesBefore() != test.before || match.MatchesExpected() != test.expected || match.Recoverable() != (test.before || test.expected) {
				t.Fatalf("whole comparison differs: %+v, %v", match, err)
			}
		})
	}
	input.Before, input.BeforeMode = aggregate.AbsentDocument(), nil
	absent, err := document.NewWholeComparison(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	match, err := absent.Match(t.Context(), aggregate.AbsentDocument(), nil)
	if err != nil || !match.MatchesBefore() || !match.Recoverable() {
		t.Fatalf("whole absence lost: %+v, %v", match, err)
	}
}

func TestDocumentComparisonJoinsNativeAndDedicatedPiPhases(t *testing.T) {
	mode := recovery.PermissionMode(0o600)
	before := `{"model":"balanced","outside":0,"packages":["npm:a","npm:b"]}`
	expected := `{"model":"fast","outside":0,"packages":["npm:b","npm:a"]}`
	input := comparisonInput(before, expected, &mode, &mode)
	order := comparisonOrder(t, before, expected)
	participants := []compose.Participant{comparisonParticipant(t, "model")}
	comparison, err := document.NewSelectedComparison(t.Context(), input, participants, []configdocument.JSONArrayEdit{order})
	if err != nil {
		t.Fatal(err)
	}
	participants[0] = compose.Participant{}
	order = configdocument.JSONArrayEdit{}
	for _, test := range []struct {
		name        string
		content     string
		before      bool
		expected    bool
		recoverable bool
	}{
		{"before outside drift", `{"model":"balanced","outside":1,"packages":["npm:a","npm:b"]}`, true, false, true},
		{"expected outside drift", `{"model":"fast","outside":2,"packages":["npm:b","npm:a"]}`, false, true, true},
		{"mixed native before", `{"model":"balanced","outside":3,"packages":["npm:b","npm:a"]}`, false, false, true},
		{"mixed order before", `{"model":"fast","outside":4,"packages":["npm:a","npm:b"]}`, false, false, true},
		{"third native", `{"model":"other","packages":["npm:b","npm:a"]}`, false, false, false},
		{"third package row", `{"model":"fast","packages":["npm:c","npm:a"]}`, false, false, false},
		{"third literal type", `{"model":"fast","packages":[true]}`, false, false, false},
		{"missing packages", `{"model":"fast"}`, false, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			match, err := comparison.Match(t.Context(), aggregate.ExistingDocument([]byte(test.content)), &mode)
			if err != nil || match.MatchesBefore() != test.before || match.MatchesExpected() != test.expected || match.Recoverable() != test.recoverable {
				t.Fatalf("joined comparison differs: %+v, %v", match, err)
			}
		})
	}
}

func TestDocumentComparisonSeparatesAbsenceAndEmptyReleaseFromMembership(t *testing.T) {
	mode := recovery.PermissionMode(0)
	input := comparisonInput("", `{"model":"fast"}`, nil, &mode)
	input.Before = aggregate.AbsentDocument()
	comparison, err := document.NewSelectedComparison(t.Context(), input, []compose.Participant{comparisonParticipant(t, "model")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	match, err := comparison.Match(t.Context(), aggregate.ExistingDocument([]byte(`{"outside":true}`)), &mode)
	if err != nil || !match.MatchesBefore() || match.MatchesExpected() || !match.Recoverable() {
		t.Fatalf("selected before value was incorrectly coupled to old root absence: %+v, %v", match, err)
	}
	releaseInput := comparisonInput(`{"released":"original"}`, `{"released":"original"}`, &mode, &mode)
	release, err := document.NewSelectedComparison(t.Context(), releaseInput, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	match, err = release.Match(t.Context(), aggregate.ExistingDocument([]byte("unselected external bytes")), &mode)
	if err != nil || !match.MatchesBefore() || !match.MatchesExpected() || !match.Recoverable() {
		t.Fatalf("empty management release compared unselected contents: %+v, %v", match, err)
	}
}

func TestDocumentComparisonJoinsRealMCPAndNativeReaders(t *testing.T) {
	mode := recovery.PermissionMode(0o600)
	native := comparisonNativeCodec(t, configdocument.TOML)
	key, err := output.NewKeyPath("model")
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := output.Parse(".codex/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	nativeContract, err := aggregate.NewHostConfigProjectionContract(target.TargetCodex, target.ScopeProject, destination, footprint, native.ContractID())
	if err != nil {
		t.Fatal(err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{nativeContract})
	if err != nil {
		t.Fatal(err)
	}
	nativeParticipant, err := compose.NewParticipant(native, selection, configdocument.TOML, []output.KeyPath{key})
	if err != nil {
		t.Fatal(err)
	}
	placement, found := aggregate.MCPPlacementForID(aggregate.MCPPlacementCodexProject)
	if !found {
		t.Fatal("Codex MCP fixture placement unavailable")
	}
	mcp, found := mcpcodec.For(placement.CodecContractID())
	if !found {
		t.Fatal("Codex MCP fixture codec unavailable")
	}
	contract, err := placement.ProjectionContract("search")
	if err != nil {
		t.Fatal(err)
	}
	mcpSelection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	mcpParticipant, err := compose.NewParticipant(mcp, mcpSelection, configdocument.TOML, []output.KeyPath{contract.Address().KeyPath()})
	if err != nil {
		t.Fatal(err)
	}
	input := comparisonInput("model='balanced'\n", "model='fast'\n[mcp_servers.search]\ncommand='node'\n", &mode, &mode)
	comparison, err := document.NewSelectedComparison(t.Context(), input, []compose.Participant{nativeParticipant, mcpParticipant}, nil)
	if err != nil {
		t.Fatal(err)
	}
	current := aggregate.ExistingDocument([]byte("model='balanced'\noutside=true\n[mcp_servers.search]\ncommand='node'\n"))
	match, err := comparison.Match(t.Context(), current, &mode)
	if err != nil || match.MatchesBefore() || match.MatchesExpected() || !match.Recoverable() {
		t.Fatalf("native-before/MCP-expected mixture was not recoverable: %+v, %v", match, err)
	}
	third := aggregate.ExistingDocument([]byte("model='balanced'\n[mcp_servers.search]\ncommand='other'\n"))
	match, err = comparison.Match(t.Context(), third, &mode)
	if err != nil || match.Recoverable() {
		t.Fatalf("third MCP value was accepted: %+v, %v", match, err)
	}
}

func TestDocumentComparisonRefusesUnboundOverlappingAndInvalidEvidence(t *testing.T) {
	mode := recovery.PermissionMode(0o600)
	input := comparisonInput(`{"model":1,"packages":["npm:a","npm:b"]}`, `{"model":2,"packages":["npm:b","npm:a"]}`, &mode, &mode)
	for _, order := range []configdocument.JSONArrayEdit{
		{},
		comparisonOrder(t, `{"packages":["npm:c","npm:d"]}`, `{"packages":["npm:d","npm:c"]}`),
	} {
		comparison, err := document.NewSelectedComparison(t.Context(), input, nil, []configdocument.JSONArrayEdit{order})
		if err == nil {
			t.Fatal("unbound package inverse was accepted")
		}
		if match, err := comparison.Match(t.Context(), input.Expected, &mode); err == nil || match.Recoverable() {
			t.Fatal("failed construction returned usable comparison evidence")
		}
	}
	order := comparisonOrder(t, string(input.Before.Content()), string(input.Expected.Content()))
	if _, err := document.NewSelectedComparison(t.Context(), input, []compose.Participant{comparisonParticipant(t, "packages")}, []configdocument.JSONArrayEdit{order}); err == nil {
		t.Fatal("generic package area overlapped the dedicated order owner")
	}
	if _, err := document.NewSelectedComparison(t.Context(), input, []compose.Participant{{}}, nil); err == nil {
		t.Fatal("uninitialized later semantic reader was accepted")
	}
	invalid := input
	invalid.BeforeMode = nil
	if _, err := document.NewWholeComparison(t.Context(), invalid); err == nil {
		t.Fatal("present document lost its exact mode")
	}
	invalid = input
	invalid.Limits.MaximumBytes = 1
	if _, err := document.NewWholeComparison(t.Context(), invalid); err == nil {
		t.Fatal("oversized witness was accepted")
	}
	comparison, err := document.NewSelectedComparison(t.Context(), input, []compose.Participant{comparisonParticipant(t, "model")}, []configdocument.JSONArrayEdit{order})
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	for _, ctx := range []context.Context{nil, canceled} {
		if match, err := comparison.Match(ctx, input.Expected, &mode); err == nil || match.Recoverable() {
			t.Fatal("invalid context returned usable comparison evidence")
		}
	}
	thirdMode := recovery.PermissionMode(0o640)
	match, err := comparison.Match(t.Context(), input.Expected, &thirdMode)
	if err != nil || match.Recoverable() {
		t.Fatalf("third mode was admitted: %+v, %v", match, err)
	}
	invalid = input
	invalid.ExpectedMode = &thirdMode
	if _, err := document.NewSelectedComparison(t.Context(), invalid, nil, nil); err == nil {
		t.Fatal("selected recovery silently changed an existing root mode")
	}
	malformedOrder := aggregate.ExistingDocument([]byte(`{"model":2,"packages":true}`))
	if match, err := comparison.Match(t.Context(), malformedOrder, &mode); err == nil || match.Recoverable() {
		t.Fatal("a later array reader failure returned usable partial comparison")
	}
	oversized := aggregate.ExistingDocument(make([]byte, 8192))
	if match, err := comparison.Match(t.Context(), oversized, &mode); err == nil || match.Recoverable() {
		t.Fatal("oversized fresh content returned usable comparison")
	}
}

func TestDocumentComparisonAppliesCallerDepthToDedicatedOrder(t *testing.T) {
	mode := recovery.PermissionMode(0o600)
	before := `{"packages":["npm:a","npm:b"]}`
	expected := `{"packages":["npm:b","npm:a"]}`
	deepBefore := `{"packages":["npm:a","npm:b"],"outside":{"a":{"b":{"c":true}}}}`
	deepExpected := `{"packages":["npm:b","npm:a"],"outside":{"a":{"b":{"c":true}}}}`
	t.Run("witness", func(t *testing.T) {
		input := comparisonInput(deepBefore, deepExpected, &mode, &mode)
		input.Limits.MaximumDepth = 3
		order := comparisonOrder(t, deepBefore, deepExpected)
		if _, err := document.NewSelectedComparison(t.Context(), input, nil, []configdocument.JSONArrayEdit{order}); err == nil {
			t.Fatal("order-only witnesses bypassed the calling operation's depth limit")
		}
	})
	t.Run("current", func(t *testing.T) {
		input := comparisonInput(before, expected, &mode, &mode)
		input.Limits.MaximumDepth = 3
		order := comparisonOrder(t, before, expected)
		comparison, err := document.NewSelectedComparison(t.Context(), input, nil, []configdocument.JSONArrayEdit{order})
		if err != nil {
			t.Fatal(err)
		}
		if match, err := comparison.Match(t.Context(), aggregate.ExistingDocument([]byte(deepExpected)), &mode); err == nil || match.Recoverable() {
			t.Fatal("fresh order-only content bypassed the calling operation's depth limit")
		}
	})
}

func comparisonInput(before, expected string, BeforeMode, expectedMode *recovery.PermissionMode) document.ComparisonInput {
	return document.ComparisonInput{
		Before: aggregate.ExistingDocument([]byte(before)), Expected: aggregate.ExistingDocument([]byte(expected)),
		BeforeMode: BeforeMode, ExpectedMode: expectedMode,
		Limits: configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32},
	}
}

func comparisonParticipant(t *testing.T, parts ...string) compose.Participant {
	t.Helper()
	codec := comparisonNativeCodec(t, configdocument.JSON)
	key, err := output.NewKeyPath(parts...)
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := output.Parse(".pi/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeProject, destination, footprint, codec.ContractID())
	if err != nil {
		t.Fatal(err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	participant, err := compose.NewParticipant(codec, selection, configdocument.JSON, []output.KeyPath{key})
	if err != nil {
		t.Fatal(err)
	}
	return participant
}

func comparisonNativeCodec(t *testing.T, syntax configdocument.Syntax) aggregate.Codec {
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

func comparisonOrder(t *testing.T, before, expected string) configdocument.JSONArrayEdit {
	t.Helper()
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	beforeDocument, err := configdocument.Parse(t.Context(), []byte(before), configdocument.JSON, limits)
	if err != nil {
		t.Fatal(err)
	}
	expectedDocument, err := configdocument.Parse(t.Context(), []byte(expected), configdocument.JSON, limits)
	if err != nil {
		t.Fatal(err)
	}
	path, err := output.NewKeyPath("packages")
	if err != nil {
		t.Fatal(err)
	}
	order, err := configdocument.NewJSONArrayPermutation(t.Context(), beforeDocument, expectedDocument, path)
	if err != nil {
		t.Fatal(err)
	}
	return order
}
