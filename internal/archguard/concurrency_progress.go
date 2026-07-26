package archguard

func analyzeConcurrencyProgressImports(packagePath string, importPath string) []GuardrailFinding {
	var violations []GuardrailFinding
	if isSourceCachePackage(packagePath) && isForbiddenSourceCacheImport(importPath) {
		violations = append(violations, GuardrailFinding{
			Rule:        ruleSourceCacheBoundaryImport,
			PackagePath: packagePath,
			ImportPath:  importPath,
			Detail:      "source/cache owns cache mutation safety only; resource, lock, workflow, presentation, CLI, and execution semantics must stay outside",
		})
	}

	if isSourcePackage(packagePath) && !isSourceCachePackage(packagePath) && isForbiddenSourceSemanticImport(importPath) {
		violations = append(violations, GuardrailFinding{
			Rule:        ruleSourceSemanticImport,
			PackagePath: packagePath,
			ImportPath:  importPath,
			Detail:      "source packages own source identity and backend execution only; resource, lock, workflow, presentation, CLI, plan/action, repair, and target semantics must stay outside",
		})
	}

	if packagePath == "internal/realization/lock/build" && isForbiddenLockBuildSourceImport(importPath) {
		violations = append(violations, GuardrailFinding{
			Rule:        ruleLockBuildSourceImport,
			PackagePath: packagePath,
			ImportPath:  importPath,
			Detail:      "lock/build may depend on internal/supply/source contracts only; backend dispatch and cache mechanics stay in source/resolution and source/backend",
		})
	}

	if isWorkflowPackage(packagePath) && matchesInternalImport(importPath, "internal/cli/present") {
		violations = append(violations, GuardrailFinding{
			Rule:        ruleWorkflowPresentImport,
			PackagePath: packagePath,
			ImportPath:  importPath,
			Detail:      "workflow may pass options and callbacks only; human rendering stays in cli/present",
		})
	}

	return violations
}

func analyzeCLIPresentationImports(packagePath string, importPath string) []GuardrailFinding {
	if !matchesInternalImport(importPath, "internal/cli/present") || packagePath == "internal/cli" {
		return nil
	}
	return []GuardrailFinding{{
		Rule:        ruleCLIPresentationReverse,
		PackagePath: packagePath,
		ImportPath:  importPath,
		Detail:      "cli/present is the process-owned output contract; only cli and test tools consume it",
	}}
}

func isForbiddenSourceCacheImport(importPath string) bool {
	for _, forbidden := range []string{
		"internal/resource",
		"internal/realization/lock",
		"internal/realization/lockfile",
		"internal/workflow",
		"internal/cli/present",
		"internal/cli",
		"internal/effect/execute",
		"internal/reconcile",
		"internal/output",
		"internal/assurance/observe",
		"internal/effect/payload",
		"internal/effect/journal",
		"internal/adopt",
		"internal/diagnose",
		"internal/compat",
	} {
		if matchesInternalImport(importPath, forbidden) {
			return true
		}
	}
	return false
}

func isForbiddenSourceSemanticImport(importPath string) bool {
	for _, forbidden := range []string{
		"internal/declaration",
		"internal/intent",
		"internal/resource",
		"internal/realization/lock",
		"internal/realization/lockfile",
		"internal/workflow",
		"internal/cli/present",
		"internal/cli",
		"internal/effect/execute",
		"internal/reconcile",
		"internal/output",
		"internal/assurance/observe",
		"internal/effect/payload",
		"internal/effect/journal",
		"internal/adopt",
		"internal/diagnose",
		"internal/compat",
		"internal/realization",
		"internal/target",
	} {
		if matchesInternalImport(importPath, forbidden) {
			return true
		}
	}
	return false
}

func isForbiddenLockBuildSourceImport(importPath string) bool {
	return matchesInternalImport(importPath, "internal/supply/source/resolution") ||
		matchesInternalImport(importPath, "internal/supply/source/backend")
}
