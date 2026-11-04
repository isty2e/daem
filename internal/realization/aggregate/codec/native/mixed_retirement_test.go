package nativecodec_test

import (
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestNativeMixedRetirementPreservesUnmanagedDocument(t *testing.T) {
	for _, syntax := range []configdocument.Syntax{configdocument.JSON, configdocument.JSONC, configdocument.JSONLineComments, configdocument.TOML} {
		t.Run(syntax.String(), func(t *testing.T) {
			codec := fixtureCodec(t, syntax)
			present := fixtureSet(t, codec, key(t, "old"), fixtureValue(t, syntax, "1"))
			missing := fixtureSet(t, codec, key(t, "gone"), fixtureValue(t, syntax, "1"))
			selection := selectSets(t, present, missing)
			text := `{"old":1,"keep":2}`
			if syntax == configdocument.TOML {
				text = "# unmanaged\nold = 1\nkeep = 2\n"
			}
			document := aggregate.ExistingDocument([]byte(text))
			before := read(t, codec, document, selection)
			plan := plan(t, before, map[output.KeyPath]*aggregate.ContributionSet{})
			rendered, failure := codec.Render(document, plan)
			if failure != nil || !rendered.Document().Exists() || !read(t, codec, rendered.Document(), selection).Equal(rendered.Expected()) {
				t.Fatal("mixed present/absent retirement lost document or expected evidence", failure)
			}
			for _, state := range rendered.Expected().States() {
				if state.Present() {
					t.Fatal("mixed retirement retained a selected value")
				}
			}
			if !lookup(t, syntax, rendered.Document(), key(t, "keep")).Equal(fixtureValue(t, syntax, "2")) {
				t.Fatal("mixed retirement lost the unmanaged sibling")
			}
			assertComments(t, syntax, document, rendered.Document())
		})
	}
}
