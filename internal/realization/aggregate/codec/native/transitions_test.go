package nativecodec_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
)

func TestNativeCodecTreeTransitionsAndRecoveryAcrossGrammars(t *testing.T) {
	for _, syntax := range []configdocument.Syntax{configdocument.JSON, configdocument.JSONC, configdocument.JSONLineComments, configdocument.TOML} {
		for _, shape := range []string{"scalar", "array", "empty-object", "object", "null"} {
			if syntax == configdocument.TOML && shape == "null" {
				continue
			}
			t.Run(syntax.String()+"/"+shape, func(t *testing.T) {
				value := map[string]string{
					"scalar": `"old"`, "array": `[1,true]`, "empty-object": `{}`,
					"object": `{"unmanaged":true}`, "null": `null`,
				}[shape]
				source := `{"a":` + value + `,"outside":true}`
				if syntax == configdocument.TOML {
					if shape == "object" {
						value = `{unmanaged=true}`
					}
					source = "# retain\na=" + value + "\noutside=true\n"
				} else if syntax != configdocument.JSON {
					source = "// retain\n" + source
				}
				codec := fixtureCodec(t, syntax)
				parent := fixtureSet(t, codec, key(t, "a"), fixtureValue(t, syntax, `"replacement"`))
				child := fixtureSet(t, codec, key(t, "a", "b"), fixtureValue(t, syntax, "true"))
				selection := selectSets(t, parent, child)
				original := aggregate.ExistingDocument([]byte(source))
				before := read(t, codec, original, selection)
				toChild := plan(t, before, map[output.KeyPath]*aggregate.ContributionSet{key(t, "a", "b"): &child})
				created, failure := codec.Render(original, toChild)
				if failure != nil {
					t.Fatal(failure)
				}
				actual := read(t, codec, created.Document(), selection)
				parentState, _ := actual.State(parent.Contract())
				childState, _ := actual.State(child.Contract())
				if !actual.Equal(created.Expected()) || !parentState.Present() || !childState.Present() ||
					!lookup(t, syntax, created.Document(), key(t, "a", "b")).Equal(fixtureValue(t, syntax, "true")) {
					t.Fatal("retired logical parent lost its actual structural presence or desired child")
				}
				if shape == "object" && !lookup(t, syntax, created.Document(), key(t, "a", "unmanaged")).Equal(fixtureValue(t, syntax, "true")) {
					t.Fatal("structural retirement erased an unrelated child of the retained object")
				}
				assertComments(t, syntax, original, created.Document())
				restored, failure := codec.Restore(created.Document(), before)
				if failure != nil || !read(t, codec, restored.Document(), selection).Equal(before) {
					t.Fatalf("parent-to-child recovery did not restore actual values/blockers: %v", failure)
				}

				toParent := plan(t, actual, map[output.KeyPath]*aggregate.ContributionSet{key(t, "a"): &parent})
				replaced, failure := codec.Render(created.Document(), toParent)
				if failure != nil {
					t.Fatal(failure)
				}
				after := read(t, codec, replaced.Document(), selection)
				childState, _ = after.State(child.Contract())
				if blocker, found := childState.BlockedBy(); !found || blocker != parent.Address().KeyPath() || !after.Equal(replaced.Expected()) {
					t.Fatal("child retirement below a scalar parent fabricated plain absence")
				}
				if !lookup(t, syntax, replaced.Document(), key(t, "outside")).Equal(fixtureValue(t, syntax, "true")) {
					t.Fatal("selected broad-value replacement erased outside-scope data")
				}
				restored, failure = codec.Restore(replaced.Document(), actual)
				if failure != nil || !read(t, codec, restored.Document(), selection).Equal(actual) {
					t.Fatalf("child-to-parent recovery failed to restore the actual broad object: %v", failure)
				}
				assertComments(t, syntax, original, restored.Document())
			})
		}
	}
}

func TestNativeCodecRemovalCoalescesNativePrefixRangesThroughActualOutput(t *testing.T) {
	for _, name := range []string{"a", "", "a/b", "~", ".", "line\n\x00"} {
		codec := fixtureCodec(t, configdocument.JSON)
		content, err := json.Marshal(map[string]json.RawMessage{
			name: json.RawMessage(`{"b":1}`), name + "!": json.RawMessage(`2`), "outside": json.RawMessage(`true`),
		})
		if err != nil {
			t.Fatal(err)
		}
		var sets []aggregate.ContributionSet
		for _, path := range []output.KeyPath{key(t, name), key(t, name, "b"), key(t, name+"!")} {
			sets = append(sets, fixtureSet(t, codec, path, fixtureValue(t, configdocument.JSON, "true")))
		}
		selection := selectSets(t, sets...)
		original := aggregate.ExistingDocument(content)
		before := read(t, codec, original, selection)
		rendered, failure := codec.Render(original, plan(t, before, nil))
		if failure != nil {
			t.Fatalf("native coalescing failed for %q: %v", name, failure)
		}
		actual := read(t, codec, rendered.Document(), selection)
		for _, state := range actual.States() {
			if !state.Absent() {
				t.Fatal("selected retirement retained native content")
			}
		}
		if !lookup(t, configdocument.JSON, rendered.Document(), key(t, "outside")).Equal(fixtureValue(t, configdocument.JSON, "true")) {
			t.Fatal("prefix-range coalescing erased an unrelated sibling")
		}
	}
}

