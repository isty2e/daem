package declaration

import (
	"testing"
)

func TestDecodeManifestHostConfigRowsAndIdentity(t *testing.T) {
	content := []byte(`version = 1
targets = ["pi"]

[[host_config]]
id = "native-settings"
path = "settings.json"
source = "/workbench/settings.json"

[[host_config]]
id = "remote-settings"
targets = ["codex"]
scope = "global"
path = "config.toml"
source = { git = "https://github.com/example/settings.git", path = "config.toml", ref = "main" }
ownership = "document"
syntax = "toml"
`)

	manifest, err := DecodeManifest(content)
	if err != nil {
		t.Fatalf("DecodeManifest() error = %v", err)
	}
	if len(manifest.HostConfigs) != 2 {
		t.Fatalf("len(HostConfigs) = %d, want 2", len(manifest.HostConfigs))
	}
	local := manifest.HostConfigs[0]
	if local.ID != "native-settings" || local.Path != "settings.json" ||
		!local.Source.Set || local.Source.Source.Path != "/workbench/settings.json" ||
		local.Source.Source.Mode != "vendor" {
		t.Fatalf("local host config = %#v, want decoded local source with vendor mode", local)
	}
	if key := local.Key(); key != (Key{Kind: KindHostConfig, Name: "native-settings"}) {
		t.Fatalf("Key() = %#v, want host_config/native-settings", key)
	}
	remote := manifest.HostConfigs[1]
	if remote.Source.Source.Git != "https://github.com/example/settings.git" ||
		remote.Source.Source.Path != "config.toml" || remote.Source.Source.Ref != "main" ||
		remote.Ownership != "document" || remote.Syntax != "toml" {
		t.Fatalf("remote host config = %#v, want its structured source and selectors", remote)
	}
}

func TestDecodeManifestRejectsUnknownHostConfigFields(t *testing.T) {
	for _, content := range []string{
		`version = 1
targets = ["pi"]
[[host_config]]
id = "settings"
path = "settings.json"
source = "/workbench/settings.json"
unknown = "value"
`,
		`version = 1
targets = ["pi"]
[[host_config]]
id = "settings"
path = "settings.json"
source = { path = "/workbench/settings.json", future = "value" }
`,
		`version = 1
targets = ["pi"]
[[host_config]]
id = "settings"
path = "settings.json"
source = { path = "/workbench/settings.json", Path = "/other/settings.json" }
`,
	} {
		if _, err := DecodeManifest([]byte(content)); err == nil {
			t.Fatalf("DecodeManifest() accepted unknown or conflicting host_config syntax:\n%s", content)
		}
	}
}

func TestDecodeManifestKeepsManifestVersionOne(t *testing.T) {
	manifest, err := DecodeManifest([]byte(`version = 1
targets = ["pi"]
[[host_config]]
id = "settings"
path = "settings.json"
source = "/workbench/settings.json"
`))
	if err != nil {
		t.Fatalf("DecodeManifest() error = %v", err)
	}
	if CurrentManifestVersion != 1 {
		t.Fatalf("CurrentManifestVersion = %d, want unchanged version 1", CurrentManifestVersion)
	}
	if manifest.Version != CurrentManifestVersion {
		t.Fatalf("manifest version = %d, want current version %d", manifest.Version, CurrentManifestVersion)
	}
}
