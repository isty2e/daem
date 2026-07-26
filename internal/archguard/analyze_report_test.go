package archguard

import (
	"testing"
)

func TestFormatReportIsDeterministic(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/resource/skill",
			Imports: []string{
				"example.com/project/internal/realization/lockfile",
				"example.com/project/internal/declaration",
			},
		},
	}

	first := FormatReport(AnalyzeRecords(records))
	second := FormatReport(AnalyzeRecords(records))
	if first != second {
		t.Fatalf("first report = %q\nsecond report = %q", first, second)
	}
}
