package archguard

import (
	"os"
	"path"
	"strings"
)

const (
	ruleCompilerHostSurfaceForbiddenImport    = "compiler-hostsurface-forbidden-import"
	ruleCompilerOperationPlanForbiddenImport  = "compiler-operationplan-forbidden-import"
	ruleCompilerOrthogonalityImport           = "compiler-orthogonality-import"
	ruleCompilerOwnerCatalogImport            = "compiler-owner-catalog-import"
	ruleCompilerStateBarrierHostSurfaceImport = "compiler-state-barrier-hostsurface-import"
	ruleCompilerWorkflowImport                = "compiler-workflow-import"
	ruleCompilerOSSpecialization              = "compiler-os-specialization"
	ruleFacetOwnerStateBarrierImport          = "facet-owner-state-barrier-import"
	ruleObserveStateBarrierImport             = "observe-state-barrier-import"
	ruleFilesetHostSurfaceImport              = "fileset-hostsurface-import"
)

func analyzeCompilerShadow(records []PackageRecord) []GuardrailFinding {
	var findings []GuardrailFinding
	for _, record := range sortedRecords(records) {
		packagePath, ok := internalPath(record.ImportPath)
		if !ok {
			continue
		}
		findings = append(findings, analyzeCompilerShadowPackage(packagePath, record)...)
	}
	return sortedFindings(dedupFindings(findings))
}

func analyzeCompilerShadowPackage(packagePath string, record PackageRecord) []GuardrailFinding {
	var findings []GuardrailFinding
	if isCompilerPackage(packagePath) {
		findings = append(findings, analyzeCompilerOSSpecialization(packagePath, record)...)
	}
	for _, importPath := range sortedStrings(record.Imports) {
		imported, isInternal := internalPath(importPath)
		if !isInternal {
			continue
		}
		findings = append(findings, compilerShadowImportFinding(packagePath, imported)...)
	}
	return findings
}

func compilerShadowImportFinding(packagePath string, imported string) []GuardrailFinding {
	switch {
	case isHostSurfaceCompilerPackage(packagePath):
		return hostSurfaceCompilerImportFindings(packagePath, imported)
	case isOperationPlanCompilerPackage(packagePath):
		return operationPlanCompilerImportFindings(packagePath, imported)
	case isStateBarrierPackage(packagePath):
		if isHostSurfaceCompilerPackage(imported) {
			return []GuardrailFinding{{
				Rule:        ruleCompilerStateBarrierHostSurfaceImport,
				PackagePath: packagePath,
				ImportPath:  imported,
				Detail:      "State Barrier may consume operationplan demand but must not import Host-Surface compiler packages",
			}}
		}
	case isTopologyPackage(packagePath) || isRealizationPackage(packagePath):
		if isHostSurfaceCompilerPackage(imported) {
			return []GuardrailFinding{{
				Rule:        ruleCompilerOwnerCatalogImport,
				PackagePath: packagePath,
				ImportPath:  imported,
				Detail:      "topology and realization retain owner-local facts; they must not import the Host-Surface compiler",
			}}
		}
		if isStateBarrierPackage(imported) {
			return []GuardrailFinding{{
				Rule:        ruleFacetOwnerStateBarrierImport,
				PackagePath: packagePath,
				ImportPath:  imported,
				Detail:      "a new realization or topology package must not pull State Barrier authority",
			}}
		}
	case isAssuranceObservePackage(packagePath):
		if isStateBarrierPackage(imported) {
			return []GuardrailFinding{{
				Rule:        ruleObserveStateBarrierImport,
				PackagePath: packagePath,
				ImportPath:  imported,
				Detail:      "a new observation purpose may join compiled catalog views, not State Barrier authority",
			}}
		}
	case isFileSetPackage(packagePath):
		if isHostSurfaceCompilerPackage(imported) {
			return []GuardrailFinding{{
				Rule:        ruleFilesetHostSurfaceImport,
				PackagePath: packagePath,
				ImportPath:  imported,
				Detail:      "file-set mechanics stay below State Barrier policy and must not import Host-Surface compiler packages",
			}}
		}
	}
	return nil
}

func hostSurfaceCompilerImportFindings(packagePath string, imported string) []GuardrailFinding {
	if isHostSurfaceCompilerPackage(imported) {
		return nil
	}
	if isOperationPlanCompilerPackage(imported) {
		return []GuardrailFinding{{
			Rule:        ruleCompilerOrthogonalityImport,
			PackagePath: packagePath,
			ImportPath:  imported,
			Detail:      "Host-Surface and Operation-Safety compilers are orthogonal and must not import each other",
		}}
	}
	if isWorkflowPackage(imported) {
		return []GuardrailFinding{{
			Rule:        ruleCompilerWorkflowImport,
			PackagePath: packagePath,
			ImportPath:  imported,
			Detail:      "workflows may import compiled views; compilers must not import workflows",
		}}
	}
	if hostSurfaceForbiddenImport(imported) {
		return []GuardrailFinding{{
			Rule:        ruleCompilerHostSurfaceForbiddenImport,
			PackagePath: packagePath,
			ImportPath:  imported,
			Detail:      "Host-Surface compilation is I/O-free and may import owner-local facet catalogs only",
		}}
	}
	return nil
}

