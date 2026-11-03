package compose_test

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/target"
)

func TestCompositionReobservesEveryParticipantsDocumentAdmission(t *testing.T) {
	bounded, err := nativecodec.New(t.Context(), "native-toml-v1", configdocument.Limits{MaximumBytes: 200, MaximumDepth: 16}, limits)
	if err != nil {
		t.Fatal(err)
	}
	content := `"` + strings.Repeat("x", 120) + `"`
	a := nativeSet(t, bounded, target.ScopeProject, ".codex/config.toml", path(t, "a"), content)
	b := nativeSet(t, bounded, target.ScopeProject, ".codex/config.toml", path(t, "b"), content)
	original := aggregate.ExistingDocument([]byte("a='old'\nb='old'\n"))
	var edits []compose.ProjectionEdit
	for _, set := range []aggregate.ContributionSet{a, b} {
		before := observe(t, bounded, original, selectSets(t, set))
		plan := desiredPlan(t, before, map[output.KeyPath]*aggregate.ContributionSet{set.Address().KeyPath(): &set})
		private, failure := bounded.Render(original, plan)
		if failure != nil || len(private.Document().Content()) > 200 {
			t.Fatalf("individual candidate fixture exceeds participant policy: %v", failure)
		}
		edits = append(edits, edit(t, bounded, plan, set.Address().KeyPath()))
	}
	candidate, err := compose.Render(t.Context(), original, limits, edits)
	if err == nil || candidate.Document().Exists() || len(candidate.Expected()) != 0 {
		t.Fatal("combined output bypassed a participant's independent document admission")
	}
	assertRenderRefused(t, t.Context(), original, configdocument.Limits{MaximumBytes: 200, MaximumDepth: 16}, edits)
}

func TestCompositionKeepsFreshOutsideValuesAndRejectsMixedGrammar(t *testing.T) {
	native := nativePort(t)
	set := nativeSet(t, native, target.ScopeProject, ".codex/config.toml", path(t, "a"), "1")
	original := aggregate.ExistingDocument([]byte("a=0\noutside=1\n"))
	before := observe(t, native, original, selectSets(t, set))
	plan := desiredPlan(t, before, map[output.KeyPath]*aggregate.ContributionSet{path(t, "a"): &set})
	projection := edit(t, native, plan, path(t, "a"))
	fresh := aggregate.ExistingDocument([]byte("# fresh outside\na=0\noutside=2\n"))
	candidate, err := compose.Render(t.Context(), fresh, limits, []compose.ProjectionEdit{projection})
	if err != nil || !nativeValue(t, candidate.Document(), path(t, "outside")).Equal(value(t, "2")) {
		t.Fatalf("unrelated fresh native data invalidated or was lost from composition: %v", err)
	}
	assertComments(t, fresh, candidate.Document())
	wrong, err := compose.NewProjectionEdit(native, plan, configdocument.JSON, []output.KeyPath{path(t, "a")})
	if err != nil {
		t.Fatal(err)
	}
	assertRenderRefused(t, t.Context(), original, limits, []compose.ProjectionEdit{projection, wrong})
}

