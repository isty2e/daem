package nativecodec_test

import (
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestNativeJSONLayoutSurvivesTwoCallReplacementAndRestore(t *testing.T) {
	codec := fixtureCodec(t, configdocument.JSONC)
	old := fixtureSet(t, codec, key(t, "old"), fixtureValue(t, configdocument.JSONC, "false"))
	added := fixtureSet(t, codec, key(t, "new"), fixtureValue(t, configdocument.JSONC, "true"))
	selection := selectSets(t, old, added)
	source := "{\n  \"keep\": 1,\n  \"old\": false,\n}\n"
	original := aggregate.ExistingDocument([]byte(source))
	before := read(t, codec, original, selection)
	change := plan(t, before, map[output.KeyPath]*aggregate.ContributionSet{key(t, "old"): nil, key(t, "new"): &added})
	rendered, failure := codec.Render(original, change)
	if failure != nil {
		t.Fatal(failure)
	}
	want := "{\n  \"keep\": 1,\n  \"new\": true,\n}\n"
	if string(rendered.Document().Content()) != want {
		t.Fatalf("two-call native rendering lost indentation/comma: %q", rendered.Document().Content())
	}
	restored, failure := codec.Restore(rendered.Document(), before)
	if failure != nil || string(restored.Document().Content()) != source || !read(t, codec, restored.Document(), selection).Equal(before) {
		t.Fatal("native inverse lost the selected baseline or existing layout", failure)
	}
}
