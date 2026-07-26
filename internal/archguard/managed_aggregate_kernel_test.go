package archguard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

const managedAggregatePackagePath = "github.com/isty2e/daem/internal/realization/aggregate"

func TestManagedAggregateKernelCannotRegainFamilyOrHostBranches(t *testing.T) {
	root := findRepoRoot(t)
	files := managedAggregateKernelFiles(t, root)
	if len(files) == 0 {
		t.Fatal("managed aggregate kernel inventory is empty")
	}

	for _, relative := range files {
		t.Run(strings.ReplaceAll(relative, "/", "_"), func(t *testing.T) {
			path := filepath.Join(root, filepath.FromSlash(relative))
			for _, violation := range managedAggregateKernelViolations(path, nil) {
				t.Error(violation)
			}
		})
	}
}

func TestManagedAggregateKernelGuardRejectsSyntheticLeakage(t *testing.T) {
	source := `package kernel

import (
	"github.com/example/daem/internal/desired/hook"
	"github.com/example/daem/internal/assurance/observe/mcp"
)

func leaked() {
	_ = hook.Hook{}
	_ = mcp.Inventory{}
	_ = HookPlacementFor
	_ = MCPPlacementClaudeProject
	_ = isMCPProjection
	_ = ismcpprojection
	_ = MCPServer
	_ = TargetCodex
	_ = "claude-code.project.mcp-server"
	_ = "claudecode"
}
`
	violations := managedAggregateKernelViolations("synthetic.go", source)
	for _, want := range []string{
		`imports family adapter`,
		`family or host identifier "HookPlacementFor"`,
		`family or host identifier "MCPPlacementClaudeProject"`,
		`family or host identifier "isMCPProjection"`,
		`family or host identifier "ismcpprojection"`,
		`family or host identifier "MCPServer"`,
		`family or host identifier "TargetCodex"`,
		`family or host literal "claude-code.project.mcp-server"`,
		`family or host literal "claudecode"`,
	} {
		found := false
		for _, violation := range violations {
			if strings.Contains(violation, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("synthetic violations = %#v, missing %q", violations, want)
		}
	}
}

func TestManagedAggregateKernelGuardDoesNotTreatPiInsideGenericWordsAsHostLeakage(t *testing.T) {
	source := `package kernel

func generic() {
	_ = Pipeline
	_ = Copy
	_ = Capability
}
`
	if violations := managedAggregateKernelViolations("synthetic.go", source); len(violations) != 0 {
		t.Fatalf("generic identifiers produced violations: %#v", violations)
	}
}

func TestManagedAggregateBoundaryImportsAreExplicitlyClassified(t *testing.T) {
	root := findRepoRoot(t)
	classifications := managedAggregateWorkflowClassifications()
	seen := make(map[string]struct{}, len(classifications))

	for _, directory := range []string{"internal/workflow/readiness", "internal/workflow/apply", "internal/workflow/status"} {
		pattern := filepath.Join(root, filepath.FromSlash(directory), "*.go")
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("glob managed aggregate workflow sources %q: %v", pattern, err)
		}
		for _, path := range matches {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			importsAggregate, err := sourceImports(path, managedAggregatePackagePath)
			if err != nil {
				t.Fatalf("inspect managed aggregate workflow import %q: %v", path, err)
			}
			if !importsAggregate {
				continue
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				t.Fatalf("relativize managed aggregate workflow source %q: %v", path, err)
			}
			relative = filepath.ToSlash(relative)
			classification, classified := classifications[relative]
			if !classified {
				t.Errorf("managed aggregate workflow importer %q is not classified", relative)
				continue
			}
			if strings.TrimSpace(classification.reason) == "" {
				t.Errorf("managed aggregate workflow importer %q has no classification reason", relative)
			}
			seen[relative] = struct{}{}
		}
	}

	for relative := range classifications {
		if _, present := seen[relative]; !present {
			t.Errorf("stale managed aggregate workflow classification %q", relative)
		}
	}
}

func TestManagedAggregateKernelInventoryExcludesCompositionAndPrivateCodecs(t *testing.T) {
	root := findRepoRoot(t)
	files := managedAggregateKernelFiles(t, root)
	inventory := make(map[string]struct{}, len(files))
	for _, path := range files {
		inventory[path] = struct{}{}
	}
	for _, allowed := range []string{
		"internal/realization/aggregate/codec/registry.go",
		"internal/realization/aggregate/codec/hook/codec.go",
		"internal/realization/aggregate/codec/mcp/codec.go",
		"internal/realization/aggregate/codec/mcp/operations.go",
		"internal/assurance/observe/mcp/projection_batch.go",
		"internal/workflow/readiness/assessment.go",
		"internal/workflow/readiness/managed_aggregate.go",
		"internal/workflow/readiness/mcp.go",
		"internal/workflow/apply/delegate_attempt_summary.go",
	} {
		if _, present := inventory[allowed]; present {
			t.Errorf("managed aggregate kernel inventory includes private composition file %q", allowed)
		}
	}
}

