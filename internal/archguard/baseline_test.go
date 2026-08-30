package archguard

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"testing"
)

var repositoryPackageRecordCache struct {
	sync.Once
	records []PackageRecord
	err     error
}

func TestTopologyGuardBaseline(t *testing.T) {
	records := loadRepoPackageRecords(t)
	report := AnalyzeReport(records)
	if report.HasFailures() {
		t.Fatalf("archguard baseline has failures:\n%s", FormatAnalysisReport(report))
	}
	t.Logf("command: tools/test-go.sh -run TestTopologyGuardBaseline -count=1 -v ./internal/archguard\n%s\n%s", FormatAnalysisReport(report), FormatShadowReport(report))
}

func TestCompilerShadowBaseline(t *testing.T) {
	records := loadRepoPackageRecords(t)
	report := AnalyzeReport(records)
	if report.HasShadowFindings() {
		t.Fatalf("archguard compiler-shadow baseline has unexplained findings:\n%s", FormatShadowReport(report))
	}
	t.Logf("command: tools/test-go.sh -run TestCompilerShadowBaseline -count=1 -v ./internal/archguard\n%s", FormatShadowReport(report))
}

func loadRepoPackageRecords(t *testing.T) []PackageRecord {
	t.Helper()
	root := findRepoRoot(t)

	repositoryPackageRecordCache.Do(func() {
		command := exec.Command("go", "list", "-json", "./...")
		command.Dir = root

		output, err := command.Output()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				repositoryPackageRecordCache.err = fmt.Errorf(
					"go list failed: %w\nstderr:\n%s",
					err,
					string(exitErr.Stderr),
				)
				return
			}
			repositoryPackageRecordCache.err = fmt.Errorf("go list failed: %w", err)
			return
		}

		repositoryPackageRecordCache.records, repositoryPackageRecordCache.err = ParseGoListJSON(output)
	})
	if repositoryPackageRecordCache.err != nil {
		t.Fatal(repositoryPackageRecordCache.err)
	}
	return clonePackageRecords(repositoryPackageRecordCache.records)
}

func clonePackageRecords(records []PackageRecord) []PackageRecord {
	cloned := make([]PackageRecord, len(records))
	for index, record := range records {
		record.Imports = slices.Clone(record.Imports)
		record.GoFiles = slices.Clone(record.GoFiles)
		record.CgoFiles = slices.Clone(record.CgoFiles)
		record.TestGoFiles = slices.Clone(record.TestGoFiles)
		record.XTestGoFiles = slices.Clone(record.XTestGoFiles)
		if record.FileContents != nil {
			record.FileContents = make(map[string]string, len(record.FileContents))
			for name, content := range record.FileContents {
				record.FileContents[name] = content
			}
		}
		cloned[index] = record
	}
	return cloned
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
