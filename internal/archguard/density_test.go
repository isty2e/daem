package archguard

import (
	"fmt"
	"testing"
)

func TestAnalyzeReportRequiresReviewWhenAdmittedPackageDensityIncreases(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/workflow/authoring",
			GoFiles:    numberedGoFiles(18),
		},
	}

	report := AnalyzeReport(records)
	if len(report.Violations) != 0 {
		t.Fatalf("violations = %+v, density growth must not be classified as semantic invalidity", report.Violations)
	}
	if !containsFinding(report.DensityReviewRequirements, GuardrailFinding{
		Rule:        ruleDensityReviewRequired,
		PackagePath: "internal/workflow/authoring",
		Path:        "internal/workflow/authoring",
	}) {
		t.Fatalf("density review requirements = %+v, want reviewed package density increase", report.DensityReviewRequirements)
	}
}

func TestReviewedDensityAdmissionsMatchCurrentTree(t *testing.T) {
	violations := validateDensityAdmissionInventory(
		loadRepoPackageRecords(t),
		packageDensityAdmissions,
		productionFileDensityAdmissions,
	)
	if len(violations) != 0 {
		t.Fatalf("density admission inventory is invalid:\n%s", FormatReport(violations))
	}
}

func TestReviewedPackageMayExceedNumericReviewThreshold(t *testing.T) {
	const packagePath = "internal/example"
	admissions := map[string]densityReviewAdmission{
		packagePath: completeDensityAdmission(26, "production files"),
	}
	report := classifyFindings(packageDensityFindings(PackageDensity{
		PackagePath:     packagePath,
		ProductionFiles: 26,
	}, admissions))

	if len(report.Violations) != 0 || len(report.DensityReviewRequirements) != 0 {
		t.Fatalf("report = %+v, reviewed package density must not fail", report)
	}
	if len(report.DensityWarnings) != 1 {
		t.Fatalf("density warnings = %+v, want retained pressure signal", report.DensityWarnings)
	}
}

func TestReviewedProductionFileMayExceedNumericReviewThreshold(t *testing.T) {
	const filePath = "internal/example/large.go"
	admissions := map[string]densityReviewAdmission{
		filePath: completeDensityAdmission(1001, "production lines"),
	}
	report := classifyFindings(fileDensityFindings("internal/example", PackageRecord{
		GoFiles:        []string{"large.go"},
		FileLineCounts: map[string]int{"large.go": 1001},
	}, admissions))

	if len(report.Violations) != 0 || len(report.DensityReviewRequirements) != 0 {
		t.Fatalf("report = %+v, reviewed file density must not fail", report)
	}
	if len(report.DensityWatchpoints) != 1 {
		t.Fatalf("density watchpoints = %+v, want retained pressure signal", report.DensityWatchpoints)
	}
}

func TestDensityAdmissionRequiresCounterfactualRationale(t *testing.T) {
	admission := completeDensityAdmission(26, "production files")
	admission.naturalSplit = "   "
	report := classifyFindings(packageDensityFindings(PackageDensity{
		PackagePath:     "internal/example",
		ProductionFiles: 26,
	}, map[string]densityReviewAdmission{"internal/example": admission}))

	if len(report.Violations) != 1 || report.Violations[0].Rule != ruleDensityAdmissionInvalid {
		t.Fatalf("violations = %+v, want malformed admission failure", report.Violations)
	}
	if len(report.DensityReviewRequirements) != 0 || len(report.DensityWatchpoints) != 0 {
		t.Fatalf("density findings = %+v %+v, malformed admission is a semantic contract failure", report.DensityReviewRequirements, report.DensityWatchpoints)
	}
}

