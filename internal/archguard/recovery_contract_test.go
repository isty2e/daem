package archguard

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestRecoveryClassificationDocumentationMatchesTypedConstants(t *testing.T) {
	root := findRepoRoot(t)
	active := typedStringConstantValuesFromGo(
		t,
		filepath.Join(root, "internal", "effect", "journal", "recovery", "model.go"),
		"Classification",
	)
	cleanup := typedStringConstantValuesFromGo(
		t,
		filepath.Join(root, "internal", "effect", "journal", "retirement", "cleanup.go"),
		"CleanupClassification",
	)
	blocked := slices.Index(active, "blocked")
	if blocked < 0 {
		t.Fatal("active recovery classifications do not contain blocked")
	}
	canonical := slices.Concat(active[:blocked], cleanup, active[blocked:])
	documented := recoveryClassificationValuesFromDocs(t, filepath.Join(root, "docs", "cli.md"))

	if !slices.Equal(documented, canonical) {
		t.Fatalf("documented recovery classifications = %#v, want typed constants %#v", documented, canonical)
	}
}

func TestParseRecoveryClassificationTableHandlesCRLF(t *testing.T) {
	content := strings.Join([]string{
		"## `recover`",
		"",
		"| Classification | Meaning |",
		"| --- | --- |",
		"| `clean_before` | Before state. |",
		"",
		"## Next",
	}, "\r\n")

	values, err := parseRecoveryClassificationTable(content)
	if err != nil {
		t.Fatalf("parseRecoveryClassificationTable returned error: %v", err)
	}
	if !slices.Equal(values, []string{"clean_before"}) {
		t.Fatalf("values = %#v, want clean_before", values)
	}
}

func TestParseRecoveryClassificationTableRejectsAmbiguousRows(t *testing.T) {
	tests := []struct {
		name string
		rows []string
	}{
		{name: "duplicate", rows: []string{"| `clean_before` | First. |", "| `clean_before` | Duplicate. |"}},
		{name: "non-code value", rows: []string{"| clean_before | Missing code span. |"}},
		{name: "extra cell", rows: []string{"| `clean_before` | Meaning | Extra |"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			content := strings.Join(append([]string{
				"## `recover`",
				"",
				"| Classification | Meaning |",
				"| --- | --- |",
			}, test.rows...), "\n")
			if _, err := parseRecoveryClassificationTable(content); err == nil {
				t.Fatalf("parseRecoveryClassificationTable accepted rows %#v", test.rows)
			}
		})
	}
}

func TestRecoveryJournalBeforeEvidenceHasSingleCanonicalInput(t *testing.T) {
	root := findRepoRoot(t)
	capturePath := filepath.Join(root, "internal", "effect", "journal", "capture.go")
	captureSet := token.NewFileSet()
	captureFile, err := parser.ParseFile(captureSet, capturePath, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("ParseFile(%s) returned error: %v", capturePath, err)
	}

	options := namedStructType(t, captureFile, "CaptureOptions")
	imports := importNames(captureFile)
	var optionFields []string
	for _, field := range options.Fields.List {
		for _, name := range field.Names {
			optionFields = append(optionFields, name.Name)
			lowerName := strings.ToLower(name.Name)
			if strings.Contains(lowerName, "observation") || strings.Contains(lowerName, "live") ||
				strings.Contains(lowerName, "before") || strings.Contains(lowerName, "evidence") {
				if name.Name != "ManagedPathEvidence" || !isObserveManagedPathEvidenceSlice(field.Type, imports) {
					t.Fatalf("CaptureOptions field %s reintroduces a parallel before-observation input", name.Name)
				}
			}
		}
		if expressionReferencesImport(field.Type, imports, "github.com/isty2e/daem/internal/assurance/observe") &&
			!isObserveManagedPathEvidenceSlice(field.Type, imports) {
			formatted, formatErr := formatNode(captureSet, field.Type)
			if formatErr != nil {
				t.Fatalf("format CaptureOptions field type: %v", formatErr)
			}
			t.Fatalf("CaptureOptions has non-canonical observe input %s", formatted)
		}
	}
	wantOptionFields := []string{
		"ClaimTransitions", "ManagedPathMutations", "ManagedAggregateMutations", "ManagedPathEvidence",
		"Resolver", "ProjectRoot", "OperationAuthority", "RootedCapability", "Codecs", "StateCodec", "Filesystem",
	}
	slices.Sort(optionFields)
	slices.Sort(wantOptionFields)
	if !slices.Equal(optionFields, wantOptionFields) {
		t.Fatalf("CaptureOptions fields = %#v, want reviewed canonical boundary %#v", optionFields, wantOptionFields)
	}

	wantArguments := []string{"ManagedPathMutations", "ManagedAggregateMutations", "ManagedPathEvidence"}
	gotArguments := pathMutationCallOptionFields(t, captureFile, "buildRecoveryJournal")
	if !slices.Equal(gotArguments, wantArguments) {
		t.Fatalf("buildRecoveryJournal pathMutations inputs = %#v, want %#v", gotArguments, wantArguments)
	}

	mutationPath := filepath.Join(root, "internal", "effect", "journal", "path_mutation.go")
	mutationSet := token.NewFileSet()
	mutationFile, err := parser.ParseFile(mutationSet, mutationPath, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("ParseFile(%s) returned error: %v", mutationPath, err)
	}
	wantParameterTypes := []string{
		"[]ManagedPathMutation",
		"[]ManagedAggregateMutation",
		"[]observe.ManagedPathEvidence",
	}
	gotParameterTypes := functionParameterTypes(t, mutationSet, mutationFile, "pathMutations")
	if !slices.Equal(gotParameterTypes, wantParameterTypes) {
		t.Fatalf("pathMutations parameter types = %#v, want %#v", gotParameterTypes, wantParameterTypes)
	}
}

