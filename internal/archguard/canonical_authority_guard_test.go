package archguard

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strings"
)

func analyzeCanonicalAuthorities(records []PackageRecord) []GuardrailFinding {
	var violations []GuardrailFinding
	for _, record := range sortedRecords(records) {
		packagePath, ok := internalPath(record.ImportPath)
		if !ok {
			continue
		}
		if isPermanentlyRetiredPackage(packagePath) {
			violations = append(violations, GuardrailFinding{
				Rule:        ruleCanonicalAuthority,
				PackagePath: packagePath,
				Path:        packagePath,
				Detail:      "package is under a permanently retired canonical path",
			})
			continue
		}
		if len(productionFiles(record)) == 0 {
			continue
		}
		root := internalRoot(packagePath)
		if root == "desired" || !suspiciousCanonicalRoot(root) {
			continue
		}
		descriptors, err := exportedAPIDescriptors(record)
		if err != nil {
			violations = append(violations, uninspectableSourceViolation(packagePath, packagePath, err.Error()))
			continue
		}
		if len(descriptors) != 0 {
			violations = append(violations, GuardrailFinding{
				Rule:        ruleCanonicalAuthority,
				PackagePath: packagePath,
				Path:        packagePath,
				Detail:      "canonical-looking root defines exported production models outside internal/desired",
			})
		}
	}
	return violations
}

func isPermanentlyRetiredPackage(packagePath string) bool {
	for _, retiredRoot := range []string{
		"internal/adopt/discover",
		"internal/declaration/codec/extension",
		"internal/declaration/codec/hook",
		"internal/declaration/codec/instructions",
		"internal/declaration/codec/mcpserver",
		"internal/declaration/codec/skill",
		"internal/declaration/codec/skillgroup",
		"internal/declaration/doc",
		"internal/declaration/edit",
		"internal/declaration/toml",
		"internal/diagnose/manifest",
		"internal/intent",
		"internal/effect/journal/pathstate",
		"internal/lifecycle",
		"internal/realization/lock/delta",
		"internal/realization/lock/snapshot",
		"internal/pathstate",
		"internal/resource",
		"internal/subprocess/capturetext",
		"internal/subprocess/childenv",
		"internal/subprocess/command",
		"internal/subprocess/processtree",
		"internal/subprocess/workdir",
		"internal/surface/destination",
		"internal/surface/directory",
		"internal/surface/file",
		"internal/surface/operation",
	} {
		if isPackageOrChild(packagePath, retiredRoot) {
			return true
		}
	}
	return false
}

func internalRoot(packagePath string) string {
	trimmed := strings.TrimPrefix(packagePath, "internal/")
	if before, _, ok := strings.Cut(trimmed, "/"); ok {
		return before
	}
	return trimmed
}

func suspiciousCanonicalRoot(root string) bool {
	switch root {
	case "canonical", "domain", "domains", "entity", "entities", "model", "models",
		"desiredstate", "desired-state", "desired_state", "locked", "lockedstate", "locked-state", "locked_state",
		"lockmodel", "lockmodels", "lockstate":
		return true
	default:
		return false
	}
}

func analyzeUnreachableProductionPackages(records []PackageRecord, admissions map[string]testToolAdmission) []GuardrailFinding {
	reachable := productionReachablePackages(records)
	seenAdmissions := make(map[string]bool, len(admissions))
	var violations []GuardrailFinding

	for _, record := range sortedRecords(records) {
		packagePath, ok := architecturePath(record.ImportPath)
		if !ok {
			continue
		}
		if _, exact := admissions[packagePath]; !exact {
			if namespace, protected := testToolImportBoundary(packagePath, admissions); protected {
				violations = append(violations, unadmittedTestToolFinding(
					packagePath,
					namespace.Reason,
					"test/tool descendant must have its own exact admission",
				))
				continue
			}
			if len(productionFiles(record)) == 0 && len(record.TestGoFiles)+len(record.XTestGoFiles) != 0 {
				violations = append(violations, unadmittedTestToolFinding(
					packagePath,
					"tests-only packages require explicit architecture ownership",
					"tests-only package must have its own exact admission",
				))
				continue
			}
		}
		if admission, admitted := admissions[packagePath]; admitted {
			seenAdmissions[packagePath] = true
			switch admission.Kind {
			case testToolTestsOnlyPackage:
				switch {
				case reachable[record.ImportPath]:
					violations = append(violations, staleTestToolAdmissionFinding(packagePath, admission.Reason, "tests-only package became reachable from a production executable root"))
				case len(productionFiles(record)) != 0:
					violations = append(violations, staleTestToolAdmissionFinding(packagePath, admission.Reason, "tests-only package gained a non-test production file"))
				case len(record.TestGoFiles)+len(record.XTestGoFiles) == 0:
					violations = append(violations, staleTestToolAdmissionFinding(packagePath, admission.Reason, "tests-only package has no build-selected test files"))
				}
				continue
			case testToolHelperPackage:
				if len(productionFiles(record)) == 0 {
					violations = append(violations, staleTestToolAdmissionFinding(packagePath, admission.Reason, "test/tool helper package has no build-selected production source"))
					continue
				}
			default:
				violations = append(violations, staleTestToolAdmissionFinding(packagePath, admission.Reason, "test/tool package has an invalid admission kind"))
				continue
			}
		} else if len(productionFiles(record)) == 0 {
			continue
		}

		descriptors, err := exportedAPIDescriptors(record)
		if err != nil {
			violations = append(violations, uninspectableSourceViolation(packagePath, packagePath, err.Error()))
			continue
		}
		if admission, admitted := admissions[packagePath]; admitted {
			switch {
			case reachable[record.ImportPath]:
				violations = append(violations, staleTestToolAdmissionFinding(packagePath, admission.Reason, "test/tool package became reachable from a production executable root"))
			case len(descriptors) == 0:
				violations = append(violations, staleTestToolAdmissionFinding(packagePath, admission.Reason, "test/tool package no longer exposes an exported production declaration"))
			}
			continue
		}
		if reachable[record.ImportPath] {
			continue
		}
		detail := "production package is unreachable from every executable root"
		if len(descriptors) != 0 {
			detail = fmt.Sprintf(
				"production package with %d exported declaration(s) is unreachable from every executable root",
				len(descriptors),
			)
		}
		violations = append(violations, GuardrailFinding{
			Rule:        ruleUnreachableProductionPackage,
			PackagePath: packagePath,
			Path:        packagePath,
			Detail:      detail,
		})
	}

	for _, admissionPath := range sortedOwnerPaths(admissions) {
		if !seenAdmissions[admissionPath] {
			violations = append(violations, GuardrailFinding{
				Rule:        ruleStaleTestToolAdmission,
				PackagePath: admissionPath,
				Path:        admissionPath,
				Reason:      admissions[admissionPath].Reason,
				Detail:      "test/tool package is missing from the build-selected production source graph",
			})
		}
	}
	return violations
}

