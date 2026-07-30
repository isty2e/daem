package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunImportWritesSourceExactExtensionRowsAcrossHostScopes(t *testing.T) {
	t.Run("Claude Code project and global", func(t *testing.T) {
		root := isolatedExtensionImportCLI(t)
		configRoot := filepath.Join(root, "claude")
		t.Setenv("CLAUDE_CONFIG_DIR", configRoot)
		testkit.WriteFile(
			t,
			filepath.Join(configRoot, "plugins"),
			"installed_plugins.json",
			`{"version":2,"plugins":{"alpha@official":[{"scope":"project","projectPath":`+
				strconv.Quote(filepath.ToSlash(root))+
				`},{"scope":"user","projectPath":""}]}}`,
		)

		manifestPath := importExtensionsCLI(
			t,
			root,
			"claude-code",
			"project",
			"global",
		)
		assertImportedExtensionRows(t, manifestPath, []extensionRowExpectation{
			{
				carrier: desiredextension.CarrierClaudeCodePlugin,
				target:  target.TargetClaudeCode,
				scope:   target.ScopeGlobal,
				kind:    desiredextension.SourceKindMarketplace,
				source:  "alpha@official",
			},
			{
				carrier: desiredextension.CarrierClaudeCodePlugin,
				target:  target.TargetClaudeCode,
				scope:   target.ScopeProject,
				kind:    desiredextension.SourceKindMarketplace,
				source:  "alpha@official",
			},
		})
	})

	t.Run("Codex global", func(t *testing.T) {
		root := isolatedExtensionImportCLI(t)
		codexHome := filepath.Join(root, "codex")
		t.Setenv("CODEX_HOME", codexHome)
		testkit.WriteFile(
			t,
			codexHome,
			"config.toml",
			"[plugins.\"alpha@official\"]\nenabled = true\n",
		)

		manifestPath := importExtensionsCLI(t, root, "codex", "global")
		assertImportedExtensionRows(t, manifestPath, []extensionRowExpectation{
			{
				carrier: desiredextension.CarrierCodexPlugin,
				target:  target.TargetCodex,
				scope:   target.ScopeGlobal,
				kind:    desiredextension.SourceKindMarketplace,
				source:  "alpha@official",
			},
		})
	})

	t.Run("OpenCode global", func(t *testing.T) {
		root := isolatedExtensionImportCLI(t)
		testkit.WriteFile(
			t,
			filepath.Join(root, "config", "opencode"),
			"opencode.json",
			`{"plugin":["@acme/alpha@1.2.3"]}`,
		)

		manifestPath := importExtensionsCLI(t, root, "opencode", "global")
		assertImportedExtensionRows(t, manifestPath, []extensionRowExpectation{
			{
				carrier: desiredextension.CarrierOpenCodePlugin,
				target:  target.TargetOpenCode,
				scope:   target.ScopeGlobal,
				kind:    desiredextension.SourceKindHostSource,
				source:  "@acme/alpha@1.2.3",
			},
		})
	})

	t.Run("Pi global", func(t *testing.T) {
		root := isolatedExtensionImportCLI(t)
		testkit.WriteFile(
			t,
			filepath.Join(root, "pi"),
			"settings.json",
			`{"packages":["npm:@acme/alpha@1.2.3"]}`,
		)

		manifestPath := importExtensionsCLI(t, root, "pi", "global")
		assertImportedExtensionRows(t, manifestPath, []extensionRowExpectation{
			{
				carrier: desiredextension.CarrierPiPackage,
				target:  target.TargetPi,
				scope:   target.ScopeGlobal,
				kind:    desiredextension.SourceKindHostSource,
				source:  "npm:@acme/alpha@1.2.3",
			},
		})
	})
}