func TestManagedAggregateCanonicalPackageCannotOwnConcreteCodecs(t *testing.T) {
	root := findRepoRoot(t)
	directory := filepath.Join(root, "internal", "realization", "aggregate")
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read canonical aggregate package: %v", err)
	}

	methodsByReceiver := make(map[string]map[string]struct{})
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") ||
			strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse canonical aggregate source %q: %v", path, err)
		}
		for _, spec := range parsed.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("decode canonical aggregate import %q: %v", spec.Path.Value, err)
			}
			if importPath == "encoding/json" ||
				importPath == "github.com/BurntSushi/toml" ||
				strings.Contains(importPath, "/internal/realization/aggregate/codec") {
				t.Errorf("canonical aggregate source %q imports concrete codec %q", path, importPath)
			}
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv == nil || len(function.Recv.List) != 1 {
				continue
			}
			receiver := receiverTypeName(function.Recv.List[0].Type)
			if receiver == "" {
				continue
			}
			if methodsByReceiver[receiver] == nil {
				methodsByReceiver[receiver] = make(map[string]struct{})
			}
			methodsByReceiver[receiver][function.Name.Name] = struct{}{}
		}
	}

	codecMethods := []string{"ContractID", "ValidateContribution", "Read", "Render", "Restore"}
	for receiver, methods := range methodsByReceiver {
		implementsCodec := true
		for _, method := range codecMethods {
			if _, ok := methods[method]; !ok {
				implementsCodec = false
				break
			}
		}
		if implementsCodec {
			t.Errorf("canonical aggregate type %q implements the concrete Codec method set", receiver)
		}
	}

	for _, retired := range []string{
		"hook_codec.go",
		"mcp_codec.go",
		"mcp_projection_config.go",
		"mcp_projection_codex_config.go",
	} {
		path := filepath.Join(directory, retired)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("retired concrete codec file %q exists or cannot be classified: %v", path, err)
		}
	}
}

func receiverTypeName(expression ast.Expr) string {
	switch typed := expression.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.StarExpr:
		return receiverTypeName(typed.X)
	case *ast.IndexExpr:
		return receiverTypeName(typed.X)
	case *ast.IndexListExpr:
		return receiverTypeName(typed.X)
	default:
		return ""
	}
}

func TestStatusWorkflowCannotRegainRetiredParallelActionProduction(t *testing.T) {
	root := findRepoRoot(t)
	matches, err := filepath.Glob(filepath.Join(root, "internal", "workflow", "status", "*.go"))
	if err != nil {
		t.Fatalf("glob status workflow sources: %v", err)
	}
	for _, path := range matches {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		for _, violation := range retiredParallelActionProducerViolations(path, nil) {
			t.Error(violation)
		}
	}
}

func TestRecoveryJournalCannotRegainParallelIdentityOrCatalogInference(t *testing.T) {
	root := findRepoRoot(t)
	for _, pattern := range []string{
		filepath.Join(root, "internal", "effect", "journal", "*.go"),
		filepath.Join(root, "internal", "realization", "aggregate", "*.go"),
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("glob recovery authority sources %q: %v", pattern, err)
		}
		for _, path := range matches {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read recovery authority source %q: %v", path, err)
			}
			for _, forbidden := range recoveryJournalLegacyTokens(string(content)) {
				t.Errorf("%s contains retired recovery authority token %q", path, forbidden)
			}
		}
	}
}

func TestRecoveryJournalLegacyTokenGuardRejectsSyntheticResidue(t *testing.T) {
	source := `package journal
type recoveryEntry struct {
	LegacyResource persistedEntityRef ` + "`json:\"resource\"`" + `
}
func LegacyProjectionContract() {}
`
	got := recoveryJournalLegacyTokens(source)
	for _, want := range []string{"LegacyResource", "persistedEntityRef", `json:"resource"`, "LegacyProjectionContract"} {
		if !slices.Contains(got, want) {
			t.Fatalf("legacy token findings = %v, want %q", got, want)
		}
	}
}

func recoveryJournalLegacyTokens(content string) []string {
	var found []string
	for _, forbidden := range []string{
		"LegacyEntityID",
		"LegacyResource",
		"persistedEntityRef",
		"recoveryIdentityForLegacy",
		"legacyAggregateProjectionContract",
		"LegacyProjectionContract",
		`json:"resource"`,
	} {
		if strings.Contains(content, forbidden) {
			found = append(found, forbidden)
		}
	}
	return found
}

