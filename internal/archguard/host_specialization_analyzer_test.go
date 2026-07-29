package archguard

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"slices"
	"strconv"
	"strings"
)

func analyzeHostSpecialization(records []PackageRecord, owners map[string]string) []GuardrailFinding {
	seenOwners := make(map[string]bool, len(owners))
	ownerReferences := make(map[string]bool, len(owners))
	ownerSourceErrors := make(map[string]bool, len(owners))
	var violations []GuardrailFinding

	for _, record := range sortedRecords(records) {
		packagePath, ok := internalPath(record.ImportPath)
		if !ok || !isGenericHostGuardPackage(packagePath) {
			continue
		}
		_, owner := owners[packagePath]
		if owner {
			seenOwners[packagePath] = true
		}
		for _, fileName := range productionFiles(record) {
			filePath := path.Join(packagePath, fileName)
			content, ok := packageFileContent(record, fileName)
			if !ok {
				violations = append(violations, uninspectableSourceViolation(packagePath, filePath, "read production source"))
				if owner {
					ownerSourceErrors[packagePath] = true
				}
				continue
			}
			references, err := genericHostReferenceDescriptors(filePath, content)
			if err != nil {
				violations = append(violations, uninspectableSourceViolation(packagePath, filePath, err.Error()))
				if owner {
					ownerSourceErrors[packagePath] = true
				}
				continue
			}
			if len(references) == 0 {
				continue
			}
			if owner {
				ownerReferences[packagePath] = true
				if packagePath == "internal/realization/lock" {
					branches, err := hostBranchDescriptors(filePath, content)
					if err != nil {
						violations = append(violations, uninspectableSourceViolation(packagePath, filePath, err.Error()))
						ownerSourceErrors[packagePath] = true
					} else if len(branches) != 0 {
						violations = append(violations, GuardrailFinding{
							Rule:        ruleGenericHostReference,
							PackagePath: packagePath,
							Path:        filePath,
							Reason:      owners[packagePath],
							Detail:      "locked realization may own static host profile rows but not host-specific control flow: " + strings.Join(branches, ", "),
						})
					}
				}
				continue
			}
			violations = append(violations, GuardrailFinding{
				Rule:        ruleGenericHostReference,
				PackagePath: packagePath,
				Path:        filePath,
				Detail:      strings.Join(uniqueSortedStrings(references), ", "),
			})
		}
	}

	for _, ownerPath := range sortedOwnerPaths(owners) {
		detail := ""
		switch {
		case !isGenericHostGuardPackage(ownerPath):
			detail = "host specialization owner is outside the guarded architecture blocks"
		case !seenOwners[ownerPath]:
			detail = "host specialization owner is missing from the build-selected production source graph"
		case !ownerReferences[ownerPath] && !ownerSourceErrors[ownerPath]:
			detail = "host specialization owner no longer contains host-specific production syntax"
		}
		if detail != "" {
			violations = append(violations, GuardrailFinding{
				Rule:        ruleInvalidArchitectureOwner,
				PackagePath: ownerPath,
				Path:        ownerPath,
				Reason:      owners[ownerPath],
				Detail:      detail,
			})
		}
	}
	return violations
}

func isGenericHostGuardPackage(packagePath string) bool {
	if _, testTool := testToolOwner(packagePath, testToolPackageAdmissions); testTool {
		return false
	}
	for _, root := range []string{
		"internal/assurance/observe/mcp/effective/host",
		"internal/assurance/observe/mcp/provider/host",
		"internal/output/hostpath",
		"internal/workflow",
		"internal/reconcile",
		"internal/workflow/readiness",
		"internal/realization/lock",
		"internal/topology",
		"internal/assurance/observe/relation",
		"internal/effect/execute",
		"internal/effect/journal",
		"internal/assurance/statefile",
	} {
		if isPackageOrChild(packagePath, root) {
			return true
		}
	}
	return false
}

