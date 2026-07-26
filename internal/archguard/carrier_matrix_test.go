package archguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCarrierFeatureMatrixKeepsSplitCarrierRows(t *testing.T) {
	content := readRepoText(t, "docs/host-integrations.md")

	if markdownTableRowExists(content, "Plugin or extension carriers") {
		t.Fatalf("docs/host-integrations.md reintroduced the overloaded Plugin or extension carriers row")
	}

	for _, row := range []string{
		"Carrier declaration and relation diagnostics",
		"Passive carrier observation",
		"Provider-scoped contribution diagnostics",
		"Host-delegated carrier lifecycle routes",
		"Carrier destructive cleanup and prune",
	} {
		if !strings.Contains(content, "| "+row+" |") {
			t.Fatalf("docs/host-integrations.md is missing split carrier matrix row %q", row)
		}
	}
}

func TestFeatureSupportKeepsUserFacingVocabularyAndCompactCells(t *testing.T) {
	content := readRepoText(t, "docs/features.md")
	lowerContent := strings.ToLower(content)

	for _, forbidden := range []string{
		"carrier",
		"host-delegated",
		"provider-scoped",
		"passive observation",
		"admitted route",
	} {
		if strings.Contains(lowerContent, forbidden) {
			t.Fatalf("docs/features.md exposes internal term %q", forbidden)
		}
	}

	for _, header := range [][]string{
		{"Feature", "Codex", "Claude Code", "OpenCode", "Pi", "Antigravity CLI"},
		{"Action", "Codex", "Claude Code", "OpenCode", "Pi", "Antigravity CLI"},
	} {
		tables := featureMatrixTablesWithHeader(content, header)
		if len(tables) != 1 {
			t.Fatalf("docs/features.md has %d tables with header %q, want 1", len(tables), header)
		}
		for _, row := range tables[0].rows {
			for column := 1; column < len(row); column++ {
				if words := len(strings.Fields(row[column])); words > 3 {
					t.Fatalf(
						"docs/features.md row %q column %q has %d words, want at most 3",
						row[0],
						header[column],
						words,
					)
				}
			}
		}
	}
}

func TestCarrierFeatureMatrixPreservesProviderContributionGuards(t *testing.T) {
	content := readRepoText(t, "docs/host-integrations.md")
	section := requireMarkdownSubsection(t, content, "Provider-Scoped Contribution Diagnostics")

	for _, want := range []string{
		"provided_by",
		"source_artifact_inspection",
		"current = non-current",
		"freshness = fresh",
		"Current contribution inventory",
		"standalone `[[mcp_server]]`",
	} {
		if !strings.Contains(section, want) {
			t.Fatalf("provider-scoped contribution section = %q, want %q", section, want)
		}
	}
}

func TestCodexPluginRemovalContractKeepsPinnedSourceAndPublicGuarantee(t *testing.T) {
	features := readRepoText(t, "docs/host-integrations.md")
	section := requireMarkdownSection(t, features, "Codex Plugin Carrier Route Summary")
	for _, want := range []string{
		"`supported` for the explicit global marketplace selector row",
		"`codex plugin remove <plugin>@<marketplace> --json`",
		"$CODEX_HOME/plugins/cache/<marketplace>/<plugin>",
		"orphan cache outside implicit prune authority",
		"Ambient non-daem consumers are not discoverable",
	} {
		if !strings.Contains(section, want) {
			t.Fatalf("public Codex removal contract is missing %q", want)
		}
	}
}

func TestCarrierRemovalMatrixKeepsCurrentCrossHostGuarantees(t *testing.T) {
	features := readRepoText(t, "docs/host-integrations.md")
	passiveRow := requireMarkdownTableRow(t, features, "Passive carrier observation")
	for _, want := range []string{
		"`supported` global config",
		"`supported` project/global",
		"`supported` config project/global",
		"`supported` package project/global",
		"`supported` global selector",
	} {
		if !strings.Contains(passiveRow, want) {
			t.Fatalf("passive carrier matrix row is missing %q: %s", want, passiveRow)
		}
	}

	antigravity := requireMarkdownSection(
		t,
		features,
		"Antigravity CLI Plugin Carrier Route Summary",
	)
	for _, want := range []string{
		"`supported` for selector-shaped explicit-global sources",
		"`agy plugin uninstall <plugin>`",
		"selected import row and plugin directory to be absent",
		"Marketplace provenance and ambient consumers remain non-claims",
	} {
		if !strings.Contains(antigravity, want) {
			t.Fatalf("public Antigravity removal contract is missing %q", want)
		}
	}

	manifestContract := readRepoText(t, "docs/manifest.md")
	for _, want := range []string{
		"source.host_source` is a safe `PLUGIN@MARKETPLACE` selector",
		"both must be consistently\npresent and identity-matching",
		"rejects distinct\nAntigravity structural sources that collapse to the same host-visible plugin\nname",
		"Opaque and local Antigravity host sources do not receive removal authority",
	} {
		if !strings.Contains(manifestContract, want) {
			t.Fatalf("public manifest removal contract is missing %q", want)
		}
	}
}

func requireMarkdownSubsection(t *testing.T, content string, heading string) string {
	t.Helper()

	prefix := "### " + heading + "\n"
	start := strings.Index(content, prefix)
	if start < 0 {
		t.Fatalf("could not find Markdown subsection %q", heading)
	}
	body := content[start+len(prefix):]
	if end := strings.Index(body, "\n### "); end >= 0 {
		body = body[:end]
	}
	return body
}

func requireMarkdownSection(t *testing.T, content string, heading string) string {
	t.Helper()

	prefix := "## " + heading + "\n"
	start := strings.Index(content, prefix)
	if start < 0 {
		t.Fatalf("could not find Markdown section %q", heading)
	}
	body := content[start+len(prefix):]
	if end := strings.Index(body, "\n## "); end >= 0 {
		body = body[:end]
	}
	return body
}

func readRepoText(t *testing.T, relativePath string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(findRepoRoot(t), relativePath))
	if err != nil {
		t.Fatalf("ReadFile(%s) returned error: %v", relativePath, err)
	}
	return string(data)
}

func requireMarkdownTableRow(t *testing.T, content string, firstCell string) string {
	t.Helper()

	if row, ok := findMarkdownTableRow(content, firstCell); ok {
		return row
	}
	t.Fatalf("could not find markdown table row with first cell %q", firstCell)
	return ""
}

func markdownTableRowExists(content string, firstCell string) bool {
	_, ok := findMarkdownTableRow(content, firstCell)
	return ok
}

func findMarkdownTableRow(content string, firstCell string) (string, bool) {
	prefix := "| " + firstCell + " |"
	for line := range strings.SplitSeq(content, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line, true
		}
	}
	return "", false
}
