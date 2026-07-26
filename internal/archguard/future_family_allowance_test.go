package archguard

import (
	"strings"
	"testing"
)

func TestAnalyzeRecordsAllowsFutureFamilyPhaseLocalAndLookalikePaths(t *testing.T) {
	records := []PackageRecord{
		{ImportPath: "example.com/project/internal/surface/mcpconfig"},
		{ImportPath: "example.com/project/internal/surface/pluginhost"},
		{ImportPath: "example.com/project/internal/output/project/mcp"},
		{ImportPath: "example.com/project/internal/output/project/packageindex"},
		{ImportPath: "example.com/project/internal/effect/payload/mcp"},
		{ImportPath: "example.com/project/internal/effect/payload/plugin"},
		{ImportPath: "example.com/project/internal/assurance/observe/mcp"},
		{ImportPath: "example.com/project/internal/adopt/mcp"},
		{ImportPath: "example.com/project/internal/diagnose/mcp"},
		{ImportPath: "example.com/project/internal/pluginhost"},
		{ImportPath: "example.com/project/internal/mcproxy"},
		{ImportPath: "example.com/project/internal/packaging"},
		{ImportPath: "example.com/project/internal/extensionbridge"},
		{ImportPath: "example.com/project/internal/supply/source/package"},
		{ImportPath: "example.com/project/internal/supply/source/packageindex"},
	}

	report := FormatReport(AnalyzeRecords(records))
	for _, unwanted := range []string{
		ruleFutureMCPPluginMonolith,
		ruleFutureFamilyResourceBucket,
		ruleFutureFamilyWorkflowCell,
		ruleFutureFamilyOperationCell,
	} {
		if strings.Contains(report, unwanted) {
			t.Fatalf("report = %q, did not want allowed phase-local or lookalike paths reported by %s", report, unwanted)
		}
	}
}

func TestAnalyzeRecordsRejectsCLIPresentationFamilyChildren(t *testing.T) {
	records := []PackageRecord{
		{ImportPath: "example.com/project/internal/cli/present/mcp"},
		{ImportPath: "example.com/project/internal/cli/present/extension"},
		{ImportPath: "example.com/project/internal/cli/present/skill"},
		{ImportPath: "example.com/project/internal/cli/present/progress/internal"},
	}

	report := FormatReport(AnalyzeRecords(records))
	for _, packagePath := range []string{
		"internal/cli/present/extension",
		"internal/cli/present/mcp",
		"internal/cli/present/progress/internal",
		"internal/cli/present/skill",
	} {
		want := ruleCLIPresentationChild + ": " + packagePath
		if !strings.Contains(report, want) {
			t.Fatalf("report = %q, want %q", report, want)
		}
	}
}

func TestAnalyzeRecordsDedupsAndSortsFutureFamilyLifecycleFindings(t *testing.T) {
	record := PackageRecord{
		ImportPath: "example.com/project/internal/lifecycle/plugininstall",
		Imports:    []string{"example.com/project/internal/effect/execute"},
		GoFiles:    []string{"plugininstall.go"},
	}

	report := FormatReport(AnalyzeRecords([]PackageRecord{record}))
	wants := []string{
		"future-family-operation-cell: internal/lifecycle/plugininstall",
		"future-family-operation-cell: internal/lifecycle/plugininstall/plugininstall.go",
		`retired-lifecycle-package: internal/lifecycle/plugininstall detail="lifecycle is a retired mixed-axis package; desired, topology, realization, assurance, and operation facts stay with their canonical owners"`,
	}
	for _, want := range wants {
		if countReportLine(report, "- "+want) != 1 {
			t.Fatalf("report = %q, want exactly one %q", report, want)
		}
	}
	first := strings.Index(report, "- "+wants[0]+"\n")
	second := strings.Index(report, "- "+wants[1]+"\n")
	third := strings.Index(report, "- "+wants[2]+"\n")
	if first == -1 || second == -1 || third == -1 || !(first < second && second < third) {
		t.Fatalf("report = %q, want deterministic sorted finding order", report)
	}
}

func countReportLine(report string, line string) int {
	count := 0
	for reportLine := range strings.SplitSeq(strings.TrimRight(report, "\n"), "\n") {
		if reportLine == line {
			count++
		}
	}
	return count
}
