package archguard

import "strings"

const (
	ruleGenericHostReference         = "generic-host-reference"
	ruleProductionImportsTestTool    = "production-imports-test-tool"
	ruleCanonicalAuthority           = "canonical-authority"
	ruleStaleTestToolAdmission       = "stale-test-tool-admission"
	ruleUnadmittedTestToolPackage    = "unadmitted-test-tool-package"
	ruleInvalidArchitectureOwner     = "invalid-architecture-owner"
	ruleUnreachableProductionPackage = "unreachable-production-package"
	ruleArchitectureSourceUnreadable = "architecture-source-unreadable"
)

var hostSpecializationOwners = map[string]string{
	"internal/effect/execute/configrelation":   "private direct config-relation adapter owns host document selection and mutation syntax",
	"internal/effect/execute/hostroute":        "private delegated-command adapter owns host argv and syntax",
	"internal/realization/lock":                "locked realization owns static host profile and operation-contract variants",
	"internal/assurance/observe/relation/host": "private relation-observation adapters own host inventory syntax",
	"internal/topology/extension":              "extension lowerer owns declarative host relation namespaces and carrier-native structural source interpretation",
	"internal/topology/hook":                   "hook lowerer owns complete desired target and scope projection namespaces",
	"internal/topology/mcp":                    "MCP lowerer owns declarative projection namespaces",
}

type testToolPackageKind string

const (
	testToolHelperPackage    testToolPackageKind = "helper-package"
	testToolTestsOnlyPackage testToolPackageKind = "tests-only-package"
)

type testToolAdmission struct {
	Reason string
	Kind   testToolPackageKind
}

var testToolPackageAdmissions = map[string]testToolAdmission{
	"internal/archguard":                     {Reason: "architecture verification is intentionally outside executable roots", Kind: testToolHelperPackage},
	"internal/desired/testfixture":           {Reason: "constructor-only canonical Desired fixtures are test support", Kind: testToolHelperPackage},
	"internal/realization/lock/snapshottest": {Reason: "constructor-only canonical lock fixtures are test support", Kind: testToolHelperPackage},
	"internal/supply/source/sourcetest":      {Reason: "constructor-only canonical Source fixtures are test support", Kind: testToolHelperPackage},
	"test/cli":                               {Reason: "full black-box CLI journeys belong only to external test files", Kind: testToolTestsOnlyPackage},
	"test/cli/authoring":                     {Reason: "manifest authoring journeys belong only to external test files", Kind: testToolTestsOnlyPackage},
	"test/cli/inspection":                    {Reason: "non-mutating CLI inspection contracts belong only to external test files", Kind: testToolTestsOnlyPackage},
	"test/cli/lifecycle/delegated":           {Reason: "delegated relation lifecycle journeys belong only to external test files", Kind: testToolTestsOnlyPackage},
	"test/cli/lifecycle/hostconfig":          {Reason: "aggregate host-config lifecycle journeys belong only to external test files", Kind: testToolTestsOnlyPackage},
	"test/cli/lifecycle/lockplan":            {Reason: "lock and plan lifecycle journeys belong only to external test files", Kind: testToolTestsOnlyPackage},
	"test/cli/lifecycle/managedpath":         {Reason: "managed-path lifecycle journeys belong only to external test files", Kind: testToolTestsOnlyPackage},
	"test/testkit":                           {Reason: "cross-package integration helpers are test support", Kind: testToolHelperPackage},
	"test/testkit/clijson":                   {Reason: "strict CLI JSON decoding fixtures are test support", Kind: testToolHelperPackage},
	"test/testkit/doctorenv":                 {Reason: "isolated cross-platform doctor environment fixture is test support", Kind: testToolHelperPackage},
	"test/testkit/execcheck":                 {Reason: "isolated executable-attempt canaries are test support", Kind: testToolHelperPackage},
	"test/testkit/metadatatx":                {Reason: "cross-workflow interrupted metadata transaction fixtures are test support", Kind: testToolHelperPackage},
}

func analyzeArchitectureContracts(records []PackageRecord) []GuardrailFinding {
	var violations []GuardrailFinding
	violations = append(violations, validateArchitectureOwnerRows("host specialization", hostSpecializationOwners)...)
	violations = append(violations, validateTestToolAdmissionRows(testToolPackageAdmissions)...)
	violations = append(violations, analyzeArchitectureDependencyDirections(records)...)
	violations = append(violations, analyzeProductionTestToolImports(records, testToolPackageAdmissions)...)
	violations = append(violations, analyzeHostSpecialization(records, hostSpecializationOwners)...)
	violations = append(violations, analyzeCanonicalAuthorities(records)...)
	violations = append(violations, analyzeUnreachableProductionPackages(records, testToolPackageAdmissions)...)
	return sortedViolations(dedupViolations(violations))
}

func validateTestToolAdmissionRows(admissions map[string]testToolAdmission) []GuardrailFinding {
	owners := make(map[string]string, len(admissions))
	var violations []GuardrailFinding
	for packagePath, admission := range admissions {
		owners[packagePath] = admission.Reason
		if admission.Kind != testToolHelperPackage && admission.Kind != testToolTestsOnlyPackage {
			violations = append(violations, GuardrailFinding{
				Rule:   ruleInvalidArchitectureOwner,
				Path:   packagePath,
				Reason: admission.Reason,
				Detail: "test/tool admission kind must be closed and valid",
			})
		}
	}
	return append(violations, validateArchitectureOwnerRows("test/tool", owners)...)
}

func validateArchitectureOwnerRows(class string, owners map[string]string) []GuardrailFinding {
	var violations []GuardrailFinding
	for packagePath, reason := range owners {
		detail := ""
		switch {
		case packagePath == "" || strings.ContainsAny(packagePath, "*?[]"):
			detail = class + " owner path must be exact and non-empty"
		case !strings.HasPrefix(packagePath, "internal/") && !strings.HasPrefix(packagePath, "test/"):
			detail = class + " owner must be an internal or test package"
		case strings.TrimSpace(reason) == "":
			detail = class + " owner reason must be non-empty"
		}
		if detail != "" {
			violations = append(violations, GuardrailFinding{
				Rule:   ruleInvalidArchitectureOwner,
				Path:   packagePath,
				Reason: reason,
				Detail: detail,
			})
		}
	}
	return violations
}

func sortedOwnerPaths[Value any](owners map[string]Value) []string {
	paths := make([]string, 0, len(owners))
	for packagePath := range owners {
		paths = append(paths, packagePath)
	}
	return sortedStrings(paths)
}

func productionFiles(record PackageRecord) []string {
	files := append([]string(nil), record.GoFiles...)
	files = append(files, record.CgoFiles...)
	return sortedStrings(files)
}