func TestDensityAdmissionRejectsCountOnlyUpdate(t *testing.T) {
	admission := completeDensityAdmission(26, "production files")
	admission.reviewedValue = 27
	report := classifyFindings(packageDensityFindings(PackageDensity{
		PackagePath:     "internal/example",
		ProductionFiles: 27,
	}, map[string]densityReviewAdmission{"internal/example": admission}))

	if len(report.Violations) != 1 || report.Violations[0].Rule != ruleDensityAdmissionInvalid {
		t.Fatalf("violations = %+v, count-only admission update must fail", report.Violations)
	}
}

func TestDensityCountsCgoFilesAsProduction(t *testing.T) {
	report := AnalyzeReport([]PackageRecord{{
		ImportPath: "example.com/project/internal/example",
		CgoFiles:   numberedGoFiles(26),
		FileLineCounts: map[string]int{
			"file_00.go": 501,
		},
	}})

	if len(report.PackageDensity) != 1 || report.PackageDensity[0].ProductionFiles != 26 {
		t.Fatalf("package density = %+v, want 26 CGo production files", report.PackageDensity)
	}
	if len(report.DensityReviewRequirements) != 1 || len(report.DensityWatchpoints) != 1 {
		t.Fatalf(
			"density review requirements = %+v, watchpoints = %+v, want package review and CGo file watchpoint",
			report.DensityReviewRequirements,
			report.DensityWatchpoints,
		)
	}
}

func TestDensityThresholdBoundariesRemainReviewSignals(t *testing.T) {
	packageCases := []struct {
		name     string
		files    int
		warnings int
		reviews  int
	}{
		{name: "package below warning", files: 18},
		{name: "package enters warning", files: 19, warnings: 1},
		{name: "package at review boundary", files: 25, warnings: 1},
		{name: "package crosses review boundary", files: 26, reviews: 1},
	}
	for _, testCase := range packageCases {
		t.Run(testCase.name, func(t *testing.T) {
			report := classifyFindings(packageDensityFindings(PackageDensity{
				PackagePath:     "internal/example",
				ProductionFiles: testCase.files,
			}, nil))
			assertDensityClassification(t, report, testCase.warnings, 0, testCase.reviews, 0)
		})
	}

	fileCases := []struct {
		name        string
		lines       int
		watchpoints int
		reviews     int
		violations  int
	}{
		{name: "file below watchpoint", lines: 500},
		{name: "file enters watchpoint", lines: 501, watchpoints: 1},
		{name: "file at review boundary", lines: 1000, watchpoints: 1},
		{name: "file crosses review boundary", lines: 1001, reviews: 1},
		{name: "file at hard limit", lines: 5000, reviews: 1},
		{name: "file crosses hard limit", lines: 5001, violations: 1},
	}
	for _, testCase := range fileCases {
		t.Run(testCase.name, func(t *testing.T) {
			report := classifyFindings(fileDensityFindings("internal/example", PackageRecord{
				GoFiles:        []string{"example.go"},
				FileLineCounts: map[string]int{"example.go": testCase.lines},
			}, nil))
			assertDensityClassification(t, report, 0, testCase.watchpoints, testCase.reviews, testCase.violations)
		})
	}

	testFileCases := []struct {
		name        string
		lines       int
		watchpoints int
		violations  int
	}{
		{name: "test file at watchpoint boundary", lines: 1000},
		{name: "test file crosses watchpoint boundary", lines: 1001, watchpoints: 1},
		{name: "test file at hard limit", lines: 5000, watchpoints: 1},
		{name: "test file crosses hard limit", lines: 5001, violations: 1},
	}
	for _, testCase := range testFileCases {
		t.Run(testCase.name, func(t *testing.T) {
			report := classifyFindings(fileDensityFindings("internal/example", PackageRecord{
				TestGoFiles:    []string{"example_test.go"},
				FileLineCounts: map[string]int{"example_test.go": testCase.lines},
			}, nil))
			assertDensityClassification(t, report, 0, testCase.watchpoints, 0, testCase.violations)
		})
	}
}

