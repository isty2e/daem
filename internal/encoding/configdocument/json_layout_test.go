package configdocument_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
)

func TestJSONLayoutFollowsOriginalObject(t *testing.T) {
	for _, fixture := range []struct {
		name, source, selected, want string
		syntax                       configdocument.Syntax
	}{
		{"compact", `{"a":1}`, "/b", "{\"a\":1,\"b\":true}\n", configdocument.JSON},
		{"padded", `{ "a" : 1 }`, "/b", "{ \"a\" : 1, \"b\" : true }\n", configdocument.JSON},
		{"four spaces", "{\n    \"a\": 1\n}\n", "/b", "{\n    \"a\": 1,\n    \"b\": true\n}\n", configdocument.JSON},
		{"last mixed indent", "{\n  \"a\": 1,\n\t\"c\": 2\n}\n", "/b", "{\n  \"a\": 1,\n\t\"c\": 2,\n\t\"b\": true\n}\n", configdocument.JSON},
		{"root empty", `{}`, "/b", "{\n  \"b\": true\n}\n", configdocument.JSON},
		{"root empty CRLF", "{}\r\n", "/b", "{\r\n  \"b\": true\r\n}\r\n", configdocument.JSON},
		{"padded empty", `{  }`, "/b", "{  \"b\":true  }\n", configdocument.JSON},
		{"empty multiline", "{\r\n}\r\n", "/b", "{\r\n  \"b\": true\r\n}\r\n", configdocument.JSON},
		{"inline child", "{\n  \"a\": {\"x\":1}\n}\n", "/a/b", "{\n  \"a\": {\"x\":1,\"b\":true}\n}\n", configdocument.JSON},
		{"empty child inherits", "{\n  \"a\": {}\n}\n", "/a/b", "{\n  \"a\": {\n    \"b\": true\n  }\n}\n", configdocument.JSON},
		{"nested newline is not mode", "{\"a\":[1,\n2]}\n", "/b", "{\"a\":[1,\n2],\"b\":true}\n", configdocument.JSON},
		{"closing only is not mode", "{\"a\":1\n}\n", "/b", "{\"a\":1,\"b\":true\n}\n", configdocument.JSON},
		{"tail comment is not mode", "{\"a\":1 // tail\n}\n", "/b", "{\"a\":1,\"b\":true // tail\n}\n", configdocument.JSONC},
		{"comment body newline is not mode", "{/* a\nb */\"a\":1}\n", "/b", "{/* a\nb */\"a\":1,\"b\":true}\n", configdocument.JSONC},
		{"obscured indent", "{\n  /* before */ \"a\":1}\n", "/b", "{\n  /* before */ \"a\":1,\n  \"b\":true}\n", configdocument.JSONC},
		{"source trailing comma", "{\"a\":1,}\n", "/b", "{\"a\":1,\"b\":true,}\n", configdocument.JSONC},
		{"inherited comma", "{\"keep\":1,}\n", "/a/b", "{\"keep\":1,\"a\":{\"b\":true,},}\n", configdocument.JSONC},
		{"CRLF comments", "{\r\n  // before\r\n  \"a\": 1\r\n}\r\n", "/b", "{\r\n  // before\r\n  \"a\": 1,\r\n  \"b\": true\r\n}\r\n", configdocument.JSONLineComments},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			original := parse(t, fixture.syntax, fixture.source)
			value := lookup(t, parse(t, configdocument.JSON, `{"v":true}`), "/v")
			result := edit(t, original, set(t, fixture.selected, value))
			if string(result.Bytes()) != fixture.want {
				t.Fatalf("object layout differs:\n got %q\nwant %q", result.Bytes(), fixture.want)
			}
			assertEqual(t, lookup(t, result, fixture.selected), value)
			assertComments(t, original, result)
			if string(original.Bytes()) != fixture.source {
				t.Fatal("layout changed the observation")
			}

			if len(fixture.want) <= len(fixture.source) {
				return
			}
			for _, maximum := range []int{len(fixture.want), len(fixture.want) - 1} {
				bounded, err := configdocument.Parse(t.Context(), []byte(fixture.source), fixture.syntax,
					configdocument.Limits{MaximumBytes: int64(maximum), MaximumDepth: 16})
				if err != nil {
					t.Fatal(err)
				}
				actual, err := configdocument.Edit(t.Context(), bounded, []configdocument.Change{set(t, fixture.selected, value)})
				if maximum == len(fixture.want) {
					if err != nil || !bytes.Equal(actual.Bytes(), result.Bytes()) {
						t.Fatalf("exact layout allowance failed: %v", err)
					}
				} else if !errors.Is(err, configdocument.ErrLimitExceeded) || actual.Exists() {
					t.Fatalf("layout escaped its ceiling or returned a partial document: %v", err)
				}
			}
		})
	}
}

