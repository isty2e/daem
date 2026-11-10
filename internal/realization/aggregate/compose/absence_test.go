package compose_test

import (
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	hookcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	commandhook "github.com/isty2e/daem/internal/realization/aggregate/hook"
	"github.com/isty2e/daem/internal/target"
)

func TestComposedHookRemovalKeepsSurvivorsAndAdmittedDocumentAbsence(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		before     string
		addTheme   bool
		wantExists bool
	}{
		{"native survivor", `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"true"}]}]}}`, true, true},
		{"hooks-only removal", `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"true"}]}]}}`, false, false},
		{"no private absence request", `{"theme":"old","hooks":{"Stop":[{"hooks":[{"type":"command","command":"true"}]}]}}`, false, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			native, err := nativecodec.New(t.Context(), "native-json-v1", limits, limits)
			if err != nil {
				t.Fatal(err)
			}
			hooks, hookSelection := hookProjection(t)
			theme := nativeSet(t, native, target.ScopeProject, ".codex/hooks.json", path(t, "theme"), `"dark"`)
			document := aggregate.ExistingDocument([]byte(scenario.before))
			nativeBefore := observe(t, native, document, selectSets(t, theme))
			hookBefore := observe(t, hooks, document, hookSelection)
			var desired *aggregate.ContributionSet
			if scenario.addTheme {
				desired = &theme
			}
			nativePlan := desiredPlan(t, nativeBefore, map[output.KeyPath]*aggregate.ContributionSet{path(t, "theme"): desired})
			hookPlan := desiredPlan(t, hookBefore, nil)
			nativeEdit, err := compose.NewProjectionEdit(native, nativePlan, configdocument.JSON, []output.KeyPath{path(t, "theme")})
			if err != nil {
				t.Fatal(err)
			}
			hookEdit, err := compose.NewProjectionEdit(hooks, hookPlan, configdocument.JSON, []output.KeyPath{path(t, "hooks")})
			if err != nil {
				t.Fatal(err)
			}

			var first aggregate.Document
			for index, edits := range [][]compose.ProjectionEdit{{nativeEdit, hookEdit}, {hookEdit, nativeEdit}} {
				candidate, err := compose.Render(t.Context(), document, limits, edits)
				if err != nil {
					t.Fatal(err)
				}
				actual := candidate.Document()
				if actual.Exists() != scenario.wantExists {
					t.Fatalf("document presence = %t, want %t", actual.Exists(), scenario.wantExists)
				}
				if index != 0 && !first.Equal(actual) {
					t.Fatal("participant order changed the composed candidate")
				}
				first = actual
				if actual.Exists() {
					parsed, err := configdocument.Parse(t.Context(), actual.Content(), configdocument.JSON, limits)
					if err != nil {
						t.Fatal(err)
					}
					_, hooksPresent, err := parsed.Lookup(path(t, "hooks"))
					if err != nil || hooksPresent {
						t.Fatal("removed hooks survived composition", err)
					}
					gotTheme, themePresent, err := parsed.Lookup(path(t, "theme"))
					if err != nil || themePresent != scenario.addTheme || scenario.addTheme && !gotTheme.Equal(value(t, `"dark"`)) {
						t.Fatal("native survivor was changed", err)
					}
					if !scenario.addTheme && parsed.HasValues() {
						t.Fatal("joint removal invented another value")
					}
				}
				for _, expected := range candidate.Expected() {
					selection, err := expected.Selection()
					if err != nil {
						t.Fatal(err)
					}
					codec := native
					if selection.CodecContractID() == hooks.ContractID() {
						codec = hooks
					}
					if !observe(t, codec, actual, selection).Equal(expected) {
						t.Fatal("composed expectation was not read from final bytes")
					}
				}
			}
		})
	}
}

