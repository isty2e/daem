package configdocument_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
)

func TestRenderBudgetFollowsConcreteEdits(t *testing.T) {
	for _, fixture := range []struct {
		name, original, selected string
		syntax                   configdocument.Syntax
		maximum                  int64
	}{
		{"bare TOML key", "", "/a", configdocument.TOML, 9},
		{"retained TOML literal", "[prefs]\nnotes = '" + strings.Repeat(`\`, 64) + "'\nenabled = false\n", "/prefs/enabled", configdocument.TOML, 0},
		{"retained JSON numbers", "{\"a\":10,\"c\":10,\"d\":10,\"enabled\":false}\n", "/enabled", configdocument.JSON, 0},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			maximum := fixture.maximum
			if maximum == 0 {
				maximum = int64(len(fixture.original))
			}
			limits := configdocument.Limits{MaximumBytes: maximum, MaximumDepth: 16}
			document, err := configdocument.Parse(t.Context(), []byte(fixture.original), fixture.syntax, limits)
			if err != nil {
				t.Fatal(err)
			}
			value := lookup(t, parse(t, configdocument.JSON, `{"value":true}`), "/value")
			result, err := configdocument.Edit(t.Context(), document, []configdocument.Change{set(t, fixture.selected, value)})
			if err != nil {
				t.Fatalf("bounded concrete edit was refused: %v", err)
			}
			assertEqual(t, lookup(t, result, fixture.selected), value)
			if int64(len(result.Bytes())) > maximum || !bytes.Equal(document.Bytes(), []byte(fixture.original)) {
				t.Fatal("edit exceeded its byte ceiling or mutated its input")
			}
			if fixture.syntax == configdocument.TOML && fixture.selected == "/prefs/enabled" {
				assertEqual(t, lookup(t, document, "/prefs/notes"), lookup(t, result, "/prefs/notes"))
			}
			if fixture.syntax == configdocument.JSON {
				assertEqual(t, lookup(t, document, "/a"), lookup(t, result, "/a"))
				assertEqual(t, lookup(t, document, "/c"), lookup(t, result, "/c"))
				assertEqual(t, lookup(t, document, "/d"), lookup(t, result, "/d"))
			}
		})
	}
}

func TestDisplayRefusesExpandedLiteralWithoutPartialText(t *testing.T) {
	value := lookup(t, parse(t, configdocument.TOML, `value = "`+strings.Repeat(`\u007f`, 20)+`"`), "/value")
	limits := configdocument.Limits{MaximumBytes: 64, MaximumDepth: 16}
	canonical, err := value.CanonicalContext(t.Context(), limits)
	if err != nil {
		t.Fatal(err)
	}
	text, err := configdocument.DisplayValue(t.Context(), configdocument.TOML, canonical, limits)
	if !errors.Is(err, configdocument.ErrLimitExceeded) || text != "" {
		t.Fatal("expanded display literal escaped its output budget")
	}
}

func TestEditSharesRenderBudgetAcrossValues(t *testing.T) {
	value := lookup(t, parse(t, configdocument.JSON, `{"value":"`+strings.Repeat("x", 40)+`"}`), "/value")
	for _, syntax := range []configdocument.Syntax{configdocument.JSON, configdocument.TOML} {
		t.Run(syntax.String(), func(t *testing.T) {
			document, err := configdocument.Absent(syntax, configdocument.Limits{MaximumBytes: 64, MaximumDepth: 16})
			if err != nil {
				t.Fatal(err)
			}
			result, err := configdocument.Edit(t.Context(), document, []configdocument.Change{set(t, "/first", value), set(t, "/second", value)})
			if !errors.Is(err, configdocument.ErrLimitExceeded) || result.Exists() || len(result.Bytes()) != 0 {
				t.Fatal("multiple values escaped one operation's ceiling or returned partial output")
			}
		})
	}
}

func TestExactEditBudgetAccountsForSyntaxAndComments(t *testing.T) {
	value := lookup(t, parse(t, configdocument.JSON, `{"value":true}`), "/value")
	for _, fixture := range []struct {
		syntax             configdocument.Syntax
		original, selected string
	}{
		{configdocument.JSON, `{}`, "/a~1~0"},
		{configdocument.JSONC, "{/* keep */}\n", "/new"},
		{configdocument.JSONLineComments, "{\n// keep\n}\n", "/new"},
		{configdocument.TOML, "# heading", "/new"},
		{configdocument.TOML, "# heading\r\n[prefs]\r\n", "/prefs/new"},
		{configdocument.TOML, "[prefs]", "/prefs/new"},
		{configdocument.TOML, "", "/a/b/new"},
		{configdocument.TOML, "prefs = { enabled = false, notes = '" + strings.Repeat(`\`, 32) + "' }\n", "/prefs/enabled"},
		{configdocument.TOML, "prefs = { enabled = [false, # retained\n true] }\n", "/prefs/enabled"},
	} {
		t.Run(fixture.syntax.String()+fixture.selected+fixture.original, func(t *testing.T) {
			changes := []configdocument.Change{set(t, fixture.selected, value)}
			original := parse(t, fixture.syntax, fixture.original)
			want := edit(t, original, changes...)
			maximum := max(len(fixture.original), len(want.Bytes()))
			limits := configdocument.Limits{MaximumBytes: int64(maximum), MaximumDepth: 16}
			bounded, err := configdocument.Parse(t.Context(), []byte(fixture.original), fixture.syntax, limits)
			if err != nil {
				t.Fatal(err)
			}
			got, err := configdocument.Edit(t.Context(), bounded, changes)
			if err != nil || !bytes.Equal(got.Bytes(), want.Bytes()) {
				t.Fatalf("exact concrete-output allowance changed admission: %v", err)
			}
			if len(want.Bytes()) > len(fixture.original) {
				limits.MaximumBytes--
				bounded, err := configdocument.Parse(t.Context(), []byte(fixture.original), fixture.syntax, limits)
				if err != nil {
					t.Fatal(err)
				}
				got, err := configdocument.Edit(t.Context(), bounded, changes)
				if !errors.Is(err, configdocument.ErrLimitExceeded) || len(got.Bytes()) != 0 {
					t.Fatal("too-small output allowance returned partial syntax")
				}
			}
		})
	}
}
