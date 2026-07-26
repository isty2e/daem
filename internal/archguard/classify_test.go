package archguard

import (
	"os"
	"strings"
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

func TestClassifyFindingsSeparatesViolationReviewAndWarningFindings(t *testing.T) {
	violation := GuardrailFinding{Rule: ruleObserveReconciliationImport, PackagePath: "internal/assurance/observe/live"}
	review := GuardrailFinding{Rule: ruleDensityReviewRequired, PackagePath: "internal/review"}
	warning := GuardrailFinding{Rule: ruleDensityThreshold, PackagePath: "internal/example"}
	report := classifyFindings([]rawFinding{
		{finding: violation, disposition: findingDispositionViolation},
		{finding: review, disposition: findingDispositionReviewRequired},
		{finding: warning, disposition: findingDispositionWarning},
		{finding: violation, disposition: findingDispositionViolation},
	})
	if len(report.Violations) != 1 || report.Violations[0] != violation {
		t.Fatalf("violations = %+v, want one deduplicated semantic violation", report.Violations)
	}
	if len(report.DensityReviewRequirements) != 1 || report.DensityReviewRequirements[0] != review {
		t.Fatalf("density review requirements = %+v, want one", report.DensityReviewRequirements)
	}
	if len(report.DensityWarnings) != 1 || report.DensityWarnings[0] != warning {
		t.Fatalf("density warnings = %+v, want one warning", report.DensityWarnings)
	}
	if !report.HasFailures() {
		t.Fatal("HasFailures returned false for failing findings")
	}
}

func TestAnalyzeReportClassifiesDensityWarningAndReviewGate(t *testing.T) {
	report := AnalyzeReport([]PackageRecord{
		{
			ImportPath: "example.com/project/internal/adopt/example",
			GoFiles: []string{
				"a.go", "b.go", "c.go", "d.go", "e.go", "f.go", "g.go",
				"h.go", "i.go", "j.go", "k.go", "l.go", "m.go", "n.go",
				"o.go", "p.go", "q.go", "r.go", "s.go",
			},
			TestGoFiles:    []string{"a_test.go"},
			FileLineCounts: map[string]int{"a.go": 351, "a_test.go": 351},
		},
		{
			ImportPath:     "example.com/project/internal/adopt/too_dense",
			GoFiles:        []string{"huge.go"},
			FileLineCounts: map[string]int{"huge.go": 501},
		},
	})
	if len(report.DensityWarnings) != 3 {
		t.Fatalf("density warnings = %+v, want package, production file, and test file warnings", report.DensityWarnings)
	}
	if len(report.Violations) != 0 {
		t.Fatalf("violations = %+v, numeric density alone must not be a semantic violation", report.Violations)
	}
	if len(report.DensityReviewRequirements) != 1 || report.DensityReviewRequirements[0].Rule != ruleDensityReviewRequired {
		t.Fatalf("density review requirements = %+v, want one actionable review gate", report.DensityReviewRequirements)
	}
}

func TestAnalyzeReportRecordsPackageDensity(t *testing.T) {
	report := AnalyzeReport([]PackageRecord{{
		ImportPath:     "example.com/project/internal/adopt/example",
		GoFiles:        []string{"a.go"},
		TestGoFiles:    []string{"a_test.go"},
		FileLineCounts: map[string]int{"a.go": 12, "a_test.go": 34},
	}})
	if len(report.PackageDensity) != 1 {
		t.Fatalf("package density = %+v, want one entry", report.PackageDensity)
	}
	density := report.PackageDensity[0]
	if density.ProductionFiles != 1 || density.TestFiles != 1 ||
		density.MaxProductionLines != 12 || density.MaxTestLines != 34 {
		t.Fatalf("density = %+v, want recorded production/test counts and lines", density)
	}
}

func TestDensityReviewRequirementFailsBaselineWithoutSemanticViolation(t *testing.T) {
	report := Report{
		DensityReviewRequirements: []GuardrailFinding{{
			Rule: ruleDensityReviewRequired,
		}},
	}
	if !report.HasFailures() {
		t.Fatal("HasFailures returned false for a density review requirement")
	}
	if len(report.Violations) != 0 {
		t.Fatalf("violations = %+v, review requirement must remain semantically distinct", report.Violations)
	}
}

func TestFormatAnalysisReportNamesReviewRequirementSeparately(t *testing.T) {
	report := FormatAnalysisReport(Report{
		DensityReviewRequirements: []GuardrailFinding{{
			Rule:        ruleDensityReviewRequired,
			PackagePath: "internal/example",
		}},
	})
	if !strings.Contains(report, "archguard: no topology violations reported") {
		t.Fatalf("report = %q, want explicit absence of semantic violations", report)
	}
	if !strings.Contains(report, "archguard: 1 density review required(s)") {
		t.Fatalf("report = %q, want separately named review gate", report)
	}
	if strings.Contains(report, "topology violation(s)") {
		t.Fatalf("report = %q, review gate must not be rendered as a topology violation", report)
	}
}
