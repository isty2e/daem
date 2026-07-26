package archguard

const (
	ruleLockBuildBoundaryImport     = "lock-build-boundary-import"
	ruleLockGenerateBoundaryImport  = "lock-generate-boundary-import"
	ruleLockCanonicalReverseImport  = "lock-canonical-reverse-import"
	ruleLockRefineBoundaryImport    = "lock-refine-boundary-import"
	ruleLockCanonicalLockfileImport = "lock-canonical-lockfile-import"
	ruleLockfileBehaviorImport      = "lockfile-behavior-import"
)

func analyzeLockImports(packagePath string, importPath string) []GuardrailFinding {
	var violations []GuardrailFinding
	if packagePath == "internal/realization/lock/build" && isForbiddenLockBuildBoundaryImport(importPath) {
		violations = append(violations, GuardrailFinding{
			Rule:        ruleLockBuildBoundaryImport,
			PackagePath: packagePath,
			ImportPath:  importPath,
		})
	}

	if packagePath == "internal/realization/lock" && isPackageOrChild(importPath, "internal/realization/lock") {
		violations = append(violations, GuardrailFinding{
			Rule:        ruleLockCanonicalReverseImport,
			PackagePath: packagePath,
			ImportPath:  importPath,
			Detail:      "canonical lock contracts may not import refinement, assembly, comparison, operation, or test-support descendants",
		})
	}

	if packagePath == "internal/realization/lock/refine" && !isAllowedLockRefineImport(importPath) {
		violations = append(violations, GuardrailFinding{
			Rule:        ruleLockRefineBoundaryImport,
			PackagePath: packagePath,
			ImportPath:  importPath,
			Detail:      "lock/refine performs pure family lowering and may import only canonical artifact, Desired, lock, Surface, target, and Topology facts",
		})
	}

	if packagePath == "internal/workflow/lock/generate" && isForbiddenLockGenerateImport(importPath) {
		violations = append(violations, GuardrailFinding{
			Rule:        ruleLockGenerateBoundaryImport,
			PackagePath: packagePath,
			ImportPath:  importPath,
			Detail:      "lock/generate owns prospective snapshot generation only; declaration ingress, command, mutation, observation, decision, effect, and presentation stay outside",
		})
	}

	if packagePath == "internal/realization/lock" && matchesInternalImport(importPath, "internal/realization/lockfile") {
		violations = append(violations, GuardrailFinding{
			Rule:        ruleLockCanonicalLockfileImport,
			PackagePath: packagePath,
			ImportPath:  importPath,
			Detail:      "canonical lock contracts and comparison behavior may not depend on persistence syntax",
		})
	}

	if packagePath == "internal/realization/lockfile" && isForbiddenLockfileBehaviorImport(importPath) {
		violations = append(violations, GuardrailFinding{
			Rule:        ruleLockfileBehaviorImport,
			PackagePath: packagePath,
			ImportPath:  importPath,
		})
	}

	return violations
}

func isForbiddenLockBuildBoundaryImport(importPath string) bool {
	for _, forbidden := range []string{
		"internal/manifest",
		"internal/realization/lockfile",
		"internal/cli/present",
		"internal/cli",
		"internal/workflow",
		"internal/diagnose",
		"internal/assurance/observe",
		"internal/reconcile",
		"internal/effect/execute",
		"internal/effect/journal",
		"internal/output/project",
		"internal/effect/payload",
		"internal/assurance/statefile",
	} {
		if matchesInternalImport(importPath, forbidden) {
			return true
		}
	}
	return false
}

func isForbiddenLockGenerateImport(importPath string) bool {
	if importPath == "internal/realization/lock" || importPath == "internal/realization/aggregate/hook" {
		return false
	}
	for _, allowed := range []string{
		"internal/supply/artifact",
		"internal/desired",
		"internal/realization/lock/build",
		"internal/realization/lock/refine",
		"internal/realization/lockfile",
		"internal/paths",
		"internal/supply/source",
	} {
		if matchesInternalImport(importPath, allowed) {
			return false
		}
	}
	return true
}

func isAllowedLockRefineImport(importPath string) bool {
	if importPath == "internal/supply/artifact" ||
		importPath == "internal/realization" ||
		importPath == "internal/realization/lock" ||
		importPath == "internal/target" {
		return true
	}
	for _, canonicalRoot := range []string{
		"internal/realization/aggregate",
		"internal/realization/aggregate/hook",
		"internal/realization/delegate",
		"internal/realization/profile",
		"internal/realization/relation",
	} {
		if matchesInternalImport(importPath, canonicalRoot) &&
			!matchesInternalImport(importPath, "internal/realization/aggregate/codec") {
			return true
		}
	}
	for _, allowed := range []string{
		"internal/desired",
		"internal/topology",
	} {
		if matchesInternalImport(importPath, allowed) {
			return true
		}
	}
	return false
}

func isAllowedLockRefineExternalImport(importPath string) bool {
	switch importPath {
	case "fmt", "path", "sort", "strings":
		return true
	default:
		return false
	}
}

func isForbiddenLockfileBehaviorImport(importPath string) bool {
	for _, forbidden := range []string{
		"internal/realization/lock/build",
		"internal/workflow/lock/generate",
		"internal/realization/lock/refine",
		"internal/workflow",
		"internal/cli/present",
		"internal/cli",
	} {
		if matchesInternalImport(importPath, forbidden) {
			return true
		}
	}
	return false
}
