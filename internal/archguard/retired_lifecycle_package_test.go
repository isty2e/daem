package archguard

import (
	"strings"
	"testing"
)

func TestAnalyzeRecordsRejectsRetiredLifecyclePackages(t *testing.T) {
	for _, importPath := range []string{
		"example.com/project/internal/lifecycle",
		"example.com/project/internal/lifecycle/plugininstall",
	} {
		t.Run(importPath, func(t *testing.T) {
			report := FormatReport(AnalyzeRecords([]PackageRecord{{
				ImportPath: importPath,
			}}))
			if !strings.Contains(report, ruleRetiredLifecyclePackage) {
				t.Fatalf("report = %q, want retired lifecycle package finding", report)
			}
		})
	}
}
