package archguard

import "testing"

func TestPiDependencyGuardPreservesOldForbiddenCounterfactuals(t *testing.T) {
	tests := []struct {
		name       string
		forbidden  PackageRecord
		oldRule    string
		newRule    string
		legitimate PackageRecord
	}{
		{
			name: "desired direction",
			forbidden: PackageRecord{
				ImportPath: "example.com/project/internal/desired/skill",
				Imports:    []string{"example.com/project/internal/workflow/lock"},
			},
			oldRule: ruleDesiredImportDirection,
			newRule: ruleDesiredImportDirection,
			legitimate: PackageRecord{
				ImportPath: "example.com/project/internal/desired/skill",
				Imports:    []string{"example.com/project/internal/supply/source"},
			},
		},
		{
			name: "topology direction",
			forbidden: PackageRecord{
				ImportPath: "example.com/project/internal/topology/mcp",
				Imports:    []string{"example.com/project/internal/desired/skill"},
			},
			oldRule: ruleTopologyImportDirection,
			newRule: ruleTopologyImportDirection,
			legitimate: PackageRecord{
				ImportPath: "example.com/project/internal/topology/mcp",
				Imports:    []string{"example.com/project/internal/desired/mcp"},
			},
		},
		{
			name: "supply direction",
			forbidden: PackageRecord{
				ImportPath: "example.com/project/internal/supply/source/acquisition",
				Imports:    []string{"example.com/project/internal/reconcile"},
			},
			oldRule: ruleSupplyImportDirection,
			newRule: ruleSupplyImportDirection,
			legitimate: PackageRecord{
				ImportPath: "example.com/project/internal/supply/source/acquisition",
				Imports:    []string{"example.com/project/internal/supply/artifact/access"},
			},
		},
		{
			name: "realization direction",
			forbidden: PackageRecord{
				ImportPath: "example.com/project/internal/realization/aggregate/hook",
				Imports:    []string{"example.com/project/internal/assurance/runtimeprobe"},
			},
			oldRule: ruleRealizationImportDirection,
			newRule: ruleRealizationImportDirection,
			legitimate: PackageRecord{
				ImportPath: "example.com/project/internal/realization/aggregate/hook",
				Imports:    []string{"example.com/project/internal/desired/hook"},
			},
		},
		{
			name: "assurance direction",
			forbidden: PackageRecord{
				ImportPath: "example.com/project/internal/assurance/runtimeprobe",
				Imports:    []string{"example.com/project/internal/reconcile"},
			},
			oldRule: ruleAssuranceImportDirection,
			newRule: ruleAssuranceImportDirection,
			legitimate: PackageRecord{
				ImportPath: "example.com/project/internal/assurance/durable",
				Imports:    []string{"example.com/project/internal/topology"},
			},
		},
		{
			name: "reconciliation direction",
			forbidden: PackageRecord{
				ImportPath: "example.com/project/internal/reconcile",
				Imports:    []string{"example.com/project/internal/supply/source"},
			},
			oldRule: ruleReconciliationImportDirection,
			newRule: ruleReconciliationImportDirection,
			legitimate: PackageRecord{
				ImportPath: "example.com/project/internal/reconcile",
				Imports:    []string{"example.com/project/internal/supply/artifact"},
			},
		},
		{
			name: "effect direction",
			forbidden: PackageRecord{
				ImportPath: "example.com/project/internal/effect/payload",
				Imports:    []string{"example.com/project/internal/supply/source/acquisition"},
			},
			oldRule: ruleEffectImportDirection,
			newRule: ruleEffectImportDirection,
			legitimate: PackageRecord{
				ImportPath: "example.com/project/internal/effect/payload",
				Imports:    []string{"example.com/project/internal/supply/artifact/access"},
			},
		},
		{
			name: "aggregate codec boundary",
			forbidden: PackageRecord{
				ImportPath: "example.com/project/internal/reconcile",
				Imports:    []string{"example.com/project/internal/realization/aggregate/codec/mcp"},
			},
			oldRule: ruleAggregateCodecBoundaryImport,
			newRule: ruleAggregateCodecBoundaryImport,
			legitimate: PackageRecord{
				ImportPath: "example.com/project/internal/workflow/apply",
				Imports:    []string{"example.com/project/internal/realization/aggregate/codec/mcp"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			newFindings := analyzeArchitectureDependencyDirections([]PackageRecord{test.forbidden})
			if countViolationRule(newFindings, test.newRule) == 0 {
				t.Fatalf("Pi guard did not reject forbidden fixture:\n%s", FormatReport(newFindings))
			}
			if test.oldRule != test.newRule {
				t.Fatalf("old rule %q changed unexpectedly to %q", test.oldRule, test.newRule)
			}
			nearFindings := analyzeArchitectureDependencyDirections([]PackageRecord{test.legitimate})
			if countViolationRule(nearFindings, test.newRule) != 0 {
				t.Fatalf("Pi guard rejected legitimate near-neighbor:\n%s", FormatReport(nearFindings))
			}
		})
	}
}

func TestPiDependencyGuardAcceptsCurrentRepository(t *testing.T) {
	records := loadRepoPackageRecords(t)
	if findings := analyzeArchitectureDependencyDirections(records); len(findings) != 0 {
		t.Fatalf("Pi dependency guard baseline:\n%s", FormatReport(findings))
	}
}

func TestPiDependencyGuardRejectsSameAffinityKernelToBoundaryImport(t *testing.T) {
	records := []PackageRecord{{
		ImportPath: "example.com/project/internal/realization/aggregate",
		Imports:    []string{"example.com/project/internal/realization/lockfile"},
	}}
	findings := analyzeArchitectureDependencyDirections(records)
	if countViolationRule(findings, ruleRealizationImportDirection) != 1 {
		t.Fatalf("same-affinity boundary findings:\n%s", FormatReport(findings))
	}
}

func TestPiDependencyGuardRejectsUnadmittedKernelCapabilityImport(t *testing.T) {
	records := []PackageRecord{{
		ImportPath: "example.com/project/internal/supply/source",
		Imports:    []string{"example.com/project/internal/supply/source/resolution"},
	}}
	findings := analyzeArchitectureDependencyDirections(records)
	if countViolationRule(findings, ruleSupplyImportDirection) != 1 {
		t.Fatalf("unadmitted capability findings:\n%s", FormatReport(findings))
	}
}

func TestPiDependencyGuardAcceptsExactKernelCapabilityImport(t *testing.T) {
	records := []PackageRecord{{
		ImportPath: "example.com/project/internal/realization/lock",
		Imports:    []string{"example.com/project/internal/supply/compat/skill/repair"},
	}}
	findings := analyzeArchitectureDependencyDirections(records)
	if countViolationRule(findings, ruleRealizationImportDirection) != 0 {
		t.Fatalf("exact capability findings:\n%s", FormatReport(findings))
	}
}
