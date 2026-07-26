package archguard

import "path"

const (
	ruleCLIDirectPhaseImport        = "cli-direct-phase-import"
	ruleObserveReconciliationImport = "observe-reconciliation-import"
	ruleStatefileBehaviorImport     = "statefile-behavior-import"
	ruleAssuranceEffectImport       = "assurance-effect-import"
	ruleHostRouteCommandAssurance   = "hostroute-command-assurance-import"
	ruleWorkflowNestedImport        = "workflow-nested-import"
	rulePathsInternalImport         = "paths-internal-import"
	ruleJournalRecoveryImport       = "journal-recovery-boundary-import"
	ruleManifestBridgeImport        = "manifest-bridge-import"
	ruleWorkflowReverseImport       = "workflow-reverse-import"
	rulePresentWorkflowImport       = "present-workflow-import"
	ruleSkillRepairLockImport       = "skill-repair-lock-import"
	rulePresentLockBuildImport      = "present-lock-build-import"
	ruleCLIPresentationReverse      = "cli-presentation-reverse-import"
	ruleCLIPresentationChild        = "cli-presentation-child-package"
	ruleDiagnoseOutputProject       = "diagnose-output-project-import"
	ruleSourceCacheBoundaryImport   = "source-cache-boundary-import"
	ruleSourceSemanticImport        = "source-semantic-import"
	ruleLockBuildSourceImport       = "lock-build-source-import"
	ruleSurfaceProfileBoundary      = "surface-profile-boundary-import"
	ruleSurfaceHardFamilyBoundary   = "surface-hard-family-boundary-import"
	ruleWorkflowPresentImport       = "workflow-present-import"
	ruleForbiddenGenericRoleShape   = "forbidden-generic-role-shape"
	ruleForbiddenStatePackageShape  = "forbidden-state-package-shape"
	ruleForbiddenResourceOperation  = "forbidden-resource-operation-cell"
	ruleForbiddenProgressBasis      = "forbidden-progress-basis-shape"
	ruleStorageCommitImport         = "storage-commit-boundary-import"
	ruleBuildIdentityImport         = "build-identity-boundary-import"
	ruleReleaseArtifactImport       = "release-artifact-boundary-import"
	ruleFutureFamilyResourceBucket  = "future-family-resource-bucket"
	ruleFutureFamilyWorkflowCell    = "future-family-workflow-cell"
	ruleFutureFamilyOperationCell   = "future-family-operation-cell"
	ruleRetiredLifecyclePackage     = "retired-lifecycle-package"
	ruleRetiredExecutePackage       = "retired-execute-package"
	ruleCoreTerminalSideEffect      = "core-terminal-side-effect"
	ruleDensityAdmissionInvalid     = "density-admission-invalid"
	ruleDensityReviewRequired       = "density-review-required"
	ruleDensityThreshold            = "density-threshold"
	ruleFutureMCPPluginMonolith     = "future-mcp-plugin-monolith"

	ruleInternalImportsCLI = "internal package imports CLI"
)

type findingDisposition uint8

const (
	findingDispositionViolation findingDisposition = iota
	findingDispositionReviewRequired
	findingDispositionWarning
)

type rawFinding struct {
	finding     GuardrailFinding
	disposition findingDisposition
}

// AnalyzeRecords returns semantic topology violations for package records.
// Use AnalyzeReport when density review requirements and warnings are relevant.
func AnalyzeRecords(records []PackageRecord) []GuardrailFinding {
	report := AnalyzeReport(records)
	return append([]GuardrailFinding(nil), report.Violations...)
}

// AnalyzeReport returns classified topology guardrail findings for package records.
func AnalyzeReport(records []PackageRecord) Report {
	rawFindings, density := analyzeRawFindings(records)
	report := classifyFindings(rawFindings)
	report.PackageDensity = density
	return report
}