func operationPlanCompilerImportFindings(packagePath string, imported string) []GuardrailFinding {
	if isOperationPlanCompilerPackage(imported) {
		return nil
	}
	if isHostSurfaceCompilerPackage(imported) {
		return []GuardrailFinding{{
			Rule:        ruleCompilerOrthogonalityImport,
			PackagePath: packagePath,
			ImportPath:  imported,
			Detail:      "Host-Surface and Operation-Safety compilers are orthogonal and must not import each other",
		}}
	}
	if isWorkflowPackage(imported) {
		return []GuardrailFinding{{
			Rule:        ruleCompilerWorkflowImport,
			PackagePath: packagePath,
			ImportPath:  imported,
			Detail:      "workflows may import compiled plans; compilers must not import workflows",
		}}
	}
	if operationPlanForbiddenImport(imported) {
		return []GuardrailFinding{{
			Rule:        ruleCompilerOperationPlanForbiddenImport,
			PackagePath: packagePath,
			ImportPath:  imported,
			Detail:      "Operation-Safety compilation is I/O-free; effect/mutation is the only admitted effect primitive",
		}}
	}
	return nil
}

func hostSurfaceForbiddenImport(imported string) bool {
	for _, prefix := range []string{
		"internal/workflow",
		"internal/effect",
		"internal/adopt",
		"internal/cli",
		"internal/recoverygate",
		"internal/subprocess",
		"internal/paths",
		"internal/assurance/observe",
	} {
		if matchesInternalImport(imported, prefix) {
			return true
		}
	}
	return false
}

func operationPlanForbiddenImport(imported string) bool {
	if isPackageOrChild(imported, "internal/effect/mutation") {
		return false
	}
	for _, prefix := range []string{
		"internal/workflow",
		"internal/effect",
		"internal/adopt",
		"internal/cli",
		"internal/recoverygate",
		"internal/subprocess",
		"internal/paths",
		"internal/assurance/observe",
		"internal/hostsurface",
	} {
		if matchesInternalImport(imported, prefix) {
			return true
		}
	}
	return false
}

func analyzeCompilerOSSpecialization(packagePath string, record PackageRecord) []GuardrailFinding {
	var findings []GuardrailFinding
	if len(record.CgoFiles) != 0 {
		findings = append(findings, GuardrailFinding{
			Rule:        ruleCompilerOSSpecialization,
			PackagePath: packagePath,
			Detail:      "compiler packages must not use cgo; OS specialization stays in physical adapters",
		})
	}
	for _, fileName := range compilerProductionGoFiles(record) {
		if !isGOOSGOARCHSpecializedGoFile(fileName) {
			continue
		}
		findings = append(findings, GuardrailFinding{
			Rule:        ruleCompilerOSSpecialization,
			PackagePath: packagePath,
			Path:        path.Join(packagePath, fileName),
			Detail:      "compiler packages must not contain GOOS/GOARCH production files; OS specialization stays in physical adapters",
		})
	}
	return findings
}

func compilerProductionGoFiles(record PackageRecord) []string {
	seen := make(map[string]struct{})
	var names []string
	add := func(name string) {
		if name == "" || strings.HasSuffix(name, "_test.go") {
			return
		}
		if _, exists := seen[name]; exists {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	for _, name := range record.GoFiles {
		add(name)
	}
	if record.Dir != "" {
		entries, err := os.ReadDir(record.Dir)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				add(entry.Name())
			}
		}
	}
	return sortedStrings(names)
}

func isGOOSGOARCHSpecializedGoFile(name string) bool {
	if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
		return false
	}
	base := strings.TrimSuffix(name, ".go")
	parts := strings.Split(base, "_")
	if len(parts) < 2 {
		return false
	}
	last := parts[len(parts)-1]
	if isKnownGOOS(last) || isKnownGOARCH(last) {
		return true
	}
	if len(parts) >= 3 && isKnownGOOS(parts[len(parts)-2]) && isKnownGOARCH(last) {
		return true
	}
	return false
}

func isKnownGOOS(value string) bool {
	switch value {
	case "unix", "linux", "darwin", "windows", "freebsd", "openbsd", "netbsd",
		"dragonfly", "solaris", "illumos", "aix", "android", "ios", "js",
		"wasip1", "plan9", "hurd", "zos":
		return true
	default:
		return false
	}
}

func isKnownGOARCH(value string) bool {
	switch value {
	case "386", "amd64", "arm", "arm64", "loong64", "mips", "mipsle", "mips64",
		"mips64le", "ppc64", "ppc64le", "riscv64", "s390x", "wasm", "sparc64":
		return true
	default:
		return false
	}
}

func isCompilerPackage(packagePath string) bool {
	return isHostSurfaceCompilerPackage(packagePath) || isOperationPlanCompilerPackage(packagePath)
}

func isHostSurfaceCompilerPackage(packagePath string) bool {
	return isPackageOrChild(packagePath, "internal/hostsurface")
}

func isOperationPlanCompilerPackage(packagePath string) bool {
	return isPackageOrChild(packagePath, "internal/operationplan")
}

func isStateBarrierPackage(packagePath string) bool {
	return isPackageOrChild(packagePath, "internal/recoverygate")
}

func isTopologyPackage(packagePath string) bool {
	return isPackageOrChild(packagePath, "internal/topology")
}

func isRealizationPackage(packagePath string) bool {
	return isPackageOrChild(packagePath, "internal/realization")
}

func isAssuranceObservePackage(packagePath string) bool {
	return isPackageOrChild(packagePath, "internal/assurance/observe")
}

func isFileSetPackage(packagePath string) bool {
	return isPackageOrChild(packagePath, "internal/effect/fileset")
}