func TestJournalRecoveryBoundaryOwnsWireNeutralModels(t *testing.T) {
	root := findRepoRoot(t)
	journalSet, journalFiles := parseProductionGoFiles(t, filepath.Join(root, "internal", "effect", "journal"))
	_, recoveryFiles := parseProductionGoFiles(t, filepath.Join(root, "internal", "effect", "journal", "recovery"))

	for path, file := range recoveryFiles {
		ast.Inspect(file, func(node ast.Node) bool {
			field, ok := node.(*ast.Field)
			if !ok || field.Tag == nil {
				return true
			}
			tag, err := strconv.Unquote(field.Tag.Value)
			if err != nil {
				t.Fatalf("unquote struct tag in %s: %v", path, err)
			}
			if strings.Contains(tag, "json:") {
				t.Fatalf("%s contains JSON-tagged recovery model field %q", path, tag)
			}
			return true
		})
	}

	for path, file := range journalFiles {
		imports := importNames(file)
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, specification := range general.Specs {
				switch typed := specification.(type) {
				case *ast.TypeSpec:
					if typed.Name.IsExported() &&
						expressionReferencesImport(typed.Type, imports, "github.com/isty2e/daem/internal/effect/journal/recovery") {
						t.Fatalf("%s re-exports or wraps recovery type %s", path, typed.Name.Name)
					}
				case *ast.ValueSpec:
					for index, name := range typed.Names {
						if !name.IsExported() || index >= len(typed.Values) {
							continue
						}
						if expressionReferencesImport(typed.Values[index], imports, "github.com/isty2e/daem/internal/effect/journal/recovery") {
							t.Fatalf("%s re-exports recovery value %s", path, name.Name)
						}
					}
				}
			}
		}
	}

	selection := namedStructInPackage(t, journalFiles, "EntrySelection")
	assertStructFields(t, journalSet, selection, []string{
		"key entrySelectionKey",
		"initialized bool",
	})
	selectionKey := namedStructInPackage(t, journalFiles, "entrySelectionKey")
	assertStructFields(t, journalSet, selectionKey, []string{
		"subject topology.SubjectID",
		"target target.Target",
		"consumers string",
		"scope target.Scope",
		"destination output.Destination",
		"contentPath output.ContentPath",
	})

	for _, name := range []string{"recoveryJournal", "recoveryBeforePathDTO", "recoveryExpectedPathDTO"} {
		if ast.IsExported(name) {
			t.Fatalf("reviewed private v8 DTO name %q unexpectedly exported", name)
		}
		_ = namedStructInPackage(t, journalFiles, name)
	}
	for _, retired := range []string{"ActionExpectedPathState", "ManagedPathExpectedStates", "ManagedAggregateExpectedStates"} {
		if packageDeclaresName(journalFiles, retired) {
			t.Fatalf("journal declares retired recovery transport %s", retired)
		}
	}
}

