package declaration

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestAppendDocumentBlockAddsToEmptyRootCollections(t *testing.T) {
	for _, root := range []string{"skill", "skill_group", "hook", "mcp_server", "extension", "instructions"} {
		for _, syntax := range []struct {
			name string
			key  string
		}{
			{name: "bare", key: root},
			{name: "quoted", key: fmt.Sprintf("%q", root)},
			{name: "literal quoted", key: "'" + root + "'"},
		} {
			t.Run(root+"/"+syntax.name, func(t *testing.T) {
				empty, block := "[\n # retained placeholder note\n]", "[["+root+"]]\nname = \"alpha\"\n"
				if root == "instructions" {
					empty, block = "{}", "[instructions.alpha]\nsource = \"AGENTS.md\"\n"
				}
				prefix := "# retained heading\r\nversion = 1\r\n"
				suffix := " # retained trailing note\nuntouched = [1, 2] # keep\n"
				original := []byte(prefix + syntax.key + " = " + empty + suffix)
				before := bytes.Clone(original)
				updated := AppendDocumentBlock(original, block)
				if !bytes.Equal(original, before) || !bytes.HasPrefix(updated, []byte(prefix)) || !bytes.Contains(updated, []byte(suffix)) {
					t.Fatalf("append changed input or unrelated bytes: %q", updated)
				}
				if root == "instructions" && !bytes.HasPrefix(updated, original) {
					t.Fatalf("already-extensible empty table was rewritten: %q", updated)
				}
				if root != "instructions" && !bytes.Contains(updated, []byte(" # retained placeholder note\n")) {
					t.Fatalf("append discarded a placeholder comment: %q", updated)
				}

				var document map[string]toml.Primitive
				metadata, err := toml.Decode(string(updated), &document)
				if err != nil {
					t.Fatalf("appended declaration is not valid TOML: %v\n%s", err, updated)
				}
				if root == "instructions" {
					var members map[string]struct{ Source string }
					if err := metadata.PrimitiveDecode(document[root], &members); err != nil {
						t.Fatal(err)
					}
					if len(members) != 1 || members["alpha"].Source != "AGENTS.md" {
						t.Fatalf("instruction members = %#v", members)
					}
				} else {
					var members []struct{ Name string }
					if err := metadata.PrimitiveDecode(document[root], &members); err != nil {
						t.Fatal(err)
					}
					if len(members) != 1 || members[0].Name != "alpha" {
						t.Fatalf("%s members = %#v", root, members)
					}
				}
			})
		}
	}
}

func TestAppendDocumentBlockLeavesUnrelatedOrPopulatedValuesAlone(t *testing.T) {
	for _, original := range []string{
		"hook = []\n",
		"skill = [{ name = \"existing\" }]\n",
		"[nested]\nskill = []\n",
		"script = '''\nskill = []\n[[skill]]\n'''\n",
		"# skill = []\n",
	} {
		appended := AppendDocumentBlock([]byte(original), "[[skill]]\nname = \"alpha\"\n")
		if !bytes.HasPrefix(appended, []byte(original)) {
			t.Fatalf("append rewrote unrelated/populated input %q: %q", original, appended)
		}
	}
}

func TestAppendDocumentBlockIgnoresHeadersInsideIncomingStrings(t *testing.T) {
	original := []byte("skill = []\nhook = []\n")
	block := "[[hook]]\ncommand = '''\n[[skill]]\n'''\n"
	updated := AppendDocumentBlock(original, block)
	if !bytes.HasPrefix(updated, []byte("skill = []\n")) {
		t.Fatalf("incoming string was treated as a declaration: %q", updated)
	}
	var result struct {
		Skill []struct{ Name string }
		Hook  []struct{ Command string }
	}
	if _, err := toml.Decode(string(updated), &result); err != nil {
		t.Fatalf("appended hook is not valid TOML: %v\n%s", err, updated)
	}
	if len(result.Skill) != 0 || len(result.Hook) != 1 || result.Hook[0].Command != "[[skill]]\n" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestAppendDocumentBlockReplacesOnlyExplicitlyRedefinedRoots(t *testing.T) {
	original := []byte("instructions = {} # map note\nskill = [] # array note\nhook = [] # untouched\n")
	block := "[instructions]\n[instructions.alpha]\nsource = \"guide.md\"\n\n[[skill]]\nname = \"alpha\"\n"
	updated := AppendDocumentBlock(original, block)
	var document struct {
		Instructions map[string]struct{ Source string }
		Skill        []struct{ Name string }
		Hook         []struct{ Name string }
	}
	if _, err := toml.Decode(string(updated), &document); err != nil {
		t.Fatalf("combined root definitions are invalid: %v\n%s", err, updated)
	}
	if len(document.Instructions) != 1 || document.Instructions["alpha"].Source != "guide.md" || len(document.Skill) != 1 || document.Skill[0].Name != "alpha" || len(document.Hook) != 0 {
		t.Fatalf("combined root definitions lost values: %#v", document)
	}
	for _, preserved := range []string{"# map note\n", "# array note\n", "hook = [] # untouched\n"} {
		if !bytes.Contains(updated, []byte(preserved)) {
			t.Fatalf("combined append lost %q: %s", preserved, updated)
		}
	}
}