func TestNativeCodecPlanComparisonPreservesFreshUnselectedValuesAndComments(t *testing.T) {
	codec := fixtureCodec(t, configdocument.JSONC)
	set := fixtureSet(t, codec, key(t, "a"), fixtureValue(t, configdocument.JSONC, `"new"`))
	selection := selectSets(t, set)
	original := aggregate.ExistingDocument([]byte(`{"a":"old","outside":0}`))
	before := read(t, codec, original, selection)
	p := plan(t, before, map[output.KeyPath]*aggregate.ContributionSet{key(t, "a"): &set})
	fresh := aggregate.ExistingDocument([]byte("// new outside comment\n{\n\"outside\":1,\"a\":\"old\"}"))
	rendered, failure := codec.Render(fresh, p)
	if failure != nil || !lookup(t, configdocument.JSONC, rendered.Document(), key(t, "outside")).Equal(fixtureValue(t, configdocument.JSONC, "1")) {
		t.Fatalf("fresh unrelated data or formatting invalidated selected approval: %v", failure)
	}
	assertComments(t, configdocument.JSONC, fresh, rendered.Document())
	stale := aggregate.ExistingDocument([]byte(`{"a":"not approved","outside":0}`))
	result, failure := codec.Render(stale, p)
	if failure == nil || failure.Reason() != aggregate.CodecFailureEquivalenceUndefined || result.Document().Exists() || len(result.Expected().States()) != 0 {
		t.Fatal("a stale selected value produced a replacement candidate")
	}
}

func TestNativeCodecAbsentRecoveryAndFailedSecondEdit(t *testing.T) {
	codec := fixtureCodec(t, configdocument.JSON)
	for _, path := range []output.KeyPath{key(t, "a"), key(t, "a", "b")} {
		set := fixtureSet(t, codec, path, fixtureValue(t, configdocument.JSON, "true"))
		selection := selectSets(t, set)
		before := read(t, codec, aggregate.AbsentDocument(), selection)
		created, failure := codec.Render(aggregate.AbsentDocument(), plan(t, before, map[output.KeyPath]*aggregate.ContributionSet{path: &set}))
		if failure != nil {
			t.Fatal(failure)
		}
		restored, failure := codec.Restore(created.Document(), before)
		if failure != nil {
			t.Fatal(failure)
		}
		if path == key(t, "a") && restored.Document().Exists() {
			t.Fatal("a completely empty formerly absent document was retained")
		}
		for _, state := range read(t, codec, restored.Document(), selection).States() {
			if !state.Absent() {
				t.Fatal("formerly absent selected value survived recovery")
			}
		}
		if path == key(t, "a", "b") && !restored.Document().Exists() {
			t.Fatal("recovery erased an intermediate container without creation history")
		}
	}
	parent := fixtureSet(t, codec, key(t, "a"), fixtureValue(t, configdocument.JSON, `"old"`))
	child := fixtureSet(t, codec, key(t, "a", "b"), fixtureValue(t, configdocument.JSON, `"`+strings.Repeat("n", 100)+`"`))
	original := aggregate.ExistingDocument([]byte(`{"a":"old"}`))
	before := read(t, codec, original, selectSets(t, parent, child))
	bounded, err := nativecodec.New(t.Context(), codec.ContractID(), configdocument.Limits{MaximumBytes: 70, MaximumDepth: 8}, fixtureLimits)
	if err != nil {
		t.Fatal(err)
	}
	rendered, failure := bounded.Render(original, plan(t, before, map[output.KeyPath]*aggregate.ContributionSet{key(t, "a", "b"): &child}))
	if failure == nil || failure.Reason() != aggregate.CodecFailureResourceLimitExceeded || rendered.Document().Exists() || len(rendered.Expected().States()) != 0 ||
		!original.Equal(aggregate.ExistingDocument([]byte(`{"a":"old"}`))) {
		t.Fatal("failed second pure edit published the earlier retirement or modified its input")
	}
}
