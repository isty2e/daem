package configdocument_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
)

func TestLineCommentsAcceptCRLFCRAndEOF(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		input := "// before" + ending + "{\"x\":1} // EOF"
		document := parse(t, configdocument.JSONLineComments, input)
		if want := []string{"// before", "// EOF"}; !slices.Equal(document.Comments(), want) {
			t.Fatalf("actual comments: %q", document.Comments())
		}
		value := lookup(t, parse(t, configdocument.JSON, `{"v":true}`), "/v")
		result := edit(t, document, set(t, "/new", value))
		assertComments(t, document, result)
		assertEqual(t, value, lookup(t, result, "/new"))
	}
}

func TestAllActualCommentsAreCountedWithoutStringMarkers(t *testing.T) {
	json := parse(t, configdocument.JSONC, "// same\n{\"x\":\"// fake /* fake */\", /* block */ \"y\": [// same\n1]} // end")
	if want := []string{"// same", "/* block */", "// same", "// end"}; !slices.Equal(json.Comments(), want) {
		t.Fatalf("JSON comments: %q", json.Comments())
	}
	toml := parse(t, configdocument.TOML, "# same\nx = {a = '''# fake''', # same\nb = 1} # end\n")
	if want := []string{"# same", "# same", "# end"}; !slices.Equal(toml.Comments(), want) {
		t.Fatalf("TOML comments: %q", toml.Comments())
	}
}

func TestTOML11EscapesInValuesAndKeys(t *testing.T) {
	document := parse(t, configdocument.TOML, "\"a\\x2fb\" = \"\\e\\x41\"\n")
	json := parse(t, configdocument.JSON, `{"v":"\u001bA"}`)
	assertEqual(t, lookup(t, document, "/a~1b"), lookup(t, json, "/v"))
	result := edit(t, document, set(t, "/new", lookup(t, json, "/v")))
	assertEqual(t, lookup(t, result, "/new"), lookup(t, json, "/v"))
}

func TestTOMLRootInsertionPreservesByteOrderMark(t *testing.T) {
	value := lookup(t, parse(t, configdocument.JSON, `{"v":true}`), "/v")
	for _, input := range []string{"\ufeff[tui]\nkeep=1\n", "\ufeffkeep=1\n", "\ufeff", "\ufeff# leading comment"} {
		document := parse(t, configdocument.TOML, input)
		result := edit(t, document, set(t, "/new", value))
		if !strings.HasPrefix(string(result.Bytes()), "\ufeff") {
			t.Fatal("root insertion moved the byte order mark", string(result.Bytes()))
		}
		assertEqual(t, value, lookup(t, result, "/new"))
		assertComments(t, document, result)
	}
}

func TestEditsRejectOversizedResultsAndPathsBeforeRendering(t *testing.T) {
	value := lookup(t, parse(t, configdocument.JSON, `{"v":"`+strings.Repeat("x", 1000)+`"}`), "/v")
	for _, syntax := range []configdocument.Syntax{configdocument.JSON, configdocument.TOML} {
		input := "{}"
		if syntax == configdocument.TOML {
			input = ""
		}
		document, err := configdocument.Parse(context.Background(), []byte(input), syntax, configdocument.Limits{MaximumBytes: 64, MaximumDepth: 4})
		if err != nil {
			t.Fatal(err)
		}
		for _, change := range []configdocument.Change{
			set(t, "/oversized", value),
			set(t, strings.Repeat("/deep", 10000), value),
		} {
			if _, err := configdocument.Edit(context.Background(), document, []configdocument.Change{change}); !errors.Is(err, configdocument.ErrLimitExceeded) {
				t.Fatalf("%s result limit: %v", syntax, err)
			}
			if string(document.Bytes()) != input {
				t.Fatal("failed edit mutated input")
			}
		}
	}
}

func TestNoOpAtByteLimitAndUnselectedTOMLDoesNotConsumeRenderingBudget(t *testing.T) {
	input := `{"x":1}`
	document, err := configdocument.Parse(context.Background(), []byte(input), configdocument.JSON, configdocument.Limits{MaximumBytes: int64(len(input)), MaximumDepth: 4})
	if err != nil {
		t.Fatal(err)
	}
	result := edit(t, document, set(t, "/x", lookup(t, document, "/x")))
	if string(result.Bytes()) != input {
		t.Fatal("no-op reserialized source")
	}

	var toml strings.Builder
	toml.WriteString("change = true\n[unselected]\n")
	for index := range 100 {
		fmt.Fprintf(&toml, "key%d = 0\n", index)
	}
	content := toml.String()
	document, err = configdocument.Parse(context.Background(), []byte(content), configdocument.TOML, configdocument.Limits{MaximumBytes: int64(len(content) + 8), MaximumDepth: 4})
	if err != nil {
		t.Fatal(err)
	}
	value := lookup(t, parse(t, configdocument.TOML, "v = false"), "/v")
	result = edit(t, document, set(t, "/change", value))
	assertEqual(t, value, lookup(t, result, "/change"))
	assertEqual(t, lookup(t, document, "/unselected"), lookup(t, result, "/unselected"))
}

func TestManySiblingChangesAreAppliedTogether(t *testing.T) {
	for _, syntax := range []configdocument.Syntax{configdocument.JSON, configdocument.TOML} {
		var content strings.Builder
		if syntax == configdocument.JSON {
			content.WriteByte('{')
		}
		for index := range 1024 {
			if syntax == configdocument.JSON {
				if index > 0 {
					content.WriteByte(',')
				}
				fmt.Fprintf(&content, "\"key%d\":false", index)
			} else {
				fmt.Fprintf(&content, "key%d = false\n", index)
			}
		}
		if syntax == configdocument.JSON {
			content.WriteByte('}')
		}
		document := parse(t, syntax, content.String())
		value := lookup(t, parse(t, configdocument.JSON, `{"v":true}`), "/v")
		var changes []configdocument.Change
		for index := range 1024 {
			pointer := fmt.Sprintf("/key%d", index)
			if index%2 == 0 {
				changes = append(changes, set(t, pointer, value))
			} else {
				changes = append(changes, remove(t, pointer))
			}
		}
		result := edit(t, document, changes...)
		entries, err := result.Leaves(t.Context(), testLimits.MaximumBytes)
		if err != nil || len(entries) != 512 {
			t.Fatalf("sibling batch: leaves=%d, err=%v", len(entries), err)
		}
		for _, entry := range entries {
			assertEqual(t, value, entry.Value)
		}
	}
}
