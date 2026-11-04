package compose_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/target"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

var limits = configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64}

func TestComposedCodexMCPAndNativeValuesFromAbsentAndExistingDocuments(t *testing.T) {
	for _, original := range []aggregate.Document{
		aggregate.AbsentDocument(),
		aggregate.ExistingDocument([]byte("# keep root\nmodel='old'\nunmanaged=2026-10-23\n[mcp_servers.foreign]\n# keep server\ncommand='keep'\nargs=[]\n")),
	} {
		native := nativePort(t)
		model := nativeSet(t, native, target.ScopeProject, ".codex/config.toml", path(t, "model"), `"review"`)
		mcp, server := mcpSet(t, "search")
		modelBefore := observe(t, native, original, selectSets(t, model))
		serverBefore := observe(t, mcp, original, selectSets(t, server))
		modelPlan := desiredPlan(t, modelBefore, map[output.KeyPath]*aggregate.ContributionSet{path(t, "model"): &model})
		serverPlan := desiredPlan(t, serverBefore, map[output.KeyPath]*aggregate.ContributionSet{server.Address().KeyPath(): &server})
		modelEdit := edit(t, native, modelPlan, path(t, "model"))
		serverEdit := edit(t, mcp, serverPlan, path(t, "mcp_servers"))
		var first aggregate.Document
		for _, edits := range [][]compose.ProjectionEdit{{modelEdit, serverEdit}, {serverEdit, modelEdit}} {
			candidate, err := compose.Render(t.Context(), original, limits, edits)
			if err != nil {
				t.Fatal(err)
			}
			if first.Exists() && !candidate.Document().Equal(first) {
				t.Fatal("participant order changed the one native candidate")
			}
			first = candidate.Document()
			if !nativeValue(t, candidate.Document(), path(t, "model")).Equal(value(t, `"review"`)) {
				t.Fatal("native model value was lost when adding the MCP entry")
			}
			serverState, _ := observe(t, mcp, candidate.Document(), selectSets(t, server)).State(server.Contract())
			if !serverState.Present() || serverState.CanonicalProjection() != server.Contributions()[0].Contribution().CanonicalContribution() {
				t.Fatal("MCP semantic entry was lost when adding the native value")
			}
			for _, expected := range candidate.Expected() {
				selection, err := expected.Selection()
				if err != nil {
					t.Fatal(err)
				}
				codec := native
				if selection.CodecContractID() == mcp.ContractID() {
					codec = mcp
				}
				if !observe(t, codec, candidate.Document(), selection).Equal(expected) {
					t.Fatal("group expected state was not re-observed from final bytes")
				}
			}
			if original.Exists() {
				for _, key := range []output.KeyPath{path(t, "unmanaged"), path(t, "mcp_servers", "foreign")} {
					if !nativeValue(t, original, key).Equal(nativeValue(t, candidate.Document(), key)) {
						t.Fatal("composition changed an unmanaged native value")
					}
				}
				assertComments(t, original, candidate.Document())
			}
			baselines := []compose.ProjectionBaseline{
				baseline(t, native, modelBefore, path(t, "model")),
				baseline(t, mcp, serverBefore, path(t, "mcp_servers")),
			}
			restored, err := compose.Restore(t.Context(), candidate.Document(), limits, baselines)
			if err != nil {
				t.Fatal(err)
			}
			if !observe(t, native, restored.Document(), selectSets(t, model)).Equal(modelBefore) ||
				!observe(t, mcp, restored.Document(), selectSets(t, server)).Equal(serverBefore) {
				t.Fatal("group recovery did not restore both actual semantic baselines")
			}
			if original.Exists() {
				assertComments(t, original, restored.Document())
			} else if restored.Document().Exists() {
				t.Fatal("combined recovery retained a completely empty originally absent document")
			}
		}
	}
}

