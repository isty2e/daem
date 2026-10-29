package configdocument_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
)

var testLimits = configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 32}

func parse(t *testing.T, syntax configdocument.Syntax, content string) configdocument.Document {
	t.Helper()
	document, err := configdocument.Parse(context.Background(), []byte(content), syntax, testLimits)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return document
}

func path(t *testing.T, pointer string) output.KeyPath {
	t.Helper()
	value, err := output.ParseKeyPath(pointer)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func lookup(t *testing.T, document configdocument.Document, pointer string) configdocument.Value {
	t.Helper()
	value, found, err := document.Lookup(path(t, pointer))
	if err != nil || !found {
		t.Fatalf("lookup %q: found=%t, err=%v", pointer, found, err)
	}
	return value
}

func set(t *testing.T, pointer string, value configdocument.Value) configdocument.Change {
	t.Helper()
	change, err := configdocument.Set(path(t, pointer), value)
	if err != nil {
		t.Fatal(err)
	}
	return change
}

func remove(t *testing.T, pointer string) configdocument.Change {
	t.Helper()
	change, err := configdocument.Remove(path(t, pointer))
	if err != nil {
		t.Fatal(err)
	}
	return change
}

func edit(t *testing.T, document configdocument.Document, changes ...configdocument.Change) configdocument.Document {
	t.Helper()
	result, err := configdocument.Edit(context.Background(), document, changes)
	if err != nil {
		t.Fatalf("edit fixture: %v", err)
	}
	return result
}

func assertEqual(t *testing.T, left, right configdocument.Value) {
	t.Helper()
	if !left.Equal(right) {
		t.Errorf("native values differ:\n%s\n%s", left.Canonical(), right.Canonical())
	}
}

func assertComments(t *testing.T, before, after configdocument.Document) {
	t.Helper()
	left, right := before.Comments(), after.Comments()
	slices.Sort(left)
	slices.Sort(right)
	if !slices.Equal(left, right) {
		t.Errorf("comments changed:\nbefore: %q\nafter: %q", left, right)
	}
}

func TestJSONGrammarAdmission(t *testing.T) {
	for _, test := range []struct {
		name    string
		syntax  configdocument.Syntax
		content string
		valid   bool
	}{
		{"strict", configdocument.JSON, `{"x":1}`, true},
		{"strict trailing comma", configdocument.JSON, `{"x":1,}`, false},
		{"strict comment", configdocument.JSON, "// hello\n{}", false},
		{"full comments", configdocument.JSONC, "/* block */ {\"x\":1,} // EOF", true},
		{"line comments", configdocument.JSONLineComments, "// line\n{\"x\":1,}", true},
		{"line only block", configdocument.JSONLineComments, "/* no */ {}", false},
		{"quoted marker", configdocument.JSONLineComments, `{"x":"/* not a comment */"}`, true},
		{"duplicate decoded key", configdocument.JSONC, `{"x":1,"\u0078":2}`, false},
		{"unpaired surrogate", configdocument.JSONC, `{"x":"\ud800"}`, false},
		{"surrogate pair", configdocument.JSONC, `{"x":"\ud83d\ude00"}`, true},
		{"single quote", configdocument.JSONC, `{'x':1}`, false},
		{"unquoted property", configdocument.JSONC, `{x:1}`, false},
		{"trailer", configdocument.JSONC, `{} {}`, false},
		{"invalid UTF8", configdocument.JSONC, "{\"x\":\"\xff\"}", false},
		{"commented invalid UTF8", configdocument.JSONC, "// \xff\n{}", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := configdocument.Parse(context.Background(), []byte(test.content), test.syntax, testLimits)
			if test.valid && err != nil || !test.valid && !errors.Is(err, configdocument.ErrInvalidDocument) {
				t.Fatalf("valid=%t, err=%v", test.valid, err)
			}
		})
	}
}

