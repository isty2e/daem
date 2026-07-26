package archguard

func isSurfaceRootPackage(packagePath string) bool {
	return packagePath == "internal/realization" ||
		isPackageOrChild(packagePath, "internal/realization/profile")
}

func isForbiddenSurfaceProfileImport(importPath string) bool {
	if importPath == "internal/output" {
		return false
	}
	for _, forbidden := range []string{
		"internal/declaration",
		"internal/supply/source",
		"internal/intent",
		"internal/lifecycle",
		"internal/realization/lock",
		"internal/realization/lockfile",
		"internal/assurance/statefile",
		"internal/output",
		"internal/render",
		"internal/hostoutput",
		"internal/effect/payload",
		"internal/assurance/observe",
		"internal/reconcile",
		"internal/effect/journal",
		"internal/effect/execute",
		"internal/adopt",
		"internal/importer",
		"internal/diagnose",
		"internal/cli/present",
		"internal/workflow",
		"internal/cli",
		"internal/compat",
		"internal/resource/skill/compat",
	} {
		if matchesInternalImport(importPath, forbidden) {
			return true
		}
	}
	return false
}

func isHardSurfaceFamilyPackage(packagePath string) bool {
	for _, leaf := range []string{
		"internal/realization/aggregate",
		"internal/realization/aggregate/codec",
		"internal/realization/aggregate/hook",
	} {
		if isPackageOrChild(packagePath, leaf) {
			return true
		}
	}
	return false
}

func isForbiddenHardSurfaceFamilyImport(importPath string) bool {
	if importPath == "internal/output" {
		return false
	}
	for _, forbidden := range []string{
		"internal/declaration",
		"internal/supply/source",
		"internal/intent",
		"internal/lifecycle",
		"internal/realization/lock",
		"internal/realization/lockfile",
		"internal/assurance/statefile",
		"internal/output",
		"internal/render",
		"internal/hostoutput",
		"internal/effect/payload",
		"internal/assurance/observe",
		"internal/reconcile",
		"internal/effect/journal",
		"internal/effect/execute",
		"internal/adopt",
		"internal/importer",
		"internal/diagnose",
		"internal/cli/present",
		"internal/workflow",
		"internal/cli",
		"internal/compat",
		"internal/resource/skill/compat",
	} {
		if matchesInternalImport(importPath, forbidden) {
			return true
		}
	}
	return false
}