func TestRunImportReportsSourceInexactAntigravityExtensionWithoutManifest(t *testing.T) {
	root := isolatedExtensionImportCLI(t)
	configRoot := filepath.Join(root, ".gemini", "config")
	testkit.WriteFile(
		t,
		configRoot,
		"import_manifest.json",
		`{"imports":[{"name":"guidance"}]}`,
	)
	testkit.WriteFile(
		t,
		filepath.Join(configRoot, "plugins", "guidance"),
		"plugin.json",
		`{"name":"guidance"}`,
	)
	manifestPath := filepath.Join(root, "daem.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(
		[]string{
			"import",
			"--target", "antigravity-cli",
			"--scope", "global",
			"--manifest", manifestPath,
		},
		&stdout,
		&stderr,
	)
	if exitCode != 1 || stdout.Len() != 0 {
		t.Fatalf(
			"import exitCode=%d stdout=%q stderr=%q, want diagnostic failure",
			exitCode,
			stdout.String(),
			stderr.String(),
		)
	}
	for _, want := range []string{
		"nothing to import",
		"source_provenance_unrecoverable",
		"import_manifest.json#plugin=guidance",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
	testkit.AssertPathMissing(t, manifestPath)
}

func TestRunImportMergeRejectsNativeExtensionOrderContradictingAuthoredIntent(t *testing.T) {
	root := isolatedExtensionImportCLI(t)
	testkit.WriteFile(
		t,
		filepath.Join(root, ".pi"),
		"settings.json",
		`{"packages":["npm:first","npm:second"]}`,
	)
	const manifest = `version = 1
targets = ["pi"]

[[extension]]
id = "second"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "npm:second" }

[[extension]]
id = "first"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "npm:first" }
`
	manifestPath := filepath.Join(root, "daem.toml")
	testkit.WriteFile(t, root, "daem.toml", manifest)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(
		[]string{
			"import",
			"--target", "pi",
			"--scope", "project",
			"--manifest", manifestPath,
			"--merge",
		},
		&stdout,
		&stderr,
	)
	if exitCode != 1 || stdout.Len() != 0 ||
		!strings.Contains(stderr.String(), "contradicts") {
		t.Fatalf(
			"import exitCode=%d stdout=%q stderr=%q, want order conflict",
			exitCode,
			stdout.String(),
			stderr.String(),
		)
	}
	testkit.AssertFileContent(t, manifestPath, manifest)
	testkit.AssertPathMissing(t, filepath.Join(root, "daem.lock.toml"))
}

type extensionRowExpectation struct {
	carrier desiredextension.Carrier
	target  target.Target
	scope   target.Scope
	kind    desiredextension.SourceKind
	source  string
}

func isolatedExtensionImportCLI(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	testkit.WithWorkingDirectory(t, root)
	t.Setenv("HOME", root)
	testkit.SetDefaultRootEnv(t, root)
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(root, "pi"))
	t.Setenv("CODEX_HOME", "")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	return root
}

func importExtensionsCLI(
	t *testing.T,
	root string,
	selectedTarget string,
	scopes ...string,
) string {
	t.Helper()
	manifestPath := filepath.Join(root, "daem.toml")
	args := []string{
		"import",
		"--target", selectedTarget,
		"--manifest", manifestPath,
	}
	for _, scope := range scopes {
		args = append(args, "--scope", scope)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf(
			"import exitCode=%d stdout=%q stderr=%q",
			exitCode,
			stdout.String(),
			stderr.String(),
		)
	}
	if !strings.Contains(stdout.String(), "extensions=") {
		t.Fatalf("stdout = %q, want extension import summary", stdout.String())
	}
	return manifestPath
}

func assertImportedExtensionRows(
	t *testing.T,
	manifestPath string,
	want []extensionRowExpectation,
) {
	t.Helper()
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile manifest returned error: %v", err)
	}
	environment, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("generated manifest did not parse: %v\n%s", err, content)
	}
	extensions := environment.Extensions()
	if len(extensions) != len(want) {
		t.Fatalf("extensions = %#v, want %#v", extensions, want)
	}
	for index, expected := range want {
		extension := extensions[index]
		if extension.Carrier() != expected.carrier ||
			extension.Target() != expected.target ||
			extension.Scope() != expected.scope ||
			extension.Source().Kind() != expected.kind ||
			extension.Source().Ref() != expected.source {
			t.Fatalf("extension[%d] = %#v, want %#v", index, extension, expected)
		}
	}
}
