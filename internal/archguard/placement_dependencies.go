package archguard

const (
	ruleDesiredImportDirection        = "desired-import-direction"
	ruleTopologyImportDirection       = "topology-import-direction"
	ruleSupplyImportDirection         = "supply-import-direction"
	ruleRealizationImportDirection    = "realization-import-direction"
	ruleAssuranceImportDirection      = "assurance-import-direction"
	ruleReconciliationImportDirection = "reconciliation-import-direction"
	ruleEffectImportDirection         = "effect-import-direction"
	ruleAggregateCodecBoundaryImport  = "aggregate-codec-boundary-import"
	rulePackageRoleImportDirection    = "package-role-import-direction"
)

func analyzeArchitectureDependencyDirections(records []PackageRecord) []GuardrailFinding {
	var findings []GuardrailFinding
	for _, record := range sortedRecords(records) {
		packagePath, internal := internalPath(record.ImportPath)
		if !internal {
			continue
		}
		importingPlacement, placed := packagePlacementFor(packagePath)
		if !placed {
			continue
		}
		if isPackageOrChild(packagePath, "internal/desired") &&
			importingPlacement.role != roleTestSupport {
			findings = append(findings, analyzeDesiredImports(packagePath, record.Imports)...)
			continue
		}
		if isPackageOrChild(packagePath, "internal/topology") {
			findings = append(findings, analyzeTopologyImports(packagePath, record.Imports)...)
			continue
		}

		for _, importPath := range sortedStrings(record.Imports) {
			importedPackage, importedInternal := internalPath(importPath)
			if !importedInternal {
				continue
			}
			importedPlacement, importedPlaced := packagePlacementFor(importedPackage)
			if !importedPlaced {
				continue
			}
			if importedPlacement.role == roleCodec &&
				importedPlacement.affinity == affinityRealization &&
				importedPlacement.specialization.kind != specializationFormat &&
				isPackageOrChild(importedPackage, "internal/realization/aggregate/codec") &&
				!allowsConcreteAggregateCodec(packagePath) {
				findings = append(findings, GuardrailFinding{
					Rule:        ruleAggregateCodecBoundaryImport,
					PackagePath: packagePath,
					ImportPath:  importedPackage,
					Detail:      "semantic kernels consume canonical aggregate codec ports; only admitted composition boundaries may select concrete codec implementations",
				})
				continue
			}
			if allowsExactCrossAffinityValue(
				importingPlacement.affinity,
				importedPackage,
				importedPlacement,
			) {
				continue
			}
			if allowsExactKernelCapabilityDependency(
				packagePath,
				importedPackage,
				importingPlacement,
				importedPlacement,
			) {
				continue
			}
			if forbiddenRoleDependency(importingPlacement, importedPlacement) {
				findings = append(findings, GuardrailFinding{
					Rule:        placementDependencyRule(importingPlacement),
					PackagePath: packagePath,
					ImportPath:  importedPackage,
					Detail:      placementDependencyDetail(importingPlacement),
				})
				continue
			}
			if forbiddenAffinityDependency(
				importingPlacement,
				importedPlacement,
			) {
				findings = append(findings, GuardrailFinding{
					Rule:        placementDependencyRule(importingPlacement),
					PackagePath: packagePath,
					ImportPath:  importedPackage,
					Detail:      placementDependencyDetail(importingPlacement),
				})
			}
		}
	}
	return sortedViolations(dedupViolations(findings))
}

func allowsConcreteAggregateCodec(packagePath string) bool {
	for _, root := range []string{
		"internal/adopt/mcp",
		"internal/realization/lock/snapshottest",
		"internal/realization/lockfile",
		"internal/assurance/statefile",
		"internal/realization/aggregate/codec",
		"internal/workflow/apply",
		"internal/workflow/authoring",
		"internal/workflow/help",
		"internal/workflow/list",
		"internal/workflow/lock",
		"internal/workflow/probe",
		"internal/workflow/recover",
		"internal/workflow/refresh",
		"internal/workflow/status",
	} {
		if isPackageOrChild(packagePath, root) {
			return true
		}
	}
	return false
}

