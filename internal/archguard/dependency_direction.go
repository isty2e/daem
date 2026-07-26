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
	ruleSemanticBlockOwnership        = "semantic-block-ownership"
)

type semanticDependencyBlock uint8

const (
	dependencyUnknown semanticDependencyBlock = iota
	dependencyBoundary
	dependencyDesired
	dependencyTopology
	dependencySupply
	dependencyRealization
	dependencyAssurance
	dependencyReconciliation
	dependencyEffect
)

// analyzeArchitectureDependencyDirections guards semantic kernel imports.
// Every package is assigned explicitly to one kernel or to the Boundary class;
// Boundary adapters and shared cross-block values remain subject to narrower
// package-specific guards.
func analyzeArchitectureDependencyDirections(records []PackageRecord) []GuardrailFinding {
	var violations []GuardrailFinding
	for _, record := range sortedRecords(records) {
		packagePath, ok := internalPath(record.ImportPath)
		if !ok {
			continue
		}
		blocks := semanticDependencyBlocksForPackage(packagePath)
		if len(blocks) != 1 {
			detail := "package is not assigned to a semantic dependency block"
			if len(blocks) > 1 {
				detail = "package is assigned to more than one semantic dependency block"
			}
			violations = append(violations, GuardrailFinding{
				Rule:        ruleSemanticBlockOwnership,
				PackagePath: packagePath,
				Path:        packagePath,
				Detail:      detail,
			})
			continue
		}
		if isPackageOrChild(packagePath, "internal/desired") {
			violations = append(violations, analyzeDesiredImports(packagePath, record.Imports)...)
			continue
		}
		if isPackageOrChild(packagePath, "internal/topology") {
			violations = append(violations, analyzeTopologyImports(packagePath, record.Imports)...)
			continue
		}
		importingBlock := semanticDependencyBlockForPackage(packagePath)
		for _, importPath := range sortedStrings(record.Imports) {
			importedPackage, internal := internalPath(importPath)
			if !internal {
				continue
			}
			if isPackageOrChild(importedPackage, "internal/realization/aggregate/codec") &&
				!allowsConcreteAggregateCodec(packagePath) {
				violations = append(violations, GuardrailFinding{
					Rule:        ruleAggregateCodecBoundaryImport,
					PackagePath: packagePath,
					ImportPath:  importedPackage,
					Detail:      "semantic kernels consume canonical aggregate codec ports; only boundary packages may select concrete codec implementations",
				})
				continue
			}
			if importingBlock == dependencyBoundary {
				continue
			}
			importedBlock := semanticDependencyBlockForPackage(importedPackage)
			if !forbiddenSemanticDependency(importingBlock, importedBlock, importedPackage) {
				continue
			}
			violations = append(violations, GuardrailFinding{
				Rule:        semanticDependencyRule(importingBlock),
				PackagePath: packagePath,
				ImportPath:  importedPackage,
				Detail:      semanticDependencyDetail(importingBlock),
			})
		}
	}
	return violations
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
		"internal/workflow/lock",
		"internal/workflow/probe",
		"internal/workflow/recover",
		"internal/workflow/status",
	} {
		if isPackageOrChild(packagePath, root) {
			return true
		}
	}
	return false
}

