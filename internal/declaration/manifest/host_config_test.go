package manifest

import (
	"fmt"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declaration/codec"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/target"
)

func TestDecodeNormalizesHostConfigDeclarationsAndRoundTrips(t *testing.T) {
	tests := []struct {
		name           string
		manifestTarget string
		defaultScope   string
		fields         string
		wantTarget     target.Target
		wantScope      target.Scope
		wantOwnership  string
		wantSyntax     string
		wantSource     source.SourceKind
	}{
		{
			name:           "Pi global inherited target scope and local path source",
			manifestTarget: "pi", defaultScope: "global",
			fields: `id = "pi-default"
path = "extensions/sample/settings.json"
source = "/workbench/settings.json"
`,
			wantTarget: target.TargetPi, wantScope: target.ScopeGlobal,
			wantOwnership: "keys", wantSource: source.SourceKindLocal,
		},
		{
			name:           "Pi project explicit local source and JSON grammar",
			manifestTarget: "pi", defaultScope: "global",
			fields: `id = "pi-project"
targets = ["pi"]
scope = "project"
path = "config/settings.json"
source = { path = "sources/settings.json", mode = "vendor" }
ownership = "document"
syntax = "json"
`,
			wantTarget: target.TargetPi, wantScope: target.ScopeProject,
			wantOwnership: "document", wantSyntax: "json", wantSource: source.SourceKindLocal,
		},
		{
			name:           "Codex global Git source and JSONC grammar",
			manifestTarget: "codex", defaultScope: "project",
			fields: `id = "codex-global"
targets = ["codex"]
scope = "global"
path = "config.toml"
source = { git = "https://github.com/example/settings.git", path = "config.toml", ref = "main" }
ownership = "document"
syntax = "jsonc"
`,
			wantTarget: target.TargetCodex, wantScope: target.ScopeGlobal,
			wantOwnership: "document", wantSyntax: "jsonc", wantSource: source.SourceKindGit,
		},
		{
			name:           "Codex project S3 file and line-comment grammar",
			manifestTarget: "codex", defaultScope: "global",
			fields: `id = "codex-project"
targets = ["codex"]
scope = "project"
path = "settings.json"
source = { s3 = "s3://settings-bucket/settings.json", format = "file" }
syntax = "json-line-comments"
`,
			wantTarget: target.TargetCodex, wantScope: target.ScopeProject,
			wantOwnership: "keys", wantSyntax: "json-line-comments", wantSource: source.SourceKindS3,
		},
		{
			name:           "TOML grammar selector",
			manifestTarget: "pi", defaultScope: "project",
			fields: `id = "pi-toml"
targets = ["pi"]
path = "settings.toml"
source = { git = "https://github.com/example/settings.git", path = "settings.toml", ref = "main" }
syntax = "toml"
`,
			wantTarget: target.TargetPi, wantScope: target.ScopeProject,
			wantOwnership: "keys", wantSyntax: "toml", wantSource: source.SourceKindGit,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			content := hostConfigManifest(test.manifestTarget, test.defaultScope, test.fields)
			environment, err := Decode(content)
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			configs := environment.HostConfigs()
			if len(configs) != 1 {
				t.Fatalf("HostConfigs() = %#v, want one config", configs)
			}
			config := configs[0]
			if config.Target() != test.wantTarget || config.Scope() != test.wantScope ||
				string(config.Ownership()) != test.wantOwnership || string(config.Syntax()) != test.wantSyntax ||
				config.Source().Kind() != test.wantSource {
				t.Fatalf("normalized config = target %q scope %q ownership %q syntax %q source %q", config.Target(), config.Scope(), config.Ownership(), config.Syntax(), config.Source().Kind())
			}

			encoded, err := codec.RenderHostConfigBlock(config)
			if err != nil {
				t.Fatalf("RenderHostConfigBlock() error = %v", err)
			}
			roundTrip, err := Decode(hostConfigDocument(fmt.Sprintf("%q", test.manifestTarget), test.defaultScope, encoded))
			if err != nil {
				t.Fatalf("Decode(rendered config) error = %v\n%s", err, encoded)
			}
			roundTripConfigs := roundTrip.HostConfigs()
			if len(roundTripConfigs) != 1 || roundTripConfigs[0].ID() != config.ID() ||
				roundTripConfigs[0].Target() != config.Target() || roundTripConfigs[0].Scope() != config.Scope() ||
				roundTripConfigs[0].Path() != config.Path() || roundTripConfigs[0].Ownership() != config.Ownership() ||
				roundTripConfigs[0].Syntax() != config.Syntax() {
				t.Fatalf("round-tripped config = %#v, want original canonical values", roundTripConfigs)
			}
			wantSourceID, err := source.SourceIDFor(config.Source())
			if err != nil {
				t.Fatal(err)
			}
			gotSourceID, err := source.SourceIDFor(roundTripConfigs[0].Source())
			if err != nil || gotSourceID != wantSourceID {
				t.Fatalf("round-tripped source identity = %q, %v; want %q", gotSourceID, err, wantSourceID)
			}
		})
	}
}