func analyzeDesiredImports(packagePath string, imports []string) []GuardrailFinding {
	var findings []GuardrailFinding
	for _, importPath := range sortedStrings(imports) {
		importedPackage, internal := internalPath(importPath)
		if !internal {
			continue
		}
		if packagePath == "internal/desired/entity" {
			findings = append(findings, GuardrailFinding{
				Rule:        ruleDesiredImportDirection,
				PackagePath: packagePath,
				ImportPath:  importedPackage,
				Detail:      "Desired EntityID is a stable primitive and must not import another internal package",
			})
			continue
		}
		if isAllowedDesiredImport(importedPackage) {
			continue
		}
		findings = append(findings, GuardrailFinding{
			Rule:        ruleDesiredImportDirection,
			PackagePath: packagePath,
			ImportPath:  importedPackage,
			Detail:      "Desired may import only its own packages, internal/target scalar identity, and internal/supply/source stable locator contracts",
		})
	}
	return findings
}

func analyzeTopologyImports(packagePath string, imports []string) []GuardrailFinding {
	var findings []GuardrailFinding
	for _, importPath := range sortedStrings(imports) {
		importedPackage, internal := internalPath(importPath)
		if !internal {
			continue
		}
		if packagePath != "internal/topology" &&
			isAllowedTopologyImport(packagePath, importedPackage) {
			continue
		}
		detail := "Topology root must not import another repository package"
		if packagePath != "internal/topology" {
			detail = "Topology family lowerers may import only Topology, their exact Desired owner, and explicitly admitted stable primitives"
		}
		findings = append(findings, GuardrailFinding{
			Rule:        ruleTopologyImportDirection,
			PackagePath: packagePath,
			ImportPath:  importedPackage,
			Detail:      detail,
		})
	}
	return findings
}

func isAllowedDesiredImport(importPath string) bool {
	return isPackageOrChild(importPath, "internal/desired") ||
		importPath == "internal/credentialtext" ||
		importPath == "internal/target" ||
		importPath == "internal/supply/source"
}

func isAllowedTopologyImport(packagePath string, importPath string) bool {
	if importPath == "internal/topology" {
		return true
	}
	if packagePath == "internal/topology/mcp" {
		return importPath == "internal/desired/mcp" ||
			importPath == "internal/target"
	}
	if packagePath == "internal/topology/hook" {
		return importPath == "internal/desired/entity" ||
			importPath == "internal/desired/hook" ||
			importPath == "internal/desired/hookasset" ||
			importPath == "internal/target" ||
			importPath == "internal/topology/projection"
	}
	if packagePath == "internal/topology/resource" {
		return importPath == "internal/desired/entity"
	}
	if packagePath == "internal/topology/projection" {
		return importPath == "internal/desired/entity"
	}
	return packagePath == "internal/topology/extension" &&
		importPath == "internal/desired/extension"
}

func forbiddenRoleDependency(
	importing packagePlacement,
	imported packagePlacement,
) bool {
	if importing.role != roleSemanticKernel {
		return false
	}
	switch imported.role {
	case roleCodec,
		roleObservationAdapter,
		roleActiveAdapter,
		roleTransactionRecovery,
		roleWorkflowComposition,
		rolePresentation,
		roleRepositoryTool,
		roleTestSupport:
		return true
	default:
		return false
	}
}

func allowsExactKernelCapabilityDependency(
	importingPackage string,
	importedPackage string,
	importing packagePlacement,
	imported packagePlacement,
) bool {
	if importing.role != roleSemanticKernel || imported.role != roleActiveAdapter {
		return false
	}
	switch importingPackage {
	case "internal/effect/mutation/filesystem",
		"internal/effect/mutation/ownership":
		return importedPackage == "internal/effect/mutation/rootedpath"
	case "internal/effect/payload":
		return importedPackage == "internal/supply/artifact/access"
	case "internal/realization/lock":
		return importedPackage == "internal/supply/compat/skill/repair"
	case "internal/supply/compat/skill",
		"internal/supply/source/acquisition",
		"internal/supply/source/directfile":
		return importedPackage == "internal/supply/artifact/access"
	default:
		return false
	}
}

