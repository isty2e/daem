package compose_test

import (
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/target"
)

func TestNativeProjectionCompositionAcrossAllAdmittedGrammars(t *testing.T) {
	for _, syntax := range []configdocument.Syntax{configdocument.JSON, configdocument.JSONC, configdocument.JSONLineComments, configdocument.TOML} {
		id, ok := aggregate.HostConfigCodecForSyntax(syntax.String())
		if !ok {
			t.Fatal("fixture grammar identity unavailable")
		}
		codec, err := nativecodec.New(t.Context(), id, limits, limits)
		if err != nil {
			t.Fatal(err)
		}
		source := `{"a":false,"b":false}`
		if syntax == configdocument.TOML {
			source = "# keep\na=false\nb=false\n"
		} else if syntax != configdocument.JSON {
			source = "// keep\n" + source
		}
		original := aggregate.ExistingDocument([]byte(source))
		var edits []compose.ProjectionEdit
		var baselines []compose.ProjectionBaseline
		for _, name := range []string{"a", "b"} {
			key := path(t, name)
			set := nativeSet(t, codec, target.ScopeProject, ".codex/extension.data", key, "true")
			before := observe(t, codec, original, selectSets(t, set))
			plan := desiredPlan(t, before, map[output.KeyPath]*aggregate.ContributionSet{key: &set})
			edit, err := compose.NewProjectionEdit(codec, plan, syntax, []output.KeyPath{key})
			if err != nil {
				t.Fatal(err)
			}
			baseline, err := compose.NewProjectionBaseline(codec, before, syntax, []output.KeyPath{key})
			if err != nil {
				t.Fatal(err)
			}
			edits = append(edits, edit)
			baselines = append(baselines, baseline)
		}
		candidate, err := compose.Render(t.Context(), original, limits, edits)
		if err != nil {
			t.Fatal(err)
		}
		parsedCandidate, err := configdocument.Parse(t.Context(), candidate.Document().Content(), syntax, limits)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"a", "b"} {
			actual, present, err := parsedCandidate.Lookup(path(t, name))
			if err != nil || !present || !actual.Equal(value(t, "true")) {
				t.Fatal("composed native candidate omitted a desired value")
			}
		}
		restored, err := compose.Restore(t.Context(), candidate.Document(), limits, baselines)
		if err != nil {
			t.Fatal(err)
		}
		left, err := configdocument.Parse(t.Context(), original.Content(), syntax, limits)
		if err != nil {
			t.Fatal(err)
		}
		right, err := configdocument.Parse(t.Context(), restored.Document().Content(), syntax, limits)
		if err != nil {
			t.Fatal(err)
		}
		leftValue, _, err := left.Lookup(output.KeyPath{})
		if err != nil {
			t.Fatal(err)
		}
		rightValue, _, err := right.Lookup(output.KeyPath{})
		leftComments, rightComments := left.Comments(), right.Comments()
		slices.Sort(leftComments)
		slices.Sort(rightComments)
		if err != nil || !leftValue.Equal(rightValue) || !slices.Equal(leftComments, rightComments) {
			t.Fatalf("composed grammar recovery lost native meaning/comments: %v", err)
		}
	}
}

func TestComposedRecoveryRetainsFreshOutsideDataFromAbsentOriginal(t *testing.T) {
	native := nativePort(t)
	model := nativeSet(t, native, target.ScopeProject, ".codex/config.toml", path(t, "model"), `"review"`)
	mcp, server := mcpSet(t, "search")
	modelBefore := observe(t, native, aggregate.AbsentDocument(), selectSets(t, model))
	serverBefore := observe(t, mcp, aggregate.AbsentDocument(), selectSets(t, server))
	modelPlan := desiredPlan(t, modelBefore, map[output.KeyPath]*aggregate.ContributionSet{path(t, "model"): &model})
	serverPlan := desiredPlan(t, serverBefore, map[output.KeyPath]*aggregate.ContributionSet{server.Address().KeyPath(): &server})
	created, err := compose.Render(t.Context(), aggregate.AbsentDocument(), limits, []compose.ProjectionEdit{
		edit(t, native, modelPlan, path(t, "model")), edit(t, mcp, serverPlan, path(t, "mcp_servers")),
	})
	if err != nil {
		t.Fatal(err)
	}
	current := aggregate.ExistingDocument(append([]byte("# fresh outside\nunmanaged=true\n"), created.Document().Content()...))
	restored, err := compose.Restore(t.Context(), current, limits, []compose.ProjectionBaseline{
		baseline(t, native, modelBefore, path(t, "model")), baseline(t, mcp, serverBefore, path(t, "mcp_servers")),
	})
	if err != nil || !restored.Document().Exists() || !nativeValue(t, restored.Document(), path(t, "unmanaged")).Equal(value(t, "true")) {
		t.Fatalf("group recovery erased fresh outside data: %v", err)
	}
	assertComments(t, current, restored.Document())
}