func parseProductionGoFiles(t *testing.T, directory string) (*token.FileSet, map[string]*ast.File) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(directory, "*.go"))
	if err != nil {
		t.Fatalf("glob production Go files in %s: %v", directory, err)
	}
	fileSet := token.NewFileSet()
	files := make(map[string]*ast.File)
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("ParseFile(%s) returned error: %v", path, err)
		}
		files[path] = file
	}
	if len(files) == 0 {
		t.Fatalf("no production Go files found in %s", directory)
	}
	return fileSet, files
}

func namedStructInPackage(t *testing.T, files map[string]*ast.File, name string) *ast.StructType {
	t.Helper()
	for _, file := range files {
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, specification := range general.Specs {
				typed, ok := specification.(*ast.TypeSpec)
				if !ok || typed.Name.Name != name {
					continue
				}
				structure, ok := typed.Type.(*ast.StructType)
				if !ok {
					t.Fatalf("%s is %T, want struct", name, typed.Type)
				}
				return structure
			}
		}
	}
	t.Fatalf("struct %s not found", name)
	return nil
}

func assertStructFields(t *testing.T, fileSet *token.FileSet, structure *ast.StructType, want []string) {
	t.Helper()
	got := make([]string, 0, len(structure.Fields.List))
	for _, field := range structure.Fields.List {
		if field.Tag != nil {
			t.Fatalf("guarded canonical struct field carries tag %s", field.Tag.Value)
		}
		formatted, err := formatNode(fileSet, field.Type)
		if err != nil {
			t.Fatalf("format guarded canonical struct field: %v", err)
		}
		for _, name := range field.Names {
			if name.IsExported() {
				t.Fatalf("guarded canonical struct field %s must remain private", name.Name)
			}
			got = append(got, name.Name+" "+formatted)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("guarded canonical struct fields = %#v, want %#v", got, want)
	}
}

func packageDeclaresName(files map[string]*ast.File, name string) bool {
	for _, file := range files {
		for _, declaration := range file.Decls {
			switch declared := declaration.(type) {
			case *ast.FuncDecl:
				if declared.Name.Name == name {
					return true
				}
			case *ast.GenDecl:
				for _, specification := range declared.Specs {
					switch typed := specification.(type) {
					case *ast.TypeSpec:
						if typed.Name.Name == name {
							return true
						}
					case *ast.ValueSpec:
						for _, declaredName := range typed.Names {
							if declaredName.Name == name {
								return true
							}
						}
					}
				}
			}
		}
	}
	return false
}

func namedStructType(t *testing.T, file *ast.File, name string) *ast.StructType {
	t.Helper()
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, specification := range general.Specs {
			typed, ok := specification.(*ast.TypeSpec)
			if !ok || typed.Name.Name != name {
				continue
			}
			structure, ok := typed.Type.(*ast.StructType)
			if !ok {
				t.Fatalf("%s is %T, want struct", name, typed.Type)
			}
			return structure
		}
	}
	t.Fatalf("struct %s not found", name)
	return nil
}

func isObserveManagedPathEvidenceSlice(expression ast.Expr, imports map[string]string) bool {
	slice, ok := expression.(*ast.ArrayType)
	if !ok || slice.Len != nil {
		return false
	}
	selector, ok := slice.Elt.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "ManagedPathEvidence" {
		return false
	}
	qualifier, ok := selector.X.(*ast.Ident)
	return ok && imports[qualifier.Name] == "github.com/isty2e/daem/internal/assurance/observe"
}

func expressionReferencesImport(expression ast.Expr, imports map[string]string, importPath string) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		qualifier, ok := selector.X.(*ast.Ident)
		if ok && imports[qualifier.Name] == importPath {
			found = true
			return false
		}
		return true
	})
	return found
}

func pathMutationCallOptionFields(t *testing.T, file *ast.File, functionName string) []string {
	t.Helper()
	function := namedFunction(t, file, functionName)
	var result []string
	callCount := 0
	ast.Inspect(function.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		callee, ok := call.Fun.(*ast.Ident)
		if !ok || callee.Name != "pathMutations" {
			return true
		}
		callCount++
		for _, argument := range call.Args {
			selector, ok := argument.(*ast.SelectorExpr)
			if !ok {
				t.Fatalf("pathMutations argument %T is not an options field selector", argument)
			}
			owner, ownerOK := selector.X.(*ast.Ident)
			if !ownerOK || owner.Name != "options" {
				t.Fatalf("pathMutations argument owner %T is not options", selector.X)
			}
			result = append(result, selector.Sel.Name)
		}
		return true
	})
	if callCount != 1 {
		t.Fatalf("%s pathMutations call count = %d, want 1", functionName, callCount)
	}
	return result
}

