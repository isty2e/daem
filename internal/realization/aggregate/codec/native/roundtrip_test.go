package nativecodec_test

import (
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestNativeCodecOrdinaryApplyRoundtripsStabilize(t *testing.T) {
	for _, test := range []struct {
		syntax configdocument.Syntax
		source string
	}{
		{configdocument.JSON, "{\n  \"outside\": {\"keep\": 1}\n}\n"},
		{configdocument.JSONC, "// outside\n{\"outside\":{\"keep\":1},} // EOF"},
		{configdocument.JSONLineComments, "// outside\r\n{\"outside\":{\"keep\":1}} // EOF\r\n"},
		{configdocument.TOML, "# outside\noutside={keep=1}\n\n"},
	} {
		t.Run(test.syntax.String(), func(t *testing.T) {
			codec := fixtureCodec(t, test.syntax)
			original := aggregate.ExistingDocument([]byte(test.source))
			current := original
			added := key(t, "added")
			set := fixtureSet(t, codec, added, fixtureValue(t, test.syntax, "true"))
			selection := selectSets(t, set)
			var settled aggregate.Document
			var lengths []int
			stable := true

			for cycle := range 6 {
				before := read(t, codec, current, selection)
				addition := plan(t, before, map[output.KeyPath]*aggregate.ContributionSet{added: &set})
				rendered, failure := codec.Render(current, addition)
				if failure != nil {
					t.Fatal(failure)
				}
				if actual := read(t, codec, rendered.Document(), selection); !actual.Equal(rendered.Expected()) {
					t.Fatal("addition evidence differs from its actual document")
				}

				removal := plan(t, rendered.Expected(), map[output.KeyPath]*aggregate.ContributionSet{added: nil})
				removed, failure := codec.Render(rendered.Document(), removal)
				if failure != nil {
					t.Fatal(failure)
				}
				state, present := removed.Expected().State(set.Contract())
				if !present || state.Present() || !read(t, codec, removed.Document(), selection).Equal(removed.Expected()) {
					t.Fatal("normal removal retained the key or returned incorrect evidence")
				}
				current = removed.Document()
				if !lookup(t, test.syntax, current, key(t, "outside")).Equal(lookup(t, test.syntax, original, key(t, "outside"))) {
					t.Fatal("normal apply changed an unselected native value")
				}
				assertComments(t, test.syntax, original, current)
				lengths = append(lengths, len(current.Content()))
				if cycle == 0 {
					settled = current
				} else if !current.Equal(settled) {
					stable = false
				}
			}

			if !stable {
				t.Fatalf("ordinary apply roundtrips accumulate formatting: lengths=%v", lengths)
			}
		})
	}
}
