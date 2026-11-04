package archguard

import "testing"

func TestNativeProjectionImportsOnlyExactStableValues(t *testing.T) {
	const projection = "example.com/project/internal/topology/projection"
	allowed := []string{
		"example.com/project/internal/topology",
		"example.com/project/internal/desired/entity",
		"example.com/project/internal/output",
		"example.com/project/internal/target",
	}
	if findings := analyzeArchitectureDependencyDirections([]PackageRecord{{ImportPath: projection, Imports: allowed}}); len(findings) != 0 {
		t.Fatalf("stable native projection values rejected: %s", FormatReport(findings))
	}
	for _, forbidden := range []string{
		"example.com/project/internal/output/project",
		"example.com/project/internal/output/ownership",
		"example.com/project/internal/realization/profile",
		"example.com/project/internal/effect/mutation",
	} {
		findings := analyzeArchitectureDependencyDirections([]PackageRecord{{ImportPath: projection, Imports: []string{forbidden}}})
		if countViolationRule(findings, ruleTopologyImportDirection) != 1 {
			t.Fatalf("projection gained policy/effect dependency: %s", FormatReport(findings))
		}
	}
	for _, other := range []string{"example.com/project/internal/topology", "example.com/project/internal/topology/resource"} {
		findings := analyzeArchitectureDependencyDirections([]PackageRecord{{ImportPath: other, Imports: allowed[2:]}})
		if countViolationRule(findings, ruleTopologyImportDirection) != 2 {
			t.Fatalf("stable-value permission leaked to another owner: %s", FormatReport(findings))
		}
	}
}
