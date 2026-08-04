package archguard

import (
	"go/build"
	"slices"
	"strings"
)

func internalPath(importPath string) (string, bool) {
	if importPath == "internal" {
		return importPath, true
	}
	if strings.HasPrefix(importPath, "internal/") {
		return importPath, true
	}
	index := strings.LastIndex(importPath, "/internal/")
	if index == -1 {
		return "", false
	}
	return importPath[index+1:], true
}

func architecturePath(importPath string) (string, bool) {
	for _, root := range []string{"internal", "test"} {
		if importPath == root || strings.HasPrefix(importPath, root+"/") {
			return importPath, true
		}
		marker := "/" + root + "/"
		if index := strings.LastIndex(importPath, marker); index >= 0 {
			return importPath[index+1:], true
		}
	}
	return "", false
}

func matchesAnyInternalImport(importPath string, paths []string) bool {
	for _, internalImport := range paths {
		if matchesInternalImport(importPath, internalImport) {
			return true
		}
	}
	return false
}

func matchesInternalImport(importPath string, forbidden string) bool {
	return importPath == forbidden || strings.HasPrefix(importPath, forbidden+"/")
}

func isPackageOrChild(packagePath string, prefix string) bool {
	return packagePath == prefix || strings.HasPrefix(packagePath, prefix+"/")
}

func isStdlibImportPath(importPath string) bool {
	pkg, err := build.Default.Import(importPath, "", build.FindOnly)
	return err == nil && pkg.Goroot
}

func isForbiddenCLIImport(importPath string) bool {
	if matchesInternalImport(importPath, "internal/workflow") ||
		importPath == "internal/cli/present" ||
		importPath == "internal/cli/present/progress" {
		return false
	}
	if importPath == "internal/target" || matchesInternalImport(importPath, "internal/target/selection") {
		return false
	}
	if importPath == "internal/platformsupport" {
		return false
	}
	if importPath == "internal/buildidentity" {
		return false
	}
	return strings.HasPrefix(importPath, "internal/")
}

func isObserveAdapterPackage(packagePath string) bool {
	return packagePath == "internal/assurance/observe/live" ||
		packagePath == "internal/assurance/observe/lock"
}

func isObserveRootPackage(packagePath string) bool {
	return packagePath == "internal/assurance/observe"
}