func analyzeDesiredImports(packagePath string, imports []string) []GuardrailFinding {
	var violations []GuardrailFinding
	for _, importPath := range sortedStrings(imports) {
		importedPackage, ok := internalPath(importPath)
		if !ok {
			continue
		}
		if packagePath == "internal/desired/entity" {
			violations = append(violations, GuardrailFinding{
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
		violations = append(violations, GuardrailFinding{
			Rule:        ruleDesiredImportDirection,
			PackagePath: packagePath,
			ImportPath:  importedPackage,
			Detail:      "Desired may import only its own packages, internal/target scalar identity, and internal/supply/source stable locator contracts",
		})
	}
	return violations
}

func analyzeTopologyImports(packagePath string, imports []string) []GuardrailFinding {
	var violations []GuardrailFinding
	for _, importPath := range sortedStrings(imports) {
		importedPackage, ok := internalPath(importPath)
		if !ok {
			continue
		}
		if packagePath != "internal/topology" && isAllowedTopologyImport(packagePath, importedPackage) {
			continue
		}
		detail := "Topology root must not import another repository package"
		if packagePath != "internal/topology" {
			detail = "Topology family lowerers may import only Topology, their exact Desired owner, and explicitly admitted stable primitives"
		}
		violations = append(violations, GuardrailFinding{
			Rule:        ruleTopologyImportDirection,
			PackagePath: packagePath,
			ImportPath:  importedPackage,
			Detail:      detail,
		})
	}
	return violations
}

func isAllowedDesiredImport(importPath string) bool {
	return isPackageOrChild(importPath, "internal/desired") ||
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

func semanticDependencyBlockForPackage(packagePath string) semanticDependencyBlock {
	blocks := semanticDependencyBlocksForPackage(packagePath)
	if len(blocks) != 1 {
		return dependencyUnknown
	}
	return blocks[0]
}

func semanticDependencyBlocksForPackage(packagePath string) []semanticDependencyBlock {
	blocks := make([]semanticDependencyBlock, 0, 1)
	if isPackageOrChild(packagePath, "internal/desired") &&
		!isPackageOrChild(packagePath, "internal/desired/testfixture") {
		blocks = append(blocks, dependencyDesired)
	}
	if isPackageOrChild(packagePath, "internal/topology") {
		blocks = append(blocks, dependencyTopology)
	}
	if isSupplyKernelPackage(packagePath) {
		blocks = append(blocks, dependencySupply)
	}
	if isRealizationKernelPackage(packagePath) {
		blocks = append(blocks, dependencyRealization)
	}
	if isAssuranceKernelPackage(packagePath) {
		blocks = append(blocks, dependencyAssurance)
	}
	if isPackageOrChild(packagePath, "internal/reconcile") {
		blocks = append(blocks, dependencyReconciliation)
	}
	if isEffectKernelPackage(packagePath) {
		blocks = append(blocks, dependencyEffect)
	}
	if isBoundaryPackage(packagePath) {
		blocks = append(blocks, dependencyBoundary)
	}
	return blocks
}

func isSupplyKernelPackage(packagePath string) bool {
	if isPackageOrChild(packagePath, "internal/supply/artifact") ||
		isPackageOrChild(packagePath, "internal/supply/compat/skill") {
		return true
	}
	return packagePath == "internal/supply/source" ||
		isPackageOrChild(packagePath, "internal/supply/source/acquisition") ||
		isPackageOrChild(packagePath, "internal/supply/source/archive") ||
		isPackageOrChild(packagePath, "internal/supply/source/directfile")
}

func isRealizationKernelPackage(packagePath string) bool {
	if !isPackageOrChild(packagePath, "internal/realization") {
		return false
	}
	for _, boundaryRoot := range []string{
		"internal/realization/aggregate/codec",
		"internal/realization/lock/snapshottest",
		"internal/realization/lockfile",
	} {
		if isPackageOrChild(packagePath, boundaryRoot) {
			return false
		}
	}
	return true
}

func isAssuranceKernelPackage(packagePath string) bool {
	if !isPackageOrChild(packagePath, "internal/assurance") {
		return false
	}
	for _, boundaryRoot := range []string{
		"internal/assurance/runtimeprobe/mcp",
		"internal/assurance/observe/antigravityplugin",
		"internal/assurance/observe/claudeplugin",
		"internal/assurance/observe/codexplugin",
		"internal/assurance/observe/live",
		"internal/assurance/observe/lock",
		"internal/assurance/observe/ownership",
		"internal/assurance/observe/pipackage",
		"internal/assurance/observe/relation/host",
		"internal/assurance/statefile",
	} {
		if isPackageOrChild(packagePath, boundaryRoot) {
			return false
		}
	}
	return true
}

func isEffectKernelPackage(packagePath string) bool {
	if isPackageOrChild(packagePath, "internal/effect/mutation") ||
		isPackageOrChild(packagePath, "internal/effect/journal") {
		return true
	}
	return packagePath == "internal/effect/execute" || packagePath == "internal/effect/payload"
}

func isBoundaryPackage(packagePath string) bool {
	if packagePath == "internal/encoding/jsonstrict" {
		return true
	}
	for _, root := range []string{
		"internal/adopt",
		"internal/archguard",
		"internal/buildidentity",
		"internal/cli",
		"internal/declaration",
		"internal/desired/testfixture",
		"internal/diagnose",
		"internal/assurance/runtimeprobe/mcp",
		"internal/effect/execute/configrelation",
		"internal/effect/execute/delegate",
		"internal/effect/execute/hostroute",
		"internal/findings",
		"internal/workflow/lock/generate",
		"internal/realization/lock/snapshottest",
		"internal/realization/lockfile",
		"internal/assurance/observe/antigravityplugin",
		"internal/assurance/observe/claudeplugin",
		"internal/assurance/observe/codexplugin",
		"internal/assurance/observe/live",
		"internal/assurance/observe/lock",
		"internal/assurance/observe/ownership",
		"internal/assurance/observe/pipackage",
		"internal/assurance/observe/relation/host",
		"internal/output",
		"internal/paths",
		"internal/effect/payload/build",
		"internal/platformsupport",
		"internal/workflow/readiness",
		"internal/releaseartifact",
		"internal/supply/source/backend",
		"internal/supply/source/cache",
		"internal/supply/source/resolution",
		"internal/supply/source/sourcetest",
		"internal/assurance/statefile",
		"internal/subprocess",
		"internal/realization/aggregate/codec",
		"internal/workflow",
	} {
		if isPackageOrChild(packagePath, root) {
			return true
		}
	}
	return packagePath == "internal/effect/storage/carrierclaim" ||
		packagePath == "internal/effect/storage/commit" ||
		packagePath == "internal/target" ||
		packagePath == "internal/target/availability" ||
		isPackageOrChild(packagePath, "internal/target/selection")
}

func forbiddenSemanticDependency(
	importing semanticDependencyBlock,
	imported semanticDependencyBlock,
	importedPackage string,
) bool {
	if allowsStableCrossBlockValue(importing, importedPackage) {
		return false
	}
	switch importing {
	case dependencySupply:
		return imported == dependencyBoundary ||
			imported == dependencyDesired ||
			imported == dependencyTopology ||
			imported == dependencyRealization ||
			imported == dependencyAssurance ||
			imported == dependencyReconciliation ||
			imported == dependencyEffect
	case dependencyRealization:
		return imported == dependencyBoundary ||
			imported == dependencyAssurance ||
			imported == dependencyReconciliation ||
			imported == dependencyEffect
	case dependencyAssurance:
		return imported == dependencyBoundary ||
			imported == dependencyDesired ||
			imported == dependencySupply ||
			imported == dependencyReconciliation ||
			imported == dependencyEffect
	case dependencyReconciliation:
		return imported == dependencyBoundary ||
			imported == dependencyDesired ||
			imported == dependencySupply ||
			imported == dependencyEffect
	case dependencyEffect:
		return imported == dependencyBoundary ||
			imported == dependencySupply ||
			(imported == dependencyDesired &&
				importedPackage != "internal/desired/entity")
	default:
		return false
	}
}

func allowsStableCrossBlockValue(importing semanticDependencyBlock, importedPackage string) bool {
	switch importedPackage {
	case "internal/target":
		return true
	case "internal/output":
		return importing == dependencyRealization ||
			importing == dependencyAssurance ||
			importing == dependencyReconciliation ||
			importing == dependencyEffect
	case "internal/output/ownership", "internal/supply/artifact":
		return importing == dependencyAssurance ||
			importing == dependencyReconciliation ||
			importing == dependencyEffect
	case "internal/supply/artifact/access":
		return importing == dependencyEffect
	default:
		return false
	}
}

func semanticDependencyRule(block semanticDependencyBlock) string {
	switch block {
	case dependencySupply:
		return ruleSupplyImportDirection
	case dependencyRealization:
		return ruleRealizationImportDirection
	case dependencyAssurance:
		return ruleAssuranceImportDirection
	case dependencyReconciliation:
		return ruleReconciliationImportDirection
	case dependencyEffect:
		return ruleEffectImportDirection
	default:
		return ""
	}
}

func semanticDependencyDetail(block semanticDependencyBlock) string {
	switch block {
	case dependencySupply:
		return "Supply kernels own source, artifact, and recipe facts; Desired, Topology, Realization, Assurance, Reconciliation, and Effect semantics stay outside"
	case dependencyRealization:
		return "Realization kernels refine Desired, Topology, and Supply facts; Assurance, Reconciliation, and Effect semantics stay outside"
	case dependencyAssurance:
		return "Assurance kernels normalize current evidence from stable Topology and Realization facts; Desired, Supply, Reconciliation, and Effect behavior stay outside"
	case dependencyReconciliation:
		return "Reconciliation kernels remain pure over stable Realization, Assurance, and identity facts; Desired, Supply, and Effect behavior stay outside"
	case dependencyEffect:
		return "Effect kernels consume authorized decisions and locked identities; Desired family policy stays outside"
	default:
		return ""
	}
}
