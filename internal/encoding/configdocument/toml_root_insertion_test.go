package configdocument_test

import (
	"context"
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
)

func TestTOMLRootInsertionKeepsLeadingTrivia(t *testing.T) {
	for _, test := range []struct{ name, prefix, body, newline, separator string }{
		{"existing roots", "# File settings\n\n", "keep=0x1f # keep\n\n# UI\n[tui]\ntheme='light'\n", "\n", ""},
		{"first table", "# File settings\n\n", "[tui]\ntheme='light'\n", "\n", ""},
		{"indented table", "# File settings\n\n", " \t[tui]\ntheme='light'\n", "\n", ""},
		{"indented root", "# File settings\n\n", " \tkeep=true # keep\n", "\n", ""},
		{"root at EOF", "# File settings\n", "keep=true # keep", "\n", ""},
		{"header at EOF", "# File settings\n", "[tui] # keep", "\n", ""},
		{"array table", "# File settings\n", "[[tools]] # tool\nname='keep'\n", "\n", ""},
		{"dotted root", "# File settings\n", "tui.theme='light'\n[profiles.fast]\nmodel='native'\n", "\n", ""},
		{"quoted names", "# File settings\n", "'a.b'=1\n['profiles'.'a/b']\n'~'=true\n", "\n", ""},
		{"multiline literal", "# File settings\n", "text='''# not a comment\n[tui] not a table\n'''\n[tui]\ntheme='light'\n", "\n", ""},
		{"multiline basic", "# File settings\n", "text=\"\"\"# not a comment\n[[tools]] not a table\n\"\"\"\n[tui]\ntheme='light'\n", "\n", ""},
		{"multiline inline", "# File settings\n", "inline={\n # inside\n keep=true,\n}\n[tui]\ntheme='light'\n", "\n", ""},
		{"CRLF", "# File settings\r\n\r\n", " \t[tui] # UI\r\ntheme='light'\r\n", "\r\n", ""},
		{"comment only", "# File settings\n\n", "", "\n", ""},
		{"comment at EOF", "# File settings", "", "\n", "\n"},
		{"comment with padding", "# File settings \t", "", "\n", "\n"},
		{"blank lines", "\n \t\n", "", "\n", ""},
		{"whitespace at EOF", " \t", "", "\n", "\n"},
		{"empty", "", "", "\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := parse(t, configdocument.TOML, test.prefix+test.body)
			value := lookup(t, parse(t, configdocument.TOML, "v=true\n"), "/v")
			current := original

			for range 3 {
				rendered := edit(t, current, set(t, "/model", value))
				want := test.prefix + test.separator + "model = true" + test.newline + test.body
				if string(rendered.Bytes()) != want {
					t.Fatalf("root insertion displaced leading trivia or unrelated bytes:\n got %q\nwant %q", rendered.Bytes(), want)
				}
				assertEqual(t, lookup(t, rendered, "/model"), value)
				assertComments(t, original, rendered)

				current = edit(t, rendered, remove(t, "/model"))
				if string(current.Bytes()) != test.prefix+test.separator+test.body {
					t.Fatalf("root roundtrip accumulated whitespace or changed original bytes: %q", current.Bytes())
				}
				assertEqual(t, lookup(t, current, ""), lookup(t, original, ""))
			}
		})
	}
}

func TestTOMLRootInsertionSurvivesFirstExpressionRemoval(t *testing.T) {
	const source = "# File settings\nold=true\n[tui]\nkeep=true\n"
	original := parse(t, configdocument.TOML, source)
	value := lookup(t, original, "/old")
	result := edit(t, original, remove(t, "/old"), set(t, "/model", value), set(t, "/features/added", value))

	assertEqual(t, lookup(t, result, "/model"), value)
	assertEqual(t, lookup(t, result, "/features/added"), value)
	assertEqual(t, lookup(t, result, "/tui/keep"), value)
	if _, present, err := result.Lookup(path(t, "/old")); err != nil || present {
		t.Fatalf("first-expression removal was lost: present=%t err=%v", present, err)
	}
	assertComments(t, original, result)
	result = edit(t, result, remove(t, "/model"), remove(t, "/features"))
	if string(result.Bytes()) != "# File settings\n[tui]\nkeep=true\n" {
		t.Fatalf("simultaneous root/section patches changed untouched bytes: %q", result.Bytes())
	}
}

func TestTOMLRootInsertionSeparatorHonorsBoundsAndCancellation(t *testing.T) {
	const source = "# File settings"
	const addition = "model = true\n"
	original, err := configdocument.Parse(t.Context(), []byte(source), configdocument.TOML, configdocument.Limits{
		MaximumBytes: int64(len(source) + len(addition)), MaximumDepth: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	value := lookup(t, parse(t, configdocument.TOML, "v=true\n"), "/v")
	changes := []configdocument.Change{set(t, "/model", value)}
	if _, err := configdocument.Edit(t.Context(), original, changes); !errors.Is(err, configdocument.ErrLimitExceeded) {
		t.Fatalf("required comment separator bypassed the document bound: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := configdocument.Edit(ctx, original, changes); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled root insertion was not refused: %v", err)
	}
	if string(original.Bytes()) != source {
		t.Fatalf("refused insertion changed the observed document: %q", original.Bytes())
	}
}