func TestJSONLayoutExactAndChildCreationAgree(t *testing.T) {
	for _, source := range []string{`{"keep":1}`, "{\n    \"keep\": 1\n}\n", "{\n  \"keep\": 1,\n}\n", "{\n  \"a\": {}\n}\n"} {
		t.Run(source, func(t *testing.T) {
			original := parse(t, configdocument.JSONC, source)
			body := lookup(t, parse(t, configdocument.JSON, `{"v":{"b":1,"c":{"d":2}}}`), "/v")
			one := lookup(t, parse(t, configdocument.JSON, `{"v":1}`), "/v")
			two := lookup(t, parse(t, configdocument.JSON, `{"v":2}`), "/v")
			exact := edit(t, original, set(t, "/a", body))
			children := edit(t, original, set(t, "/a/c/d", two), set(t, "/a/b", one))
			if !bytes.Equal(exact.Bytes(), children.Bytes()) {
				t.Fatalf("equivalent new-object forms disagree:\nexact %q\nchild %q", exact.Bytes(), children.Bytes())
			}
		})
	}
}

func TestJSONLayoutKeepsNestedStyleButSerializesAtomicArrays(t *testing.T) {
	source := "{\n  \"body\": {\n    \"args\": [\n      \"old\"\n    ],\n    \"env\": { \"TOKEN\": \"x\" }\n  },\n  \"outside\": true\n}\n"
	before := parse(t, configdocument.JSON, source)
	body := lookup(t, parse(t, configdocument.JSON, `{"v":{"args":["new"],"env":{"TOKEN":"x"}}}`), "/v")
	want := "{\n  \"body\": {\n    \"args\": [\"new\"],\n    \"env\": { \"TOKEN\": \"x\" }\n  },\n  \"outside\": true\n}\n"
	got := edit(t, before, set(t, "/body", body))
	if string(got.Bytes()) != want {
		t.Fatalf("whole-value edit lost the nested style: %q", got.Bytes())
	}

	body = lookup(t, parse(t, configdocument.JSON, `{"v":{"args":["old"],"env":{"TOKEN":"y"}}}`), "/v")
	got = edit(t, before, set(t, "/body", body))
	want = "{\n  \"body\": {\n    \"args\": [\"old\"],\n    \"env\": { \"TOKEN\": \"y\" }\n  },\n  \"outside\": true\n}\n"
	if string(got.Bytes()) != want {
		t.Fatalf("arrays in a changed whole value did not stay atomic: %q", got.Bytes())
	}
}

func TestJSONLayoutSkipsSameValueSetInChangedBatch(t *testing.T) {
	for _, source := range []string{
		"{\"a\":{/* keep */\"x\":1},\"b\":0}\n",
		"{\"a\":{// keep\r\n\"x\":1},\"b\":0}\r\n",
	} {
		original := parse(t, configdocument.JSONC, source)
		before := lookup(t, original, "/a")
		one := lookup(t, parse(t, configdocument.JSON, `{"v":1}`), "/v")
		got := edit(t, original, set(t, "/a", before), set(t, "/b", one))
		want := bytes.Replace([]byte(source), []byte(`"b":0`), []byte(`"b":1`), 1)
		if !bytes.Equal(got.Bytes(), want) {
			t.Fatalf("an unchanged selected value was reserialized: %q", got.Bytes())
		}
	}
}

