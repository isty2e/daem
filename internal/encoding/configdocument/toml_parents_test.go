package configdocument_test

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
)

func TestTOMLDottedParentSiblingRoundtripAndManualExtension(t *testing.T) {
	for _, test := range []struct{ name, content, pointer, manual, manualPointer string }{
		{"root", "# UI\ntui.theme = 'dark' # retained\n", "/tui/notifications", "tui.foo = 1\n", "/tui/foo"},
		{"root EOF", "tui.theme = 'dark'", "/tui/notifications", "\ntui.foo = 1\n", "/tui/foo"},
		{"scoped", "# profile\n[profiles.fast] # retained\ntui.theme = 'dark' # theme\n", "/profiles/fast/tui/notifications", "tui.foo = 1\n", "/profiles/fast/tui/foo"},
		{"scoped EOF", "[profiles.fast]\ntui.theme = 'dark'", "/profiles/fast/tui/notifications", "\ntui.foo = 1\n", "/profiles/fast/tui/foo"},
		{"quoted literal CRLF", "['profiles'.\"fast.name\"] # retained\r\n\"t.ui\".'th/eme' = 'dark' # theme\r\n", "/profiles/fast.name/t.ui/notifications", "'t.ui'.'foo/~' = 1\r\n", "/profiles/fast.name/t.ui/foo~1~0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := parse(t, configdocument.TOML, test.content)
			value := lookup(t, parse(t, configdocument.TOML, "v=true"), "/v")
			added := edit(t, original, set(t, test.pointer, value))
			manual := parse(t, configdocument.TOML, string(added.Bytes())+test.manual)
			assertEqual(t, lookup(t, manual, test.manualPointer), lookup(t, parse(t, configdocument.TOML, "v=1"), "/v"))
			restored := edit(t, added, remove(t, test.pointer))
			if string(restored.Bytes()) != test.content {
				t.Fatalf("dotted sibling roundtrip changed original bytes:\n got %q\nwant %q", restored.Bytes(), test.content)
			}
		})
	}
}

func TestTOMLImplicitParentTemporaryChildPreservesManualDeclaration(t *testing.T) {
	for _, test := range []struct{ name, content, pointer, manual, manualPointer string }{
		{"root", "# profile\n[profiles.fast] # retained\nmodel='native' # model\n", "/profiles/notifications", "[profiles]\nmanual=true\n", "/profiles/manual"},
		{"scoped", "[owner] # owner\nkeep=0x1f\n[owner.profiles.fast] # retained\nmodel='native'\n", "/owner/profiles/notifications", "[owner.profiles]\nmanual=true\n", "/owner/profiles/manual"},
		{"quoted interleaved", "['pro.files'.'fast/name'] # retained\nmodel='native'\n[other]\nkeep=0x1f # other\n", "/pro.files/notifications", "['pro.files']\nmanual=true\n", "/pro.files/manual"},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := parse(t, configdocument.TOML, test.content)
			value := lookup(t, parse(t, configdocument.TOML, "v=true"), "/v")
			added := edit(t, original, set(t, test.pointer, value))
			restored := edit(t, added, remove(t, test.pointer))
			if string(restored.Bytes()) != test.content {
				t.Fatalf("implicit parent gained unrelated syntax:\n got %q\nwant %q", restored.Bytes(), test.content)
			}
			manual := parse(t, configdocument.TOML, string(restored.Bytes())+test.manual)
			assertEqual(t, lookup(t, manual, test.manualPointer), value)
		})
	}
}

func TestTOMLRemoveRetainsCreatedAndSelectedEmptyParents(t *testing.T) {
	original := parse(t, configdocument.TOML, "# empty document\n")
	value := lookup(t, parse(t, configdocument.TOML, "v=true"), "/v")
	empty := lookup(t, parse(t, configdocument.TOML, "v={}"), "/v")
	created := edit(t, original, set(t, "/profiles/fast/notifications", value), set(t, "/selected", empty))
	removed := edit(t, created, remove(t, "/profiles/fast/notifications"))
	assertEqual(t, lookup(t, removed, "/profiles/fast"), empty)
	assertEqual(t, lookup(t, removed, "/selected"), empty)
	if _, present, err := removed.Lookup(path(t, "/profiles")); err != nil || !present {
		t.Fatalf("remove pruned created structural ancestor: present=%t err=%v", present, err)
	}
}

