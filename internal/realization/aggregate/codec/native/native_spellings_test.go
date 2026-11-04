package nativecodec_test

import (
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestNativeCodecUsesNativeTOMLValuesAndEquivalentSpellings(t *testing.T) {
	for _, test := range []struct {
		name, source, desired string
		parts                 []string
	}{
		{"array tables", "outside=true\n[[a]]\nb=1\n", `[{b=2}]`, []string{"a"}},
		{"quoted project path", "[projects.\"/absolute/path\"]\nmodel='old'\n", `"new"`, []string{"projects", "/absolute/path", "model"}},
		{"dotted key", "a.b.c=1\n", "2", []string{"a", "b", "c"}},
		{"temporal", "a=2026-10-23T01:02:03Z\n", "2026-10-24T01:02:03Z", []string{"a"}},
		{"nonfinite float", "a=inf\n", "nan", []string{"a"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			codec := fixtureCodec(t, configdocument.TOML)
			path := key(t, test.parts...)
			value := fixtureValue(t, configdocument.TOML, test.desired)
			set := fixtureSet(t, codec, path, value)
			selection := selectSets(t, set)
			original := aggregate.ExistingDocument([]byte(test.source))
			before := read(t, codec, original, selection)
			rendered, failure := codec.Render(original, plan(t, before, map[output.KeyPath]*aggregate.ContributionSet{path: &set}))
			if failure != nil || !lookup(t, configdocument.TOML, rendered.Document(), path).Equal(value) {
				t.Fatalf("native TOML value/spelling was narrowed by the codec: %v", failure)
			}
			restored, failure := codec.Restore(rendered.Document(), before)
			if failure != nil || !read(t, codec, restored.Document(), selection).Equal(before) {
				t.Fatalf("native TOML recovery changed type or value: %v", failure)
			}
		})
	}
}

func TestNativeCodecRetainsCommentsInsideReplacedObjects(t *testing.T) {
	for _, test := range []struct {
		syntax configdocument.Syntax
		source string
	}{
		{configdocument.JSONC, `{"a":{/* nested */"b":1},"outside":true}`},
		{configdocument.TOML, "a={ # nested\n b=1,\n}\noutside=true\n"},
	} {
		codec := fixtureCodec(t, test.syntax)
		parent := fixtureSet(t, codec, key(t, "a"), fixtureValue(t, test.syntax, `"new"`))
		child := fixtureSet(t, codec, key(t, "a", "b"), fixtureValue(t, test.syntax, "1"))
		selection := selectSets(t, parent, child)
		original := aggregate.ExistingDocument([]byte(test.source))
		before := read(t, codec, original, selection)
		rendered, failure := codec.Render(original, plan(t, before, map[output.KeyPath]*aggregate.ContributionSet{key(t, "a"): &parent}))
		if failure != nil {
			t.Fatal(failure)
		}
		assertComments(t, test.syntax, original, rendered.Document())
		restored, failure := codec.Restore(rendered.Document(), before)
		if failure != nil {
			t.Fatal(failure)
		}
		assertComments(t, test.syntax, original, restored.Document())
		if !read(t, codec, restored.Document(), selection).Equal(before) {
			t.Fatal("comment-bearing object recovery did not restore selected native values")
		}
	}
}