func TestCompositionRetainsNativeTreeFactsAndLogicalAliases(t *testing.T) {
	native := nativePort(t)
	parent := nativeSet(t, native, target.ScopeProject, ".codex/config.toml", path(t, "a"), `"old"`)
	child := nativeSet(t, native, target.ScopeProject, ".codex/config.toml", path(t, "a", "b"), "true")
	other := nativeSet(t, native, target.ScopeGlobal, "@codex-home/config.toml", path(t, "other"), "42")
	original := aggregate.ExistingDocument([]byte("# keep\na='old'\nother=1\n"))
	shapeBefore := observe(t, native, original, selectSets(t, parent, child))
	otherBefore := observe(t, native, original, selectSets(t, other))
	shape := desiredPlan(t, shapeBefore, map[output.KeyPath]*aggregate.ContributionSet{path(t, "a", "b"): &child})
	otherPlan := desiredPlan(t, otherBefore, map[output.KeyPath]*aggregate.ContributionSet{path(t, "other"): &other})
	candidate, err := compose.Render(t.Context(), original, limits, []compose.ProjectionEdit{
		edit(t, native, shape, path(t, "a"), path(t, "a", "b")),
		edit(t, native, otherPlan, path(t, "other")),
	})
	if err != nil {
		t.Fatal(err)
	}
	actual := observe(t, native, candidate.Document(), selectSets(t, parent, child))
	parentState, _ := actual.State(parent.Contract())
	childState, _ := actual.State(child.Contract())
	if !parentState.Present() || !childState.Present() {
		t.Fatal("composed shape transition fabricated parent absence")
	}
	restored, err := compose.Restore(t.Context(), candidate.Document(), limits, []compose.ProjectionBaseline{
		baseline(t, native, shapeBefore, path(t, "a")),
		baseline(t, native, otherBefore, path(t, "other")),
	})
	if err != nil || !observe(t, native, restored.Document(), selectSets(t, parent, child)).Equal(shapeBefore) ||
		!observe(t, native, restored.Document(), selectSets(t, other)).Equal(otherBefore) {
		t.Fatalf("composed native recovery lost correlated blockers or logical identity: %v", err)
	}
	assertComments(t, original, restored.Document())
}

