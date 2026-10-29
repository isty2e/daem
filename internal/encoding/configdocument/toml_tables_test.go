package configdocument_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	tomlsyntax "github.com/isty2e/daem/internal/encoding/toml"
)

func TestTOMLStandardTableAddRemoveRestoresOriginalBytes(t *testing.T) {
	for _, test := range []struct{ name, content, pointer string }{
		{"tui", "# intro\n[tui] # UI\ntheme = 'dark' # theme\n\n# feature notes\n[features]\nkeep = true\n", "/tui/added"},
		{"features", "[features]\n# existing\nkeep = true # keep\n", "/features/added"},
		{"quoted profile CRLF", "['profiles'.\"a.b\"] # selected\r\nmodel = 'native'\r\n[other]\r\nkeep = 1\r\n[profiles.second]\r\nmodel = 'second'\r\n[profiles.'a.b'.tui]\r\ntheme = 'dark' # nested\r\n", "/profiles/a.b/tui/added"},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := parse(t, configdocument.TOML, test.content)
			value := lookup(t, parse(t, configdocument.TOML, "v=true"), "/v")
			current := edit(t, original, set(t, test.pointer, value))
			current = edit(t, current, remove(t, test.pointer))
			if string(current.Bytes()) != test.content {
				t.Fatalf("table add/remove changed original bytes:\n got %q\nwant %q", current.Bytes(), test.content)
			}
		})
	}
}

func TestTOMLStandardTableEditPreservesSiblingsAndManualExtension(t *testing.T) {
	content := "# intro\n[profiles.first]\nmodel = 'one' # model\n[other]\nkeep = 0x1f\n[profiles.second]\nmodel = 'two'\n[profiles.first.tui] # UI\ntheme = 'dark' # theme\n"
	original := parse(t, configdocument.TOML, content)
	value := lookup(t, parse(t, configdocument.TOML, "v='light'"), "/v")
	result := edit(t, original, set(t, "/profiles/first/tui/theme", value))
	want := strings.Replace(content, "theme = 'dark'", `theme = "light"`, 1)
	if string(result.Bytes()) != want {
		t.Fatalf("unselected table bytes changed:\n got %q\nwant %q", result.Bytes(), want)
	}
	manual := parse(t, configdocument.TOML, string(result.Bytes())+"manual = true\n[profiles.third]\nmodel = 'three'\n")
	assertEqual(t, lookup(t, manual, "/profiles/first/tui/manual"), lookup(t, parse(t, configdocument.TOML, "v=true"), "/v"))
}

func TestTOMLMissingSectionsAppendAtEnd(t *testing.T) {
	content := "# intro\n[tui]\ntheme = 'dark'\n[profiles.first]\nmodel = 'one' # retained\n"
	original := parse(t, configdocument.TOML, content)
	value := lookup(t, parse(t, configdocument.TOML, "v=true"), "/v")
	result := edit(t, original, set(t, "/features/added", value), set(t, "/profiles/a.b/tui/added", value))
	if !strings.HasPrefix(string(result.Bytes()), content) {
		t.Fatalf("missing section rewrote original bytes: %q", result.Bytes())
	}
	manual := parse(t, configdocument.TOML, string(result.Bytes())+"manual = false\n")
	assertEqual(t, lookup(t, manual, "/profiles/a.b/tui/added"), value)
	index, err := tomlsyntax.IndexDocument(t.Context(), result.Bytes()[len(content):])
	if err != nil {
		t.Fatalf("appended sections do not parse: %v", err)
	}
	var sections [][]string
	for _, expression := range index.Expressions() {
		if expression.Kind != tomlsyntax.Table {
			continue
		}
		sections = append(sections, expression.Path)
	}
	for _, expected := range [][]string{{"features"}, {"profiles", "a.b", "tui"}} {
		if !slices.ContainsFunc(sections, func(parts []string) bool { return slices.Equal(parts, expected) }) {
			t.Fatalf("missing appended ordinary section %q: %q", expected, result.Bytes())
		}
	}
}

