package configdocument_test

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
)

func TestTOMLInlineEditsPreserveAuthoredSiblingMembers(t *testing.T) {
	for _, test := range []struct {
		name, source, expected, kept string
		setPath, setValue, remove    string
	}{
		{
			"nested", "box = { inner = { a = 0x1f, keep = 'literal' }, other = 'x,}#=' }\n",
			"box = { inner = { a = true, keep = 'literal' }, other = 'x,}#=' }\n", "keep = 'literal'",
			"/box/inner/a", "true", "",
		},
		{
			"dotted mixed", "box = { a.b = 1, a.c = 'keep', z = 0x1f }\n",
			"box = { a.d = true, a.c = 'keep', z = 31 }\n", "a.c = 'keep'",
			"/box/a/d", "true", "/box/a/b",
		},
		{
			"implicit to scalar", "box = { a.b = 1, a.c = 2, z = 'keep' }\n",
			"box = { a = false, z = 'keep' }\n", "z = 'keep'",
			"/box/a", "false", "",
		},
		{
			"remove first", "box = { a = 1, b = 'keep', c = 3 }\n",
			"box = { b = 'keep', c = 3 }\n", "b = 'keep'",
			"", "", "/box/a",
		},
		{
			"remove middle", "box = { a = 1, b = 2, c = 'keep' }\n",
			"box = { a = 1, c = 'keep' }\n", "c = 'keep'",
			"", "", "/box/b",
		},
		{
			"remove last CRLF", "box = { a = 'keep', b = 2, } # after\r\n",
			"box = { a = 'keep' } # after\r\n", "a = 'keep'",
			"", "", "/box/b",
		},
		{
			"remove dotted parent", "box = { a.b = 1, a.c = 2, z = 0x1f }\n",
			"box = { z = 31 }\n", "z = 0x1f",
			"", "", "/box/a",
		},
		{
			"removed nested comments", "box = {\n # parent\n gone = {\n # nested\n v = 1,\n }, keep = 'native',\n} # after\n",
			"box = { keep = 'native' }\n", "keep = 'native'",
			"", "", "/box/gone",
		},
		{
			"last member", "box = { a = 1 }\n", "box = {}\n", "",
			"", "", "/box/a",
		},
		{
			"quoted key", "box = { 'a.b' = { '' = false, keep = 'native' } }\n",
			"box = { 'a.b' = { '' = true, keep = 'native' } }\n", "keep = 'native'",
			"/box/a.b/", "true", "",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := parse(t, configdocument.TOML, test.source)
			var changes []configdocument.Change
			if test.setPath != "" {
				value := lookup(t, parse(t, configdocument.TOML, "v="+test.setValue), "/v")
				changes = append(changes, set(t, test.setPath, value))
			}
			if test.remove != "" {
				changes = append(changes, remove(t, test.remove))
			}
			actual := edit(t, original, changes...)
			expected := parse(t, configdocument.TOML, test.expected)
			assertEqual(t, lookup(t, actual, ""), lookup(t, expected, ""))
			assertComments(t, original, actual)
			if test.kept != "" && !strings.Contains(string(actual.Bytes()), test.kept) {
				t.Fatal("inline edit changed an unselected native member", string(actual.Bytes()))
			}
		})
	}
}

func TestTOMLInlineEditsMoveDisplacedCommentsBeforeContainingAssignment(t *testing.T) {
	for _, test := range []struct {
		name, path, value, expected string
	}{
		{"remove member", "/mcp_servers/mine", "", "manual = { command = 'keep' }"},
		{"replace member", "/mcp_servers/mine", "false", "manual = { command = 'keep' }, mine = false"},
		{"replace nested array", "/mcp_servers/mine/args", "[true]", "manual = { command = 'keep' }, mine = { args = [true] }"},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := parse(t, configdocument.TOML, "# header\nmcp_servers = { manual = { command = 'keep' }, mine = { args = [\n 'a', # displaced\n] } }\ntail = 'unchanged'\n")
			change := remove(t, test.path)
			if test.value != "" {
				change = set(t, test.path, lookup(t, parse(t, configdocument.TOML, "v="+test.value), "/v"))
			}
			actual := edit(t, original, change)
			expected := parse(t, configdocument.TOML, "mcp_servers = { "+test.expected+" }\ntail = 'unchanged'\n")
			assertEqual(t, lookup(t, actual, ""), lookup(t, expected, ""))
			assertComments(t, original, actual)
			text := string(actual.Bytes())
			if strings.Index(text, "# displaced") > strings.Index(text, "mcp_servers =") {
				t.Fatal("displaced comment introduced standalone inline-table trivia", text)
			}
			if !strings.Contains(text, "manual = { command = 'keep' }") || !strings.HasSuffix(text, "tail = 'unchanged'\n") {
				t.Fatal("inline edit rewrote an unchanged native value", text)
			}
		})
	}
}
