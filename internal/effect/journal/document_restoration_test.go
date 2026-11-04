package journal_test

import (
	"context"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	piconfig "github.com/isty2e/daem/internal/realization/configrelation/pi"
	"github.com/isty2e/daem/internal/target"
)

func TestDocumentRestorationRollsBackBothMixedNativePiDirections(t *testing.T) {
	mode := recovery.PermissionMode(0o600)
	before := `{"model":"balanced","outside":"old","packages":["npm:a","npm:b"]}`
	expected := `{"model":"fast","outside":"old","packages":["npm:b","npm:a"]}`
	order := comparisonOrder(t, before, expected)
	comparison, err := journal.NewSelectedDocumentComparison(t.Context(), comparisonInput(before, expected, &mode, &mode), []compose.Participant{comparisonParticipant(t, "model")}, &order)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		current string
		changed bool
	}{
		{"native expected order before", `{"model":"fast","outside":"new","packages":["npm:a","npm:b"]}`, true},
		{"native before order expected", `{"model":"balanced","outside":"new","packages":["npm:b","npm:a"]}`, true},
		{"both expected", `{"model":"fast","outside":"new","packages":["npm:b","npm:a"]}`, true},
		{"already before", `{"model":"balanced","outside":"new","packages":["npm:a","npm:b"]}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			current := aggregate.ExistingDocument([]byte(test.current))
			restored, err := comparison.RestoreBefore(t.Context(), current, &mode)
			if err != nil || restored.Validate() != nil || restored.Changed() != test.changed {
				t.Fatalf("mixed restoration failed: %+v, %v", restored, err)
			}
			assertRestorationValue(t, restored.Document(), "model", `"balanced"`)
			assertRestorationValue(t, restored.Document(), "outside", `"new"`)
			parsed, err := piconfig.Parse(restored.Document().Content())
			if err != nil || !order.MatchesBefore(parsed) {
				t.Fatalf("restoration did not restore the dedicated array: %v", err)
			}
			if actual, present := restored.RegularFileMode(); !present || actual != mode {
				t.Fatal("selected restoration changed existing root metadata")
			}
			if !test.changed && !restored.Document().Equal(current) {
				t.Fatal("already-before recovery rewrote current bytes")
			}
			again, err := comparison.RestoreBefore(t.Context(), restored.Document(), &mode)
			if err != nil || again.Validate() != nil || again.Changed() || !again.Document().Equal(restored.Document()) {
				t.Fatalf("mixed recovery was not idempotent: %v", err)
			}
		})
	}
}

func TestDocumentRestorationPreservesForeignMCPParentAndRetries(t *testing.T) {
	mode := recovery.PermissionMode(0o600)
	before := "model='balanced'\n"
	expected := "model='fast'\n[mcp_servers.search]\ncommand='node'\n"
	comparison, err := journal.NewSelectedDocumentComparison(t.Context(), comparisonInput(before, expected, &mode, &mode), restorationMCPParticipants(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	current := aggregate.ExistingDocument([]byte("model='fast'\noutside=true\n[mcp_servers.search]\ncommand='node'\n[mcp_servers.foreign]\ncommand='keep'\n"))
	restored, err := comparison.RestoreBefore(t.Context(), current, &mode)
	if err != nil || restored.Validate() != nil || !restored.Changed() {
		t.Fatalf("MCP/native restoration failed: %v", err)
	}
	parsed, err := configdocument.Parse(t.Context(), restored.Document().Content(), configdocument.TOML, configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path []string
		want string
	}{
		{[]string{"model"}, `"balanced"`},
		{[]string{"outside"}, "true"},
		{[]string{"mcp_servers", "foreign", "command"}, `"keep"`},
	} {
		key, err := output.NewKeyPath(test.path...)
		if err != nil {
			t.Fatal(err)
		}
		value, present, err := parsed.Lookup(key)
		if err != nil || !present || !value.Equal(expectedRestorationValue(t, test.want)) {
			t.Fatalf("restored value %v differs: %v", test.path, err)
		}
	}
	search, err := output.NewKeyPath("mcp_servers", "search")
	if err != nil {
		t.Fatal(err)
	}
	if _, present, err := parsed.Lookup(search); err != nil || present {
		t.Fatalf("created MCP entry was not removed: %v", err)
	}
	match, err := comparison.Match(t.Context(), restored.Document(), &mode)
	if err != nil || match.Recoverable() {
		t.Fatal("comparison weakened original MCP parent facts to manufacture a before phase")
	}
	again, err := comparison.RestoreBefore(t.Context(), restored.Document(), &mode)
	if err != nil || again.Validate() != nil || again.Changed() || !again.Document().Equal(restored.Document()) {
		t.Fatalf("foreign-parent restoration retry was not an exact no-op: %v", err)
	}
}

func TestDocumentRestorationKeepsWholeAndSelectedExistenceSeparate(t *testing.T) {
	beforeMode, expectedMode := recovery.PermissionMode(0o644), recovery.PermissionMode(0o600)
	input := comparisonInput("opaque original", "opaque expected", &beforeMode, &expectedMode)
	whole, err := journal.NewWholeDocumentComparison(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := whole.RestoreBefore(t.Context(), input.Expected, &expectedMode)
	if err != nil || !restored.Document().Equal(input.Before) || !restored.Changed() {
		t.Fatalf("whole restoration did not recover original bytes: %v", err)
	}
	if actual, present := restored.RegularFileMode(); !present || actual != beforeMode {
		t.Fatal("whole restoration did not recover original exact mode")
	}
	input.Before, input.BeforeMode = aggregate.AbsentDocument(), nil
	whole, err = journal.NewWholeDocumentComparison(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	restored, err = whole.RestoreBefore(t.Context(), input.Expected, &expectedMode)
	if err != nil || restored.Document().Exists() || !restored.Changed() {
		t.Fatalf("whole creation rollback did not restore absence: %v", err)
	}
	if _, present := restored.RegularFileMode(); present {
		t.Fatal("absent restoration carried a file mode")
	}
	input = comparisonInput("", `{"model":"fast"}`, nil, &expectedMode)
	input.Before = aggregate.AbsentDocument()
	selected, err := journal.NewSelectedDocumentComparison(t.Context(), input, []compose.Participant{comparisonParticipant(t, "model")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	current := aggregate.ExistingDocument([]byte(`{"model":"fast","outside":true}`))
	restored, err = selected.RestoreBefore(t.Context(), current, &expectedMode)
	if err != nil || !restored.Document().Exists() {
		t.Fatalf("selected rollback removed the foreign root: %v", err)
	}
	assertRestorationValue(t, restored.Document(), "outside", "true")
	parsed, err := configdocument.Parse(t.Context(), restored.Document().Content(), configdocument.JSON, input.Limits)
	if err != nil {
		t.Fatal(err)
	}
	key, err := output.NewKeyPath("model")
	if err != nil {
		t.Fatal(err)
	}
	if _, present, err := parsed.Lookup(key); err != nil || present {
		t.Fatalf("selected creation was not removed: %v", err)
	}
}

func TestDocumentRestorationRefusesThirdStateWithoutUsableCandidate(t *testing.T) {
	mode := recovery.PermissionMode(0o600)
	before, expected := `{"model":1,"packages":["npm:a","npm:b"]}`, `{"model":2,"packages":["npm:b","npm:a"]}`
	order := comparisonOrder(t, before, expected)
	comparison, err := journal.NewSelectedDocumentComparison(t.Context(), comparisonInput(before, expected, &mode, &mode), []compose.Participant{comparisonParticipant(t, "model")}, &order)
	if err != nil {
		t.Fatal(err)
	}
	for _, current := range []string{
		`{"model":3,"packages":["npm:b","npm:a"]}`,
		`{"model":2,"packages":["npm:c","npm:a"]}`,
		`{"model":2,"packages":[true]}`,
	} {
		candidate, err := comparison.RestoreBefore(t.Context(), aggregate.ExistingDocument([]byte(current)), &mode)
		if err == nil || candidate.Validate() == nil {
			t.Fatal("third/later-invalid state returned a usable restoration")
		}
	}
	thirdMode := recovery.PermissionMode(0o640)
	if candidate, err := comparison.RestoreBefore(t.Context(), aggregate.ExistingDocument([]byte(expected)), &thirdMode); err == nil || candidate.Validate() == nil {
		t.Fatal("third mode returned a usable restoration")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	for _, ctx := range []context.Context{nil, canceled} {
		if candidate, err := comparison.RestoreBefore(ctx, aggregate.ExistingDocument([]byte(expected)), &mode); err == nil || candidate.Validate() == nil {
			t.Fatal("invalid context returned a usable restoration")
		}
	}
}

func TestDocumentRestorationEmptySelectionDoesNotRestoreUnmanagedBytes(t *testing.T) {
	mode := recovery.PermissionMode(0)
	input := comparisonInput(`{"released":"old"}`, `{"released":"old"}`, &mode, &mode)
	comparison, err := journal.NewSelectedDocumentComparison(t.Context(), input, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	current := aggregate.ExistingDocument([]byte("all current content is unselected"))
	restored, err := comparison.RestoreBefore(t.Context(), current, &mode)
	if err != nil || restored.Validate() != nil || restored.Changed() || !restored.Document().Equal(current) {
		t.Fatalf("empty release rewrote unmanaged contents: %v", err)
	}
	if actual, present := restored.RegularFileMode(); !present || actual != mode {
		t.Fatal("empty release lost exact0000 mode")
	}
}

func restorationMCPParticipants(t *testing.T) []compose.Participant {
	t.Helper()
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
	contract, err := aggregate.NewHostConfigProjectionContract(target.TargetCodex, target.ScopeProject, destination, footprint, native.ContractID())
	if err != nil {
		t.Fatal(err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	participant, err := compose.NewParticipant(native, selection, configdocument.TOML, []output.KeyPath{key})
	if err != nil {
		t.Fatal(err)
	}
	placement, found := aggregate.MCPPlacementForID(aggregate.MCPPlacementCodexProject)
	if !found {
		t.Fatal("MCP fixture placement unavailable")
	}
	mcp, found := mcpcodec.For(placement.CodecContractID())
	if !found {
		t.Fatal("MCP fixture codec unavailable")
	}
	mcpContract, err := placement.ProjectionContract("search")
	if err != nil {
		t.Fatal(err)
	}
	mcpSelection, err := aggregate.NewSelection([]aggregate.ProjectionContract{mcpContract})
	if err != nil {
		t.Fatal(err)
	}
	mcpParticipant, err := compose.NewParticipant(mcp, mcpSelection, configdocument.TOML, []output.KeyPath{mcpContract.Address().KeyPath()})
	if err != nil {
		t.Fatal(err)
	}
	return []compose.Participant{participant, mcpParticipant}
}

func assertRestorationValue(t *testing.T, document aggregate.Document, property, want string) {
	t.Helper()
	parsed, err := configdocument.Parse(t.Context(), document.Content(), configdocument.JSON, configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32})
	if err != nil {
		t.Fatal(err)
	}
	key, err := output.NewKeyPath(property)
	if err != nil {
		t.Fatal(err)
	}
	value, present, err := parsed.Lookup(key)
	if err != nil || !present || !value.Equal(expectedRestorationValue(t, want)) {
		t.Fatalf("restored property %q differs: %v", property, err)
	}
}

func expectedRestorationValue(t *testing.T, rawJSON string) configdocument.Value {
	t.Helper()
	document, err := configdocument.Parse(t.Context(), []byte(`{"expected":`+rawJSON+`}`), configdocument.JSON, configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32})
	if err != nil {
		t.Fatal(err)
	}
	key, err := output.NewKeyPath("expected")
	if err != nil {
		t.Fatal(err)
	}
	value, present, err := document.Lookup(key)
	if err != nil || !present {
		t.Fatalf("expected fixture value unavailable: %v", err)
	}
	return value
}