func functionParameterTypes(t *testing.T, fileSet *token.FileSet, file *ast.File, name string) []string {
	t.Helper()
	function := namedFunction(t, file, name)
	var result []string
	for _, field := range function.Type.Params.List {
		formatted, err := formatNode(fileSet, field.Type)
		if err != nil {
			t.Fatalf("format %s parameter type: %v", name, err)
		}
		for range field.Names {
			result = append(result, formatted)
		}
	}
	return result
}

func namedFunction(t *testing.T, file *ast.File, name string) *ast.FuncDecl {
	t.Helper()
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Recv == nil && function.Name.Name == name {
			return function
		}
	}
	t.Fatalf("function %s not found", name)
	return nil
}

func typedStringConstantValuesFromGo(
	t *testing.T,
	path string,
	typeName string,
) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("ParseFile(%s) returned error: %v", path, err)
	}

	values := make([]string, 0)
	seen := make(map[string]struct{})
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.CONST {
			continue
		}
		for _, spec := range general.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok || !isNamedType(valueSpec.Type, typeName) {
				continue
			}
			if len(valueSpec.Values) != len(valueSpec.Names) {
				t.Fatalf("%s declaration %v does not use one explicit value per name", typeName, valueSpec.Names)
			}
			for _, expression := range valueSpec.Values {
				literal, ok := expression.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					t.Fatalf("%s value %T is not an explicit string literal", typeName, expression)
				}
				value, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatalf("Unquote %s value %q returned error: %v", typeName, literal.Value, err)
				}
				if _, duplicate := seen[value]; duplicate {
					t.Fatalf("duplicate typed %s value %q", typeName, value)
				}
				seen[value] = struct{}{}
				values = append(values, value)
			}
		}
	}
	if len(values) == 0 {
		t.Fatalf("no typed %s string constants found", typeName)
	}

	return values
}

func recoveryClassificationValuesFromDocs(t *testing.T, path string) []string {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) returned error: %v", path, err)
	}
	values, err := parseRecoveryClassificationTable(string(content))
	if err != nil {
		t.Fatalf("parse recovery classification table: %v", err)
	}
	return values
}

func parseRecoveryClassificationTable(content string) ([]string, error) {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	inRecoverSection := false
	for index, line := range lines {
		switch {
		case line == "## `recover`":
			inRecoverSection = true
		case inRecoverSection && strings.HasPrefix(line, "## "):
			return nil, fmt.Errorf("classification table not found in recover section")
		case inRecoverSection && line == "| Classification | Meaning |":
			return parseRecoveryClassificationRows(lines[index+1:])
		}
	}
	return nil, fmt.Errorf("recover section not found")
}

func parseRecoveryClassificationRows(lines []string) ([]string, error) {
	if len(lines) == 0 || lines[0] != "| --- | --- |" {
		return nil, fmt.Errorf("classification table separator is missing or malformed")
	}

	values := make([]string, 0)
	seen := make(map[string]struct{})
	for _, line := range lines[1:] {
		if !strings.HasPrefix(line, "|") {
			break
		}
		cells := strings.Split(line, "|")
		if len(cells) != 4 {
			return nil, fmt.Errorf("malformed classification row %q", line)
		}
		cell := strings.TrimSpace(cells[1])
		if len(cell) < 2 || cell[0] != '`' || cell[len(cell)-1] != '`' {
			return nil, fmt.Errorf("classification cell %q must be one code span", cell)
		}
		value := cell[1 : len(cell)-1]
		if value == "" {
			return nil, fmt.Errorf("classification value is empty")
		}
		if _, duplicate := seen[value]; duplicate {
			return nil, fmt.Errorf("duplicate documented classification %q", value)
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("classification table has no rows")
	}

	return values, nil
}

func isNamedType(expression ast.Expr, name string) bool {
	identifier, ok := expression.(*ast.Ident)
	return ok && identifier.Name == name
}
