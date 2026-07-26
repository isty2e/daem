package declaration

import (
	"strings"
	"testing"
)

func TestDecodeExtension(t *testing.T) {
	manifest, err := DecodeManifest([]byte(`
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
targets = ["claude-code"]
scope = "project"
source = { marketplace = "context7@market" }
`))
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if len(manifest.Extensions) != 1 {
		t.Fatalf("len(Extensions) = %d, want 1", len(manifest.Extensions))
	}
	extension := manifest.Extensions[0]
	if extension.ID != "context7" ||
		extension.Carrier != "claude-code-plugin" ||
		len(extension.Targets) != 1 ||
		extension.Targets[0] != "claude-code" ||
		extension.Scope != "project" ||
		extension.Source.Marketplace != "context7@market" {
		t.Fatalf("extension = %#v, want admitted Claude Code marketplace row", extension)
	}
}

func TestDecodeRejectsUnsupportedExtensionFields(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		want     string
	}{
		{
			name: "removed extension absence policy",
			manifest: `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
source = { marketplace = "context7@market" }
on_absent = "block"
`,
			want: "extension.on_absent",
		},
		{
			name: "label override",
			manifest: `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
source = { marketplace = "context7@market" }
label = "pretty"
`,
			want: "extension.label",
		},
		{
			name: "plugin key override",
			manifest: `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
source = { marketplace = "context7@market" }
plugin_key = "pretty"
`,
			want: "extension.plugin_key",
		},
		{
			name: "url source",
			manifest: `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
source = { url = "https://example.com/context7.git" }
`,
			want: "extension.source.url",
		},
		{
			name: "git source",
			manifest: `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
source = { marketplace = "context7@market", git = "https://example.com/context7.git" }
`,
			want: "extension.source.git",
		},
		{
			name: "npm source",
			manifest: `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
source = { marketplace = "context7@market", npm = "@example/context7" }
`,
			want: "extension.source.npm",
		},
		{
			name: "inline secret",
			manifest: `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
source = { marketplace = "context7@market", token = "secret" }
`,
			want: "extension.source.token",
		},
		{
			name: "selector include field",
			manifest: `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
source = { marketplace = "context7@market" }
include = ["context*"]
`,
			want: "extension.include",
		},
		{
			name: "group names field",
			manifest: `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
source = { marketplace = "context7@market" }
names = ["context7"]
`,
			want: "extension.names",
		},
		{
			name: "raw host config",
			manifest: `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
source = { marketplace = "context7@market" }
config = { plugins = ["context7"] }
`,
			want: "extension.config",
		},
		{
			name: "freeform install command",
			manifest: `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
source = { marketplace = "context7@market" }
command = "claude plugin install context7"
`,
			want: "extension.command",
		},
		{
			name: "source command",
			manifest: `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
source = { marketplace = "context7@market", command = "npx install-plugin context7" }
`,
			want: "extension.source.command",
		},
		{
			name: "exact artifact field",
			manifest: `
version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
source = { marketplace = "context7@market" }
version = "1.2.3"
`,
			want: "extension.version",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeManifest([]byte(test.manifest))
			if err == nil {
				t.Fatal("Decode returned nil error")
			}
			if !strings.Contains(err.Error(), "unknown manifest key") ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %q, want unknown key containing %q", err, test.want)
			}
		})
	}
}
