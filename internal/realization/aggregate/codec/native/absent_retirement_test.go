package nativecodec_test

import (
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestNativeAbsentRetirementKeepsDocumentPresenceAndBytes(t *testing.T) {
	for _, syntax := range []configdocument.Syntax{configdocument.JSON, configdocument.JSONC, configdocument.JSONLineComments, configdocument.TOML} {
		t.Run(syntax.String(), func(t *testing.T) {
			codec := fixtureCodec(t, syntax)
			set := fixtureSet(t, codec, key(t, "gone"), fixtureValue(t, syntax, "1"))
			selection := selectSets(t, set)
			content := []byte("{\"keep\":2}\n")
			if syntax == configdocument.TOML {
				content = []byte("# keep comment\nkeep = 2\n")
			}
			for _, document := range []aggregate.Document{aggregate.AbsentDocument(), aggregate.ExistingDocument(content)} {
				before := read(t, codec, document, selection)
				state, present := before.State(set.Contract())
				if !present || state.Present() {
					t.Fatal("fixture does not describe a selected absent value")
				}
				intent, err := aggregate.NewProjectionIntent(state, nil)
				if err != nil {
					t.Fatal("canonical native plan could not retain selected absence", err)
				}
				plan, err := aggregate.NewPlan(before, []aggregate.ProjectionIntent{intent})
				if err != nil {
					t.Fatal(err)
				}
				rendered, failure := codec.Render(document, plan)
				if failure != nil || !rendered.Document().Equal(document) || !rendered.Expected().Equal(before) {
					t.Fatal("absent retirement created a document, lost bytes or invented expected evidence", failure)
				}
			}
		})
	}
}