func TestComposedRestoreKeepsSurvivorsAcrossBothAbsenceDirections(t *testing.T) {
	hooksOnly := aggregate.ExistingDocument([]byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"true"}]}]}}`))
	for _, scenario := range []struct {
		name                  string
		before, current, want aggregate.Document
	}{
		{
			name:    "originally absent",
			current: aggregate.ExistingDocument([]byte(`{"theme":"dark","hooks":{"Stop":[{"hooks":[{"type":"command","command":"true"}]}]}}`)),
		},
		{
			name:    "foreign root survives",
			current: aggregate.ExistingDocument([]byte(`{"theme":"dark","hooks":{"Stop":[{"hooks":[{"type":"command","command":"true"}]}]},"foreign":"keep"}`)),
			want:    aggregate.ExistingDocument([]byte(`{"foreign":"keep"}`)),
		},
		{
			name:    "existing empty baseline survives",
			before:  aggregate.ExistingDocument([]byte(`{}`)),
			current: aggregate.ExistingDocument([]byte(`{"theme":"dark"}`)),
			want:    aggregate.ExistingDocument([]byte(`{}`)),
		},
		{name: "restore removed hooks-only file", before: hooksOnly, want: hooksOnly},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			native, err := nativecodec.New(t.Context(), "native-json-v1", limits, limits)
			if err != nil {
				t.Fatal(err)
			}
			hooks, hookSelection := hookProjection(t)
			theme := nativeSet(t, native, target.ScopeProject, ".codex/hooks.json", path(t, "theme"), `"dark"`)
			nativeBefore := observe(t, native, scenario.before, selectSets(t, theme))
			hookBefore := observe(t, hooks, scenario.before, hookSelection)
			nativeBaseline, err := compose.NewProjectionBaseline(native, nativeBefore, configdocument.JSON, []output.KeyPath{path(t, "theme")})
			if err != nil {
				t.Fatal(err)
			}
			hookBaseline, err := compose.NewProjectionBaseline(hooks, hookBefore, configdocument.JSON, []output.KeyPath{path(t, "hooks")})
			if err != nil {
				t.Fatal(err)
			}

			var first aggregate.Document
			for index, baselines := range [][]compose.ProjectionBaseline{{nativeBaseline, hookBaseline}, {hookBaseline, nativeBaseline}} {
				candidate, err := compose.Restore(t.Context(), scenario.current, limits, baselines)
				if err != nil {
					t.Fatal(err)
				}
				actual := candidate.Document()
				if actual.Exists() != scenario.want.Exists() {
					t.Fatalf("restored document presence = %t, want %t", actual.Exists(), scenario.want.Exists())
				}
				if index != 0 && !first.Equal(actual) {
					t.Fatal("baseline order changed the restored document")
				}
				first = actual
				if actual.Exists() {
					parsed, err := configdocument.Parse(t.Context(), actual.Content(), configdocument.JSON, limits)
					if err != nil {
						t.Fatal(err)
					}
					wanted, err := configdocument.Parse(t.Context(), scenario.want.Content(), configdocument.JSON, limits)
					if err != nil {
						t.Fatal(err)
					}
					root, _, err := parsed.Lookup(output.KeyPath{})
					if err != nil {
						t.Fatal(err)
					}
					wantRoot, _, err := wanted.Lookup(output.KeyPath{})
					if err != nil || !root.Equal(wantRoot) {
						t.Fatal("composed recovery changed restored or foreign values", err)
					}
				}
				for _, expected := range candidate.Expected() {
					selection, err := expected.Selection()
					if err != nil {
						t.Fatal(err)
					}
					codec := native
					if selection.CodecContractID() == hooks.ContractID() {
						codec = hooks
					}
					if !observe(t, codec, actual, selection).Equal(expected) {
						t.Fatal("recovery expectation was not read from final bytes")
					}
				}
			}
		})
	}
}

func TestComposedMCPInsertKeepsAnAbsentNativeSelection(t *testing.T) {
	native := nativePort(t)
	model := nativeSet(t, native, target.ScopeProject, ".codex/config.toml", path(t, "model"), `"review"`)
	mcp, server := mcpSet(t, "search")
	original := aggregate.AbsentDocument()
	nativeBefore := observe(t, native, original, selectSets(t, model))
	mcpBefore := observe(t, mcp, original, selectSets(t, server))
	nativeEdit := edit(t, native, desiredPlan(t, nativeBefore, nil), path(t, "model"))
	mcpEdit := edit(t, mcp, desiredPlan(t, mcpBefore, map[output.KeyPath]*aggregate.ContributionSet{server.Address().KeyPath(): &server}), path(t, "mcp_servers"))

	var first aggregate.Document
	for index, edits := range [][]compose.ProjectionEdit{{nativeEdit, mcpEdit}, {mcpEdit, nativeEdit}} {
		candidate, err := compose.Render(t.Context(), original, limits, edits)
		if err != nil {
			t.Fatal(err)
		}
		actual := candidate.Document()
		if !actual.Exists() || index != 0 && !first.Equal(actual) {
			t.Fatal("MCP survivor was absent or depended on participant order")
		}
		first = actual
		nativeState, found := observe(t, native, actual, selectSets(t, model)).State(model.Contract())
		if !found || !nativeState.Absent() {
			t.Fatal("MCP insertion changed the absent native selection")
		}
		serverState, found := observe(t, mcp, actual, selectSets(t, server)).State(server.Contract())
		if !found || !serverState.Present() || serverState.CanonicalProjection() != server.Contributions()[0].Contribution().CanonicalContribution() {
			t.Fatal("MCP insertion did not preserve its actual selected entry")
		}
		restored, err := compose.Restore(t.Context(), actual, limits, []compose.ProjectionBaseline{
			baseline(t, native, nativeBefore, path(t, "model")),
			baseline(t, mcp, mcpBefore, path(t, "mcp_servers")),
		})
		if err != nil || restored.Document().Exists() {
			t.Fatal("recovery did not return both absent baselines", err)
		}
	}
}

func TestManagedProjectionEqualityKeepsOwnedContainerPresence(t *testing.T) {
	native := nativePort(t)
	model := nativeSet(t, native, target.ScopeProject, ".codex/config.toml", path(t, "model"), `"review"`)
	nativeMissing := observe(t, native, aggregate.AbsentDocument(), selectSets(t, model))
	nativeEmpty := observe(t, native, aggregate.ExistingDocument([]byte("\n")), selectSets(t, model))
	hooks, hookSelection := hookProjection(t)
	hookMissing := observe(t, hooks, aggregate.AbsentDocument(), hookSelection)
	hookEmpty := observe(t, hooks, aggregate.ExistingDocument([]byte("{}")), hookSelection)
	mcp, server := mcpSet(t, "search")
	mcpMissing := observe(t, mcp, aggregate.AbsentDocument(), selectSets(t, server))
	mcpNoParent := observe(t, mcp, aggregate.ExistingDocument([]byte("model='unmanaged'\n")), selectSets(t, server))
	mcpParent := observe(t, mcp, aggregate.ExistingDocument([]byte("[mcp_servers]\n")), selectSets(t, server))

	for _, pair := range [][2]aggregate.Snapshot{
		{nativeMissing, nativeEmpty}, {hookMissing, hookEmpty}, {mcpMissing, mcpNoParent},
	} {
		if pair[0].Equal(pair[1]) {
			t.Fatal("full evidence equality ignored outer document presence")
		}
		if !pair[0].SameManagedProjection(pair[1]) || !pair[1].SameManagedProjection(pair[0]) {
			t.Fatal("unowned outer presence changed the managed projection")
		}
	}
	if mcpNoParent.SameManagedProjection(mcpParent) || mcpParent.SameManagedProjection(mcpNoParent) {
		t.Fatal("MCP managed container presence was discarded")
	}
	if nativeEmpty.SameManagedProjection(hookEmpty) || (aggregate.Snapshot{}).SameManagedProjection(aggregate.Snapshot{}) {
		t.Fatal("different or absent selections were equated")
	}
	changed := observe(t, native, aggregate.ExistingDocument([]byte("model='different'\n")), selectSets(t, model))
	if nativeEmpty.SameManagedProjection(changed) {
		t.Fatal("selected native value changed without changing its managed projection")
	}
}

func hookProjection(t testing.TB) (aggregate.Codec, aggregate.Selection) {
	t.Helper()
	placement, ok := aggregate.HookPlacementFor(target.TargetCodex, target.ScopeProject)
	if !ok {
		t.Fatal("Hook placement unavailable")
	}
	canonical, err := hookcodec.CanonicalHookContribution(commandhook.ContributionInput{Event: "Stop", Type: "command", Command: "true"})
	if err != nil {
		t.Fatal(err)
	}
	contribution, err := placement.Contribution(canonical)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contribution.Contract()})
	if err != nil {
		t.Fatal(err)
	}
	codec, ok := aggregatecodec.Catalog().Lookup(placement.CodecContractID())
	if !ok {
		t.Fatal("Hook codec unavailable")
	}
	return codec, selection
}