func TestRetiredParallelActionProducerGuardRejectsSyntheticProduction(t *testing.T) {
	source := `package status

import reconciliation "github.com/example/daem/internal/reconcile"

func build() reconciliation.Result {
	return reconciliation.Result{Actions: []reconciliation.Action{{}}}
}
`
	violations := retiredParallelActionProducerViolations("synthetic.go", source)
	for _, want := range []string{
		"constructs retired reconcile.Action",
		"sets retired Result.Actions",
	} {
		found := false
		for _, violation := range violations {
			if strings.Contains(violation, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("synthetic violations = %#v, missing %q", violations, want)
		}
	}
}

func TestRetiredDesiredOutputAxisStaysAbsent(t *testing.T) {
	root := findRepoRoot(t)
	projectPackage := filepath.Join(root, "internal", "output", "project")
	if _, err := os.Stat(projectPackage); !os.IsNotExist(err) {
		t.Fatalf("retired output/project package exists or cannot be classified: %v", err)
	}

	for _, check := range []struct {
		pattern   string
		forbidden map[string]struct{}
	}{
		{
			pattern: filepath.Join(root, "internal", "plan", "*.go"),
			forbidden: map[string]struct{}{
				"func:Build": {},
			},
		},
		{
			pattern: filepath.Join(root, "internal", "output", "*.go"),
			forbidden: map[string]struct{}{
				"type:DeclaredResource": {},
				"type:DesiredOutput":    {},
			},
		},
	} {
		matches, err := filepath.Glob(check.pattern)
		if err != nil {
			t.Fatalf("glob retired desired-output axis %q: %v", check.pattern, err)
		}
		for _, path := range matches {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse %q: %v", path, err)
			}
			for _, declaration := range parsed.Decls {
				switch value := declaration.(type) {
				case *ast.FuncDecl:
					if _, forbidden := check.forbidden["func:"+value.Name.Name]; forbidden {
						t.Errorf("retired root function %s remains in %s", value.Name.Name, path)
					}
				case *ast.GenDecl:
					for _, spec := range value.Specs {
						typeSpec, ok := spec.(*ast.TypeSpec)
						if !ok {
							continue
						}
						if _, forbidden := check.forbidden["type:"+typeSpec.Name.Name]; forbidden {
							t.Errorf("retired root type %s remains in %s", typeSpec.Name.Name, path)
						}
					}
				}
			}
		}
	}
}

func managedAggregateKernelViolations(filename string, source any) []string {
	parsed, err := parser.ParseFile(token.NewFileSet(), filename, source, parser.SkipObjectResolution)
	if err != nil {
		return []string{"parse managed aggregate kernel source: " + err.Error()}
	}

	var violations []string
	for _, spec := range parsed.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			violations = append(violations, "decode import "+spec.Path.Value+": "+err.Error())
			continue
		}
		if _, forbidden := managedAggregateForbiddenSemanticToken(importPath); forbidden {
			violations = append(
				violations,
				"managed aggregate kernel imports family adapter "+strconv.Quote(importPath),
			)
		}
	}
	ast.Inspect(parsed, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.Ident:
			if _, forbidden := managedAggregateForbiddenSemanticToken(value.Name); forbidden {
				violations = append(
					violations,
					"managed aggregate kernel uses family or host identifier "+strconv.Quote(value.Name),
				)
			}
		case *ast.BasicLit:
			if value.Kind != token.STRING {
				return true
			}
			literal, err := strconv.Unquote(value.Value)
			if err != nil {
				violations = append(violations, "decode string literal "+value.Value+": "+err.Error())
				return true
			}
			if _, forbidden := managedAggregateForbiddenSemanticToken(literal); forbidden {
				violations = append(
					violations,
					"managed aggregate kernel uses family or host literal "+strconv.Quote(literal),
				)
			}
		}
		return true
	})
	sort.Strings(violations)
	return violations
}