func TestTOMLMixedDottedEditsPreserveAtomicValuesAndScopes(t *testing.T) {
	for _, prefix := range []string{"", "[owner] # scope\n"} {
		t.Run(prefix, func(t *testing.T) {
			pointer := ""
			if prefix != "" {
				pointer = "/owner"
			}
			content := prefix + "# UI\ntui.theme = 'dark' # theme\ntui.args = [1, 2] # args\ninline = {child={keep=1}} # inline\nprofiles.fast.model = 'native' # model\n"
			original := parse(t, configdocument.TOML, content)
			value := lookup(t, parse(t, configdocument.TOML, "v=true"), "/v")
			array := lookup(t, parse(t, configdocument.TOML, "v=[]"), "/v")
			result := edit(
				t, original,
				set(t, pointer+"/tui/notifications", value),
				set(t, pointer+"/profiles/enabled", value),
				set(t, pointer+"/inline/child/notifications", value),
				set(t, pointer+"/tui/args", array),
			)
			assertEqual(t, lookup(t, result, pointer+"/tui/args"), array)
			assertEqual(t, lookup(t, result, pointer+"/inline/child/notifications"), value)
			assertComments(t, original, result)
			manual := parse(t, configdocument.TOML, string(result.Bytes())+"tui.foo = 1\nprofiles.slow.model = 'manual'\n")
			assertEqual(t, lookup(t, manual, pointer+"/tui/foo"), lookup(t, parse(t, configdocument.TOML, "v=1"), "/v"))
			if !strings.Contains(string(result.Bytes()), "profiles.fast.model = 'native' # model\n") {
				t.Fatalf("unrelated dotted assignment changed: %q", result.Bytes())
			}
		})
	}
}

func TestTOMLImplicitParentMixedEditsRetainCreatedEmptySections(t *testing.T) {
	content := "['profiles'.'fast.name'] # fast\nargs=[] # args\n[other]\nkeep=1979-05-27 # date\n[[profiles.workers]] # worker\nname='native'\n"
	original := parse(t, configdocument.TOML, content)
	value := lookup(t, parse(t, configdocument.TOML, "v=true"), "/v")
	empty := lookup(t, parse(t, configdocument.TOML, "v={}"), "/v")
	array := lookup(t, parse(t, configdocument.TOML, "v=['new']"), "/v")
	added := edit(
		t, original,
		set(t, "/profiles/notifications", value),
		set(t, "/profiles/created/notifications", value),
		set(t, "/profiles/selected", empty),
		set(t, "/profiles/fast.name/args", array),
	)
	removed := edit(t, added, remove(t, "/profiles/notifications"), remove(t, "/profiles/created/notifications"))
	assertEqual(t, lookup(t, removed, "/profiles/created"), empty)
	assertEqual(t, lookup(t, removed, "/profiles/selected"), empty)
	assertEqual(t, lookup(t, removed, "/profiles/fast.name/args"), array)
	assertEqual(t, lookup(t, removed, "/profiles/workers"), lookup(t, original, "/profiles/workers"))
	assertComments(t, original, removed)
	manual := parse(t, configdocument.TOML, string(removed.Bytes())+"[profiles]\nmanual=true\n")
	assertEqual(t, lookup(t, manual, "/profiles/manual"), value)
	if !strings.Contains(string(removed.Bytes()), "[other]\nkeep=1979-05-27 # date\n[[profiles.workers]] # worker\nname='native'\n") {
		t.Fatalf("unrelated native sections changed: %q", removed.Bytes())
	}
}
