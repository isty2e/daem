package archguard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"strconv"
	"strings"
	"testing"
)

func TestPreparedWorkflowCapabilitiesRemainAtAdmittedBoundaries(t *testing.T) {
	const modulePath = "github.com/isty2e/daem/"
	const cliImportPath = modulePath + "internal/cli"

	allowedConsumers := map[string]map[string]struct{}{
		modulePath + "internal/workflow/apply": {
			cliImportPath:                       {},
			modulePath + "internal/cli/present": {},
		},
		modulePath + "internal/workflow/recover": {
			cliImportPath: {},
		},
	}

	for _, record := range loadRepoPackageRecords(t) {
		for _, imported := range record.Imports {
			consumers, isCapabilityPackage := allowedConsumers[imported]
			if !isCapabilityPackage {
				continue
			}
			if _, allowed := consumers[record.ImportPath]; !allowed {
				t.Errorf(
					"production package %q imports workflow capability package %q without an admitted execution or immutable-result boundary",
					record.ImportPath,
					imported,
				)
			}
		}
	}
}

func TestPresentationBoundaryOmitsRetiredWorkflowDTOsAndCLIAdapters(t *testing.T) {
	forbiddenWorkflowTypes := map[string]map[string]struct{}{
		"internal/workflow/list": {
			"Row": {},
		},
		"internal/workflow/status": {
			"MCPProjectionStatus": {},
			"MCPStatusDimension":  {},
		},
		"internal/workflow/probe": {
			"RuntimeDimensionRow": {},
		},
	}
	forbiddenWorkflowFunctions := map[string]map[string]struct{}{
		"internal/workflow/probe": {
			"RuntimeDimensions": {},
		},
	}
	forbiddenCLIFiles := map[string]struct{}{
		"apply_status_present.go": {},
		"authoring_present.go":    {},
	}
	forbiddenCLIFunctionPrefixes := []string{
		"filterListRows",
		"presentAuthoring",
		"presentDryRun",
		"presentInventory",
		"presentList",
		"presentMCP",
	}

	for _, record := range loadRepoPackageRecords(t) {
		packagePath, internal := internalPath(record.ImportPath)
		if !internal {
			continue
		}
		for _, fileName := range productionFiles(record) {
			content, ok := packageFileContent(record, fileName)
			if !ok {
				t.Fatalf("read production source %s/%s", packagePath, fileName)
			}
			file, err := parser.ParseFile(token.NewFileSet(), fileName, content, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse production source %s/%s: %v", packagePath, fileName, err)
			}

			if forbiddenTypes, guarded := forbiddenWorkflowTypes[packagePath]; guarded {
				assertFileOmitsTypeDeclarations(t, packagePath, fileName, file, forbiddenTypes)
			}
			if forbiddenFunctions, guarded := forbiddenWorkflowFunctions[packagePath]; guarded {
				assertFileOmitsFunctionDeclarations(t, packagePath, fileName, file, forbiddenFunctions)
			}
			if packagePath != "internal/cli" {
				continue
			}
			if _, forbidden := forbiddenCLIFiles[fileName]; forbidden {
				t.Errorf("CLI reintroduced result-to-view conversion file %q", fileName)
			}
			for _, declaration := range file.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok {
					continue
				}
				for _, prefix := range forbiddenCLIFunctionPrefixes {
					if strings.HasPrefix(function.Name.Name, prefix) {
						t.Errorf("CLI reintroduced result-to-view conversion helper %s in %s", function.Name.Name, fileName)
					}
				}
			}
			if fileName == "list.go" && fileCallsSelector(file, "strings", "Split") {
				t.Error("CLI list command reintroduced target filtering by parsing rendered strings")
			}
		}
	}
}

