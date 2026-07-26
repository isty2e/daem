package clipresent

import (
	"bytes"
	"strings"
	"testing"
)

func TestEveryHelpPageUsesAnInstalledDocumentationReference(t *testing.T) {
	for key, page := range helpPages(UsageContext{SupportedTargets: "codex", ImportTargets: "codex"}) {
		if page.Reference != manifestDocumentReference && page.Reference != cliDocumentReference {
			t.Errorf("help page %q reference = %q, want a canonical installed document title", key, page.Reference)
		}
		if strings.Contains(page.Reference, "docs/") || strings.Contains(page.Reference, ".md") {
			t.Errorf("help page %q reference = %q, contains checkout-relative path", key, page.Reference)
		}
	}
}

func TestInitHelpUsesStarterManifestWording(t *testing.T) {
	page := helpPages(UsageContext{})["init"]
	if page.Summary != "create a starter manifest" {
		t.Fatalf("init summary = %q, want starter manifest wording", page.Summary)
	}

	var root bytes.Buffer
	PrintUsage(&root, UsageContext{})
	if !strings.Contains(root.String(), "Create a starter manifest.") {
		t.Fatalf("root usage = %q, want starter manifest wording", root.String())
	}
}

func TestHelpJSONDescriptionNamesDiffOnlyWhenTheCommandHasDiff(t *testing.T) {
	for key, page := range helpPages(UsageContext{SupportedTargets: "codex", ImportTargets: "codex"}) {
		hasDiff := false
		jsonDescription := ""
		for _, section := range page.Sections {
			for _, row := range section.Rows {
				switch row.Label {
				case "--diff":
					hasDiff = true
				case "--json":
					jsonDescription = row.Text
				}
			}
		}
		if jsonDescription == "" {
			continue
		}
		if mentionsDiff := strings.Contains(jsonDescription, "--diff"); mentionsDiff != hasDiff {
			t.Errorf("help page %q: --json description = %q, has --diff = %t", key, jsonDescription, hasDiff)
		}
	}
}

func TestShellExamplesWrapAtTokenBoundariesAndRemainCopyable(t *testing.T) {
	const width = 80
	for key, page := range helpPages(UsageContext{SupportedTargets: "codex, claude-code, opencode, pi, antigravity-cli", ImportTargets: "codex, claude-code"}) {
		for _, example := range page.Examples {
			var output bytes.Buffer
			printShellExample(&output, example, width)
			rendered := output.String()
			for lineNumber, line := range strings.Split(strings.TrimSuffix(rendered, "\n"), "\n") {
				if len(line) > width {
					t.Errorf("help page %q example %q line %d has %d bytes: %q", key, example, lineNumber+1, len(line), line)
				}
			}
			restored := strings.Join(strings.Fields(strings.ReplaceAll(rendered, "\\\n", "")), " ")
			if restored != example {
				t.Errorf("help page %q example restored as %q, want %q; rendered:\n%s", key, restored, example, rendered)
			}
		}
	}

	var quoted bytes.Buffer
	printShellExample(&quoted, "daem add hook format PostToolUse 'make fmt' --timeout 2m --dry-run", 48)
	if strings.Contains(quoted.String(), "'make \\\n") {
		t.Fatalf("quoted shell token was split:\n%s", quoted.String())
	}
}

func TestMCPHelpUsesCommandSpecificAdmissionFacts(t *testing.T) {
	context := UsageContext{
		SupportedTargets:          "all-targets-sentinel",
		ImportTargets:             "import-targets-sentinel",
		MCPAuthoringTargets:       "authoring-targets-sentinel",
		MCPAuthoringScopes:        "authoring-scopes-sentinel",
		MCPAuthoringPlacements:    "authoring-placements-sentinel",
		MCPRuntimeProbeTargets:    "probe-targets-sentinel",
		MCPRuntimeProbeScopes:     "probe-scopes-sentinel",
		MCPRuntimeProbePlacements: "probe-placements-sentinel",
	}
	pages := helpPages(context)

	assertHelpPageContains(t, pages["add mcp-server"], "authoring-targets-sentinel", "authoring-scopes-sentinel", "authoring-placements-sentinel")
	assertHelpPageOmits(t, pages["add mcp-server"], "all-targets-sentinel", "repeat for multiple targets")
	assertHelpPageContains(t, pages["probe mcp-server"], "probe-targets-sentinel", "probe-scopes-sentinel", "probe-placements-sentinel")
	assertHelpPageOmits(t, pages["probe mcp-server"], "all-targets-sentinel", "repeat for multiple targets")
}

func assertHelpPageContains(t *testing.T, page helpPage, values ...string) {
	t.Helper()
	rendered := renderHelpPageText(page)
	for _, value := range values {
		if !strings.Contains(rendered, value) {
			t.Errorf("help page %q does not contain %q:\n%s", page.Path, value, rendered)
		}
	}
}

func assertHelpPageOmits(t *testing.T, page helpPage, values ...string) {
	t.Helper()
	rendered := renderHelpPageText(page)
	for _, value := range values {
		if strings.Contains(rendered, value) {
			t.Errorf("help page %q unexpectedly contains %q:\n%s", page.Path, value, rendered)
		}
	}
}

func renderHelpPageText(page helpPage) string {
	var text strings.Builder
	for _, section := range page.Sections {
		for _, row := range section.Rows {
			text.WriteString(row.Label)
			text.WriteString(" ")
			text.WriteString(row.Text)
			text.WriteString("\n")
		}
	}
	return text.String()
}
