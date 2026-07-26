package archguard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"strconv"
)

func analyzeCoreTerminalSideEffects(packagePath string, record PackageRecord) []GuardrailFinding {
	if !isCoreTerminalSideEffectPackage(packagePath) {
		return nil
	}

	var violations []GuardrailFinding
	for _, fileName := range sortedStrings(record.GoFiles) {
		content, ok := packageFileContent(record, fileName)
		if !ok {
			continue
		}

		file, err := parser.ParseFile(token.NewFileSet(), fileName, content, parser.SkipObjectResolution)
		if err != nil {
			continue
		}

		imports := importNames(file)
		filePath := path.Join(packagePath, fileName)
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}

			detail, ok := terminalSideEffectDetail(selector, imports)
			if !ok {
				return true
			}

			violations = append(violations, GuardrailFinding{
				Rule:        ruleCoreTerminalSideEffect,
				PackagePath: packagePath,
				Path:        filePath,
				Detail:      detail + "; core packages emit phase facts only and presentation/CLI owns rendering",
			})
			return true
		})
	}
	return violations
}

func isCoreTerminalSideEffectPackage(packagePath string) bool {
	return isPackageOrChild(packagePath, "internal/supply/source") ||
		packagePath == "internal/realization/lock/build" ||
		isPackageOrChild(packagePath, "internal/effect/execute") ||
		isPackageOrChild(packagePath, "internal/effect/journal")
}

func importNames(file *ast.File) map[string]string {
	names := make(map[string]string, len(file.Imports))
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		if spec.Name != nil {
			if spec.Name.Name == "." || spec.Name.Name == "_" {
				continue
			}
			names[spec.Name.Name] = importPath
			continue
		}
		names[path.Base(importPath)] = importPath
	}
	return names
}

func terminalSideEffectDetail(selector *ast.SelectorExpr, imports map[string]string) (string, bool) {
	owner, ok := selector.X.(*ast.Ident)
	if !ok {
		return "", false
	}

	importPath, ok := imports[owner.Name]
	if !ok {
		return "", false
	}

	name := selector.Sel.Name
	switch importPath {
	case "fmt":
		if name == "Print" || name == "Printf" || name == "Println" {
			return "direct terminal output through fmt." + name, true
		}
	case "log":
		if name == "Print" || name == "Printf" || name == "Println" {
			return "direct terminal output through log." + name, true
		}
	case "os":
		if name == "Stdout" || name == "Stderr" {
			return "direct terminal handle use through os." + name, true
		}
	}
	return "", false
}