func analyzeRawFindings(records []PackageRecord) ([]rawFinding, []PackageDensity) {
	findings := hardFindings(analyzeArchitectureDependencyDirections(records))
	for _, record := range sortedRecords(records) {
		packagePath, ok := internalPath(record.ImportPath)
		if !ok {
			continue
		}

		findings = append(findings, hardFindings(analyzeImports(packagePath, record.Imports))...)
		findings = append(findings, hardFindings(analyzeForbiddenShapes(packagePath, record))...)
		findings = append(findings, hardFindings(analyzeCoreTerminalSideEffects(packagePath, record))...)
	}

	densityFindings, density := analyzeDensity(records)
	findings = append(findings, densityFindings...)

	return sortedRawFindings(dedupRawFindings(findings)), density
}

func analyzeImports(packagePath string, imports []string) []GuardrailFinding {
	var violations []GuardrailFinding
	for _, importPath := range sortedStrings(imports) {
		importInternalPath, isInternal := internalPath(importPath)
		if packagePath == "internal/realization/lock/refine" && !isInternal && !isAllowedLockRefineExternalImport(importPath) {
			violations = append(violations, GuardrailFinding{
				Rule:        ruleLockRefineBoundaryImport,
				PackagePath: packagePath,
				ImportPath:  importPath,
				Detail:      "lock/refine may import only its reviewed pure standard-library set and canonical internal inputs",
			})
		}

		if packagePath == "internal/paths" && !isStdlibImportPath(importPath) {
			reportedImportPath := importPath
			if isInternal {
				reportedImportPath = importInternalPath
			}
			violations = append(violations, GuardrailFinding{
				Rule:        rulePathsInternalImport,
				PackagePath: packagePath,
				ImportPath:  reportedImportPath,
			})
		}

		if packagePath == "internal/effect/journal/recovery" && !isAllowedJournalRecoveryImport(importPath, importInternalPath, isInternal) {
			reportedImportPath := importPath
			if isInternal {
				reportedImportPath = importInternalPath
			}
			violations = append(violations, GuardrailFinding{
				Rule:        ruleJournalRecoveryImport,
				PackagePath: packagePath,
				ImportPath:  reportedImportPath,
				Detail:      "journal/recovery owns wire-neutral pure recovery authority and may import only its reviewed canonical value dependencies",
			})
		}

		if !isInternal {
			continue
		}
		if packagePath == "internal/effect/storage/commit" && !isAllowedStorageCommitImport(importInternalPath) {
			violations = append(violations, GuardrailFinding{
				Rule:        ruleStorageCommitImport,
				PackagePath: packagePath,
				ImportPath:  importInternalPath,
				Detail:      "storage/commit is a final filesystem leaf and must not import internal policy or workflow packages",
			})
		}
		if packagePath == "internal/buildidentity" && importInternalPath != "internal/platformsupport" {
			violations = append(violations, GuardrailFinding{
				Rule:        ruleBuildIdentityImport,
				PackagePath: packagePath,
				ImportPath:  importInternalPath,
				Detail:      "build identity may consume canonical platform identity only; archives, workflows, presentation, and effects stay outside",
			})
		}
		if packagePath == "internal/releaseartifact" && importInternalPath != "internal/buildidentity" && importInternalPath != "internal/platformsupport" {
			violations = append(violations, GuardrailFinding{
				Rule:        ruleReleaseArtifactImport,
				PackagePath: packagePath,
				ImportPath:  importInternalPath,
				Detail:      "release artifact construction consumes build identity and platform admission only; file I/O, workflows, presentation, and publication stay outside",
			})
		}

		violations = append(violations, analyzeConcurrencyProgressImports(packagePath, importInternalPath)...)
		violations = append(violations, analyzeCLIPresentationImports(packagePath, importInternalPath)...)

		if matchesInternalImport(importInternalPath, "internal/manifest") {
			violations = append(violations, GuardrailFinding{
				Rule:        ruleManifestBridgeImport,
				PackagePath: packagePath,
				ImportPath:  importInternalPath,
			})
		}

		if packagePath == "internal/cli" && isForbiddenCLIImport(importInternalPath) {
			violations = append(violations, GuardrailFinding{
				Rule:        ruleCLIDirectPhaseImport,
				PackagePath: packagePath,
				ImportPath:  importInternalPath,
			})
		}

		violations = append(violations, analyzeLockImports(packagePath, importInternalPath)...)

		if isObserveAdapterPackage(packagePath) && matchesInternalImport(importInternalPath, "internal/reconcile") {
			violations = append(violations, GuardrailFinding{
				Rule:        ruleObserveReconciliationImport,
				PackagePath: packagePath,
				ImportPath:  importInternalPath,
			})
		}

		if packagePath == "internal/assurance/statefile" && isForbiddenStatefileBehaviorImport(importInternalPath) {
			violations = append(violations, GuardrailFinding{
				Rule:        ruleStatefileBehaviorImport,
				PackagePath: packagePath,
				ImportPath:  importInternalPath,
			})
		}

		if isPackageOrChild(packagePath, "internal/assurance") &&
			matchesInternalImport(importInternalPath, "internal/effect/execute") {
			violations = append(violations, GuardrailFinding{
				Rule:        ruleAssuranceEffectImport,
				PackagePath: packagePath,
				ImportPath:  importInternalPath,
				Detail:      "assurance classifies evidence and postconditions; effect command and executor models stay outside",
			})
		}

		if isPackageOrChild(packagePath, "internal/effect/execute/hostroute") &&
			matchesInternalImport(importInternalPath, "internal/assurance") {
			violations = append(violations, GuardrailFinding{
				Rule:        ruleHostRouteCommandAssurance,
				PackagePath: packagePath,
				ImportPath:  importInternalPath,
				Detail:      "execute/hostroute lowers admitted commands only; post-attempt classification stays in assurance/hostroute",
			})
		}

		if isPresentPackage(packagePath) &&
			matchesInternalImport(importInternalPath, "internal/workflow") &&
			!isAllowedPresentWorkflowResultImport(packagePath, importInternalPath) {
			violations = append(violations, GuardrailFinding{
				Rule:        rulePresentWorkflowImport,
				PackagePath: packagePath,
				ImportPath:  importInternalPath,
				Detail:      "presentation may read only an explicitly admitted workflow-local result; workflows never import presentation",
			})
		}

		if isPackageOrChild(packagePath, "internal/supply/compat/skill/repair") && matchesInternalImport(importInternalPath, "internal/realization/lock") {
			violations = append(violations, GuardrailFinding{
				Rule:        ruleSkillRepairLockImport,
				PackagePath: packagePath,
				ImportPath:  importInternalPath,
				Detail:      "skill repair owns canonical recipes and replay; lock schema adaptation stays in lock-owned adapters",
			})
		}

		if packagePath == "internal/cli/present" && matchesInternalImport(importInternalPath, "internal/realization/lock/build") {
			violations = append(violations, GuardrailFinding{
				Rule:        rulePresentLockBuildImport,
				PackagePath: packagePath,
				ImportPath:  importInternalPath,
				Detail:      "cli/present renders output-owned progress facts; lock/build event adaptation happens at command or workflow boundaries",
			})
		}

		if isPackageOrChild(packagePath, "internal/diagnose") && matchesInternalImport(importInternalPath, "internal/output/project") {
			violations = append(violations, GuardrailFinding{
				Rule:        ruleDiagnoseOutputProject,
				PackagePath: packagePath,
				ImportPath:  importInternalPath,
				Detail:      "diagnose uses surface capability facts and findings; output/project owns desired host projections only",
			})
		}

		if isSurfaceRootPackage(packagePath) && isForbiddenSurfaceProfileImport(importInternalPath) {
			violations = append(violations, GuardrailFinding{
				Rule:        ruleSurfaceProfileBoundary,
				PackagePath: packagePath,
				ImportPath:  importInternalPath,
				Detail:      "surface profile and binding facts are T-owned static capabilities; lifecycle, lock, state, observation, planning, execution, presentation, source, declaration, and workflow behavior stay outside this leaf",
			})
		}

		if isHardSurfaceFamilyPackage(packagePath) && isForbiddenHardSurfaceFamilyImport(importInternalPath) {
			violations = append(violations, GuardrailFinding{
				Rule:        ruleSurfaceHardFamilyBoundary,
				PackagePath: packagePath,
				ImportPath:  importInternalPath,
				Detail:      "hard surface family leaves own static aggregate, command-hook, or bridge diagnostics only; lifecycle, lock, route evidence, observation, execution, presentation, output, payload, source, declaration, and workflow behavior stay outside",
			})
		}

		if !matchesInternalImport(importInternalPath, "internal/cli/present") &&
			matchesInternalImport(importInternalPath, "internal/cli") {
			violations = append(violations, GuardrailFinding{
				Rule:        ruleInternalImportsCLI,
				PackagePath: packagePath,
				ImportPath:  importInternalPath,
			})
		}

		if isForbiddenNestedWorkflowImport(packagePath, importInternalPath) {
			violations = append(violations, GuardrailFinding{
				Rule:        ruleWorkflowNestedImport,
				PackagePath: packagePath,
				ImportPath:  importInternalPath,
				Detail:      "one workflow may not reuse another workflow; move shared sequencing to its exact boundary owner",
			})
		}

		for _, rule := range importRules {
			if !rule.subject(packagePath) {
				continue
			}
			for _, forbidden := range rule.forbiddenImports {
				if matchesForbiddenImport(importInternalPath, forbidden) {
					if isAllowedImportRuleException(rule.rule, packagePath, importInternalPath) {
						continue
					}
					violations = append(violations, GuardrailFinding{
						Rule:        rule.rule + ": " + forbidden.name,
						PackagePath: packagePath,
						ImportPath:  importInternalPath,
					})
				}
			}
		}

		if isForbiddenWorkflowReverseImportPackage(packagePath) &&
			matchesInternalImport(importInternalPath, "internal/workflow") {
			violations = append(violations, GuardrailFinding{
				Rule:        ruleWorkflowReverseImport,
				PackagePath: packagePath,
				ImportPath:  importInternalPath,
			})
		}
	}
	return violations
}

