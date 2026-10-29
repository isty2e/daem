package configdocument_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
)

func TestTOMLQuotedCharactersRoundTripKeysAndValues(t *testing.T) {
	characters := make([]rune, 0, 38)
	for character := range rune(0x20) {
		characters = append(characters, character)
	}
	characters = append(characters, '"', '\\', 0x7f, 0x80, 0x2028, 0x1f642)
	for _, character := range characters {
		t.Run(fmt.Sprintf("U+%04X", character), func(t *testing.T) {
			escaped := fmt.Sprintf("\\u%04x", character)
			if character > 0xffff {
				escaped = fmt.Sprintf("\\U%08x", character)
			}
			key, err := output.NewKeyPath(string(character))
			if err != nil {
				t.Fatal(err)
			}
			table, err := output.NewKeyPath("tables", string(character))
			if err != nil {
				t.Fatal(err)
			}
			for _, test := range []struct {
				name, source, sourcePointer, targetPointer, existing string
			}{
				{"new string", "selected = \"" + escaped + "\"\n", "/selected", "/selected", ""},
				{"replace string", "selected = \"" + escaped + "\"\n", "/selected", "/selected", "selected = false\n"},
				{"quoted key", "\"" + escaped + "\" = true\n", key.String(), key.String(), ""},
				{"table header", "selected = {}\n", "/selected", table.String(), ""},
				{"nested strings and keys", "selected = {\"" + escaped + "\" = \"" + escaped + "\", nested = [\"" + escaped + "\"]}\n", "/selected", "/selected", "selected = {}\n"},
			} {
				t.Run(test.name, func(t *testing.T) {
					desired := lookup(t, parse(t, configdocument.TOML, test.source), test.sourcePointer)
					original := parse(t, configdocument.TOML, "# user header\n"+test.existing+"keep = {flag = true} # user value\n")
					result := edit(t, original, set(t, test.targetPointer, desired))
					reparsed := parse(t, configdocument.TOML, string(result.Bytes()))
					assertEqual(t, lookup(t, reparsed, test.targetPointer), desired)
					assertEqual(t, lookup(t, reparsed, "/keep"), lookup(t, original, "/keep"))
					assertComments(t, original, reparsed)
				})
			}
		})
	}
}

func TestTOMLQuotedEscapesRespectDocumentButNotJSONEncodingLimits(t *testing.T) {
	desired := lookup(t, parse(t, configdocument.TOML, "selected = \""+strings.Repeat("\\u007f", 20)+"\"\n"), "/selected")
	limits := configdocument.Limits{MaximumBytes: 64, MaximumDepth: 32}
	const content = "# user header\nkeep = true\n"
	original, err := configdocument.Parse(t.Context(), []byte(content), configdocument.TOML, limits)
	if err != nil {
		t.Fatal(err)
	}
	result, err := configdocument.Edit(t.Context(), original, []configdocument.Change{set(t, "/selected", desired)})
	if !errors.Is(err, configdocument.ErrLimitExceeded) || result.Exists() || len(result.Bytes()) != 0 {
		t.Fatalf("oversize escaped TOML returned a partial document or wrong refusal: %v", err)
	}
	if string(original.Bytes()) != content {
		t.Fatal("refused edit modified the observed document")
	}
	canonical, err := desired.CanonicalContext(t.Context(), limits)
	if err != nil {
		t.Fatalf("TOML-only escape cost changed private JSON encoding: %v", err)
	}
	decoded, err := configdocument.DecodeValue(t.Context(), configdocument.TOML, canonical, limits)
	if err != nil || !decoded.Equal(desired) {
		t.Fatalf("private value encoding lost the TOML string: %v", err)
	}
}
