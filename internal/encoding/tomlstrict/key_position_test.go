package tomlstrict

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestKeyPositionFollowsDecoderKeysAndFirstOccurrences(t *testing.T) {
	for _, test := range []struct {
		name    string
		content string
		key     toml.Key
		line    int
		column  int
	}{
		{"root", "a=1\n  b=2\n", toml.Key{"b"}, 2, 3},
		{"dotted assignment", "  a.b=1\n", toml.Key{"a", "b"}, 1, 3},
		{"header", "\t[a.b]\n c=1\n", toml.Key{"a", "b"}, 1, 3},
		{"header child", "\t[a.b]\n c=1\n", toml.Key{"a", "b", "c"}, 2, 2},
		{"inline parent", "a = { b = 1, c = { d = 2 } }\n", toml.Key{"a"}, 1, 1},
		{"inline child", "a = { b = 1, c = { d = 2 } }\n", toml.Key{"a", "b"}, 1, 7},
		{"nested inline parent", "a = { b = 1, c = { d = 2 } }\n", toml.Key{"a", "c"}, 1, 14},
		{"nested inline child", "a = { b = 1, c = { d = 2 } }\n", toml.Key{"a", "c", "d"}, 1, 20},
		{"array inline first", "a = [{ b = 1 }, { b = 2 }]\n", toml.Key{"a", "b"}, 1, 8},
		{"array header first", "[[rows]]\n k=1\n[[rows]]\n k=2\n", toml.Key{"rows"}, 1, 3},
		{"array child first", "[[rows]]\n k=1\n[[rows]]\n k=2\n", toml.Key{"rows", "k"}, 2, 2},
		{"quoted literal dot", "\"a.b\"=1\na.b=2\n", toml.Key{"a.b"}, 1, 1},
		{"unquoted path dot", "\"a.b\"=1\na.b=2\n", toml.Key{"a", "b"}, 2, 1},
		{"quoted escape", "\t\"\\u0061\"=1\n", toml.Key{"a"}, 1, 2},
		{"empty key", "\"\"=1\n", toml.Key{""}, 1, 1},
		{"BOM root", "\ufeffa={ b=\"가\", c=1 }\r\n", toml.Key{"a"}, 1, 1},
		{"rune column", "\ufeffa={ b=\"가\", c=1 }\r\n", toml.Key{"a", "c"}, 1, 12},
		{"CRLF second line", "\ufeffa=1\r\n\tb=2\r\n", toml.Key{"b"}, 2, 2},
		{"string and comment decoys", "# real=0\ntext=\"\"\"\n[real]\nreal=1\n\"\"\"\nreal=3\n", toml.Key{"real"}, 6, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			metadata := decodePositionMetadata(t, test.content)
			line, column, ok := KeyPosition([]byte(test.content), metadata, test.key)
			if !ok || line != test.line || column != test.column {
				t.Fatalf("key=%q position=(%d,%d,%t), want (%d,%d,true)", test.key, line, column, ok, test.line, test.column)
			}
		})
	}
}

func TestKeyPositionDoesNotClaimIncompleteObservations(t *testing.T) {
	metadata := decodePositionMetadata(t, "a=1\n")
	for _, test := range []struct {
		name    string
		content string
		key     toml.Key
	}{
		{"missing key", "a=1\n", toml.Key{"missing"}},
		{"empty path", "a=1\n", nil},
		{"metadata key count mismatch", "a=1\nb=2\n", toml.Key{"a"}},
		{"later malformed input", "a=1\nbroken=[\n", toml.Key{"a"}},
		{"later invalid UTF8", "a=1\n\xff", toml.Key{"a"}},
		{"later limit failure", "a=1\nb=" + strings.Repeat("[", MaximumDepth+1) + "0" + strings.Repeat("]", MaximumDepth+1), toml.Key{"a"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if line, column, ok := KeyPosition([]byte(test.content), metadata, test.key); ok || line != 0 || column != 0 {
				t.Fatalf("claimed incomplete position=(%d,%d,%t)", line, column, ok)
			}
		})
	}
}

func decodePositionMetadata(t *testing.T, content string) toml.MetaData {
	t.Helper()
	var target struct{}
	metadata, err := toml.Decode(content, &target)
	if err != nil {
		t.Fatalf("invalid key-position fixture: %v", err)
	}
	return metadata
}