func TestTOMLTableEditsRetainNativeSiblingsAndComments(t *testing.T) {
	content := "[tui] # UI\n# selected value\ntheme = 'dark' # theme\ninteger = 0x7fff_ffff\nfloat = -0.0\ndate = 1979-05-27\nlocal = 1979-05-27T07:32:00.123456789\ntext = '''literal # text'''\n"
	original := parse(t, configdocument.TOML, content)
	value := lookup(t, parse(t, configdocument.TOML, "v=true"), "/v")
	result := edit(t, original, set(t, "/tui/new", value))
	if !strings.HasSuffix(string(result.Bytes()), content[strings.Index(content, "# selected value"):]) {
		t.Fatalf("native siblings changed: %q", result.Bytes())
	}
	assertComments(t, original, result)
}

func TestTOMLTableRepresentationTransitions(t *testing.T) {
	for _, test := range []struct {
		name, content, pointer, replacement string
		remove                              bool
	}{
		{"last dotted child", "[tui]\n'a.b'.'~' = 1 # keep\n[other]\nx=2\n", "/tui/a.b/~0", "", true},
		{"last implied child", "[profiles.first]\nx=1 # keep\n[other]\nx=2\n", "/profiles/first", "", true},
		{"standard table to scalar", "[profiles.first]\nx=1 # keep\n[other]\nx=2\n", "/profiles/first", "false", false},
		{"standard table to empty", "[profiles.first]\nx=1 # keep\n[other]\nx=2\n", "/profiles/first", "{}", false},
		{"dotted group with nested section", "[tui]\na.b=1 # b\n[other]\nx=2\n[tui.a.c]\nx=2 # c\n", "/tui/a/b", "true", false},
		{"dotted group removal", "a.b=1 # b\n[other]\nx=2\n[a.c]\nx=2 # c\n", "/a", "", true},
		{"nested array tables", "[profiles.first]\nkeep=0x1f\n[[profiles.first.items]] # first\nx=1\n[other]\nx=2\n[[profiles.first.items]]\nx=2 # second\n[profiles.second]\nkeep='native'\n", "/profiles/first/items", "[1, 2]", false},
		{"multiline inline comments", "[tui]\na = {\n # inner\n b=1,\n c=2,\n} # outer\n[other]\nx=2\n", "/tui/a/b", "true", false},
		{"nested empty parent", "[''.'a/b']\n'~'=1\n[other]\nx=2\n", "//a~1b/~0", "", true},
		{"EOF header", "[tui] # end", "/tui/new", "true", false},
		{"EOF assignment", "[tui]\nkeep=1 # end", "/tui/new", "true", false},
		{"implied parent extension", "[profiles.first.tui]\ntheme='native'\n[other]\nx=2\n", "/profiles/first/model", "'new'", false},
		{"empty missing section", "[tui]\nkeep=1\n", "/features", "{}", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := parse(t, configdocument.TOML, test.content)
			var result configdocument.Document
			if test.remove {
				result = edit(t, original, remove(t, test.pointer))
				if _, present, err := result.Lookup(path(t, test.pointer)); present || err != nil {
					t.Fatalf("removed value remains: present=%t err=%v", present, err)
				}
			} else {
				value := lookup(t, parse(t, configdocument.TOML, "v="+test.replacement), "/v")
				result = edit(t, original, set(t, test.pointer, value))
				assertEqual(t, lookup(t, result, test.pointer), value)
			}
			assertComments(t, original, result)
			if strings.Contains(test.content, "[other]\nx=2\n") && !strings.Contains(string(result.Bytes()), "[other]\nx=2\n") {
				t.Fatalf("unrelated section changed: %q", result.Bytes())
			}
		})
	}
}

