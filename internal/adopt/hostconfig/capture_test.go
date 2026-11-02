package hostconfig_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/adopt/hostconfig"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
)

func fragment(t *testing.T, text string) configdocument.Document {
	t.Helper()
	doc, err := configdocument.Parse(t.Context(), []byte(text), configdocument.JSONC, configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32})
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func key(t *testing.T, text string) output.KeyPath {
	t.Helper()
	path, err := output.ParseKeyPath(text)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInitialCaptureUsesCanonicalAtomicValuesAndNoHostComments(t *testing.T) {
	host := fragment(t, `/* host-only */{"obj":{"a":1,"b":2},"array":[{"a":1}],"null":null,"empty":{},"a/b":{"x.y":"secret-literal"},"unselected":9}`)
	empty := fragment(t, `{}`)
	selectors := []output.KeyPath{key(t, "/obj"), key(t, "/array"), key(t, "/null"), key(t, "/empty"), key(t, "/a~1b")}
	result, err := hostconfig.Initial(t.Context(), host, empty, selectors, 4096)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/a~1b/x.y", "/array", "/empty", "/null", "/obj/a", "/obj/b"}
	var actual []string
	for _, selected := range result.Selected() {
		actual = append(actual, selected.String())
		before, _, _ := host.Lookup(selected)
		after, present, err := result.After().Lookup(selected)
		if err != nil || !present || !before.Equal(after) {
			t.Fatal("capture changed an actual native literal", err)
		}
	}
	if !reflect.DeepEqual(actual, want) || len(result.After().Comments()) != 0 {
		t.Fatal("capture used competing leaf semantics or copied host comments", actual)
	}
	if _, err := hostconfig.Initial(t.Context(), host, empty, []output.KeyPath{key(t, "/missing")}, 4096); err == nil {
		t.Fatal("initial missing selection became absence/null")
	}
	if _, err := hostconfig.Initial(t.Context(), host, empty, []output.KeyPath{key(t, "/array/0")}, 4096); !errors.Is(err, configdocument.ErrPathConflict) {
		t.Fatal("array descendant was treated as a selectable key", err)
	}
}

func TestUpdateCaptureNarrowsDeclaredLeavesAndReleasesMissingValues(t *testing.T) {
	source := fragment(t, `/* source */{"obj":{"a":1,"b":2},"keep":{},"other":3}`)
	host := fragment(t, `/* host */{"obj":{"a":4,"c":5},"keep":{},"other":9}`)
	result, err := hostconfig.Update(t.Context(), source, host, []output.KeyPath{key(t, "/obj")}, 4096)
	if err != nil {
		t.Fatal(err)
	}
	wanted := fragment(t, `{"obj":{"a":4},"keep":{},"other":3}`)
	leaves, _ := wanted.Leaves(t.Context(), 4096)
	for _, leaf := range leaves {
		actual, present, err := result.After().Lookup(leaf.Path)
		if err != nil || !present || !actual.Equal(leaf.Value) {
			t.Fatal("source update changed unrelated or selected data incorrectly", err)
		}
	}
	for _, path := range []string{"/obj/b", "/obj/c"} {
		if _, present, err := result.After().Lookup(key(t, path)); err != nil || present {
			t.Fatal("missing value was retained or host sibling acquired", err)
		}
	}
	beforeComments, afterComments := source.Comments(), result.After().Comments()
	slices.Sort(beforeComments)
	slices.Sort(afterComments)
	if !slices.Equal(beforeComments, afterComments) {
		t.Fatal("source comments were replaced by host comments")
	}
	for _, selector := range []string{"/obj/c", "/other/child"} {
		if _, err := hostconfig.Update(t.Context(), source, host, []output.KeyPath{key(t, selector)}, 4096); err == nil {
			t.Fatal("update broadened declaration membership", selector)
		}
	}
	if _, err := hostconfig.Update(t.Context(), source, fragment(t, `{"obj":false}`), []output.KeyPath{key(t, "/obj")}, 4096); !errors.Is(err, configdocument.ErrPathConflict) {
		t.Fatal("blocked ancestor became absence", err)
	}
}

func TestCaptureRetainsTOMLNativeValuesAndExistingSourceComments(t *testing.T) {
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	host, err := configdocument.Parse(t.Context(), []byte(`# host comment
unselected = 9
[values]
"literal.key" = 1979-05-27T07:32:00Z
"a/b" = +inf
float = 1.0
integer = 1
inline = { flag = true, empty = {} }
[[workers]]
name = "first"
[[workers]]
name = "second"
`), configdocument.TOML, limits)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := configdocument.Absent(configdocument.TOML, limits)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := hostconfig.Initial(t.Context(), host, empty, []output.KeyPath{key(t, "/values"), key(t, "/workers")}, limits.MaximumBytes)
	if err != nil {
		t.Fatal(err)
	}
	leaves, err := initial.After().Leaves(t.Context(), limits.MaximumBytes)
	if err != nil || len(leaves) != 7 || len(initial.After().Comments()) != 0 {
		t.Fatal("TOML capture did not retain the selected native shape", len(leaves), err)
	}
	for _, leaf := range leaves {
		value, present, err := host.Lookup(leaf.Path)
		if err != nil || !present || !value.Equal(leaf.Value) {
			t.Fatal("TOML capture changed native meaning", leaf.Path, err)
		}
	}
	source, err := configdocument.Parse(t.Context(), []byte("# source\nuntouched = 17\n[values] # structure\nold = 1 # selected removal\n"), configdocument.TOML, limits)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := hostconfig.Update(t.Context(), source, host, nil, limits.MaximumBytes)
	if err != nil {
		t.Fatal(err)
	}
	if updated.After().HasValues() {
		t.Fatal("missing declared host values did not release source membership")
	}
	beforeComments, afterComments := source.Comments(), updated.After().Comments()
	slices.Sort(beforeComments)
	slices.Sort(afterComments)
	if !slices.Equal(beforeComments, afterComments) {
		t.Fatal("TOML structural pruning lost source comments")
	}
}

func TestCaptureSelectionFailureReturnsNoPartialValues(t *testing.T) {
	host := fragment(t, `{"a":1,"b":2}`)
	empty := fragment(t, `{}`)
	if result, err := hostconfig.Initial(t.Context(), host, empty, []output.KeyPath{key(t, "/a"), key(t, "/b")}, 3); !errors.Is(err, configdocument.ErrLimitExceeded) || len(result.Selected()) != 0 || result.After().Exists() {
		t.Fatal("bounded projection failure retained a partial capture", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if result, err := hostconfig.Update(ctx, host, host, nil, 4096); !errors.Is(err, context.Canceled) || len(result.Selected()) != 0 || result.After().Exists() {
		t.Fatal("canceled capture retained a candidate", err)
	}
	if result, err := hostconfig.Initial(t.Context(), host, empty, []output.KeyPath{key(t, "/a"), key(t, "/missing")}, 4096); err == nil || len(result.Selected()) != 0 || result.After().Exists() {
		t.Fatal("one missing key retained the successful subset", err)
	}
	selected, err := hostconfig.Initial(t.Context(), host, empty, []output.KeyPath{key(t, "/a"), key(t, "/a")}, 4096)
	if err != nil || len(selected.Selected()) != 1 {
		t.Fatal("overlapping selectors duplicated membership", err)
	}
	paths := selected.Selected()
	paths[0] = key(t, "/b")
	if selected.Selected()[0].String() != "/a" {
		t.Fatal("caller could change retained capture selection")
	}
	if _, err := hostconfig.Initial(t.Context(), host, empty, []output.KeyPath{key(t, "/a")}, 2); err != nil {
		t.Fatal("unselected host paths consumed source projection allowance", err)
	}
	if result, err := hostconfig.Update(t.Context(), fragment(t, `{"x":0}`), fragment(t, `{"x":{"abcdefgh":1}}`), nil, 3); !errors.Is(err, configdocument.ErrLimitExceeded) || len(result.Selected()) != 0 || result.After().Exists() {
		t.Fatal("updated fragment escaped the caller-owned path allowance", err)
	}
}

func TestInitialSelectionUsesSegmentsAndIgnoresOverlappingSelectorOrder(t *testing.T) {
	host := fragment(t, `{"obj":{"a":1,"b":2},"obj-":3,"":{"x":0},"a/b":{"~.":4},"ignored":9}`)
	for _, selectors := range [][]string{
		{"/obj", "/obj/a", "/obj-", "/", "/a~1b", "/obj"},
		{"/a~1b", "/", "/obj-", "/obj/a", "/obj", "/obj/a"},
	} {
		var paths []output.KeyPath
		for _, selector := range selectors {
			paths = append(paths, key(t, selector))
		}
		result, err := hostconfig.Initial(t.Context(), host, fragment(t, `{}`), paths, 4096)
		if err != nil {
			t.Fatal(err)
		}
		leaves, err := result.After().Leaves(t.Context(), 4096)
		if err != nil || len(leaves) != 5 {
			t.Fatal("overlapping/literal selectors changed selected membership", len(leaves), err)
		}
		for _, leaf := range leaves {
			value, present, err := host.Lookup(leaf.Path)
			if err != nil || !present || !value.Equal(leaf.Value) {
				t.Fatal("capture treated native key segments as filesystem paths", leaf.Path, err)
			}
		}
		if _, present, err := result.After().Lookup(key(t, "/ignored")); err != nil || present {
			t.Fatal("initial capture acquired an unselected host sibling", err)
		}
	}
}

func TestUpdateUsesDeclaredAtomicSubtreesWithoutAcquiringSiblings(t *testing.T) {
	source := fragment(t, `{"selected":1,"untouched":{"a":1},"nullable":null}`)
	host := fragment(t, `{"selected":{"nested":{},"array":[1,2]},"untouched":{"a":2,"new":3},"nullable":null}`)
	result, err := hostconfig.Update(t.Context(), source, host, []output.KeyPath{key(t, "/selected"), key(t, "/nullable")}, 4096)
	if err != nil {
		t.Fatal(err)
	}
	value, _, _ := host.Lookup(key(t, "/selected"))
	actual, present, err := result.After().Lookup(key(t, "/selected"))
	if err != nil || !present || !actual.Equal(value) {
		t.Fatal("selected atomic subtree type change was narrowed accidentally", err)
	}
	before, _, _ := source.Lookup(key(t, "/untouched"))
	actual, present, err = result.After().Lookup(key(t, "/untouched"))
	if err != nil || !present || !actual.Equal(before) {
		t.Fatal("unselected source subtree was refreshed", err)
	}
	if _, present, err := result.After().Lookup(key(t, "/nullable")); err != nil || !present {
		t.Fatal("present null was interpreted as absence", err)
	}
	missing, err := configdocument.Absent(configdocument.JSONC, configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32})
	if err != nil {
		t.Fatal(err)
	}
	released, err := hostconfig.Update(t.Context(), source, missing, nil, 4096)
	if err != nil || !released.After().Exists() || released.After().HasValues() {
		t.Fatal("missing key-mode host document failed to release its declared values", err)
	}
}