func forbiddenAffinityDependency(
	importing packagePlacement,
	imported packagePlacement,
) bool {
	if importing.role != roleSemanticKernel &&
		importing.role != roleLoweringRefinement {
		return false
	}
	if importing.affinity == affinityNone {
		return false
	}
	if imported.affinity == affinityNone {
		return true
	}
	switch importing.affinity {
	case affinityDesired:
		return imported.affinity != affinityDesired
	case affinityTopology:
		return imported.affinity != affinityDesired &&
			imported.affinity != affinityTopology
	case affinitySupply:
		return imported.affinity != affinitySupply
	case affinityRealization:
		return imported.affinity != affinityDesired &&
			imported.affinity != affinityTopology &&
			imported.affinity != affinitySupply &&
			imported.affinity != affinityRealization
	case affinityAssurance:
		return imported.affinity != affinityTopology &&
			imported.affinity != affinityRealization &&
			imported.affinity != affinityAssurance
	case affinityReconciliation:
		return imported.affinity != affinityTopology &&
			imported.affinity != affinityRealization &&
			imported.affinity != affinityAssurance &&
			imported.affinity != affinityReconciliation
	case affinityEffect:
		return imported.affinity != affinityTopology &&
			imported.affinity != affinityRealization &&
			imported.affinity != affinityAssurance &&
			imported.affinity != affinityReconciliation &&
			imported.affinity != affinityEffect
	default:
		return false
	}
}

func allowsExactCrossAffinityValue(
	importing semanticAffinity,
	importedPackage string,
	imported packagePlacement,
) bool {
	switch importedPackage {
	case "internal/credentialtext":
		return imported.role == roleStableValue
	case "internal/contractversion":
		return imported.role == roleStableValue
	case "internal/target":
		return imported.role == roleStableValue
	case "internal/supply/source":
		return importing == affinityDesired &&
			imported.role == roleSemanticKernel
	case "internal/output":
		return imported.role == roleStableValue &&
			(importing == affinityRealization ||
				importing == affinityAssurance ||
				importing == affinityReconciliation ||
				importing == affinityEffect)
	case "internal/output/ownership":
		return imported.role == roleStableValue &&
			(importing == affinityAssurance ||
				importing == affinityReconciliation ||
				importing == affinityEffect)
	case "internal/assurance/stateauthority":
		return imported.role == roleStableValue &&
			(importing == affinityAssurance ||
				importing == affinityReconciliation ||
				importing == affinityEffect)
	case "internal/assurance/pathauthority":
		return imported.role == roleStableValue &&
			(importing == affinityAssurance ||
				importing == affinityReconciliation ||
				importing == affinityEffect)
	case "internal/supply/artifact":
		return imported.role == roleSemanticKernel &&
			(importing == affinityAssurance ||
				importing == affinityReconciliation ||
				importing == affinityEffect)
	case "internal/desired/entity":
		return importing == affinityEffect &&
			imported.role == roleSemanticKernel
	default:
		return false
	}
}

func placementDependencyRule(placement packagePlacement) string {
	switch placement.affinity {
	case affinityDesired:
		return ruleDesiredImportDirection
	case affinityTopology:
		return ruleTopologyImportDirection
	case affinitySupply:
		return ruleSupplyImportDirection
	case affinityRealization:
		return ruleRealizationImportDirection
	case affinityAssurance:
		return ruleAssuranceImportDirection
	case affinityReconciliation:
		return ruleReconciliationImportDirection
	case affinityEffect:
		return ruleEffectImportDirection
	default:
		return rulePackageRoleImportDirection
	}
}

func placementDependencyDetail(placement packagePlacement) string {
	if placement.role == roleSemanticKernel {
		return placement.affinity.String() +
			" semantic kernels may import only inward semantic affinities, exact stable-value admissions, and owner-local non-boundary mechanisms"
	}
	return placement.affinity.String() +
		" lowering/refinement may import only inward semantic affinities and exact stable-value admissions"
}
