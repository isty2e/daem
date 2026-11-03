package archguard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
)

func analyzeRestrictedStateCodecs(packagePath string, record PackageRecord) []GuardrailFinding {
	var findings []GuardrailFinding
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
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "NewCodec" {
				return true
			}
			owner, ok := selector.X.(*ast.Ident)
			if !ok || imports[owner.Name] != "github.com/isty2e/daem/internal/assurance/statefile" {
				return true
			}
			findings = append(findings, GuardrailFinding{
				Rule: ruleRestrictedStateCodec, PackagePath: packagePath,
				Path:   path.Join(packagePath, fileName),
				Detail: "production persistence must preserve complete current snapshots; use statefile.Codec or operation-selected NewNativeCodec, not restricted NewCodec",
			})
			return true
		})
	}
	return findings
}