func staleTestToolAdmissionFinding(packagePath string, reason string, detail string) GuardrailFinding {
	return GuardrailFinding{
		Rule:        ruleStaleTestToolAdmission,
		PackagePath: packagePath,
		Path:        packagePath,
		Reason:      reason,
		Detail:      detail,
	}
}

func unadmittedTestToolFinding(packagePath string, reason string, detail string) GuardrailFinding {
	return GuardrailFinding{
		Rule:        ruleUnadmittedTestToolPackage,
		PackagePath: packagePath,
		Path:        packagePath,
		Reason:      reason,
		Detail:      detail,
	}
}

func productionReachablePackages(records []PackageRecord) map[string]bool {
	recordByPath := make(map[string]PackageRecord, len(records))
	queue := make([]string, 0)
	for _, record := range records {
		recordByPath[record.ImportPath] = record
		if record.Name == "main" {
			queue = append(queue, record.ImportPath)
		}
	}
	reachable := make(map[string]bool, len(records))
	for queueIndex := 0; queueIndex < len(queue); queueIndex++ {
		importPath := queue[queueIndex]
		if reachable[importPath] {
			continue
		}
		reachable[importPath] = true
		for _, imported := range recordByPath[importPath].Imports {
			if _, exists := recordByPath[imported]; exists && !reachable[imported] {
				queue = append(queue, imported)
			}
		}
	}
	return reachable
}

func exportedAPIDescriptors(record PackageRecord) ([]string, error) {
	var descriptors []string
	for _, fileName := range productionFiles(record) {
		content, ok := packageFileContent(record, fileName)
		if !ok {
			return nil, fmt.Errorf("read production source %s", fileName)
		}
		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, fileName, content, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("parse production source %s: %w", fileName, err)
		}
		for _, declaration := range file.Decls {
			switch typed := declaration.(type) {
			case *ast.FuncDecl:
				if !ast.IsExported(typed.Name.Name) {
					continue
				}
				copy := *typed
				copy.Body = nil
				copy.Doc = nil
				formatted, err := formatNode(fileSet, &copy)
				if err != nil {
					return nil, fmt.Errorf("format exported function %s: %w", typed.Name.Name, err)
				}
				descriptors = append(descriptors, fileName+":"+formatted)
			case *ast.GenDecl:
				for _, specification := range typed.Specs {
					if !specExportsName(specification) {
						continue
					}
					formatted, err := formatNode(fileSet, specification)
					if err != nil {
						return nil, fmt.Errorf("format exported %s declaration: %w", typed.Tok, err)
					}
					descriptors = append(descriptors, fileName+":"+typed.Tok.String()+" "+formatted)
				}
			}
		}
	}
	return descriptors, nil
}

func specExportsName(specification ast.Spec) bool {
	switch typed := specification.(type) {
	case *ast.TypeSpec:
		return ast.IsExported(typed.Name.Name)
	case *ast.ValueSpec:
		for _, name := range typed.Names {
			if ast.IsExported(name.Name) {
				return true
			}
		}
	}
	return false
}

func formatNode(fileSet *token.FileSet, node ast.Node) (string, error) {
	var buffer bytes.Buffer
	if err := format.Node(&buffer, fileSet, node); err != nil {
		return "", err
	}
	return buffer.String(), nil
}

func uninspectableSourceViolation(packagePath string, filePath string, detail string) GuardrailFinding {
	return GuardrailFinding{
		Rule:        ruleArchitectureSourceUnreadable,
		PackagePath: packagePath,
		Path:        filePath,
		Detail:      detail,
	}
}
