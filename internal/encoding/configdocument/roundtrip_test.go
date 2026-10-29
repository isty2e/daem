package configdocument_test

import (
	"bytes"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
)

func TestEditRoundtripsReachAStableDocument(t *testing.T) {
	for _, test := range []struct {
		name    string
		syntax  configdocument.Syntax
		content string
	}{
		{"JSON multiline", configdocument.JSON, "{\n  \"model\": false,\n  \"outside\": {\"keep\": 1}\n}\n"},
		{"JSON missing final newline", configdocument.JSON, `{"model":false,"outside":{"keep":1}}`},
		{"JSON existing blank lines", configdocument.JSON, "{\"model\":false,\"outside\":{\"keep\":1}}\n\n\n"},
		{"JSONC EOF comment", configdocument.JSONC, "// before\n{\"model\":false,\"outside\":{\"keep\":1},} // EOF"},
		{"JSON line comments CRLF", configdocument.JSONLineComments, "// before\r\n{\"model\":false,\"outside\":{\"keep\":1}} // EOF\r\n"},
		{"JSON line comments CR", configdocument.JSONLineComments, "// before\r{\"model\":false,\"outside\":{\"keep\":1}} // EOF\r"},
		{"TOML statements", configdocument.TOML, "model=false\noutside={keep=1}\n"},
		{"TOML comments and blank lines", configdocument.TOML, "# before\nmodel=false # inline\n\noutside={keep=1}\n\n"},
		{"TOML CRLF", configdocument.TOML, "model=false\r\noutside={keep=1}\r\n\r\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := parse(t, test.syntax, test.content)
			current := original
			value := lookup(t, parse(t, configdocument.JSON, `{"v":true}`), "/v")
			var settled []byte
			var lengths []int
			stable := true

			for cycle := range 6 {
				current = edit(t, current, set(t, "/added", value))
				assertEqual(t, lookup(t, current, "/added"), value)
				current = edit(t, current, remove(t, "/added"))
				if _, present, err := current.Lookup(path(t, "/added")); err != nil || present {
					t.Fatalf("removed key remains: %v", err)
				}
				current = edit(t, current, set(t, "/model", value))
				current = edit(t, current, set(t, "/model", lookup(t, original, "/model")))

				assertEqual(t, lookup(t, current, "/outside"), lookup(t, original, "/outside"))
				assertEqual(t, lookup(t, current, "/model"), lookup(t, original, "/model"))
				assertComments(t, original, current)
				content := current.Bytes()
				lengths = append(lengths, len(content))
				if cycle == 0 {
					settled = content
				} else if !bytes.Equal(content, settled) {
					stable = false
				}
			}

			if !stable {
				t.Fatalf("repeated add/remove and value roundtrips changed bytes: lengths=%v", lengths)
			}
			if !bytes.Equal(original.Bytes(), []byte(test.content)) {
				t.Fatal("editing mutated the original observation")
			}
		})
	}
}

func TestJSONEditsPreserveExistingFinalWhitespace(t *testing.T) {
	for _, syntax := range []configdocument.Syntax{configdocument.JSON, configdocument.JSONC, configdocument.JSONLineComments} {
		for _, test := range []struct {
			name, ending string
		}{
			{"one LF", "\n"},
			{"existing blank lines", "\n\n\n"},
			{"CRLF", "\r\n\r\n"},
			{"spaces and tabs", "\n \t\n \t\n"},
		} {
			t.Run(syntax.String()+"/"+test.name, func(t *testing.T) {
				original := parse(t, syntax, `{"model":false}`+test.ending)
				value := lookup(t, parse(t, configdocument.JSON, `{"v":true}`), "/v")
				result := edit(t, original, set(t, "/model", value))
				content := result.Bytes()
				closing := bytes.LastIndexByte(content, '}')
				if closing < 0 || string(content[closing+1:]) != test.ending {
					t.Fatalf("existing final whitespace changed: got=%q want=%q", content, test.ending)
				}
			})
		}
	}
}

func TestTOMLRemovalKeepsUnselectedWhitespaceAndComments(t *testing.T) {
	for _, test := range []struct {
		name, content, remaining string
	}{
		{"LF", "removed=1 \t\n\n# keep\nkept=2\n\n", "\n# keep\nkept=2\n\n"},
		{"CRLF", "removed=1 \t\r\n\r\n# keep\r\nkept=2\r\n\r\n", "\r\n# keep\r\nkept=2\r\n\r\n"},
		{"inline comment", "removed=1 # keep\n\nkept=2\n", " # keep\n\nkept=2\n"},
		{"leading blank lines", "\n\nremoved=1\nkept=2\n", "\n\nkept=2\n"},
		{"EOF padding", "removed=1 \t", " \t"},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := parse(t, configdocument.TOML, test.content)
			result := edit(t, original, remove(t, "/removed"))
			if string(result.Bytes()) != test.remaining {
				t.Fatalf("removal changed unselected whitespace/comments: got=%q want=%q", result.Bytes(), test.remaining)
			}
			assertComments(t, original, result)
			if _, present, err := result.Lookup(path(t, "/removed")); err != nil || present {
				t.Fatalf("selected statement was not removed: %v", err)
			}
		})
	}
}
