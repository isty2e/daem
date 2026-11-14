package tomlsyntax_test

import (
	"slices"
	"strings"
	"testing"

	tomlsyntax "github.com/isty2e/daem/internal/encoding/toml"
)

func TestInlineTableIndexRetainsCompleteMemberSpansAndNoArrayAddresses(t *testing.T) {
	content := []byte("\ufeff['profile.dot']\r\nbox = { 'a/b' = { keep = 'literal', enabled = [false, # inside\ntrue] }, dotted.key = 1, arr = [{ nested = { x = 2 } }] }\r\n" +
		"[[items]]\nsettings = { x = 1 }\n[items.extra]\nnested = { v = 2 }\n")
	index, err := tomlsyntax.IndexDocument(t.Context(), content)
	if err != nil {
		t.Fatal(err)
	}
	tables := index.InlineTables()
	if len(tables) != 2 {
		t.Fatal("array elements acquired native key addresses", len(tables))
	}
	for _, table := range tables {
		literal := string(content[table.Span.Start:table.Span.End])
		if !strings.HasPrefix(literal, "{") || !strings.HasSuffix(literal, "}") {
			t.Fatal("inline table span excludes its delimiters")
		}
		for _, entry := range table.Entries {
			if !slices.Equal(entry.Path[:len(table.Path)], table.Path) {
				t.Fatal("inline member changed native key scope")
			}
			value := append([]byte("v = "), content[entry.Value.Start:entry.Value.End]...)
			if _, err := tomlsyntax.Native(t.Context(), value); err != nil {
				t.Fatal("inline value span is not a complete native value", string(value), err)
			}
		}
	}
	want := slices.Clone(tables[0].Path)
	wantEntry := slices.Clone(tables[0].Entries[0].Path)
	tables[0].Path[0] = "changed"
	tables[0].Entries[0].Path[0] = "changed"
	again := index.InlineTables()
	if !slices.Equal(again[0].Path, want) || !slices.Equal(again[0].Entries[0].Path, wantEntry) {
		t.Fatal("inline source facts escaped mutable storage")
	}
}
