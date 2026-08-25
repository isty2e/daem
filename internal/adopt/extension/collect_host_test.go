package extension

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/target"
)

func TestCollectImportsClaudeProjectAndGlobalExactSelectors(t *testing.T) {
	root := isolatedExtensionImportRoot(t)
	configRoot := filepath.Join(root, "claude")
	t.Setenv("CLAUDE_CONFIG_DIR", configRoot)
	writeExtensionFixture(t, filepath.Join(configRoot, "plugins", "installed_plugins.json"), `{
  "version": 2,
  "plugins": {
    "alpha@official": [
      {"scope": "project", "projectPath": "`+filepath.ToSlash(root)+`"},
      {"scope": "user", "projectPath": ""}
    ]
  }
}`)

	result, err := Collect(Input{
		ManifestRoot: root,
		Targets:      []target.Target{target.TargetClaudeCode},
		Scopes:       []target.Scope{target.ScopeProject, target.ScopeGlobal},
	})
	if err != nil {
		t.Fatal(err)
	}
	extensions := result.Extensions()
	if len(extensions) != 2 {
		t.Fatalf("extensions = %#v, want project and global rows", extensions)
	}
	for index, scope := range []target.Scope{target.ScopeGlobal, target.ScopeProject} {
		extension := extensions[index]
		if extension.Carrier() != desiredextension.CarrierClaudeCodePlugin ||
			extension.Scope() != scope ||
			extension.Source().Kind() != desiredextension.SourceKindMarketplace ||
			extension.Source().Ref() != "alpha@official" {
			t.Fatalf("extension[%d] = %#v", index, extension)
		}
	}
	if len(result.Scans()) != 2 {
		t.Fatalf("scans = %#v, want one selected inventory fact per scope", result.Scans())
	}
}

func TestCollectImportsCodexGlobalExactConfiguredSelectors(t *testing.T) {
	root := isolatedExtensionImportRoot(t)
	codexHome := filepath.Join(root, "codex")
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", codexHome)
	writeExtensionFixture(t, filepath.Join(codexHome, "config.toml"), `
[plugins."alpha@official"]
enabled = true

[plugins."beta@other"]
enabled = false
`)

	result, err := Collect(Input{
		ManifestRoot: root,
		Targets:      []target.Target{target.TargetCodex},
		Scopes:       []target.Scope{target.ScopeGlobal},
	})
	if err != nil {
		t.Fatal(err)
	}
	extensions := result.Extensions()
	sources := extensionSources(extensions)
	slices.Sort(sources)
	if !slices.Equal(sources, []string{"alpha@official", "beta@other"}) {
		t.Fatalf("extensions = %#v", extensions)
	}
}

func TestCollectPreservesOpenCodePhysicalSequencesAndExactSources(t *testing.T) {
	root := isolatedExtensionImportRoot(t)
	configRoot := filepath.Join(root, ".opencode")
	writeExtensionFixture(t, filepath.Join(configRoot, "opencode.json"), `{
  "plugin": ["@acme/tool@1.2.3", "./plugins/local.ts"]
}`)
	writeExtensionFixture(t, filepath.Join(configRoot, "tui.jsonc"), `{
  // separate physical sequence
  "plugin": ["other@beta"],
}`)

	result, err := Collect(Input{
		ManifestRoot: root,
		Targets:      []target.Target{target.TargetOpenCode},
		Scopes:       []target.Scope{target.ScopeProject},
	})
	if err != nil {
		t.Fatal(err)
	}
	sources := extensionSources(result.Extensions())
	slices.Sort(sources)
	if !slices.Equal(sources, []string{
		"./plugins/local.ts",
		"@acme/tool@1.2.3",
		"other@beta",
	}) {
		t.Fatalf("sources = %#v", sources)
	}
	sequences := result.sequences
	if len(sequences) != 2 {
		t.Fatalf("sequences = %#v, want server and TUI", sequences)
	}
	if sequences[0].SequenceID() != "opencode:project:server.json.plugins" ||
		sequences[1].SequenceID() != "opencode:project:tui.jsonc.plugins" {
		t.Fatalf(
			"sequence identities = %q, %q",
			sequences[0].SequenceID(),
			sequences[1].SequenceID(),
		)
	}
	serverRows := sequences[0].OrderedRows()
	if len(serverRows) != 2 ||
		string(serverRows[0].HostLoadIdentity()) != "@acme/tool" ||
		string(serverRows[1].HostLoadIdentity()) !=
			"file://"+filepath.ToSlash(filepath.Join(configRoot, "plugins", "local.ts")) {
		t.Fatalf("server rows = %#v", serverRows)
	}
	if len(sequences[1].OrderedRows()) != 1 ||
		string(sequences[1].OrderedRows()[0].HostLoadIdentity()) != "other" {
		t.Fatalf("TUI rows = %#v", sequences[1].OrderedRows())
	}
}

func TestCollectRejectsOpenCodeSourcesThatCollapseToOneLoadIdentity(t *testing.T) {
	root := isolatedExtensionImportRoot(t)
	writeExtensionFixture(
		t,
		filepath.Join(root, ".opencode", "opencode.json"),
		`{"plugin":["@acme/tool@1.2.3","@acme/tool@2.0.0"]}`,
	)

	_, err := Collect(Input{
		ManifestRoot: root,
		Targets:      []target.Target{target.TargetOpenCode},
		Scopes:       []target.Scope{target.ScopeProject},
	})
	if err == nil || !strings.Contains(err.Error(), `host load identity "@acme/tool" appears more than once`) {
		t.Fatalf("Collect error = %v, want ambiguous OpenCode load identity", err)
	}
}

