package archguard

import (
	"strings"
	"testing"
)

func TestProjectRootAuthorityRejectsInvertedBoundaryImports(t *testing.T) {
	report := FormatReport(AnalyzeRecords([]PackageRecord{
		{
			ImportPath: "example.com/project/internal/effect/mutation/rootedpath",
			Imports: []string{
				"example.com/project/internal/effect/storage/commit",
				"example.com/project/internal/realization",
				"example.com/project/internal/supply/compat/skill",
			},
		},
	}))
	for _, want := range []string{
		"project root authority imports forbidden boundary: storage commit: internal/effect/mutation/rootedpath -> internal/effect/storage/commit",
		"project root authority imports forbidden boundary: surface: internal/effect/mutation/rootedpath -> internal/realization",
		"project root authority imports forbidden boundary: compat: internal/effect/mutation/rootedpath -> internal/supply/compat/skill",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report missing %q:\n%s", want, report)
		}
	}
}
