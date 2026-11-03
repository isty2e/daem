package configdocument_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
)

func TestFragmentRemovalPrunesOnlyNewlyEmptyStructuralParents(t *testing.T) {
	for _, test := range []struct {
		name    string
		syntax  configdocument.Syntax
		content string
	}{
		{"json", configdocument.JSON, `{"parent":{"nested":{"a":1}},"keep":{},"other":2}`},
		{"jsonc", configdocument.JSONC, "// original\n{\"parent\":{/*nested*/\"nested\":{\"a\":1}},\"keep\":{},\"other\":2}// end\n"},
		{"json-line-comments", configdocument.JSONLineComments, "// original\n{\"parent\":{\"nested\":{\"a\":1}},\"keep\":{},\"other\":2}\n"},
		{"toml", configdocument.TOML, "# original\nkeep={}\nother=2\n[parent.nested] # nested\na=1 # leaf\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc, err := configdocument.Parse(t.Context(), []byte(test.content), test.syntax, configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32})
			if err != nil {
				t.Fatal(err)
			}
			path, _ := output.ParseKeyPath("/parent/nested/a")
			remove, _ := configdocument.Remove(path)
			ordinary, err := configdocument.Edit(t.Context(), doc, []configdocument.Change{remove})
			if err != nil {
				t.Fatal(err)
			}
			parent, _ := output.ParseKeyPath("/parent/nested")
			if _, present, err := ordinary.Lookup(parent); err != nil || !present {
				t.Fatal("ordinary host editing lost its empty-parent contract", err)
			}
			fragment, err := configdocument.EditFragment(t.Context(), doc, []configdocument.Change{remove})
			if err != nil {
				t.Fatal(err)
			}
			rootParent, _ := output.ParseKeyPath("/parent")
			if _, present, err := fragment.Lookup(rootParent); err != nil || present {
				t.Fatal("fragment deletion introduced empty-object ownership", err)
			}
			leaves, err := fragment.Leaves(t.Context(), 4096)
			beforeComments, afterComments := doc.Comments(), fragment.Comments()
			slices.Sort(beforeComments)
			slices.Sort(afterComments)
			if err != nil || len(leaves) != 2 || !reflect.DeepEqual(afterComments, beforeComments) {
				t.Fatal("fragment deletion lost explicit empty/unselected values or comments", err)
			}
		})
	}
}

func TestFragmentRemovalKeepsExplicitEmptyValuesAndValidEmptyRoot(t *testing.T) {
	doc, err := configdocument.Parse(t.Context(), []byte(`{"parent":{"a":1}}`), configdocument.JSON, configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32})
	if err != nil {
		t.Fatal(err)
	}
	path, _ := output.ParseKeyPath("/parent/a")
	remove, _ := configdocument.Remove(path)
	empty, err := configdocument.EditFragment(t.Context(), doc, []configdocument.Change{remove})
	if err != nil || !empty.Exists() {
		t.Fatal("removing the last source value lost a valid fragment", err)
	}
	leaves, err := empty.Leaves(t.Context(), 4096)
	if err != nil || len(leaves) != 0 {
		t.Fatal("last source removal produced ownership", err)
	}
	root, _ := output.ParseKeyPath("/parent")
	valueDoc, _ := configdocument.Parse(t.Context(), []byte(`{"value":{}}`), configdocument.JSON, configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32})
	valuePath, _ := output.ParseKeyPath("/value")
	value, _, _ := valueDoc.Lookup(valuePath)
	set, _ := configdocument.Set(root, value)
	kept, err := configdocument.EditFragment(t.Context(), doc, []configdocument.Change{set})
	if err != nil {
		t.Fatal(err)
	}
	if actual, present, err := kept.Lookup(root); err != nil || !present || !actual.Equal(value) {
		t.Fatal("explicit empty object was pruned", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := configdocument.EditFragment(ctx, doc, []configdocument.Change{remove}); !errors.Is(err, context.Canceled) {
		t.Fatal("fragment edit ignored cancellation", err)
	}
}

func TestFragmentPruningComposesDisjointRemovalsAndSets(t *testing.T) {
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	doc, err := configdocument.Parse(t.Context(), []byte(`{"a":{"only":1},"b":{"only":2},"keep":{"x":0},"literal/key":{"only":3}}`), configdocument.JSON, limits)
	if err != nil {
		t.Fatal(err)
	}
	var changes []configdocument.Change
	for _, pointer := range []string{"/a/only", "/b/only", "/literal~1key/only"} {
		path, _ := output.ParseKeyPath(pointer)
		remove, _ := configdocument.Remove(path)
		changes = append(changes, remove)
	}
	valuePath, _ := output.ParseKeyPath("/keep/x")
	valueDoc, _ := configdocument.Parse(t.Context(), []byte(`{"keep":{"x":5}}`), configdocument.JSON, limits)
	value, _, _ := valueDoc.Lookup(valuePath)
	set, _ := configdocument.Set(valuePath, value)
	newPath, _ := output.ParseKeyPath("/new/x")
	create, _ := configdocument.Set(newPath, value)
	result, err := configdocument.EditFragment(t.Context(), doc, append(changes, set, create))
	if err != nil {
		t.Fatal(err)
	}
	leaves, err := result.Leaves(t.Context(), limits.MaximumBytes)
	if err != nil || len(leaves) != 2 {
		t.Fatal("disjoint pruning retained structural parents or lost set values", err)
	}
	for _, leaf := range leaves {
		if !leaf.Value.Equal(value) || leaf.Path != valuePath && leaf.Path != newPath {
			t.Fatal("pruning changed a disjoint set", leaf.Path)
		}
	}
}
