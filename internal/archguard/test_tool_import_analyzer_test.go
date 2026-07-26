package archguard

// analyzeProductionTestToolImports keeps admitted helpers out of executable
// package graphs.
func analyzeProductionTestToolImports(records []PackageRecord, testTools map[string]testToolAdmission) []GuardrailFinding {
	reachable := productionReachablePackages(records)
	var violations []GuardrailFinding
	for _, record := range sortedRecords(records) {
		if !reachable[record.ImportPath] {
			continue
		}
		packagePath, repositoryPackage := architecturePath(record.ImportPath)
		for _, importPath := range sortedStrings(record.Imports) {
			importedPackage, ok := architecturePath(importPath)
			if !ok {
				continue
			}
			admission, testTool := testToolImportBoundary(importedPackage, testTools)
			if !testTool {
				continue
			}
			if !repositoryPackage {
				packagePath = record.ImportPath
			}
			violations = append(violations, GuardrailFinding{
				Rule:        ruleProductionImportsTestTool,
				PackagePath: packagePath,
				ImportPath:  importedPackage,
				Reason:      admission.Reason,
				Detail:      "architecture test/tool support must remain outside every production package graph",
			})
		}
	}
	return violations
}

func testToolOwner(packagePath string, testTools map[string]testToolAdmission) (testToolAdmission, bool) {
	admission, exact := testTools[packagePath]
	return admission, exact
}

func testToolImportBoundary(packagePath string, testTools map[string]testToolAdmission) (testToolAdmission, bool) {
	if admission, exact := testToolOwner(packagePath, testTools); exact {
		return admission, true
	}
	for _, ownerPath := range sortedOwnerPaths(testTools) {
		if isPackageOrChild(packagePath, ownerPath) {
			return testTools[ownerPath], true
		}
	}
	return testToolAdmission{}, false
}