func TestFragmentLeavesAndLiteralKeyPaths(t *testing.T) {
	document := parse(t, configdocument.JSON, `{"a/b":{"~":{"":null}},"a.b":{},"array":[{"nested":1}],"0":true}`)
	entries, err := document.Leaves(t.Context(), testLimits.MaximumBytes)
	if err != nil {
		t.Fatal(err)
	}
	var pointers []string
	for _, entry := range entries {
		pointers = append(pointers, entry.Path.String())
		assertEqual(t, entry.Value, lookup(t, document, entry.Path.String()))
	}
	if want := []string{"/0", "/a.b", "/a~1b/~0/", "/array"}; !reflect.DeepEqual(pointers, want) {
		t.Fatalf("leaves=%q, want %q", pointers, want)
	}
	for _, pointer := range []string{"/array/0", "/a~1b/~0//child", "/0/child"} {
		if _, _, err := document.Lookup(path(t, pointer)); !errors.Is(err, configdocument.ErrPathConflict) {
			t.Errorf("crossed atomic value %q: %v", pointer, err)
		}
	}
	for _, syntax := range []configdocument.Syntax{configdocument.JSON, configdocument.TOML} {
		content := "{}"
		if syntax == configdocument.TOML {
			content = ""
		}
		entries, err := parse(t, syntax, content).Leaves(t.Context(), testLimits.MaximumBytes)
		if err != nil || len(entries) != 0 {
			t.Fatalf("empty root contributes ownership: %v, %v", entries, err)
		}
	}
}

func TestJSONEditsPreserveValuesAndEveryComment(t *testing.T) {
	original := `// top
{
  "keep": {"n":9007199254740993123456789,"text":"// not /* a comment */"},
  "replace": /* before */ [
    // same
    {"x":1 /* nested */},
    // same
  ], // boundary
  "remove" /* key */ : {"a": /* inner */ true},
  "nested": {"old":1, /* end */},
} // EOF`
	document := parse(t, configdocument.JSONC, original)
	value := lookup(t, parse(t, configdocument.JSON, `{"v":{"new":null}}`), "/v")
	result := edit(
		t, document,
		set(t, "/replace", value), remove(t, "/remove"),
		set(t, "/nested/new", value), remove(t, "/nested/old"),
	)
	assertEqual(t, lookup(t, document, "/keep"), lookup(t, result, "/keep"))
	assertEqual(t, lookup(t, result, "/replace"), value)
	assertEqual(t, lookup(t, result, "/nested/new"), value)
	assertComments(t, document, result)
	if _, found, err := result.Lookup(path(t, "/remove")); err != nil || found {
		t.Fatalf("removed value remains: %t, %v", found, err)
	}
	if string(document.Bytes()) != original {
		t.Fatal("edit mutated original observation")
	}
}