func managedAggregateKernelFiles(t *testing.T, root string) []string {
	t.Helper()
	patterns := []string{
		"internal/assurance/observe/*.go",
		"internal/reconcile/*.go",
		"internal/effect/execute/*.go",
		"internal/effect/journal/*.go",
	}
	files := make(map[string]struct{})
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(pattern)))
		if err != nil {
			t.Fatalf("glob managed aggregate kernel sources %q: %v", pattern, err)
		}
		for _, path := range matches {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				t.Fatalf("relativize managed aggregate kernel source %q: %v", path, err)
			}
			files[filepath.ToSlash(relative)] = struct{}{}
		}
	}
	for relative, classification := range managedAggregateWorkflowClassifications() {
		if classification.guard {
			files[relative] = struct{}{}
		}
	}
	result := make([]string, 0, len(files))
	for path := range files {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

type managedAggregateWorkflowClassification struct {
	guard  bool
	reason string
}

func managedAggregateWorkflowClassifications() map[string]managedAggregateWorkflowClassification {
	return map[string]managedAggregateWorkflowClassification{
		"internal/workflow/readiness/aggregate_projection_summary.go": {
			guard: true, reason: "generic post-delegation aggregate equivalence observation",
		},
		"internal/workflow/apply/managed_aggregate_authority.go": {
			guard: true, reason: "generic aggregate execution-authority fingerprinting",
		},
		"internal/workflow/readiness/assessment.go": {
			reason: "readiness composition over generic aggregate planning inputs",
		},
		"internal/workflow/readiness/managed_aggregate.go": {
			reason: "family lowering and composition root",
		},
		"internal/workflow/readiness/managed_aggregate_observe.go": {
			guard: true, reason: "generic aggregate observation orchestration",
		},
		"internal/workflow/readiness/projection.go": {
			reason: "readiness composition over canonical projection builders",
		},
		"internal/workflow/readiness/mcp.go": {
			reason: "MCP-specific assurance observation sequencing",
		},
		"internal/workflow/readiness/ownership.go": {
			guard: true, reason: "generic aggregate ownership orchestration",
		},
	}
}

func sourceImports(path string, wanted string) (bool, error) {
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		return false, err
	}
	for _, spec := range parsed.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return false, err
		}
		if importPath == wanted {
			return true, nil
		}
	}
	return false, nil
}

func managedAggregateForbiddenSemanticToken(value string) (string, bool) {
	lower := strings.ToLower(value)
	for _, forbidden := range []string{"hook", "mcp", "antigravity", "claude", "codex", "opencode"} {
		if strings.Contains(lower, forbidden) {
			return forbidden, true
		}
	}
	for _, semanticToken := range semanticTokens(value) {
		if strings.EqualFold(semanticToken, "pi") {
			return "pi", true
		}
	}
	return "", false
}

func semanticTokens(value string) []string {
	runes := []rune(value)
	tokens := make([]string, 0, 4)
	start := -1
	flush := func(end int) {
		if start >= 0 && start < end {
			tokens = append(tokens, string(runes[start:end]))
		}
	}
	for index, current := range runes {
		if !unicode.IsLetter(current) && !unicode.IsDigit(current) {
			flush(index)
			start = -1
			continue
		}
		if start < 0 {
			start = index
			continue
		}
		previous := runes[index-1]
		nextIsLower := index+1 < len(runes) && unicode.IsLower(runes[index+1])
		if unicode.IsUpper(current) &&
			(unicode.IsLower(previous) || unicode.IsDigit(previous) ||
				(unicode.IsUpper(previous) && nextIsLower)) {
			flush(index)
			start = index
		}
	}
	flush(len(runes))
	return tokens
}

func retiredParallelActionProducerViolations(filename string, source any) []string {
	parsed, err := parser.ParseFile(token.NewFileSet(), filename, source, parser.SkipObjectResolution)
	if err != nil {
		return []string{"parse status reconciliation source: " + err.Error()}
	}
	reconciliationAliases := make(map[string]struct{})
	for _, spec := range parsed.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil || !strings.HasSuffix(importPath, "/internal/reconcile") {
			continue
		}
		alias := "reconcile"
		if spec.Name != nil {
			alias = spec.Name.Name
		}
		reconciliationAliases[alias] = struct{}{}
	}

	var violations []string
	ast.Inspect(parsed, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.CompositeLit:
			if isRetiredReconciliationActionType(value.Type, reconciliationAliases) {
				violations = append(violations, filename+" constructs retired reconcile.Action")
			}
			for _, element := range value.Elts {
				keyValue, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := keyValue.Key.(*ast.Ident)
				if ok && key.Name == "Actions" {
					violations = append(violations, filename+" sets retired Result.Actions")
				}
			}
		case *ast.AssignStmt:
			for _, expression := range value.Lhs {
				selector, ok := expression.(*ast.SelectorExpr)
				if ok && selector.Sel.Name == "Actions" {
					violations = append(violations, filename+" assigns retired Result.Actions")
				}
			}
		}
		return true
	})
	sort.Strings(violations)
	return violations
}

func isRetiredReconciliationActionType(expression ast.Expr, reconciliationAliases map[string]struct{}) bool {
	switch value := expression.(type) {
	case *ast.ArrayType:
		return isRetiredReconciliationActionType(value.Elt, reconciliationAliases)
	case *ast.SelectorExpr:
		qualifier, ok := value.X.(*ast.Ident)
		if !ok || value.Sel.Name != "Action" {
			return false
		}
		_, reconciliationAlias := reconciliationAliases[qualifier.Name]
		return reconciliationAlias
	default:
		return false
	}
}
