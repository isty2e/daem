package clipresent

import (
	"bytes"
	"regexp"
	"strconv"
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

func TestHelpDescriptionsReferenceOnlyDeclaredPageOptions(t *testing.T) {
	optionName := regexp.MustCompile(`--[a-z][a-z0-9-]*`)
	referenceCount := 0
	for key, page := range helpPages(UsageContext{SupportedTargets: "codex", ImportTargets: "codex"}) {
		declared := make(map[string]bool)
		for _, section := range page.Sections {
			if section.Title != "Options" && section.Title != "Operands" {
				continue
			}
			for _, row := range section.Rows {
				for _, name := range optionName.FindAllString(row.Label, -1) {
					declared[name] = true
				}
			}
		}

		for _, section := range page.Sections {
			if section.Title != "Options" && section.Title != "Operands" {
				continue
			}
			for _, row := range section.Rows {
				for _, reference := range optionName.FindAllString(row.Text, -1) {
					referenceCount++
					if !declared[reference] {
						t.Errorf("help page %q %s row %q references undeclared option %q", key, section.Title, row.Label, reference)
					}
				}
			}
		}
	}
	if referenceCount == 0 {
		t.Fatal("no option-description references were checked")
	}
}

func TestCommandHelpRendersCurrentPageContentAtSupportedWidths(t *testing.T) {
	for _, width := range []int{80, 120} {
		context := UsageContext{SupportedTargets: "codex", ImportTargets: "codex", Width: width}
		for key, page := range helpPages(context) {
			t.Run(key+"_"+strconv.Itoa(width), func(t *testing.T) {
				var output bytes.Buffer
				if !PrintCommandUsage(&output, strings.Fields(key), context) {
					t.Fatal("current help page was not rendered")
				}
				if output.Len() == 0 {
					t.Fatal("current help page output is empty")
				}

				normalizedOutput := strings.Join(strings.Fields(strings.ReplaceAll(output.String(), "\\\n", "")), " ")
				content := []string{page.Path, page.Summary, page.Usage, page.Reference}
				for _, section := range page.Sections {
					content = append(content, section.Title)
					for _, row := range section.Rows {
						content = append(content, row.Label, row.Text)
					}
				}
				content = append(content, page.Examples...)

				for _, value := range content {
					expected := strings.Join(strings.Fields(value), " ")
					if !strings.Contains(normalizedOutput, expected) {
						t.Errorf("help page omits current content %q:\n%s", value, output.String())
					}
				}
			})
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

	assertHelpPageContains(
		t,
		pages["add mcp-server"],
		"authoring-targets-sentinel",
		"authoring-scopes-sentinel",
		"authoring-placements-sentinel",
	)
	assertHelpPageOmits(t, pages["add mcp-server"], "all-targets-sentinel", "probe-targets-sentinel")
	assertHelpPageContains(t, pages["probe mcp-server"], "probe-targets-sentinel", "probe-scopes-sentinel", "probe-placements-sentinel")
	assertHelpPageOmits(t, pages["probe mcp-server"], "all-targets-sentinel", "authoring-targets-sentinel")
	assertHelpPageContains(t, pages["remove mcp-server"], "all-targets-sentinel")
	assertHelpPageOmits(t, pages["remove mcp-server"], "authoring-targets-sentinel", "probe-targets-sentinel")
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
