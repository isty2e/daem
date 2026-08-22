package codexplugin

import (
	"path/filepath"
	"testing"

	observecontribution "github.com/isty2e/daem/internal/assurance/observe/contribution"
)

func TestObserveConfiguredPluginContributionsBlocksDuplicatePluginManifestKeys(t *testing.T) {
	cases := []struct {
		name     string
		manifest string
		setup    func(*testing.T, string)
	}{
		{name: "name", manifest: `{"name":"first","name":"second"}`},
		{name: "version", manifest: `{"version":"1","version":"2"}`},
		{
			name:     "skills",
			manifest: `{"skills":"./first","skills":"./second"}`,
			setup: func(t *testing.T, root string) {
				writeFile(t, filepath.Join(root, "first", "SKILL.md"), "---\nname: first\n---\n")
				writeFile(t, filepath.Join(root, "second", "SKILL.md"), "---\nname: second\n---\n")
			},
		},
		{
			name:     "mcpServers",
			manifest: `{"mcpServers":{"first":{}},"mcpServers":{"second":{}}}`,
		},
		{
			name:     "apps",
			manifest: `{"apps":"./first.json","apps":"./second.json"}`,
			setup: func(t *testing.T, root string) {
				writeFile(t, filepath.Join(root, "first.json"), `{}`)
				writeFile(t, filepath.Join(root, "second.json"), `{}`)
			},
		},
		{
			name:     "hooks",
			manifest: `{"hooks":{"first":{}},"hooks":{"second":{}}}`,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			homeDirectory := t.TempDir()
			root := codexPluginRoot(homeDirectory, "market", "alpha", "local")
			writeFile(t, filepath.Join(root, ".codex-plugin", "plugin.json"), testCase.manifest)
			if testCase.setup != nil {
				testCase.setup(t, root)
			}

			observations := observeIndependentPluginContributions(
				t.Context(),
				homeDirectory,
				configuredPluginObservation(t, "alpha@market"),
			)
			assertMalformedContributionObservation(t, observations)
		})
	}
}

func TestObserveConfiguredPluginContributionsBlocksNestedDuplicateKeys(t *testing.T) {
	cases := []struct {
		name     string
		manifest string
	}{
		{name: "escaped equivalent root key", manifest: `{"mcpServers":{"first":{}},"\u006dcpServers":{"second":{}}}`},
		{name: "inline MCP server key", manifest: `{"mcpServers":{"same":{},"same":{}}}`},
		{name: "nested MCP server field", manifest: `{"mcpServers":{"server":{"command":"first","command":"second"}}}`},
		{name: "inline hook key", manifest: `{"hooks":{"same":{},"same":{}}}`},
		{name: "nested inline hook field", manifest: `{"hooks":{"event":{"command":"first","command":"second"}}}`},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			homeDirectory := t.TempDir()
			root := codexPluginRoot(homeDirectory, "market", "alpha", "local")
			writeFile(t, filepath.Join(root, ".codex-plugin", "plugin.json"), testCase.manifest)

			observations := observeIndependentPluginContributions(
				t.Context(),
				homeDirectory,
				configuredPluginObservation(t, "alpha@market"),
			)
			assertMalformedContributionObservation(t, observations)
		})
	}
}

func TestObserveConfiguredPluginContributionsBlocksReferencedMCPDuplicateKeys(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{name: "repeated wrapper", content: `{"mcpServers":{"first":{}},"mcpServers":{"second":{}}}`},
		{name: "repeated wrapped server", content: `{"mcpServers":{"same":{},"same":{}}}`},
		{name: "repeated direct server", content: `{"same":{},"same":{}}`},
		{name: "nested server field", content: `{"mcpServers":{"server":{"command":"first","command":"second"}}}`},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			homeDirectory := t.TempDir()
			root := codexPluginRoot(homeDirectory, "market", "alpha", "local")
			writeFile(t, filepath.Join(root, ".codex-plugin", "plugin.json"), `{"mcpServers":"./mcp.json"}`)
			writeFile(t, filepath.Join(root, "mcp.json"), testCase.content)

			observations := observeIndependentPluginContributions(
				t.Context(),
				homeDirectory,
				configuredPluginObservation(t, "alpha@market"),
			)
			assertMalformedContributionObservation(t, observations)
		})
	}
}

func TestObserveConfiguredPluginContributionsKeepsDuplicateKeysScopedToOneObject(t *testing.T) {
	homeDirectory := t.TempDir()
	root := codexPluginRoot(homeDirectory, "market", "alpha", "local")
	writeFile(t, filepath.Join(root, ".codex-plugin", "plugin.json"), `{
  "mcpServers": {
    "first": {"command": "node"},
    "second": {"command": "node"}
  }
}`)

	observations := observeIndependentPluginContributions(
		t.Context(),
		homeDirectory,
		configuredPluginObservation(t, "alpha@market"),
	)
	if len(observations) != 1 {
		t.Fatalf("observations = %#v, want one", observations)
	}
	rows := observations[0].DiagnosticRows()
	if len(rows) != 2 {
		t.Fatalf("rows = %#v, want two source-declared MCP rows", rows)
	}
	assertSourceContribution(t, rows, observecontribution.SourceContributionMCPServer, "first")
	assertSourceContribution(t, rows, observecontribution.SourceContributionMCPServer, "second")
}

func TestObserveConfiguredPluginContributionsKeepsValidProviderSiblingAfterDuplicate(t *testing.T) {
	homeDirectory := t.TempDir()
	alphaRoot := codexPluginRoot(homeDirectory, "market", "alpha", "local")
	writeFile(t, filepath.Join(alphaRoot, ".codex-plugin", "plugin.json"), `{
  "skills": "./skills",
  "mcpServers": {"same": {}, "same": {}}
}`)
	writeFile(t, filepath.Join(alphaRoot, "skills", "review", "SKILL.md"), "---\nname: review\n---\n")
	betaRoot := codexPluginRoot(homeDirectory, "market", "beta", "local")
	writeFile(t, filepath.Join(betaRoot, ".codex-plugin", "plugin.json"), `{"mcpServers":{"valid":{}}}`)

	observations := observeIndependentPluginContributions(
		t.Context(),
		homeDirectory,
		configuredPluginObservation(t, "alpha@market", "beta@market"),
	)
	if len(observations) != 2 {
		t.Fatalf("observations = %#v, want malformed alpha and valid beta", observations)
	}
	assertMalformedContributionObservation(t, observations[:1])
	betaRows := observations[1].DiagnosticRows()
	if len(betaRows) != 1 || betaRows[0].State() != observecontribution.SourceContributionDeclared {
		t.Fatalf("beta observation = %#v, want one declared row", observations[1])
	}
	assertSourceContribution(t, betaRows, observecontribution.SourceContributionMCPServer, "valid")
}

func assertMalformedContributionObservation(
	t *testing.T,
	observations []observecontribution.SourceContributionObservation,
) {
	t.Helper()
	if len(observations) != 1 {
		t.Fatalf("observations = %#v, want one provider blocker", observations)
	}
	rows := observations[0].DiagnosticRows()
	if len(rows) != 1 {
		t.Fatalf("rows = %#v, want one provider blocker row", rows)
	}
	row := rows[0]
	if row.State() != observecontribution.SourceContributionBlocked ||
		row.Reason() != observecontribution.SourceContributionReasonArtifactMalformed ||
		row.HasContribution() {
		t.Fatalf("observation = %#v, want malformed blocker without contributions", observations[0])
	}
}