func TestJSONLayoutRemovalPreservesSourceCommaPreference(t *testing.T) {
	for _, fixture := range []struct {
		source, want string
	}{
		{"{\"a\":1,\"b\":2,}\n", "{\"a\":1,}\n"},
		{"{\"a\":1 /* keep */,\"b\":2}\n", "{\"a\":1 /* keep */}\n"},
		{"{\"a\":1 ,\"b\":2}\n", "{\"a\":1 }\n"},
	} {
		original := parse(t, configdocument.JSONC, fixture.source)
		got := edit(t, original, remove(t, "/b"))
		if string(got.Bytes()) != fixture.want {
			t.Fatalf("removal changed comma preference: got %q want %q", got.Bytes(), fixture.want)
		}
		assertComments(t, original, got)
	}
}

func TestJSONLayoutCapturesEvidenceBeforeRemovalAndRescue(t *testing.T) {
	original := parse(t, configdocument.JSONC, "{\"a\": /* keep */ 1}\n")
	one := lookup(t, parse(t, configdocument.JSON, `{"v":1}`), "/v")
	first := edit(t, original, remove(t, "/a"), set(t, "/b", one))
	second := edit(t, original, set(t, "/b", one), remove(t, "/a"))
	if string(first.Bytes()) != "{\"b\":1\n/* keep */\n}\n" || !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatalf("rescued comments or change order became layout evidence: %q", first.Bytes())
	}
	assertComments(t, original, first)
}

func TestJSONLayoutCorrelatesLiteralObjectKeys(t *testing.T) {
	original := parse(t, configdocument.JSON, `{"v":{"a.b":{ "":1 }, "a/b":{"~x":1}}}`)
	body := lookup(t, parse(t, configdocument.JSON, `{"v":{"a.b":{"":2},"a/b":{"~x":2}}}`), "/v")
	got := edit(t, original, set(t, "/v", body))
	if string(got.Bytes()) != "{\"v\":{\"a.b\":{ \"\":2 }, \"a/b\":{\"~x\":2}}}\n" {
		t.Fatalf("literal keys lost their distinct source styles: %q", got.Bytes())
	}
}

func TestJSONLayoutBoundsInheritedIndentation(t *testing.T) {
	source := "{\n" + strings.Repeat(" ", 4096) + "\"keep\":0\n}\n"
	limits := configdocument.Limits{MaximumBytes: int64(len(source) + 32), MaximumDepth: 32}
	original, err := configdocument.Parse(t.Context(), []byte(source), configdocument.JSON, limits)
	if err != nil {
		t.Fatal(err)
	}
	value := lookup(t, parse(t, configdocument.JSON, `{"v":true}`), "/v")
	got, err := configdocument.Edit(t.Context(), original, []configdocument.Change{set(t, "/a/b/c", value)})
	if !errors.Is(err, configdocument.ErrLimitExceeded) || got.Exists() || len(got.Bytes()) != 0 || string(original.Bytes()) != source {
		t.Fatal("large inherited indentation escaped its budget or returned partial state", err)
	}
}

func TestJSONLayoutSharedScannerKeepsCRLFArrayComments(t *testing.T) {
	source := "{\r\n\"rows\":[1, // keep\r\n2],\"outside\":1e+02\r\n}\r\n"
	original := parse(t, configdocument.JSONLineComments, source)
	standard, err := configdocument.StandardJSON(t.Context(), []byte(source), configdocument.JSONLineComments, testLimits)
	if err != nil || bytes.Contains(standard, []byte("\n\n")) || !bytes.Contains(standard, []byte("\r\n")) {
		t.Fatal("shared standardization expanded a CRLF comment terminator", err)
	}
	got, err := configdocument.EditJSONArray(t.Context(), original, path(t, "/rows"), []int{0}, []int{0})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, lookup(t, got, "/rows"), lookup(t, parse(t, configdocument.JSON, `{"rows":[2]}`), "/rows"))
	assertComments(t, original, got)
	if !bytes.Contains(got.Bytes(), []byte("// keep\r\n")) || !bytes.Contains(got.Bytes(), []byte(`"outside":1e+02`)) {
		t.Fatalf("array sibling lost raw comment or unselected numeric bytes: %q", got.Bytes())
	}
}
