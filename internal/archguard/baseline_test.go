package archguard

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestTopologyGuardBaseline(t *testing.T) {
	records := loadRepoPackageRecords(t)
	if violations := validateDensityAdmissionInventory(
		records,
		packageDensityAdmissions,
		productionFileDensityAdmissions,
	); len(violations) != 0 {
		t.Fatalf("archguard density admission inventory is invalid:\n%s", FormatReport(violations))
	}
	report := AnalyzeReport(records)
	if report.HasFailures() {
		t.Fatalf("archguard baseline has failures:\n%s", FormatAnalysisReport(report))
	}
	assertFindingMetadata(t, report.DensityReviewRequirements)
	assertFindingMetadata(t, report.DensityWatchpoints)
	assertFindingMetadata(t, report.DensityWarnings)
	t.Logf("command: tools/test-go.sh -run TestTopologyGuardBaseline -count=1 -v ./internal/archguard\n%s", FormatAnalysisReport(report))
}

func loadRepoPackageRecords(t *testing.T) []PackageRecord {
	t.Helper()

	root := findRepoRoot(t)
	command := exec.Command("go", "list", "-json", "./...")
	command.Dir = root

	output, err := command.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("go list failed: %v\nstderr:\n%s", err, string(exitErr.Stderr))
		}
		t.Fatalf("go list failed: %v", err)
	}

	records, err := ParseGoListJSON(output)
	if err != nil {
		t.Fatalf("ParseGoListJSON returned error: %v", err)
	}
	return records
}

func TestTestToolsAreNotImportedByProductionPackages(t *testing.T) {
	records := loadRepoPackageRecords(t)
	if violations := analyzeProductionTestToolImports(records, testToolPackageAdmissions); len(violations) != 0 {
		t.Fatalf("production package imports test/tool support:\n%s", FormatReport(violations))
	}
}

func assertFindingMetadata(t *testing.T, findings []GuardrailFinding) {
	t.Helper()
	for _, finding := range findings {
		if finding.Reason == "" {
			t.Fatalf("finding %+v has empty reason", finding)
		}
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find repo root from %q", dir)
		}
		dir = parent
	}
}