func TestCollectPreservesPiSourceClassesAndOrder(t *testing.T) {
	root := isolatedExtensionImportRoot(t)
	localRoot := filepath.Join(root, ".pi", "local-package")
	if err := os.MkdirAll(localRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	writeExtensionFixture(t, filepath.Join(root, ".pi", "settings.json"), `{
  "packages": ["npm:@acme/tool@1.2.3", "github:owner/repo", "./local-package"]
}`)

	result, err := Collect(Input{
		ManifestRoot: root,
		Targets:      []target.Target{target.TargetPi},
		Scopes:       []target.Scope{target.ScopeProject},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := extensionSources(result.Extensions()); !slices.Equal(got, []string{
		"npm:@acme/tool@1.2.3",
		"github:owner/repo",
		filepath.ToSlash(filepath.Join(".pi", "local-package")),
	}) {
		t.Fatalf("sources = %#v", got)
	}
	rows := result.sequences[0].OrderedRows()
	if len(rows) != 3 ||
		string(rows[0].HostLoadIdentity()) != "npm:@acme/tool" ||
		string(rows[1].HostLoadIdentity()) != "git:github.com/owner/repo" ||
		string(rows[2].HostLoadIdentity()) !=
			"local:project:"+filepath.Clean(localRoot) {
		t.Fatalf("Pi sequence rows = %#v", rows)
	}
}

func TestCollectRejectsOverlongPiSourceAtRawAdmission(t *testing.T) {
	root := isolatedExtensionImportRoot(t)
	overlong := "npm:@acme/tool@" + strings.Repeat("a", 3000)
	writeExtensionFixture(
		t,
		filepath.Join(root, ".pi", "settings.json"),
		`{"packages":["`+overlong+`"]}`,
	)

	_, err := Collect(Input{
		ManifestRoot: root,
		Targets:      []target.Target{target.TargetPi},
		Scopes:       []target.Scope{target.ScopeProject},
	})
	if err == nil ||
		!strings.Contains(err.Error(), "admit Pi settings package source") ||
		!strings.Contains(err.Error(), "admission length limit") {
		t.Fatalf("Collect error = %v, want raw admission length rejection", err)
	}
}

func TestCollectRejectsPiAliasesThatCollapseToOneLoadIdentity(t *testing.T) {
	tests := []struct {
		name     string
		packages string
		identity string
	}{
		{
			name:     "npm versions",
			packages: `"npm:@acme/tool@1.2.3","npm:@acme/tool@2.0.0"`,
			identity: "npm:@acme/tool",
		},
		{
			name:     "Git transports and refs",
			packages: `"github:acme/tool@v1","git:https://github.com/acme/tool@v2"`,
			identity: "git:github.com/acme/tool",
		},
		{
			name:     "local spellings",
			packages: `"./local-package","local-package"`,
			identity: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := isolatedExtensionImportRoot(t)
			writeExtensionFixture(
				t,
				filepath.Join(root, ".pi", "settings.json"),
				`{"packages":[`+test.packages+`]}`,
			)

			_, err := Collect(Input{
				ManifestRoot: root,
				Targets:      []target.Target{target.TargetPi},
				Scopes:       []target.Scope{target.ScopeProject},
			})
			if err == nil || !strings.Contains(err.Error(), "host load identity") ||
				(test.identity != "" && !strings.Contains(err.Error(), test.identity)) {
				t.Fatalf("Collect error = %v, want ambiguous Pi load identity %q", err, test.identity)
			}
		})
	}
}

func TestCollectReportsAntigravitySourceProvenanceSkip(t *testing.T) {
	root := isolatedExtensionImportRoot(t)
	importManifest := filepath.Join(
		root,
		".gemini",
		"config",
		"import_manifest.json",
	)
	writeExtensionFixture(t, importManifest, `{"imports":[{"name":"guidance"}]}`)
	writeExtensionFixture(
		t,
		filepath.Join(root, ".gemini", "config", "plugins", "guidance", "plugin.json"),
		`{"name":"guidance"}`,
	)

	result, err := Collect(Input{
		ManifestRoot: root,
		Targets:      []target.Target{target.TargetAntigravityCLI},
		Scopes:       []target.Scope{target.ScopeGlobal},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Extensions()) != 0 {
		t.Fatalf("extensions = %#v, want no source-inexact declaration", result.Extensions())
	}
	skipped := result.Skipped()
	if len(skipped) != 1 ||
		skipped[0].Reason != reasonSourceProvenanceUnrecoverable ||
		skipped[0].Target != target.TargetAntigravityCLI ||
		skipped[0].Scope != target.ScopeGlobal {
		t.Fatalf("skipped = %#v", skipped)
	}
}

func isolatedExtensionImportRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(root, ".pi-agent"))
	t.Setenv("CODEX_HOME", "")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	return root
}

func writeExtensionFixture(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func extensionSources(extensions []desiredextension.Extension) []string {
	sources := make([]string, 0, len(extensions))
	for _, extension := range extensions {
		sources = append(sources, extension.Source().Ref())
	}
	return sources
}
