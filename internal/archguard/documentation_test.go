package archguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocumentationGuardBaseline(t *testing.T) {
	report, err := analyzeDocumentation(findRepoRoot(t))
	if err != nil {
		t.Fatalf("AnalyzeDocumentation returned error: %v", err)
	}
	if report.hasFailures() {
		t.Fatalf("documentation guard baseline has failures:\n%s", formatDocumentationReport(report))
	}
	t.Log("command: tools/test-go.sh -run TestDocumentationGuardBaseline -count=1 -v ./internal/archguard")
}

func TestReleaseIntegrityDocumentationContract(t *testing.T) {
	root := findRepoRoot(t)
	documents, err := loadMarkdownDocuments(root)
	if err != nil {
		t.Fatal(err)
	}

	forbidden := []string{
		"same immutable tag",
		"published release tags and attached assets are immutable",
		"## release immutability",
	}
	installDocument := ""
	for _, document := range documents {
		if !isUserDocumentation(document.path) {
			continue
		}
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(document.path)))
		if err != nil {
			t.Fatal(err)
		}
		lowerContent := strings.Join(strings.Fields(strings.ToLower(string(content))), " ")
		for _, claim := range forbidden {
			if strings.Contains(lowerContent, claim) {
				t.Errorf("%s contains unsupported release-integrity claim %q", document.path, claim)
			}
		}
		if document.path == "docs/install.md" {
			installDocument = strings.Join(strings.Fields(string(content)), " ")
		}
	}

	for _, disclosure := range []string{
		"Release `v0.1.0` was published while GitHub release immutability was disabled",
		"archive and its checksum sidecar share the same mutable GitHub release authority",
		"does not prove publisher identity, provenance, or post-publication immutability",
	} {
		if !strings.Contains(installDocument, disclosure) {
			t.Errorf("docs/install.md is missing release-integrity disclosure %q", disclosure)
		}
	}
}

func TestDocumentationGuardAcceptsSupportedMarkdown(t *testing.T) {
	root := t.TempDir()
	writeDocumentationFixture(t, root, "README.md", "# Entry\n\n[Guide](docs/guide.md#same-1)\n")
	writeDocumentationFixture(t, root, "docs/guide.md", strings.Join([]string{
		"# Guide",
		"",
		"## Same",
		"## Same",
		"",
		"[reference]: reference.md#reference",
		"[Reference][reference]",
		"",
		"`[ignored](missing.md)`",
	}, "\n"))
	writeDocumentationFixture(t, root, "docs/reference.md", "# Reference\n")

	report := analyzeDocumentationFixture(t, root)
	if report.hasFailures() {
		t.Fatalf("supported Markdown produced findings:\n%s", formatDocumentationReport(report))
	}
}

func TestDocumentationGuardIgnoresEphemeralSubagentArtifactsOnly(t *testing.T) {
	root := t.TempDir()
	writeDocumentationFixture(t, root, "README.md", "# Entry\n")
	writeDocumentationFixture(
		t,
		root,
		".pi-subagents/artifacts/review.md",
		"[missing](missing.md)\n\n`internal/missing.go`\n",
	)

	report := analyzeDocumentationFixture(t, root)
	if report.hasFailures() {
		t.Fatalf("ephemeral subagent artifact produced findings:\n%s", formatDocumentationReport(report))
	}

	writeDocumentationFixture(t, root, ".pi-subagents-backup/review.md", "[missing](missing.md)\n")
	report = analyzeDocumentationFixture(t, root)
	assertDocumentationRule(t, report, documentationLinkTargetRule, "missing.md")
}

func TestDocumentationGuardReportsBrokenTargetsAndAnchors(t *testing.T) {
	root := t.TempDir()
	writeDocumentationFixture(t, root, "README.md", strings.Join([]string{
		"# Entry",
		"",
		"[missing](docs/missing.md)",
		"[bad anchor](docs/guide.md#not-there)",
	}, "\n"))
	writeDocumentationFixture(t, root, "docs/guide.md", "# Guide\n")

	report := analyzeDocumentationFixture(t, root)
	assertDocumentationRule(t, report, documentationLinkTargetRule, "docs/missing.md")
	assertDocumentationRule(t, report, documentationLinkAnchorRule, "docs/guide.md#not-there")
}