func TestPresentationMayConsumeAdmittedWorkflowFactsButCannotInvokeWorkflows(t *testing.T) {
	records := loadRepoPackageRecords(t)
	workflowFunctions := exportedWorkflowFunctions(t, records)
	workflowPackageNames := packageNames(records)
	for _, record := range records {
		packagePath, internal := internalPath(record.ImportPath)
		if !internal || !isPresentPackage(packagePath) {
			continue
		}
		for _, fileName := range productionFiles(record) {
			content, ok := packageFileContent(record, fileName)
			if !ok {
				t.Fatalf("read production source %s/%s", packagePath, fileName)
			}
			file, err := parser.ParseFile(token.NewFileSet(), fileName, content, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse production source %s/%s: %v", packagePath, fileName, err)
			}
			workflowImports := importedWorkflowPackages(t, file, workflowPackageNames)
			for _, reference := range workflowFunctionReferences(file, workflowImports, workflowFunctions) {
				t.Errorf(
					"presentation %s/%s references workflow function %s; only admitted result or progress-fact consumption is allowed",
					packagePath,
					fileName,
					reference,
				)
			}
		}
	}
}

func TestPresentationBoundaryGuardRejectsRetiredShapesAndWorkflowCalls(t *testing.T) {
	source := `package cli
import statusworkflow "example.com/project/internal/workflow/status"
import "strings"
type MCPProjectionStatus struct{}
func presentMCPStatus(value string) {
	_ = strings.Split(value, ",")
	_, _ = statusworkflow.Execute(nil, statusworkflow.CommandInput{})
}
`
	file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	forbidden := map[string]struct{}{"MCPProjectionStatus": {}}
	if got := declaredForbiddenTypes(file, forbidden); len(got) != 1 || got[0] != "MCPProjectionStatus" {
		t.Fatalf("declared forbidden types = %#v, want MCPProjectionStatus", got)
	}
	if !fileCallsSelector(file, "strings", "Split") {
		t.Fatal("guard did not detect display-string splitting")
	}
	fixturePackageNames := map[string]string{
		"example.com/project/internal/workflow/status": "statusworkflow",
	}
	workflowImports := importedWorkflowPackages(t, file, fixturePackageNames)
	workflowFunctions := map[string]map[string]struct{}{
		"example.com/project/internal/workflow/status": {"Execute": {}},
	}
	references := workflowFunctionReferences(file, workflowImports, workflowFunctions)
	if len(references) != 1 || references[0] != "statusworkflow.Execute" {
		t.Fatalf("workflow function references = %#v, want statusworkflow.Execute", references)
	}

	aliasSource := `package cli
import statusworkflow "example.com/project/internal/workflow/status"
var runStatus = statusworkflow.Execute
func presentStatus() { _, _ = runStatus(nil, statusworkflow.CommandInput{}) }
`
	aliasFile, err := parser.ParseFile(token.NewFileSet(), "alias_fixture.go", aliasSource, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse alias fixture: %v", err)
	}
	references = workflowFunctionReferences(
		aliasFile,
		importedWorkflowPackages(t, aliasFile, fixturePackageNames),
		workflowFunctions,
	)
	if len(references) != 1 || references[0] != "statusworkflow.Execute" {
		t.Fatalf("aliased workflow function references = %#v, want statusworkflow.Execute", references)
	}

	unaliasedSource := `package cli
import "example.com/project/internal/workflow/status"
var runStatus = statusworkflow.Execute
`
	unaliasedFile, err := parser.ParseFile(token.NewFileSet(), "unaliased_fixture.go", unaliasedSource, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse unaliased fixture: %v", err)
	}
	references = workflowFunctionReferences(
		unaliasedFile,
		importedWorkflowPackages(t, unaliasedFile, fixturePackageNames),
		workflowFunctions,
	)
	if len(references) != 1 || references[0] != "statusworkflow.Execute" {
		t.Fatalf("unaliased workflow function references = %#v, want statusworkflow.Execute", references)
	}
}

func assertFileOmitsFunctionDeclarations(
	t *testing.T,
	packagePath string,
	fileName string,
	file *ast.File,
	forbidden map[string]struct{},
) {
	t.Helper()
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if _, blocked := forbidden[function.Name.Name]; blocked {
			t.Errorf("workflow %s reintroduced presentation-ready function %s in %s", packagePath, function.Name.Name, fileName)
		}
	}
}