func isAllowedJournalRecoveryImport(importPath string, internalImport string, isInternal bool) bool {
	if isInternal {
		switch internalImport {
		case "internal/assurance/durable",
			"internal/effect/mutation/ownership",
			"internal/output",
			"internal/output/ownership",
			"internal/realization",
			"internal/realization/aggregate",
			"internal/target",
			"internal/topology":
			return true
		default:
			return false
		}
	}
	switch importPath {
	case "fmt", "io/fs", "path", "slices", "strings":
		return true
	default:
		return false
	}
}

func isAllowedImportRuleException(rule string, packagePath string, importPath string) bool {
	return rule == "mutation package imports forbidden phase" &&
		packagePath == "internal/effect/mutation/ownership" &&
		importPath == "internal/output/ownership"
}

func isAllowedStorageCommitImport(importPath string) bool {
	return importPath == "internal/effect/mutation/filesystem" ||
		importPath == "internal/effect/mutation/rootedpath"
}

func analyzeForbiddenShapes(packagePath string, record PackageRecord) []GuardrailFinding {
	var violations []GuardrailFinding
	if isCLIPresentationChildPackage(packagePath) && !isAllowedCLIPresentationChildPackage(packagePath) {
		violations = append(violations, GuardrailFinding{
			Rule:        ruleCLIPresentationChild,
			PackagePath: packagePath,
			Path:        packagePath,
			Detail:      "CLI presentation is one process-owned output contract; only the stateful terminal-progress lifecycle is an allowed child package",
		})
	}
	if isForbiddenGenericRolePackageShape(packagePath) {
		violations = append(violations, GuardrailFinding{
			Rule:        ruleForbiddenGenericRoleShape,
			PackagePath: packagePath,
			Path:        packagePath,
		})
	}

	if isForbiddenStatePackageShape(packagePath) {
		violations = append(violations, GuardrailFinding{
			Rule:        ruleForbiddenStatePackageShape,
			PackagePath: packagePath,
			Path:        packagePath,
		})
	}

	if isRetiredLifecyclePackage(packagePath) {
		violations = append(violations, GuardrailFinding{
			Rule:        ruleRetiredLifecyclePackage,
			PackagePath: packagePath,
			Path:        packagePath,
			Detail:      "lifecycle is a retired mixed-axis package; desired, topology, realization, assurance, and operation facts stay with their canonical owners",
		})
	}

	if detail, retired := retiredExecutePackageDetail(packagePath); retired {
		violations = append(violations, GuardrailFinding{
			Rule:        ruleRetiredExecutePackage,
			PackagePath: packagePath,
			Path:        packagePath,
			Detail:      detail,
		})
	}

	if isForbiddenResourceOperationPackageShape(packagePath) {
		violations = append(violations, GuardrailFinding{
			Rule:        ruleForbiddenResourceOperation,
			PackagePath: packagePath,
			Path:        packagePath,
		})
	}

	if isFutureMCPPluginMonolith(packagePath) {
		violations = append(violations, GuardrailFinding{
			Rule:        ruleFutureMCPPluginMonolith,
			PackagePath: packagePath,
			Path:        packagePath,
		})
	}

	if isFutureFamilyResourceBucket(packagePath) {
		violations = append(violations, GuardrailFinding{
			Rule:        ruleFutureFamilyResourceBucket,
			PackagePath: packagePath,
			Path:        packagePath,
		})
	}

	if isFutureFamilyWorkflowCell(packagePath) {
		violations = append(violations, GuardrailFinding{
			Rule:        ruleFutureFamilyWorkflowCell,
			PackagePath: packagePath,
			Path:        packagePath,
		})
	}

	if isFutureFamilyOperationCellPath(packagePath) {
		violations = append(violations, GuardrailFinding{
			Rule:        ruleFutureFamilyOperationCell,
			PackagePath: packagePath,
			Path:        packagePath,
		})
	}

	if isForbiddenProgressBasisPackageShape(packagePath) {
		violations = append(violations, GuardrailFinding{
			Rule:        ruleForbiddenProgressBasis,
			PackagePath: packagePath,
			Path:        packagePath,
			Detail:      "progress/concurrency facts must stay phase-owned; do not introduce workflow, generic task, operation, progress, or resource lockable packages",
		})
	}

	for _, fileName := range sortedStrings(packageFiles(record)) {
		filePath := path.Join(packagePath, fileName)
		if isForbiddenGenericRoleFileShape(filePath) {
			violations = append(violations, GuardrailFinding{
				Rule:        ruleForbiddenGenericRoleShape,
				PackagePath: packagePath,
				Path:        filePath,
			})
		}
		if isForbiddenResourceOperationFileShape(filePath) {
			violations = append(violations, GuardrailFinding{
				Rule:        ruleForbiddenResourceOperation,
				PackagePath: packagePath,
				Path:        filePath,
			})
		}
		if isFutureFamilyOperationCellPath(filePath) {
			violations = append(violations, GuardrailFinding{
				Rule:        ruleFutureFamilyOperationCell,
				PackagePath: packagePath,
				Path:        filePath,
			})
		}
	}
	return violations
}

func packageFiles(record PackageRecord) []string {
	files := make([]string, 0, len(record.GoFiles)+len(record.TestGoFiles)+len(record.XTestGoFiles))
	files = append(files, record.GoFiles...)
	files = append(files, record.TestGoFiles...)
	files = append(files, record.XTestGoFiles...)
	return files
}

func hardFindings(violations []GuardrailFinding) []rawFinding {
	findings := make([]rawFinding, 0, len(violations))
	for _, violation := range violations {
		findings = append(findings, rawFinding{
			finding:     violation,
			disposition: findingDispositionViolation,
		})
	}
	return findings
}
