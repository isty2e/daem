package declaration

import (
	"strings"
	"testing"
)

func TestDecodeManifestHeaderExtractsTargetsAndDefaults(t *testing.T) {
	header, err := DecodeManifestHeader([]byte(`version = 1
targets = ["codex", "claude-code"]

[defaults]
scope = "global"
install_mode = "copy"

[[skill]]
name = "review"
source = { path = "skills/review", mode = "vendor" }
`))
	if err != nil {
		t.Fatalf("DecodeManifestHeader returned error: %v", err)
	}
	if strings.Join(header.Targets, ",") != "codex,claude-code" {
		t.Fatalf("targets = %#v, want codex and claude-code", header.Targets)
	}
	if header.Defaults.Scope != "global" {
		t.Fatalf("defaults.scope = %q, want global", header.Defaults.Scope)
	}
	if header.Defaults.InstallMode != "copy" {
		t.Fatalf("defaults.install_mode = %q, want copy", header.Defaults.InstallMode)
	}
}

func TestDecodeManifestHeaderPreservesParseErrorPrefix(t *testing.T) {
	_, err := DecodeManifestHeader([]byte("version =\n"))
	if err == nil || !strings.Contains(err.Error(), "parse manifest header:") {
		t.Fatalf("err = %v, want parse manifest header prefix", err)
	}
}

func TestManifestHeaderResolvesDocumentLocalDefaultsDefensively(t *testing.T) {
	header := ManifestHeader{Targets: []string{"codex"}, Defaults: Defaults{Scope: " global "}}
	if got := header.EffectiveScope(""); got != "global" {
		t.Fatalf("EffectiveScope = %q", got)
	}
	if got := (ManifestHeader{}).EffectiveScope(""); got != "project" {
		t.Fatalf("zero-header EffectiveScope = %q", got)
	}
	targets := header.EffectiveTargets(nil)
	targets[0] = "mutated"
	if header.Targets[0] != "codex" {
		t.Fatal("EffectiveTargets returned an alias of header targets")
	}
	if got := header.EffectiveTargets([]string{"pi"}); len(got) != 1 || got[0] != "pi" {
		t.Fatalf("explicit EffectiveTargets = %#v", got)
	}
}

func TestManifestDecodersRejectDuplicateKeysAndTables(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "duplicate key", content: "version = 1\nversion = 1\n"},
		{name: "duplicate table", content: "version = 1\n[defaults]\nscope = \"project\"\n[defaults]\ninstall_mode = \"copy\"\n"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := DecodeManifest([]byte(test.content)); err == nil {
				t.Fatal("DecodeManifest() error = nil, want duplicate syntax rejection")
			}
			if _, err := DecodeManifestHeader([]byte(test.content)); err == nil {
				t.Fatal("DecodeManifestHeader() error = nil, want duplicate syntax rejection")
			}
		})
	}
}