func assertFileOmitsTypeDeclarations(
	t *testing.T,
	packagePath string,
	fileName string,
	file *ast.File,
	forbidden map[string]struct{},
) {
	t.Helper()
	for _, name := range declaredForbiddenTypes(file, forbidden) {
		t.Errorf("workflow %s reintroduced presentation-ready type %s in %s", packagePath, name, fileName)
	}
}

func declaredForbiddenTypes(file *ast.File, forbidden map[string]struct{}) []string {
	var result []string
	for _, declaration := range file.Decls {
		generic, ok := declaration.(*ast.GenDecl)
		if !ok || generic.Tok != token.TYPE {
			continue
		}
		for _, spec := range generic.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			if _, blocked := forbidden[typeSpec.Name.Name]; blocked {
				result = append(result, typeSpec.Name.Name)
			}
		}
	}
	return result
}

func importedWorkflowPackages(
	t *testing.T,
	file *ast.File,
	packageNames map[string]string,
) map[string]string {
	t.Helper()
	result := make(map[string]string)
	for _, importSpec := range file.Imports {
		importPath, err := strconv.Unquote(importSpec.Path.Value)
		if err != nil {
			t.Fatalf("unquote import path %q: %v", importSpec.Path.Value, err)
		}
		internalImport, internal := internalPath(importPath)
		if !internal || !matchesInternalImport(internalImport, "internal/workflow") {
			continue
		}
		alias := packageNames[importPath]
		if alias == "" {
			alias = path.Base(importPath)
		}
		if importSpec.Name != nil {
			alias = importSpec.Name.Name
		}
		if alias == "." || alias == "_" {
			t.Fatalf("presentation workflow import %q must have a finite inspectable qualifier", importPath)
		}
		result[alias] = importPath
	}
	return result
}

func packageNames(records []PackageRecord) map[string]string {
	result := make(map[string]string, len(records))
	for _, record := range records {
		result[record.ImportPath] = record.Name
	}
	return result
}

func exportedWorkflowFunctions(t *testing.T, records []PackageRecord) map[string]map[string]struct{} {
	t.Helper()
	result := make(map[string]map[string]struct{})
	for _, record := range records {
		packagePath, internal := internalPath(record.ImportPath)
		if !internal || !matchesInternalImport(packagePath, "internal/workflow") {
			continue
		}
		functions := make(map[string]struct{})
		for _, fileName := range productionFiles(record) {
			content, ok := packageFileContent(record, fileName)
			if !ok {
				t.Fatalf("read production source %s/%s", packagePath, fileName)
			}
			file, err := parser.ParseFile(token.NewFileSet(), fileName, content, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse production source %s/%s: %v", packagePath, fileName, err)
			}
			for _, declaration := range file.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok || function.Recv != nil || !ast.IsExported(function.Name.Name) {
					continue
				}
				functions[function.Name.Name] = struct{}{}
			}
		}
		result[record.ImportPath] = functions
	}
	return result
}

func workflowFunctionReferences(
	file *ast.File,
	workflowImports map[string]string,
	workflowFunctions map[string]map[string]struct{},
) []string {
	var result []string
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		qualifier, ok := selector.X.(*ast.Ident)
		if !ok {
			return true
		}
		importPath, workflowImport := workflowImports[qualifier.Name]
		if !workflowImport {
			return true
		}
		if _, function := workflowFunctions[importPath][selector.Sel.Name]; function {
			result = append(result, qualifier.Name+"."+selector.Sel.Name)
		}
		return true
	})
	return result
}

func fileCallsSelector(file *ast.File, qualifier string, name string) bool {
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != name {
			return true
		}
		identifier, ok := selector.X.(*ast.Ident)
		if ok && identifier.Name == qualifier {
			found = true
			return false
		}
		return true
	})
	return found
}
