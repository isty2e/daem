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

func TestLoadedBaselineRoundTripAcrossNativeGrammars(t *testing.T) {
	for _, syntax := range []configdocument.Syntax{configdocument.JSON, configdocument.JSONC, configdocument.JSONLineComments, configdocument.TOML} {
		id, found := aggregate.HostConfigCodecForSyntax(syntax.String())
		if !found {
			t.Fatal("fixture syntax has no native codec")
		}
		native, err := nativecodec.New(t.Context(), id, limits, limits)
		if err != nil {
			t.Fatal(err)
		}
		source := `{"a":[1e999,null,true],"unselected":"keep"}`
		if syntax == configdocument.TOML {
			source = "# original\na=1979-05-27\nunselected='keep'\n"
		} else if syntax != configdocument.JSON {
			source = "// original\n" + source
		}
		original := aggregate.ExistingDocument([]byte(source))
		set := nativeSet(t, native, target.ScopeProject, ".codex/extension.data", path(t, "a"), "true")
		selection := selectSets(t, set)
		owner, err := compose.NewParticipant(native, selection, syntax, []output.KeyPath{path(t, "a")})
		if err != nil {
			t.Fatal(err)
		}
		baselines, err := compose.ReadBaselines(t.Context(), original, limits, []compose.Participant{owner})
		if err != nil {
			t.Fatal(err)
		}
		plan := desiredPlan(t, baselines[0].Snapshot(), map[output.KeyPath]*aggregate.ContributionSet{path(t, "a"): &set})
		change, err := compose.NewProjectionEdit(native, plan, syntax, []output.KeyPath{path(t, "a")})
		if err != nil {
			t.Fatal(err)
		}
		candidate, err := compose.Render(t.Context(), original, limits, []compose.ProjectionEdit{change})
		if err != nil {
			t.Fatal(err)
		}
		restored, err := compose.Restore(t.Context(), candidate.Document(), limits, baselines)
		if err != nil || !observe(t, native, restored.Document(), selection).Equal(baselines[0].Snapshot()) {
			t.Fatalf("reconstructed native baseline did not restore its actual typed value: %v", err)
		}
		before, err := configdocument.Parse(t.Context(), original.Content(), syntax, limits)
		if err != nil {
			t.Fatal(err)
		}
		after, err := configdocument.Parse(t.Context(), restored.Document().Content(), syntax, limits)
		if err != nil {
			t.Fatal(err)
		}
		beforeValue, beforePresent, err := before.Lookup(path(t, "unselected"))
		if err != nil || !beforePresent {
			t.Fatal("fixture lacks outside data")
		}
		afterValue, afterPresent, err := after.Lookup(path(t, "unselected"))
		beforeComments, afterComments := before.Comments(), after.Comments()
		slices.Sort(beforeComments)
		slices.Sort(afterComments)
		if err != nil || !afterPresent || !beforeValue.Equal(afterValue) || !slices.Equal(beforeComments, afterComments) {
			t.Fatal("reconstructed recovery lost outside native values or comments")
		}
	}
}

func TestReconstructedBaselinesRestoreBothParentChildTransitions(t *testing.T) {
	native := nativePort(t)
	parent := nativeSet(t, native, target.ScopeProject, ".codex/config.toml", path(t, "a"), `"desired-parent"`)
	child := nativeSet(t, native, target.ScopeProject, ".codex/config.toml", path(t, "a", "b"), "true")
	selection := selectSets(t, parent, child)
	owner := participant(t, native, selection, path(t, "a"))
	for _, source := range []string{"a='actual'\n", "a={}\n", "a={b=false, nested=2026-10-24}\n"} {
		original := aggregate.ExistingDocument([]byte(source))
		baselines, err := compose.ReadBaselines(t.Context(), original, limits, []compose.Participant{owner})
		if err != nil {
			t.Fatal(err)
		}
		before := baselines[0].Snapshot()
		variants := []map[output.KeyPath]*aggregate.ContributionSet{{path(t, "a", "b"): &child}}
		if childBefore, _ := before.State(child.Contract()); childBefore.Present() {
			variants = append(variants, map[output.KeyPath]*aggregate.ContributionSet{path(t, "a"): &parent})
		}
		for _, desired := range variants {
			plan := desiredPlan(t, before, desired)
			candidate, err := compose.Render(t.Context(), original, limits, []compose.ProjectionEdit{edit(t, native, plan, path(t, "a"))})
			if err != nil {
				t.Fatal(err)
			}
			current := aggregate.ExistingDocument(append([]byte("# fresh\noutside='keep'\n"), candidate.Document().Content()...))
			restored, err := compose.Restore(t.Context(), current, limits, baselines)
			if err != nil || !observe(t, native, restored.Document(), selection).Equal(before) {
				t.Fatalf("reconstructed recovery lost a selected container/value/blocker: %v", err)
			}
			if !nativeValue(t, restored.Document(), path(t, "outside")).Equal(value(t, `"keep"`)) {
				t.Fatal("shape recovery overwrote unselected outside data")
			}
			assertComments(t, current, restored.Document())
		}
	}
}