func TestDecodeNormalizesHostConfigNestedSourceTable(t *testing.T) {
	environment, err := Decode([]byte(`version = 1
targets = ["pi"]
[defaults]
scope = "global"

[[host_config]]
id = "nested-source"
path = "settings.json"

[host_config.source]
git = "https://github.com/example/settings.git"
path = "settings.json"
ref = "main"
`))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	configs := environment.HostConfigs()
	if len(configs) != 1 || configs[0].Source().Kind() != source.SourceKindGit {
		t.Fatalf("HostConfigs() = %#v, want one Git-backed config", configs)
	}
	git, ok := configs[0].Source().Git()
	if !ok || git.Locator().String() != "https://github.com/example/settings.git" ||
		git.RepositoryPath().String() != "settings.json" || git.Ref().String() != "main" {
		t.Fatalf("normalized nested source = %#v, want its Git declaration values", git)
	}
}

func TestDecodeRejectsInvalidHostConfigDeclarations(t *testing.T) {
	tests := []struct {
		name            string
		manifestTargets string
		body            string
		wantErr         string
	}{
		{
			name: "missing source",
			body: `[[host_config]]
id = "missing-source"
path = "settings.json"
`,
			wantErr: "source: required",
		},
		{
			name: "multiple targets",
			body: `[[host_config]]
id = "two-targets"
targets = ["pi", "codex"]
path = "settings.json"
source = "/workbench/settings.json"
`,
			wantErr: "exactly one target",
		},
		{
			name: "duplicate target",
			body: `[[host_config]]
id = "duplicate-target"
targets = ["pi", "pi"]
path = "settings.json"
source = "/workbench/settings.json"
`,
			wantErr: "duplicate target",
		},
		{
			name: "unknown host",
			body: `[[host_config]]
id = "unsupported-host"
targets = ["unknown"]
path = "settings.json"
source = "/workbench/settings.json"
`,
			wantErr: "unknown target",
		},
		{
			name:            "inherited multiple hosts",
			manifestTargets: `"pi", "codex"`,
			body: `[[host_config]]
id = "inherited-hosts"
path = "settings.json"
source = "/workbench/settings.json"
`,
			wantErr: "exactly one target",
		},
		{
			name: "duplicate ID",
			body: `[[host_config]]
id = "duplicate"
path = "first.json"
source = "/workbench/first.json"
[[host_config]]
id = "duplicate"
path = "second.json"
source = "/workbench/second.json"
`,
			wantErr: "duplicate host_config id",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			targets := test.manifestTargets
			if targets == "" {
				targets = `"pi"`
			}
			content := hostConfigDocument(targets, "global", test.body)
			_, err := Decode(content)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Decode() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestDecodeRejectsInvalidHostConfigFieldsAndSources(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name: "invalid scope",
			body: `[[host_config]]
id = "invalid-scope"
scope = "machine"
path = "settings.json"
source = "/workbench/settings.json"
`,
			wantErr: "unknown scope",
		},
		{
			name: "traversal path",
			body: `[[host_config]]
id = "traversal"
path = "../settings.json"
source = "/workbench/settings.json"
`,
			wantErr: "relative components",
		},
		{
			name: "absolute destination",
			body: `[[host_config]]
id = "absolute"
path = "/etc/settings.json"
source = "/workbench/settings.json"
`,
			wantErr: "host-root-relative",
		},
		{
			name: "unknown ownership",
			body: `[[host_config]]
id = "ownership"
path = "settings.json"
source = "/workbench/settings.json"
ownership = "keys-and-file"
`,
			wantErr: "unknown host configuration ownership",
		},
		{
			name: "unknown syntax",
			body: `[[host_config]]
id = "syntax"
path = "settings.json"
source = "/workbench/settings.json"
syntax = "yaml"
`,
			wantErr: "unknown host configuration syntax",
		},
		{
			name: "local link source",
			body: `[[host_config]]
id = "link"
path = "settings.json"
source = { path = "/workbench/settings.json", mode = "link" }
`,
			wantErr: "must use vendor mode",
		},
		{
			name: "global relative local source",
			body: `[[host_config]]
id = "relative-global"
path = "settings.json"
source = "settings.json"
`,
			wantErr: "absolute path",
		},
		{
			name: "S3 archive source",
			body: `[[host_config]]
id = "archive"
path = "settings.json"
source = { s3 = "s3://settings-bucket/settings.tar.gz", format = "tar.gz" }
`,
			wantErr: "must use file format",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Decode(hostConfigDocument(`"pi"`, "global", test.body))
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Decode() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func hostConfigManifest(manifestTarget string, defaultScope string, fields string) []byte {
	return hostConfigDocument(fmt.Sprintf("%q", manifestTarget), defaultScope, "[[host_config]]\n"+fields)
}

func hostConfigDocument(manifestTargets string, defaultScope string, body string) []byte {
	return []byte(fmt.Sprintf(`version = 1
targets = [%s]

[defaults]
scope = %q

%s`, manifestTargets, defaultScope, body))
}