func TestComposedInverseTreeTransitionAndDedicatedEmptyParent(t *testing.T) {
	native := nativePort(t)
	parent := nativeSet(t, native, target.ScopeProject, ".codex/config.toml", path(t, "a"), `"new"`)
	child := nativeSet(t, native, target.ScopeProject, ".codex/config.toml", path(t, "a", "b"), "1")
	mcp, server := mcpSet(t, "search")
	seed := aggregate.ExistingDocument([]byte("a={b=1,foreign=true}\n"))
	serverBefore := observe(t, mcp, seed, selectSets(t, server))
	serverPlan := desiredPlan(t, serverBefore, map[output.KeyPath]*aggregate.ContributionSet{server.Address().KeyPath(): &server})
	withServer, failure := mcp.Render(seed, serverPlan)
	if failure != nil {
		t.Fatal(failure)
	}
	original := withServer.Document()
	shapeBefore := observe(t, native, original, selectSets(t, parent, child))
	mcpBefore := observe(t, mcp, original, selectSets(t, server))
	shape := desiredPlan(t, shapeBefore, map[output.KeyPath]*aggregate.ContributionSet{path(t, "a"): &parent})
	removeServer := desiredPlan(t, mcpBefore, nil)
	candidate, err := compose.Render(t.Context(), original, limits, []compose.ProjectionEdit{
		edit(t, native, shape, path(t, "a")),
		edit(t, mcp, removeServer, path(t, "mcp_servers")),
	})
	if err != nil {
		t.Fatal(err)
	}
	actual := observe(t, native, candidate.Document(), selectSets(t, parent, child))
	childState, _ := actual.State(child.Contract())
	if blocker, present := childState.BlockedBy(); !present || blocker != parent.Address().KeyPath() {
		t.Fatal("composed inverse transition lost the child's actual obstruction")
	}
	serverState, _ := observe(t, mcp, candidate.Document(), selectSets(t, server)).State(server.Contract())
	if serverState.Present() || !serverState.ParentPresent() {
		t.Fatal("composition dropped the dedicated codec's retained empty-parent policy")
	}
	restored, err := compose.Restore(t.Context(), candidate.Document(), limits, []compose.ProjectionBaseline{
		baseline(t, native, shapeBefore, path(t, "a")),
		baseline(t, mcp, mcpBefore, path(t, "mcp_servers")),
	})
	if err != nil || !observe(t, native, restored.Document(), selectSets(t, parent, child)).Equal(shapeBefore) ||
		!observe(t, mcp, restored.Document(), selectSets(t, server)).Equal(mcpBefore) {
		t.Fatalf("composed inverse recovery failed to restore actual broad values: %v", err)
	}
}

func TestCompositionRewriteCoverageHandlesInterposedLiteralNames(t *testing.T) {
	native := nativePort(t)
	child := nativeSet(t, native, target.ScopeProject, ".codex/config.toml", path(t, "a", "b"), "2")
	lookalike := nativeSet(t, native, target.ScopeProject, ".codex/config.toml", path(t, "a!"), "3")
	original := aggregate.ExistingDocument([]byte("a={b=0,foreign=true}\n\"a!\"=0\n"))
	before := observe(t, native, original, selectSets(t, child, lookalike))
	plan := desiredPlan(t, before, map[output.KeyPath]*aggregate.ContributionSet{path(t, "a", "b"): &child, path(t, "a!"): &lookalike})
	projection := edit(t, native, plan, path(t, "a"), path(t, "a!"))
	candidate, err := compose.Render(t.Context(), original, limits, []compose.ProjectionEdit{projection})
	if err != nil || !nativeValue(t, candidate.Document(), path(t, "a", "foreign")).Equal(value(t, "true")) {
		t.Fatalf("literal prefix-range coverage lost a native scope or foreign value: %v", err)
	}
}

func TestCompositionOfAlreadyDesiredNativeValuesIsANoOp(t *testing.T) {
	native := nativePort(t)
	set := nativeSet(t, native, target.ScopeProject, ".codex/config.toml", path(t, "a"), "2")
	original := aggregate.ExistingDocument([]byte("# unchanged\na = 2\n"))
	before := observe(t, native, original, selectSets(t, set))
	plan := desiredPlan(t, before, map[output.KeyPath]*aggregate.ContributionSet{path(t, "a"): &set})
	candidate, err := compose.Render(t.Context(), original, limits, []compose.ProjectionEdit{edit(t, native, plan, path(t, "a"))})
	if err != nil || !candidate.Document().Equal(original) || len(candidate.Expected()) != 1 || !candidate.Expected()[0].Equal(before) {
		t.Fatalf("a converged semantic plan changed the native document: %v", err)
	}
}
