package declaration

import (
	"errors"
	"strings"
	"testing"
)

func TestManifestRejectsNonCanonicalFieldNames(t *testing.T) {
	for _, test := range []struct {
		name      string
		content   string
		canonical string
		line      int
		column    int
	}{
		{"root", "  Version=1\n", "version", 1, 3},
		{"canonical first", "version=1\n[[mcp_server]]\nname=\"alpha\"\nName=\"beta\"\n", "name", 4, 1},
		{"alias first", "version=1\n[[mcp_server]]\nName=\"beta\"\nname=\"alpha\"\n", "name", 3, 1},
		{"inline", "version=1\nmcp_server=[{Name=\"alpha\"}]\n", "name", 2, 14},
		{"dotted leaf", "version=1\ndefaults.Scope=\"project\"\n", "scope", 2, 1},
		{"dotted parent", "version=1\nDefaults.scope=\"project\"\n", "defaults", 2, 1},
		{"table", "version=1\n[Defaults]\nscope=\"project\"\n", "defaults", 2, 2},
		{"array table", "version=1\n[[MCP_SERVER]]\nname=\"alpha\"\n", "mcp_server", 2, 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeManifest([]byte(test.content))
			var positioned UnknownManifestKeyError
			if !errors.As(err, &positioned) {
				t.Fatalf("error=%v, want positioned manifest field failure", err)
			}
			line, column, ok := positioned.Position()
			if !ok || line != test.line || column != test.column {
				t.Fatalf("position=(%d,%d,%t), want (%d,%d,true)", line, column, ok, test.line, test.column)
			}
			if !strings.Contains(err.Error(), `"`+test.canonical+`"`) {
				t.Fatalf("error=%v, want canonical spelling %q", err, test.canonical)
			}
		})
	}
}

func TestManifestHeaderRejectsNonCanonicalFieldNames(t *testing.T) {
	for _, content := range []string{
		"Targets=[\"codex\"]\n",
		"targets=[\"codex\"]\nTargets=[\"claude-code\"]\n",
		"Targets=[\"claude-code\"]\ntargets=[\"codex\"]\n",
		"[defaults]\nScope=\"project\"\n",
	} {
		_, err := DecodeManifestHeader([]byte(content))
		var positioned UnknownManifestKeyError
		if !errors.As(err, &positioned) {
			t.Fatalf("content=%q error=%v, want positioned header field failure", content, err)
		}
		if _, _, ok := positioned.Position(); !ok {
			t.Fatalf("content=%q error=%v has no source position", content, err)
		}
	}
}

func TestCanonicalManifestKeepsMapNamesAndPartialHeader(t *testing.T) {
	content := []byte(`version = 1
targets = ["claude-code"]
[defaults]
scope = "project"
[[skill]]
name = "review"
source = { path = "skills/review", mode = "vendor" }
targets = ["claude-code"]
[[mcp_server]]
name = "docs"
scope = "project"
transport = "stdio"
command = "npx"
env = { TOKEN = { from_env = "SRC" }, Token = { from_env = "Mixed_Source" } }
`)
	manifest, err := DecodeManifest(content)
	if err != nil {
		t.Fatal(err)
	}
	if got := manifest.Skills[0].Targets; len(got) != 1 || got[0] != "claude-code" {
		t.Fatalf("skill targets=%v", got)
	}
	env := manifest.MCPServers[0].Env
	if len(env) != 2 || env["TOKEN"].FromEnv != "SRC" || env["Token"].FromEnv != "Mixed_Source" {
		t.Fatalf("case-sensitive env entries changed: %#v", env)
	}

	header, err := DecodeManifestHeader(content)
	if err != nil {
		t.Fatal(err)
	}
	if len(header.Targets) != 1 || header.Targets[0] != "claude-code" || header.Defaults.Scope != "project" {
		t.Fatalf("partial header changed: %#v", header)
	}
}
