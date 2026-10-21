package declaration_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/declaration/manifest"
)

func TestManifestEditorSchemaAcceptedManifests(t *testing.T) {
	_, _, schema := loadEditorSchema(t)
	examples, err := filepath.Glob(filepath.Join("..", "..", "examples", "*.toml"))
	if err != nil || len(examples) == 0 {
		t.Fatalf("cannot enumerate shipped manifest examples: %v", err)
	}
	fixtures, err := filepath.Glob(filepath.Join("testdata", "editor_schema", "*.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range append(examples, fixtures...) {
		t.Run(path, func(t *testing.T) {
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := manifest.Decode(content); err != nil {
				t.Fatalf("fixture is not accepted by daem: %v", err)
			}
			if !editorSchemaAccepts(t, schema, editorSchemaInstance(t, content)) {
				t.Fatal("schema rejects a manifest accepted by daem")
			}
		})
	}

	for _, test := range []struct {
		name    string
		content string
	}{
		{"empty inherited scope and targets", `version = 1
targets = ["codex"]
[defaults]
scope = ""
[instructions.project]
source = "./missing.md"
scope = ""
targets = []
`},
		{"empty unused source keys", `version = 1
targets = ["codex"]
[[skill]]
name = "review"
source = { path = "./missing", mode = "vendor", git = "", ref = "", s3 = "", version_id = "", region = "", format = "" }
`},
		{"structured remote source forms", `version = 1
targets = ["codex"]
[[skill]]
name = "git-review"
source = { git = "https://github.com/example/skills.git", ref = "main", path = "review" }
[[skill]]
name = "s3-review"
source = { s3 = "s3://example-bucket/review.tar.gz", format = "tar.gz", region = "us-east-1" }
[instructions.shared]
source = { s3 = "s3://example-bucket/instructions.md", format = "file" }
[hook_asset.guard]
source = { git = "https://github.com/example/hooks.git", ref = "main", path = "guard.sh" }
kind = "file"
`},
		{"data names are not field names", `version = 1
targets = ["claude-code"]
[instructions.scope]
source = "./missing.md"
[[mcp_server]]
name = "docs"
transport = "stdio"
command = "npx"
env = { scope = { from_env = "target" }, TOKEN = { from_env = "source_Name" }, Token = { from_env = "source_Other" } }
`},
	} {
		t.Run(test.name, func(t *testing.T) {
			content := []byte(test.content)
			if _, err := manifest.Decode(content); err != nil {
				t.Fatalf("invalid positive baseline: %v", err)
			}
			if !editorSchemaAccepts(t, schema, editorSchemaInstance(t, content)) {
				t.Fatal("schema is stricter than daem")
			}
		})
	}
}

func TestManifestEditorSchemaStructuralRejections(t *testing.T) {
	_, _, schema := loadEditorSchema(t)
	const codex = "version = 1\ntargets = [\"codex\"]\n"
	const claude = "version = 1\ntargets = [\"claude-code\"]\n"
	const mcp = "[[mcp_server]]\nname = \"docs\"\ntransport = \"stdio\"\n"
	for _, test := range []struct {
		name    string
		content string
	}{
		{"unknown field", codex + "unknown = true\n"},
		{"noncanonical field", codex + "Version = 1\n"},
		{"missing version", "targets = [\"codex\"]\n"},
		{"wrong targets kind", "version = 1\ntargets = \"codex\"\n"},
		{"unknown target", "version = 1\ntargets = [\"unsupported\"]\n"},
		{"unknown scope", codex + "[defaults]\nscope = \"elsewhere\"\n"},
		{"missing skill source", codex + "[[skill]]\nname = \"review\"\n"},
		{"skill source is table only", codex + "[[skill]]\nname = \"review\"\nsource = \"./missing\"\n"},
		{"unknown source key", codex + "[[skill]]\nname = \"review\"\nsource = { path = \"./missing\", mode = \"vendor\", extra = \"value\" }\n"},
		{"wrong source value kind", codex + "[[skill]]\nname = \"review\"\nsource = { path = true, mode = \"vendor\" }\n"},
		{"missing command", claude + mcp},
		{"extra command property", claude + mcp + "command = { path = \"/missing/tool\", runner = \"other\" }\n"},
		{"wrong command path kind", claude + mcp + "command = { path = true }\n"},
		{"env value is a reference object", claude + mcp + "command = \"npx\"\nenv = { TOKEN = \"not-a-reference\" }\n"},
		{"extra env reference key", claude + mcp + "command = \"npx\"\nenv = { TOKEN = { from_env = \"SRC\", value = \"inline-value\" } }\n"},
		{"unknown target override", codex + "[instructions.project]\nsource = \"./missing.md\"\n[instructions.project.target.unknown]\nrender_to = \"AGENTS.md\"\n"},
		{"datetime in integer slot", "version = 1979-05-27T07:32:00Z\ntargets = [\"codex\"]\n"},
		{"date in target array", "version = 1\ntargets = [1979-05-27]\n"},
		{"time in boolean slot", codex + "[[skill]]\nname = \"review\"\nsource = { path = \"./missing\", mode = \"vendor\" }\nportable = 07:32:00\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			content := []byte(test.content)
			if _, err := manifest.Decode(content); err == nil {
				t.Fatal("negative fixture is accepted by daem")
			}
			if editorSchemaAccepts(t, schema, editorSchemaInstance(t, content)) {
				t.Fatal("schema missed a selected structural rejection")
			}
		})
	}
}

func TestManifestEditorSchemaDocumentedMisses(t *testing.T) {
	_, _, schema := loadEditorSchema(t)
	for _, test := range []struct {
		name    string
		content string
	}{
		{"integral float version", "version = 1.0\ntargets = [\"codex\"]\n"},
		{"empty root targets", "version = 1\ntargets = []\n"},
		{"global scope authority", `version = 1
targets = ["claude-code"]
[defaults]
scope = "global"
[[mcp_server]]
name = "docs"
transport = "stdio"
command = "npx"
`},
		{"unresolved declaration reference", `version = 1
targets = ["codex"]
[[hook]]
name = "guard"
event = "PreToolUse"
command = "{hook_file:absent}"
`},
		{"temporal value in a string slot", `version = 1
targets = ["claude-code"]
[[mcp_server]]
name = 1979-05-27T07:32:00
transport = "stdio"
command = "npx"
`},
	} {
		t.Run(test.name, func(t *testing.T) {
			content := []byte(test.content)
			if _, err := manifest.Decode(content); err == nil {
				t.Fatal("documented missed rejection unexpectedly became valid in daem")
			}
			if !editorSchemaAccepts(t, schema, editorSchemaInstance(t, content)) {
				t.Fatal("documented permissive projection unexpectedly changed")
			}
		})
	}
}
