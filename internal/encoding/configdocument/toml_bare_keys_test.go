package configdocument_test

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
)

func TestTOMLGeneratedKeysUseBareSegmentsAndQuoteRequiredNames(t *testing.T) {
	for _, test := range []struct {
		name     string
		key      string
		rendered string
	}{
		{"simple", "alpha", "alpha"},
		{"numeric", "123", "123"},
		{"boolean-looking", "true", "true"},
		{"underscore-and-hyphen", "part_9-x", "part_9-x"},
		{"empty", "", `""`},
		{"dot", "a.b", `"a.b"`},
		{"slash", "a/b", `"a/b"`},
		{"tilde", "~", `"~"`},
		{"unicode", "café", `"café"`},
		{"quotes", `say"hi`, `"say\"hi"`},
		{"control", "tab\tkey", `"tab\tkey"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			pointerSegment := strings.ReplaceAll(test.key, "~", "~0")
			pointerSegment = strings.ReplaceAll(pointerSegment, "/", "~1")
			value := lookup(t, parse(t, configdocument.TOML, "v = true\n"), "/v")

			t.Run("assignment", func(t *testing.T) {
				const source = "[assignments] # selected\r\n[other] # untouched\r\nkeep = 2 # keep\r\n"
				original := parse(t, configdocument.TOML, source)
				pointer := "/assignments/" + pointerSegment
				result := edit(t, original, set(t, pointer, value))
				want := "[assignments] # selected\r\n" + test.rendered + " = true\r\n" +
					"[other] # untouched\r\nkeep = 2 # keep\r\n"
				if string(result.Bytes()) != want {
					t.Fatalf("generated assignment key changed unexpectedly:\n got %q\nwant %q", result.Bytes(), want)
				}
				reparsed := parse(t, configdocument.TOML, string(result.Bytes()))
				assertEqual(t, lookup(t, reparsed, pointer), value)
				assertComments(t, original, reparsed)
			})

			t.Run("table-header", func(t *testing.T) {
				const source = "# intro\r\n[other] # untouched\r\nkeep = 2 # keep\r\n"
				original := parse(t, configdocument.TOML, source)
				empty := lookup(t, parse(t, configdocument.TOML, "v = {}\n"), "/v")
				pointer := "/tables/" + pointerSegment
				result := edit(t, original, set(t, pointer, empty))
				want := source + "[tables." + test.rendered + "]\r\n"
				if string(result.Bytes()) != want {
					t.Fatalf("generated table header changed unexpectedly:\n got %q\nwant %q", result.Bytes(), want)
				}
				reparsed := parse(t, configdocument.TOML, string(result.Bytes()))
				assertEqual(t, lookup(t, reparsed, pointer), empty)
				assertComments(t, original, reparsed)
			})

			t.Run("inline-object", func(t *testing.T) {
				const source = "inline = {} # inline retained\r\n[other] # untouched\r\nkeep = 2 # keep\r\n"
				original := parse(t, configdocument.TOML, source)
				pointer := "/inline/" + pointerSegment
				result := edit(t, original, set(t, pointer, value))
				want := "inline = {" + test.rendered + " = true} # inline retained\r\n" +
					"[other] # untouched\r\nkeep = 2 # keep\r\n"
				if string(result.Bytes()) != want {
					t.Fatalf("generated inline key changed unexpectedly:\n got %q\nwant %q", result.Bytes(), want)
				}
				reparsed := parse(t, configdocument.TOML, string(result.Bytes()))
				assertEqual(t, lookup(t, reparsed, pointer), value)
				assertComments(t, original, reparsed)
			})
		})
	}
}
