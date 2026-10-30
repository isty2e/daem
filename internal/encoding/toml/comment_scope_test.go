package tomlsyntax_test

import (
	"slices"
	"testing"

	tomlsyntax "github.com/isty2e/daem/internal/encoding/toml"
)

func TestScopedCommentsUseContainingNativeKeys(t *testing.T) {
	content := []byte("# root\n[plugins.chosen] # header\n# table\nenabled=true # value\n[ui]\ncolor=true # keep\n")
	index, err := tomlsyntax.IndexDocument(t.Context(), content)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"# root": nil, "# header": {"plugins", "chosen"}, "# table": {"plugins", "chosen"},
		"# value": {"plugins", "chosen", "enabled"}, "# keep": {"ui", "color"},
	}
	for _, comment := range index.ScopedComments() {
		text := string(content[comment.Span.Start:comment.Span.End])
		t.Logf("%q: %q", text, comment.Path)
		path, present := want[text]
		if !present || !slices.Equal(path, comment.Path) {
			t.Errorf("comment %q scope=%q want=%q", text, comment.Path, path)
		}
		delete(want, text)
	}
	if len(want) != 0 {
		t.Fatal("comments disappeared", want)
	}
	if t.Failed() {
		t.FailNow()
	}
	comments := index.ScopedComments()
	comments[1].Path[0] = "changed"
	if slices.Equal(comments[1].Path, index.ScopedComments()[1].Path) {
		t.Fatal("scoped comments expose mutable index data")
	}
}
