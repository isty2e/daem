package nativecodec_test

import (
	"bytes"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestNativeCodecRootInsertionKeepsLeadingTrivia(t *testing.T) {
	for _, source := range []string{
		"# File settings\n\nkeep=true\n[tui]\ntheme='light'\n",
		"# File settings\n\n[tui]\ntheme='light'\n",
		"# File settings\r\n\r\n[tui]\r\ntheme='light'\r\n",
		"# File settings",
	} {
		t.Run(source, func(t *testing.T) {
			codec := fixtureCodec(t, configdocument.TOML)
			selected := key(t, "model")
			set := fixtureSet(t, codec, selected, fixtureValue(t, configdocument.TOML, "true"))
			selection := selectSets(t, set)
			original := aggregate.ExistingDocument([]byte(source))
			before := read(t, codec, original, selection)
			rendered, failure := codec.Render(original, plan(t, before, map[output.KeyPath]*aggregate.ContributionSet{selected: &set}))
			if failure != nil {
				t.Fatal(failure)
			}
			if !bytes.HasPrefix(rendered.Document().Content(), []byte("# File settings")) || !lookup(t, configdocument.TOML, rendered.Document(), selected).Equal(fixtureValue(t, configdocument.TOML, "true")) {
				t.Fatalf("ordinary Render displaced the leading comment or lost root scope: %q", rendered.Document().Content())
			}
			assertComments(t, configdocument.TOML, original, rendered.Document())

			restored, failure := codec.Restore(rendered.Document(), before)
			if failure != nil || !read(t, codec, restored.Document(), selection).Equal(before) {
				t.Fatalf("root insertion recovery lost its actual before values: %v", failure)
			}
			want := []byte(source)
			if source == "# File settings" {
				want = append(want, '\n')
			}
			if !bytes.Equal(restored.Document().Content(), want) {
				t.Fatalf("Restore changed unrelated bytes: got=%q want=%q", restored.Document().Content(), want)
			}
		})
	}
}
