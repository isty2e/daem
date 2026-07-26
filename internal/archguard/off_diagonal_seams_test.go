package archguard

import (
	"strings"
	"testing"
)

func TestAnalyzeRecordsRejectsCurrentOffDiagonalSeams(t *testing.T) {
	report := FormatReport(AnalyzeRecords([]PackageRecord{
		{
			ImportPath: "example.com/project/internal/supply/compat/skill/repair",
			Imports:    []string{"example.com/project/internal/realization/lock"},
		},
		{
			ImportPath: "example.com/project/internal/cli/present",
			Imports:    []string{"example.com/project/internal/realization/lock/build"},
		},
		{
			ImportPath: "example.com/project/internal/diagnose",
			Imports:    []string{"example.com/project/internal/output/project/hook"},
		},
	}))

	for _, want := range []string{
		"skill-repair-lock-import: internal/supply/compat/skill/repair -> internal/realization/lock",
		"present-lock-build-import: internal/cli/present -> internal/realization/lock/build",
		"diagnose-output-project-import: internal/diagnose -> internal/output/project/hook",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report = %q, want %q", report, want)
		}
	}
}

func TestAnalyzeRecordsAllowsOffDiagonalLookalikes(t *testing.T) {
	report := FormatReport(AnalyzeRecords([]PackageRecord{
		{
			ImportPath: "example.com/project/internal/cli/present",
			Imports:    []string{"example.com/project/internal/realization/lock"},
		},
		{
			ImportPath: "example.com/project/internal/output/project/hook",
			Imports:    []string{"example.com/project/internal/realization"},
		},
		{
			ImportPath: "example.com/project/internal/diagnose",
			Imports:    []string{"example.com/project/internal/output"},
		},
		{
			ImportPath: "example.com/project/internal/effect/payload/skill",
			Imports:    []string{"example.com/project/internal/realization/lock"},
		},
	}))

	for _, unwanted := range []string{
		"skill-repair-lock-import",
		"present-lock-build-import",
		"diagnose-output-project-import",
	} {
		if strings.Contains(report, unwanted) {
			t.Fatalf("report = %q, did not want %q", report, unwanted)
		}
	}
}