func TestDocumentationGuardReportsSupersededCUXGrammarOnlyInUserDocs(t *testing.T) {
	root := t.TempDir()
	writeDocumentationFixture(t, root, "README.md", strings.Join([]string{
		"# Entry",
		"",
		"```sh",
		"daem init --yes",
		"daem add hook lint --event Stop --command 'make lint'",
		"daem add skill-group ./skills --name review,test",
		"daem list --manifest daem.toml",
		"daem status --target codex,claude-code",
		"```",
	}, "\n"))
	writeDocumentationFixture(t, root, "notes/history.md", "```sh\ndaem init --yes\n```\n")

	report := analyzeDocumentationFixture(t, root)
	if countDocumentationRule(report, documentationDeprecatedCLIRule) != 5 {
		t.Fatalf("deprecated CLI findings = %d, want 5:\n%s", countDocumentationRule(report, documentationDeprecatedCLIRule), formatDocumentationReport(report))
	}
}

func TestDocumentationGuardAllowsDeprecatedLookingTokensInsideManagedCommandOperands(t *testing.T) {
	root := t.TempDir()
	writeDocumentationFixture(t, root, "README.md", "```sh\ndaem add hook report Stop 'tool --output report.json'\n```\n")

	report := analyzeDocumentationFixture(t, root)
	if report.hasFailures() {
		t.Fatalf("managed command operand produced findings:\n%s", formatDocumentationReport(report))
	}
}

func TestDocumentationGuardHandlesEscapesBalancedDestinationsAndFences(t *testing.T) {
	root := t.TempDir()
	writeDocumentationFixture(t, root, "README.md", strings.Join([]string{
		"# Entry",
		"",
		"[encoded](docs/a%20b.md#heading-one)",
		"[balanced](docs/name_(old).md#old-name)",
		"[escaped](docs/name_\\(old\\).md#old-name) [second](docs/a%20b.md#heading-one)",
		"[external](https://example.com/missing.md#missing)",
		"",
		"````md",
		"[ignored](missing.md)",
		"```",
		"````",
	}, "\n"))
	writeDocumentationFixture(t, root, "docs/a b.md", "# Heading One\n")
	writeDocumentationFixture(t, root, "docs/name_(old).md", "# Old Name\n")

	report := analyzeDocumentationFixture(t, root)
	if report.hasFailures() {
		t.Fatalf("edge-case Markdown produced findings:\n%s", formatDocumentationReport(report))
	}
}

func TestDocumentationGuardRejectsAmbiguousOrEscapingLocalDestinations(t *testing.T) {
	root := t.TempDir()
	writeDocumentationFixture(t, root, "docs/guide.md", strings.Join([]string{
		"# Guide",
		"",
		"[query](target.md?view=current)",
		"[escape](../../outside.md)",
		"[bad escape](bad%zz.md)",
	}, "\n"))

	report := analyzeDocumentationFixture(t, root)
	if countDocumentationRule(report, documentationLinkTargetRule) != 3 {
		t.Fatalf("target findings = %d, want 3:\n%s", countDocumentationRule(report, documentationLinkTargetRule), formatDocumentationReport(report))
	}
}

func TestDocumentationGuardRejectsWrongCaseOnCaseInsensitiveFilesystems(t *testing.T) {
	root := t.TempDir()
	writeDocumentationFixture(t, root, "README.md", "[guide](docs/guide.md)\n")
	writeDocumentationFixture(t, root, "docs/Guide.md", "# Guide\n")

	report := analyzeDocumentationFixture(t, root)
	assertDocumentationRule(t, report, documentationLinkTargetRule, "docs/guide.md")
}

func analyzeDocumentationFixture(t *testing.T, root string) documentationReport {
	t.Helper()
	report, err := analyzeDocumentation(root)
	if err != nil {
		t.Fatalf("AnalyzeDocumentation returned error: %v", err)
	}
	return report
}

func writeDocumentationFixture(t *testing.T, root string, relativePath string, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) returned error: %v", relativePath, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) returned error: %v", relativePath, err)
	}
}

func assertDocumentationRule(t *testing.T, report documentationReport, rule string, target string) {
	t.Helper()
	for _, finding := range report.findings {
		if finding.Rule == rule && finding.Target == target {
			return
		}
	}
	t.Fatalf("missing rule %q target %q:\n%s", rule, target, formatDocumentationReport(report))
}

func countDocumentationRule(report documentationReport, rule string) int {
	count := 0
	for _, finding := range report.findings {
		if finding.Rule == rule {
			count++
		}
	}
	return count
}