func TestJSONStrictDeletionAndNestedCreation(t *testing.T) {
	value := lookup(t, parse(t, configdocument.JSON, `{"value":7}`), "/value")
	for _, input := range []string{`{"a":1,"b":2}`, `{"a":1,"b":2,"c":3}`, `{"b":2}`} {
		document := parse(t, configdocument.JSON, input)
		result := edit(t, document, remove(t, "/b"), set(t, "/nested/new/value", value))
		assertEqual(t, lookup(t, result, "/nested/new/value"), value)
		if _, err := configdocument.Parse(context.Background(), result.Bytes(), configdocument.JSON, testLimits); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEquivalentTOMLSpellingsEditWithoutExclusions(t *testing.T) {
	var baseline configdocument.Value
	for _, input := range []string{
		"[tui]\ntheme = 'dark'\nkeep = 1\n",
		"tui.theme = 'dark'\ntui.keep = 1\n",
		"tui = { theme = 'dark', keep = 1 }\n",
		"tui = {\n # inside\n theme = 'dark',\n keep = 1,\n}\n",
	} {
		document := parse(t, configdocument.TOML, input)
		if baseline.Canonical() == "" {
			baseline = lookup(t, document, "/tui")
		}
		assertEqual(t, lookup(t, document, "/tui"), baseline)
		desired := parse(t, configdocument.TOML, "theme = 'light'\nadded = true\n")
		result := edit(
			t, document,
			set(t, "/tui/theme", lookup(t, desired, "/theme")),
			set(t, "/tui/added", lookup(t, desired, "/added")),
		)
		assertEqual(t, lookup(t, result, "/tui/theme"), lookup(t, desired, "/theme"))
		assertEqual(t, lookup(t, result, "/tui/keep"), lookup(t, document, "/tui/keep"))
		assertComments(t, document, result)
	}
}

func TestTOMLInterleavedArraysTablesAndComments(t *testing.T) {
	document := parse(t, configdocument.TOML, `# top
"a.b"."/" = "# a value"
[[item]] # first
name = "one"
[other]
keep = 123
[[item]]
name = "two"
[item.meta]
detail = { x = 1 } # detail
[[item.child]]
value = 2
[elsewhere]
keep = 'yes'
`)
	array := parse(t, configdocument.TOML, "item = [1, 2]\n")
	result := edit(
		t, document,
		set(t, "/item", lookup(t, array, "/item")),
		set(t, "/a.b/~1", lookup(t, parse(t, configdocument.TOML, `v = "changed"`), "/v")),
	)
	for _, pointer := range []string{"/other", "/elsewhere"} {
		assertEqual(t, lookup(t, document, pointer), lookup(t, result, pointer))
	}
	assertEqual(t, lookup(t, result, "/item"), lookup(t, array, "/item"))
	assertComments(t, document, result)
	removed := edit(t, document, remove(t, "/item"))
	if _, found, err := removed.Lookup(path(t, "/item")); err != nil || found {
		t.Fatalf("array table removal: found=%t, err=%v", found, err)
	}
	assertComments(t, document, removed)
	for _, pointer := range []string{"/other", "/elsewhere"} {
		assertEqual(t, lookup(t, document, pointer), lookup(t, removed, pointer))
	}
}

func TestTOMLNativeTypesSurviveUnrelatedEdit(t *testing.T) {
	document := parse(t, configdocument.TOML, `root = {
 integer = 0x7fff_ffff,
 float = 1.0,
 positive_zero = 0.0,
 negative_zero = -0.0,
 infinite = +inf,
 missing = nan,
 date = 1979-05-27,
 time = 07:32,
 local = 1979-05-27T07:32:00.123456789,
 offset = 1979-05-27T07:32:00-07:00,
 literal = '''literal # text''',
 multiline = """
first
second
""",
 empty = {},
 array = [{"" = "value"}],
}
`)
	result := edit(t, document, set(t, "/root/new", lookup(t, parse(t, configdocument.TOML, "v = true"), "/v")))
	entries, err := document.Leaves(t.Context(), testLimits.MaximumBytes)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		assertEqual(t, entry.Value, lookup(t, result, entry.Path.String()))
		decoded, err := configdocument.DecodeValue(context.Background(), configdocument.TOML, entry.Value.Canonical(), testLimits)
		if err != nil {
			t.Fatal(err)
		}
		assertEqual(t, entry.Value, decoded)
	}
	if lookup(t, document, "/root/integer").Equal(lookup(t, document, "/root/float")) {
		t.Fatal("integer/float distinction lost")
	}
}

func TestTOMLExplicitEmptyAndQuotedKeys(t *testing.T) {
	document := parse(t, configdocument.TOML, "[\"\"] # empty key\n\"a/b\".\"~\" = 1\n[keep]\nx=2\n")
	result := edit(t, document, remove(t, "//a~1b/~0"))
	if !lookup(t, result, "//a~1b").IsObject() {
		t.Fatal("removal pruned parent")
	}
	assertEqual(t, lookup(t, document, "/keep"), lookup(t, result, "/keep"))
	assertComments(t, document, result)
	if _, err := configdocument.Parse(context.Background(), []byte("x = 1\nx = 2"), configdocument.TOML, testLimits); !errors.Is(err, configdocument.ErrInvalidDocument) {
		t.Fatalf("duplicate TOML accepted: %v", err)
	}
}

func TestAtomicFailuresDoNotAuthorizeAncestorReplacement(t *testing.T) {
	for _, syntax := range []configdocument.Syntax{configdocument.JSON, configdocument.TOML} {
		input := `{"a":"outside","array":[1],"keep":1}`
		valueInput := `{"v":2}`
		if syntax == configdocument.TOML {
			input, valueInput = "a = 'outside'\narray = [1]\nkeep = 1", "v = 2"
		}
		document := parse(t, syntax, input)
		value := lookup(t, parse(t, syntax, valueInput), "/v")
		for _, pointer := range []string{"/a/b", "/array/0"} {
			_, err := configdocument.Edit(context.Background(), document, []configdocument.Change{
				set(t, "/keep", value), set(t, pointer, value),
			})
			if !errors.Is(err, configdocument.ErrPathConflict) || string(document.Bytes()) != input {
				t.Fatalf("unsafe ancestor change: %v", err)
			}
		}
		for _, changes := range [][]configdocument.Change{
			{set(t, "/keep", value), remove(t, "/keep")},
			{set(t, "/new", value), set(t, "/new/b", value)},
			{set(t, "/new", value), set(t, "/new-b", value), set(t, "/new/b", value)},
		} {
			if _, err := configdocument.Edit(context.Background(), document, changes); !errors.Is(err, configdocument.ErrOverlappingChange) {
				t.Errorf("overlapping changes: %v", err)
			}
		}
	}
}

func TestAbsentAndImmutableObservations(t *testing.T) {
	for _, syntax := range []configdocument.Syntax{configdocument.JSON, configdocument.TOML} {
		document, err := configdocument.Absent(syntax, testLimits)
		if err != nil || document.Exists() {
			t.Fatalf("absence: %v", err)
		}
		if result := edit(t, document, remove(t, "/missing")); result.Exists() {
			t.Fatal("removal created absent file")
		}
		value := lookup(t, parse(t, configdocument.JSON, `{"v":true}`), "/v")
		created := edit(t, document, set(t, "/new/flag", value))
		if !created.Exists() || document.Exists() {
			t.Fatal("creation changed original absence")
		}
		assertEqual(t, value, lookup(t, created, "/new/flag"))
		content := created.Bytes()
		content[0] = 'x'
		assertEqual(t, value, lookup(t, created, "/new/flag"))
		if created.Bytes()[0] == 'x' {
			t.Fatal("exposed mutable source")
		}
	}
	input := []byte("// preserved\n{}")
	document, err := configdocument.Parse(context.Background(), input, configdocument.JSONC, testLimits)
	if err != nil {
		t.Fatal(err)
	}
	input[3] = 'X'
	comments := document.Comments()
	comments[0] = "changed"
	if document.Comments()[0] != "// preserved" {
		t.Fatal("exposed mutable input/comments")
	}
}

func TestDocumentBoundsCancellationAndInvalidAuthority(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := configdocument.Parse(ctx, []byte("{}"), configdocument.JSON, testLimits); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := configdocument.Edit(ctx, parse(t, configdocument.JSON, "{}"), nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, syntax := range []configdocument.Syntax{configdocument.JSON, configdocument.TOML} {
		input := `{"a":{"b":{}}}`
		if syntax == configdocument.TOML {
			input = "a.b = {}"
		}
		if _, err := configdocument.Parse(context.Background(), []byte(input), syntax, configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 2}); !errors.Is(err, configdocument.ErrLimitExceeded) {
			t.Errorf("depth: %s: %v", syntax, err)
		}
		if _, err := configdocument.Parse(context.Background(), []byte(input), syntax, configdocument.Limits{MaximumBytes: 1, MaximumDepth: 32}); !errors.Is(err, configdocument.ErrLimitExceeded) {
			t.Errorf("bytes: %s: %v", syntax, err)
		}
	}
	malformed := "]" + strings.Repeat("[", 50000)
	if _, err := configdocument.Parse(context.Background(), []byte(malformed), configdocument.JSONC, testLimits); !errors.Is(err, configdocument.ErrInvalidDocument) {
		t.Fatalf("malformed depth prefix: %v", err)
	}
	if _, err := configdocument.Set(output.KeyPath{}, lookup(t, parse(t, configdocument.JSON, `{"v":null}`), "/v")); err == nil {
		t.Fatal("root selector granted whole-file authority")
	}
	if _, err := configdocument.Remove(output.KeyPath{}); err == nil {
		t.Fatal("root removal granted whole-file authority")
	}
	if _, err := configdocument.Edit(context.Background(), configdocument.Document{}, nil); err == nil {
		t.Fatal("zero document accepted")
	}
	if _, err := parse(t, configdocument.JSON, "[]").Leaves(t.Context(), testLimits.MaximumBytes); err == nil {
		t.Fatal("array root treated as contribution fragment")
	}
}