func TestTOMLSectionPatchesKeepBoundsAndCancellationAtomic(t *testing.T) {
	content := "[tui]\ntheme='dark' # retained\n"
	original, err := configdocument.Parse(context.Background(), []byte(content), configdocument.TOML, configdocument.Limits{
		MaximumBytes: int64(len(content)), MaximumDepth: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	value := lookup(t, parse(t, configdocument.TOML, "v=true"), "/v")
	changes := []configdocument.Change{set(t, "/features/added", value)}
	if _, err := configdocument.Edit(context.Background(), original, changes); !errors.Is(err, configdocument.ErrLimitExceeded) {
		t.Fatalf("section insertion exceeded byte bound without error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := configdocument.Edit(ctx, original, changes); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled section edit: %v", err)
	}
	if string(original.Bytes()) != content {
		t.Fatalf("failed edit mutated observation: %q", original.Bytes())
	}
}

func TestTOMLEOFOnlyHeaderRoundtripNormalizesOnlyRequiredSeparator(t *testing.T) {
	const original = "# intro\n[tui] # retained"
	current := parse(t, configdocument.TOML, original)
	value := lookup(t, parse(t, configdocument.TOML, "v=true"), "/v")
	for range 3 {
		current = edit(t, current, set(t, "/tui/new", value))
		current = edit(t, current, remove(t, "/tui/new"))
		if string(current.Bytes()) != original+"\n" {
			t.Fatalf("EOF separator normalization changed other bytes or grew: %q", current.Bytes())
		}
		current = parse(t, configdocument.TOML, string(current.Bytes()))
	}
}

func TestTOMLArrayAssignmentReplacementAndRecovery(t *testing.T) {
	for _, test := range []struct{ name, content, pointer, desired string }{
		{"empty root", "args=[]\n", "/args", "['new']"},
		{"root after sibling", "outside=0x1f\nargs = [1, 2]\n", "/args", "[]"},
		{"section with comments", "# intro\n[mcp_servers.search] # server\ncommand='old'\nargs = [] # args\n[other]\nkeep=true\n", "/mcp_servers/search/args", "['new']"},
		{"multiline array comments", "# intro\n[tui]\nargs = [\n # first\n 1,\n # second\n 2,\n] # args\n[other]\nkeep=true\n", "/tui/args", "[]"},
		{"array of tables recovery", "outside=true\n[[a]] # array\nb=1\n", "/a", "[{b=2}]"},
		{"quoted key separator", "[profiles.'first=profile']\n'args=key'  =  [] # args\n", "/profiles/first=profile/args=key", "[1]"},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := parse(t, configdocument.TOML, test.content)
			before := lookup(t, original, test.pointer)
			desired := lookup(t, parse(t, configdocument.TOML, "v="+test.desired), "/v")
			rendered := edit(t, original, set(t, test.pointer, desired))
			assertEqual(t, lookup(t, rendered, test.pointer), desired)
			assertComments(t, original, rendered)
			restored := edit(t, rendered, set(t, test.pointer, before))
			assertEqual(t, lookup(t, restored, test.pointer), before)
			assertComments(t, original, restored)
			if strings.Contains(test.content, "[other]\nkeep=true\n") && !strings.Contains(string(rendered.Bytes()), "[other]\nkeep=true\n") {
				t.Fatalf("array replacement changed unrelated table bytes: %q", rendered.Bytes())
			}
		})
	}
}

func TestTOMLRootAndSectionInsertionOrdering(t *testing.T) {
	absent, err := configdocument.Absent(configdocument.TOML, testLimits)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		original configdocument.Document
	}{
		{"absent", absent},
		{"empty", parse(t, configdocument.TOML, "")},
		{"comment only", parse(t, configdocument.TOML, "# intro")},
		{"EOF header", parse(t, configdocument.TOML, "[tui] # retained")},
		{"interleaved EOF header", parse(t, configdocument.TOML, "[profiles.first]\nmodel='native'\n[tui] # retained")},
		{"newline terminated header", parse(t, configdocument.TOML, "[tui] # retained\n")},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := lookup(t, parse(t, configdocument.TOML, "v=true"), "/v")
			pointers := []string{"/features/new", "/model", "/profiles/first/features/new", "/tui/new"}
			var changes []configdocument.Change
			for _, pointer := range pointers {
				changes = append(changes, set(t, pointer, value))
			}
			result := edit(t, test.original, changes...)
			for _, pointer := range pointers {
				assertEqual(t, lookup(t, result, pointer), value)
			}
			assertComments(t, test.original, result)
		})
	}
}