func isForbiddenStatefileBehaviorImport(importPath string) bool {
	if importPath == "internal/assurance/observe/relation" {
		return false
	}
	for _, forbidden := range []string{
		"internal/reconcile",
		"internal/assurance/observe",
		"internal/effect/execute",
		"internal/effect/journal",
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

func isPresentPackage(packagePath string) bool {
	return isPackageOrChild(packagePath, "internal/cli/present")
}

func isCLIPresentationChildPackage(packagePath string) bool {
	return strings.HasPrefix(packagePath, "internal/cli/present/")
}

func isAllowedCLIPresentationChildPackage(packagePath string) bool {
	return packagePath == "internal/cli/present/progress"
}

func isAllowedPresentWorkflowResultImport(packagePath string, importPath string) bool {
	if packagePath == "internal/cli/present/progress" {
		return importPath == "internal/workflow/lock"
	}
	if packagePath != "internal/cli/present" {
		return false
	}
	switch importPath {
	case "internal/workflow/apply",
		"internal/workflow/authoring",
		"internal/workflow/init",
		"internal/workflow/list",
		"internal/workflow/probe",
		"internal/workflow/refresh",
		"internal/workflow/status":
		return true
	}
	return false
}

func isForbiddenWorkflowReverseImportPackage(packagePath string) bool {
	for _, allowed := range []string{
		"internal/app",
		"internal/cli",
		"internal/realization/lock/build",
		"internal/realization/lockfile",
		"internal/paths",
		"internal/workflow",
		"internal/assurance/statefile",
	} {
		if isPackageOrChild(packagePath, allowed) {
			return false
		}
	}
	return strings.HasPrefix(packagePath, "internal/")
}

func isResourcePackage(packagePath string) bool {
	return isPackageOrChild(packagePath, "internal/resource") &&
		!isDeclaredResourcePackage(packagePath)
}

func isDeclaredResourcePackage(packagePath string) bool {
	return isPackageOrChild(packagePath, "internal/resource/declared")
}

func isDeclarationPackage(packagePath string) bool {
	return isPackageOrChild(packagePath, "internal/declaration")
}

func isRetiredLifecyclePackage(packagePath string) bool {
	return isPackageOrChild(packagePath, "internal/lifecycle")
}

func isTargetSelectionPackage(packagePath string) bool {
	return isPackageOrChild(packagePath, "internal/target/selection")
}

func isOutputPackage(packagePath string) bool {
	return isPackageOrChild(packagePath, "internal/output")
}

func isPayloadPackage(packagePath string) bool {
	return isPackageOrChild(packagePath, "internal/effect/payload")
}

func isReconciliationPackage(packagePath string) bool {
	return isPackageOrChild(packagePath, "internal/reconcile")
}

func isJournalOrExecutePackage(packagePath string) bool {
	return isPackageOrChild(packagePath, "internal/effect/journal") ||
		isPackageOrChild(packagePath, "internal/effect/execute")
}

func isWorkflowPackage(packagePath string) bool {
	return isPackageOrChild(packagePath, "internal/workflow")
}

func isForbiddenNestedWorkflowImport(packagePath string, importPath string) bool {
	if !isWorkflowPackage(packagePath) || !matchesInternalImport(importPath, "internal/workflow") {
		return false
	}
	if importPath == "internal/workflow/lock/generate" {
		return packagePath != "internal/workflow/lock" && packagePath != "internal/workflow/authoring"
	}
	if importPath == "internal/workflow/readiness" {
		return packagePath != "internal/workflow/apply" &&
			packagePath != "internal/workflow/list" &&
			packagePath != "internal/workflow/status"
	}
	return true
}

func isForbiddenGenericRolePackageShape(packagePath string) bool {
	if isPackageOrChild(packagePath, "internal/workflow/app") ||
		hasForbiddenAppServiceBase(packagePath) {
		return true
	}
	return hasForbiddenGenericRoleBase(packagePath)
}

func isForbiddenGenericRoleFileShape(filePath string) bool {
	return hasForbiddenGenericRoleBase(filePath) ||
		hasForbiddenAppServiceBase(filePath)
}

func isForbiddenStatePackageShape(packagePath string) bool {
	return isPackageOrChild(packagePath, "internal/state")
}

func retiredExecutePackageDetail(packagePath string) (string, bool) {
	for _, retired := range []struct {
		root   string
		detail string
	}{
		{
			root:   "internal/effect/execute/stateupdate",
			detail: "durable models own canonical state transitions; execute owns only reconciliation mapping and effectful commit sequencing",
		},
		{
			root:   "internal/effect/execute/commandexec",
			detail: "generic subprocess command execution belongs to internal/subprocess, outside the Effect aggregate",
		},
		{
			root:   "internal/effect/execute/processcwd",
			detail: "descriptor-backed subprocess working-directory authority belongs to internal/subprocess, outside the Effect aggregate",
		},
		{
			root:   "internal/effect/execute/mcpprobe",
			detail: "MCP runtime probe effects belong to the Assurance-owned runtime probe boundary at internal/assurance/runtimeprobe/mcp",
		},
		{
			root:   "internal/effect/execute/delegateexec",
			detail: "delegated-action execution belongs to the precisely named internal/effect/execute/delegate adapter",
		},
	} {
		if isPackageOrChild(packagePath, retired.root) {
			return retired.detail, true
		}
	}
	return "", false
}

func isForbiddenResourceOperationPackageShape(packagePath string) bool {
	if matchesResourceFamilySubpath(packagePath, "compat") ||
		matchesResourceFamilySubpath(packagePath, "repair") ||
		matchesResourceFamilySubpath(packagePath, "render") ||
		matchesResourceFamilySubpath(packagePath, "import") ||
		matchesResourceFamilySubpath(packagePath, "doctor") ||
		matchesResourceFamilySubpath(packagePath, "apply") {
		return true
	}

	if isForbiddenResourceFamilyPhasePath(packagePath) {
		return true
	}

	return hasForbiddenOperationCellBase(packagePath)
}

func isForbiddenResourceOperationFileShape(filePath string) bool {
	return hasForbiddenOperationCellBase(filePath) ||
		hasForbiddenCLIResourceFamilyFileBase(filePath)
}

func isFutureMCPPluginMonolith(packagePath string) bool {
	for _, root := range []string{
		"internal/mcp",
		"internal/plugin",
		"internal/package",
		"internal/packages",
		"internal/extension",
		"internal/extensions",
	} {
		if isPackageOrChild(packagePath, root) {
			return true
		}
	}
	return false
}

func isFutureFamilyResourceBucket(packagePath string) bool {
	parts := strings.Split(packagePath, "/")
	return len(parts) >= 3 &&
		parts[0] == "internal" &&
		parts[1] == "resource" &&
		isFutureLifecycleFamilySegment(parts[2])
}

func isFutureFamilyWorkflowCell(packagePath string) bool {
	parts := strings.Split(packagePath, "/")
	return len(parts) >= 3 &&
		parts[0] == "internal" &&
		parts[1] == "workflow" &&
		isFutureLifecycleFamilySegment(parts[2])
}

func isFutureFamilyOperationCellPath(packagePath string) bool {
	parts := strings.Split(packagePath, "/")
	if len(parts) < 3 || parts[0] != "internal" {
		return false
	}
	return hasFutureFamilyOperationBase(parts[len(parts)-1])
}

func isFutureLifecycleFamilySegment(pathSegment string) bool {
	switch pathSegment {
	case "mcp", "plugin", "package", "packages", "extension", "extensions":
		return true
	default:
		return false
	}
}

func hasFutureFamilyOperationBase(base string) bool {
	operations := []string{"add", "apply", "doctor", "import", "install", "lock", "remove", "render", "update"}
	families := []string{"mcp", "plugin", "package", "packages", "extension", "extensions"}
	for _, family := range families {
		for _, operation := range operations {
			if strings.HasPrefix(base, family+"_"+operation) ||
				strings.HasPrefix(base, operation+"_"+family) ||
				strings.HasPrefix(base, family+operation) ||
				strings.HasPrefix(base, operation+family) {
				return true
			}
		}
	}
	return false
}

func isSourcePackage(packagePath string) bool {
	return isPackageOrChild(packagePath, "internal/supply/source")
}

func isSourceCachePackage(packagePath string) bool {
	return isPackageOrChild(packagePath, "internal/supply/source/cache")
}

func isMutationPackage(packagePath string) bool {
	return isPackageOrChild(packagePath, "internal/effect/mutation")
}

func isProjectRootAuthorityPackage(packagePath string) bool {
	return isPackageOrChild(packagePath, "internal/effect/mutation/rootedpath")
}

func isForbiddenProgressBasisPackageShape(packagePath string) bool {
	for _, forbidden := range []string{
		"internal/workflow/progress",
		"internal/task",
		"internal/tasks",
		"internal/operation",
		"internal/operations",
		"internal/progress",
		"internal/resource/lockable",
		"internal/lockable",
	} {
		if isPackageOrChild(packagePath, forbidden) {
			return true
		}
	}
	return false
}

func isForbiddenResourceFamilyPhasePath(packagePath string) bool {
	if !containsCurrentResourceFamilySegment(packagePath) {
		return false
	}
	for _, allowedRoot := range []string{
		"internal/declaration/codec",
		"internal/desired",
		"internal/resource",
		"internal/supply/compat",
		"internal/output/project",
		"internal/effect/payload",
		"internal/adopt",
		"internal/cli/present",
		"internal/realization/aggregate/hook",
		"internal/realization/aggregate/codec",
		"internal/topology",
	} {
		if isPackageOrChild(packagePath, allowedRoot) {
			return false
		}
	}
	return true
}

func containsCurrentResourceFamilySegment(packagePath string) bool {
	parts := strings.Split(packagePath, "/")
	if len(parts) < 3 || parts[0] != "internal" {
		return false
	}
	return slices.ContainsFunc(parts[1:], isCurrentResourceFamilySegment)
}

func isCurrentResourceFamilySegment(pathSegment string) bool {
	switch pathSegment {
	case "skill", "skillgroup", "hook", "instructions":
		return true
	default:
		return false
	}
}

func hasForbiddenOperationCellBase(packagePath string) bool {
	parts := strings.Split(packagePath, "/")
	if len(parts) < 3 || parts[0] != "internal" {
		return false
	}
	base := parts[len(parts)-1]
	return strings.HasPrefix(base, "skill_add") ||
		strings.HasPrefix(base, "skill_lock") ||
		strings.HasPrefix(base, "hook_render") ||
		strings.HasPrefix(base, "instruction_import")
}

func hasForbiddenGenericRoleBase(packagePath string) bool {
	parts := strings.Split(packagePath, "/")
	if len(parts) < 3 || parts[0] != "internal" {
		return false
	}
	base := parts[len(parts)-1]
	return strings.HasPrefix(base, "manager") ||
		strings.HasPrefix(base, "handler") ||
		strings.HasPrefix(base, "processor")
}

func hasForbiddenAppServiceBase(packagePath string) bool {
	parts := strings.Split(packagePath, "/")
	if len(parts) < 3 || parts[0] != "internal" || parts[1] != "app" {
		return false
	}
	base := parts[len(parts)-1]
	return strings.HasPrefix(base, "service")
}

func hasForbiddenCLIResourceFamilyFileBase(filePath string) bool {
	parts := strings.Split(filePath, "/")
	if len(parts) != 3 || parts[0] != "internal" || parts[1] != "cli" {
		return false
	}
	base := parts[2]
	return strings.HasPrefix(base, "skill") ||
		strings.HasPrefix(base, "hook") ||
		strings.HasPrefix(base, "instruction")
}

func matchesResourceFamilySubpath(packagePath string, subpath string) bool {
	parts := strings.Split(packagePath, "/")
	if len(parts) < 4 || parts[0] != "internal" || parts[1] != "resource" {
		return false
	}
	return parts[3] == subpath
}
