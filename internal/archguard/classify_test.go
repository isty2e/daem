package archguard

import (
	"os"
	"testing"
)

func TestAnalyzeReportReportsCLIDirectPathsImport(t *testing.T) {
	report := AnalyzeReport([]PackageRecord{{
		ImportPath: "example.com/project/internal/cli",
		Imports:    []string{"example.com/project/internal/paths"},
	}})
	if len(report.Violations) != 1 || report.Violations[0].Rule != ruleCLIDirectPhaseImport {
		t.Fatalf("violations = %+v, want CLI direct phase import", report.Violations)
	}
}

func TestAnalyzeGoListReportClassifiesFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/classification.golist.json")
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	report, err := AnalyzeGoListReport(data)
	if err != nil {
		t.Fatalf("AnalyzeGoListReport returned error: %v", err)
	}
	if len(report.Violations) != 1 {
		t.Fatalf("violations = %+v, want one", report.Violations)
	}
	if report.Violations[0].Rule != ruleCLIDirectPhaseImport {
		t.Fatalf("report = %+v, want CLI boundary violation", report)
	}
}
