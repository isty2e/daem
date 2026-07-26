package archguard

import (
	"strings"
	"testing"
)

func TestSurfaceMayImportCanonicalOutputButNotOutputAdapters(t *testing.T) {
	t.Parallel()

	for _, packagePath := range []string{"internal/realization", "internal/realization/aggregate"} {
		allowed := AnalyzeRecords([]PackageRecord{{
			ImportPath: "example.com/project/" + packagePath,
			Imports:    []string{"example.com/project/internal/output"},
		}})
		if len(allowed) != 0 {
			t.Errorf("%s canonical output import produced violations:\n%s", packagePath, FormatReport(allowed))
		}

		report := FormatReport(AnalyzeRecords([]PackageRecord{{
			ImportPath: "example.com/project/" + packagePath,
			Imports:    []string{"example.com/project/internal/output/hostpath"},
		}}))
		if !strings.Contains(report, packagePath+" -> internal/output/hostpath") {
			t.Errorf("%s output adapter import was not rejected:\n%s", packagePath, report)
		}
	}
}