func assertDensityClassification(
	t *testing.T,
	report Report,
	warnings int,
	watchpoints int,
	reviews int,
	violations int,
) {
	t.Helper()
	if len(report.Violations) != violations {
		t.Fatalf("violations = %+v, want %d", report.Violations, violations)
	}
	if len(report.DensityWarnings) != warnings {
		t.Fatalf("density warnings = %+v, want %d", report.DensityWarnings, warnings)
	}
	if len(report.DensityWatchpoints) != watchpoints {
		t.Fatalf("density watchpoints = %+v, want %d", report.DensityWatchpoints, watchpoints)
	}
	if len(report.DensityReviewRequirements) != reviews {
		t.Fatalf("density review requirements = %+v, want %d", report.DensityReviewRequirements, reviews)
	}
	if report.HasFailures() != (reviews != 0 || violations != 0) {
		t.Fatalf("HasFailures = %v, want %v", report.HasFailures(), reviews != 0 || violations != 0)
	}
}

func TestDensityAdmissionInventoryRejectsMissingAndStaleTargets(t *testing.T) {
	records := []PackageRecord{{
		ImportPath:     "example.com/project/internal/cli/present",
		GoFiles:        []string{"current.go"},
		FileLineCounts: map[string]int{"current.go": 20},
	}}
	packageAdmissions := map[string]densityReviewAdmission{
		"internal/missing":     completeDensityAdmission(1, "production files"),
		"internal/cli/present": completeDensityAdmission(2, "production files"),
	}
	fileAdmissions := map[string]densityReviewAdmission{
		"internal/cli/present/missing.go": completeDensityAdmission(20, "production lines"),
		"internal/cli/present/current.go": completeDensityAdmission(21, "production lines"),
	}

	violations := validateDensityAdmissionInventory(records, packageAdmissions, fileAdmissions)
	if len(violations) != 4 {
		t.Fatalf("violations = %+v, want missing package, stale package, missing file, and stale file", violations)
	}
	for _, violation := range violations {
		if violation.Rule != ruleDensityAdmissionInvalid {
			t.Fatalf("violation = %+v, want density admission invalid rule", violation)
		}
	}
}

func TestDensityAdmissionGrowthRequiresReview(t *testing.T) {
	records := []PackageRecord{{
		ImportPath:     "example.com/project/internal/example",
		GoFiles:        []string{"large.go"},
		FileLineCounts: map[string]int{"large.go": 1002},
	}}
	admission := completeDensityAdmission(1001, "production lines")
	if violations := validateDensityAdmissionInventory(
		records,
		nil,
		map[string]densityReviewAdmission{"internal/example/large.go": admission},
	); len(violations) != 0 {
		t.Fatalf("inventory violations = %+v, upward drift belongs to review classification", violations)
	}
	report := classifyFindings(fileDensityFindings(
		"internal/example",
		records[0],
		map[string]densityReviewAdmission{"internal/example/large.go": admission},
	))
	if len(report.DensityReviewRequirements) != 1 || len(report.Violations) != 0 {
		t.Fatalf("report = %+v, upward drift must require renewed review without claiming semantic invalidity", report)
	}
}

func completeDensityAdmission(reviewedValue int, metric string) densityReviewAdmission {
	return densityReviewAdmission{
		reviewedValue:       reviewedValue,
		owner:               "one semantic owner",
		reason:              fmt.Sprintf("at %d %s, the current unit owns one cohesive invariant", reviewedValue, metric),
		naturalSplit:        "split the unit by its visible file groups",
		alternativeRejected: "the groups share private state and have no independent caller",
	}
}

func containsFinding(findings []GuardrailFinding, want GuardrailFinding) bool {
	for _, finding := range findings {
		if finding.Rule == want.Rule &&
			finding.PackagePath == want.PackagePath &&
			finding.ImportPath == want.ImportPath &&
			finding.Path == want.Path {
			return true
		}
	}
	return false
}

func numberedGoFiles(count int) []string {
	files := make([]string, 0, count)
	for index := range count {
		files = append(files, fmt.Sprintf("file_%02d.go", index))
	}
	return files
}
