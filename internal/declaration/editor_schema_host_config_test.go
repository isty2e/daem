package declaration_test

import (
	"fmt"
	"testing"

	"github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/target"
)

func TestManifestEditorSchemaHostConfigBehavior(t *testing.T) {
	_, _, schema := loadEditorSchema(t)
	valid := []string{
		`version = 1
targets = ["pi"]
[[host_config]]
id = "pi-defaults"
path = "settings.json"
source = "/workbench/settings.json"
`,
		`version = 1
targets = ["codex"]
[[host_config]]
id = "codex-explicit"
targets = ["codex"]
scope = "global"
path = "config.toml"
source = { git = "https://github.com/example/config.git", path = "config.toml", ref = "main" }
ownership = "document"
syntax = "toml"
`,
		`version = 1
targets = ["pi"]
[[host_config]]
id = "pi-empty-inheritance"
targets = []
scope = ""
path = "settings.json"
source = "/workbench/settings.json"
ownership = ""
syntax = ""
`,
	}
	for _, selected := range target.SupportedTargets() {
		valid = append(valid, fmt.Sprintf(`version = 1
targets = [%q]
[[host_config]]
id = "registered-host"
path = "settings.json"
source = "/workbench/settings.json"
`, selected))
	}
	for index, content := range valid {
		t.Run(fmt.Sprintf("valid-%d", index+1), func(t *testing.T) {
			if _, err := manifest.Decode([]byte(content)); err != nil {
				t.Fatalf("manifest.Decode() error = %v", err)
			}
			if !editorSchemaAccepts(t, schema, editorSchemaInstance(t, []byte(content))) {
				t.Fatal("schema rejected a manifest accepted by daem")
			}
		})
	}

	invalid := []struct {
		name    string
		content string
	}{
		{
			name: "unknown host",
			content: `version = 1
targets = ["pi"]
[[host_config]]
id = "unknown-config"
targets = ["unknown"]
path = "settings.json"
source = "/workbench/settings.json"
`,
		},
		{
			name: "multiple hosts",
			content: `version = 1
targets = ["pi"]
[[host_config]]
id = "two-hosts"
targets = ["pi", "codex"]
path = "settings.json"
source = "/workbench/settings.json"
`,
		},
		{
			name: "unknown ownership",
			content: `version = 1
targets = ["pi"]
[[host_config]]
id = "bad-ownership"
path = "settings.json"
source = "/workbench/settings.json"
ownership = "whole"
`,
		},
		{
			name: "unsupported syntax",
			content: `version = 1
targets = ["pi"]
[[host_config]]
id = "bad-syntax"
path = "settings.json"
source = "/workbench/settings.json"
syntax = "yaml"
`,
		},
		{
			name: "unknown field",
			content: `version = 1
targets = ["pi"]
[[host_config]]
id = "unknown-field"
path = "settings.json"
source = "/workbench/settings.json"
credential_denylist = true
`,
		},
	}
	for _, test := range invalid {
		t.Run("reject-"+test.name, func(t *testing.T) {
			content := []byte(test.content)
			if _, err := manifest.Decode(content); err == nil {
				t.Fatal("manifest.Decode() accepted an invalid host_config")
			}
			if editorSchemaAccepts(t, schema, editorSchemaInstance(t, content)) {
				t.Fatal("schema accepted an invalid host_config declaration")
			}
		})
	}
}