func genericHostReferenceDescriptors(filePath string, content []byte) ([]string, error) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, filePath, content, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parse production source: %w", err)
	}

	targetAliases := make(map[string]bool)
	hostImportAliases := make(map[string]string)
	importPositions := make(map[token.Pos]bool)
	var descriptors []string
	if containsHostPathVocabulary(filePath) {
		descriptors = append(descriptors, "path:"+filePath)
	}
	for _, importSpec := range file.Imports {
		importPositions[importSpec.Path.Pos()] = true
		importPath, err := strconv.Unquote(importSpec.Path.Value)
		if err != nil {
			return nil, fmt.Errorf("unquote import path: %w", err)
		}
		alias := path.Base(importPath)
		if importSpec.Name != nil {
			alias = importSpec.Name.Name
		}
		if containsHostPathVocabulary(importPath) {
			descriptors = append(descriptors, "import:"+importPath)
			if alias != "." && alias != "_" {
				hostImportAliases[alias] = importPath
			}
		}
		if strings.HasSuffix(importPath, "/internal/target") {
			if alias == "." {
				descriptors = append(descriptors, "target-import:dot")
			}
			targetAliases[alias] = true
		}
	}

	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.SelectorExpr:
			identifier, ok := typed.X.(*ast.Ident)
			if ok {
				if importPath, hostImport := hostImportAliases[identifier.Name]; hostImport {
					descriptors = append(descriptors, "host-selector:"+importPath+"."+typed.Sel.Name)
				}
			}
			if ok && targetAliases[identifier.Name] && typed.Sel.Name != "Target" && strings.HasPrefix(typed.Sel.Name, "Target") {
				descriptors = append(descriptors, "target:"+typed.Sel.Name)
			}
		case *ast.ValueSpec:
			if isTargetTypeExpression(typed.Type, targetAliases) {
				for _, valueExpression := range typed.Values {
					if value, ok := stringExpressionValue(valueExpression); ok && value != "" {
						descriptors = append(descriptors, "target-literal:"+value)
					}
				}
			}
		case *ast.CallExpr:
			if isTargetLiteralConstructor(typed.Fun, targetAliases) {
				for _, argument := range typed.Args {
					if value, ok := stringExpressionValue(argument); ok && value != "" {
						descriptors = append(descriptors, "target-literal:"+value)
					}
				}
			}
		case *ast.Ident:
			if containsHostIdentifierVocabulary(typed.Name) {
				descriptors = append(descriptors, "identifier:"+typed.Name)
			}
		case *ast.BasicLit:
			if typed.Kind == token.STRING && !importPositions[typed.Pos()] {
				value, err := strconv.Unquote(typed.Value)
				if err == nil && containsHostVocabulary(value) {
					descriptors = append(descriptors, "string:"+value)
				}
			}
		}
		return true
	})
	return uniqueSortedStrings(descriptors), nil
}

func hostBranchDescriptors(filePath string, content []byte) ([]string, error) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, filePath, content, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parse production source: %w", err)
	}

	var descriptors []string
	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.IfStmt:
			if expressionContainsHostReference(typed.Cond) {
				descriptors = append(descriptors, fmt.Sprintf("if@%d", fileSet.Position(typed.If).Line))
			}
		case *ast.SwitchStmt:
			if typed.Tag != nil && expressionContainsHostReference(typed.Tag) {
				descriptors = append(descriptors, fmt.Sprintf("switch@%d", fileSet.Position(typed.Switch).Line))
			}
		case *ast.CaseClause:
			if slices.ContainsFunc(typed.List, expressionContainsHostReference) {
				descriptors = append(descriptors, fmt.Sprintf("case@%d", fileSet.Position(typed.Case).Line))
			}
		}
		return true
	})
	return descriptors, nil
}

func expressionContainsHostReference(expression ast.Expr) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.Ident:
			found = containsHostIdentifierVocabulary(typed.Name)
		case *ast.BasicLit:
			if typed.Kind == token.STRING {
				value, err := strconv.Unquote(typed.Value)
				found = err == nil && containsHostVocabulary(value)
			}
		}
		return !found
	})
	return found
}

func uniqueSortedStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		unique = append(unique, value)
	}
	return sortedStrings(unique)
}