func TestCompositionRefusesCollisionsStaleInputAndPartialCandidates(t *testing.T) {
	native := nativePort(t)
	a := nativeSet(t, native, target.ScopeProject, ".codex/config.toml", path(t, "a"), "1")
	b := nativeSet(t, native, target.ScopeProject, ".codex/config.toml", path(t, "b"), "2")
	original := aggregate.ExistingDocument([]byte("a=0\nb=0\n"))
	aBefore, bBefore := observe(t, native, original, selectSets(t, a)), observe(t, native, original, selectSets(t, b))
	aPlan := desiredPlan(t, aBefore, map[output.KeyPath]*aggregate.ContributionSet{path(t, "a"): &a})
	bPlan := desiredPlan(t, bBefore, map[output.KeyPath]*aggregate.ContributionSet{path(t, "b"): &b})
	aEdit, bEdit := edit(t, native, aPlan, path(t, "a")), edit(t, native, bPlan, path(t, "b"))
	for _, edits := range [][]compose.ProjectionEdit{{aEdit, aEdit}, {{}}} {
		assertRenderRefused(t, t.Context(), original, limits, edits)
	}
	stale := aggregate.ExistingDocument([]byte("a=99\nb=0\n"))
	assertRenderRefused(t, t.Context(), stale, limits, []compose.ProjectionEdit{aEdit, bEdit})
	assertRenderRefused(t, t.Context(), original, configdocument.Limits{MaximumBytes: 1, MaximumDepth: 4}, []compose.ProjectionEdit{aEdit})
	assertRenderRefused(t, t.Context(), original, limits, nil)
	assertRenderRefused(t, nil, original, limits, []compose.ProjectionEdit{aEdit})
	if _, err := compose.NewProjectionEdit(native, aPlan, configdocument.TOML, nil); err == nil {
		t.Fatal("unscoped projection acquired a document rewrite")
	}
	for _, areas := range [][]output.KeyPath{{{}}, {path(t, "unselected")}, {path(t, "a", "child")}} {
		if _, err := compose.NewProjectionEdit(native, aPlan, configdocument.TOML, areas); err == nil {
			t.Fatal("invalid rewrite coverage was admitted")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	wrapper := cancelRender{Codec: native, cancel: cancel}
	interrupted := edit(t, wrapper, aPlan, path(t, "a"))
	candidate, err := compose.Render(ctx, original, limits, []compose.ProjectionEdit{interrupted, bEdit})
	if !errors.Is(err, context.Canceled) || candidate.Document().Exists() || len(candidate.Expected()) != 0 {
		t.Fatal("cancellation after one semantic producer published partial work")
	}
}

type cancelRender struct {
	aggregate.Codec
	cancel context.CancelFunc
}

func (codec cancelRender) Render(document aggregate.Document, plan aggregate.Plan) (aggregate.RenderedDocument, *aggregate.CodecFailure) {
	result, failure := codec.Codec.Render(document, plan)
	codec.cancel()
	return result, failure
}

func assertRenderRefused(t testing.TB, ctx context.Context, document aggregate.Document, bounds configdocument.Limits, edits []compose.ProjectionEdit) {
	t.Helper()
	candidate, err := compose.Render(ctx, document, bounds, edits)
	if err == nil || candidate.Document().Exists() || len(candidate.Expected()) != 0 {
		t.Fatal("invalid composition produced a partial candidate")
	}
}

func nativePort(t testing.TB) aggregate.Codec {
	t.Helper()
	codec, err := nativecodec.New(t.Context(), "native-toml-v1", limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	return codec
}

func nativeSet(t testing.TB, codec aggregate.Codec, scope target.Scope, root string, key output.KeyPath, text string) aggregate.ContributionSet {
	t.Helper()
	destination, err := output.Parse(root)
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := aggregate.NewHostConfigProjectionContract(target.TargetCodex, scope, destination, footprint, codec.ContractID())
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := value(t, text).CanonicalContext(t.Context(), limits)
	if err != nil {
		t.Fatal(err)
	}
	body, err := aggregate.NewManagedContribution(contract, canonical)
	if err != nil {
		t.Fatal(err)
	}
	id, err := entity.New(entity.KindHostConfig, "composed")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.HostConfigKeySubject(id, target.TargetCodex, scope, key)
	if err != nil {
		t.Fatal(err)
	}
	item, err := aggregate.NewSubjectContribution(subject, body)
	if err != nil {
		t.Fatal(err)
	}
	set, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{item})
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func mcpSet(t testing.TB, name string) (aggregate.Codec, aggregate.ContributionSet) {
	t.Helper()
	placement, ok := aggregate.MCPPlacementForID(aggregate.MCPPlacementCodexProject)
	if !ok {
		t.Fatal("Codex MCP placement unavailable")
	}
	codec, ok := mcpcodec.For(placement.CodecContractID())
	if !ok {
		t.Fatal("Codex MCP codec unavailable")
	}
	canonical, err := mcpcodec.CanonicalCodexProjectMCPServerEntry(mcpcodec.MCPNoEnvServerProjection{
		ServerID: name, Command: "npx", Args: []string{"--mode", "review"}, AdapterContract: aggregate.CodexProjectMCPStdioCommandV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := placement.Contribution(name, string(canonical))
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologymcp.ProjectionSubject(target.TargetCodex, target.ScopeProject, name)
	if err != nil {
		t.Fatal(err)
	}
	item, err := aggregate.NewSubjectContribution(subject, body)
	if err != nil {
		t.Fatal(err)
	}
	set, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{item})
	if err != nil {
		t.Fatal(err)
	}
	return codec, set
}

func path(t testing.TB, parts ...string) output.KeyPath {
	t.Helper()
	key, err := output.NewKeyPath(parts...)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func selectSets(t testing.TB, sets ...aggregate.ContributionSet) aggregate.Selection {
	t.Helper()
	contracts := make([]aggregate.ProjectionContract, len(sets))
	for index, set := range sets {
		contracts[index] = set.Contract()
	}
	selection, err := aggregate.NewSelection(contracts)
	if err != nil {
		t.Fatal(err)
	}
	return selection
}

func observe(t testing.TB, codec aggregate.Codec, document aggregate.Document, selection aggregate.Selection) aggregate.Snapshot {
	t.Helper()
	snapshot, failure := codec.Read(document, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	return snapshot
}

func desiredPlan(t testing.TB, before aggregate.Snapshot, desired map[output.KeyPath]*aggregate.ContributionSet) aggregate.Plan {
	t.Helper()
	var intents []aggregate.ProjectionIntent
	for _, state := range before.States() {
		intent, err := aggregate.NewProjectionIntent(state, desired[state.Contract().Address().KeyPath()])
		if err != nil {
			t.Fatal(err)
		}
		intents = append(intents, intent)
	}
	plan, err := aggregate.NewPlan(before, intents)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func edit(t testing.TB, codec aggregate.Codec, plan aggregate.Plan, areas ...output.KeyPath) compose.ProjectionEdit {
	t.Helper()
	edit, err := compose.NewProjectionEdit(codec, plan, configdocument.TOML, areas)
	if err != nil {
		t.Fatal(err)
	}
	return edit
}

func baseline(t testing.TB, codec aggregate.Codec, snapshot aggregate.Snapshot, areas ...output.KeyPath) compose.ProjectionBaseline {
	t.Helper()
	baseline, err := compose.NewProjectionBaseline(codec, snapshot, configdocument.TOML, areas)
	if err != nil {
		t.Fatal(err)
	}
	return baseline
}

func value(t testing.TB, text string) configdocument.Value {
	t.Helper()
	return nativeValue(t, aggregate.ExistingDocument([]byte("value="+text)), path(t, "value"))
}

func nativeParse(t testing.TB, document aggregate.Document) configdocument.Document {
	t.Helper()
	parsed, err := configdocument.Parse(t.Context(), document.Content(), configdocument.TOML, limits)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func nativeValue(t testing.TB, document aggregate.Document, key output.KeyPath) configdocument.Value {
	t.Helper()
	value, present, err := nativeParse(t, document).Lookup(key)
	if err != nil || !present {
		t.Fatalf("native value missing: %v", err)
	}
	return value
}

func assertComments(t testing.TB, before, after aggregate.Document) {
	t.Helper()
	left, right := nativeParse(t, before).Comments(), nativeParse(t, after).Comments()
	slices.Sort(left)
	slices.Sort(right)
	if !slices.Equal(left, right) {
		t.Fatal("composed document lost or duplicated actual comments")
	}
}
