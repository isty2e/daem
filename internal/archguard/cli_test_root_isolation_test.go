package archguard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLITestPackagesIsolateDefaultRoots(t *testing.T) {
	root := findRepoRoot(t)
	for _, packagePath := range sortedOwnerPaths(testToolPackageAdmissions) {
		admission := testToolPackageAdmissions[packagePath]
		if admission.Kind != testToolTestsOnlyPackage ||
			(packagePath != "test/cli" && !strings.HasPrefix(packagePath, "test/cli/")) {
			continue
		}

		mainPath := filepath.Join(root, filepath.FromSlash(packagePath), "main_test.go")
		source, err := os.ReadFile(mainPath)
		if err != nil {
			t.Errorf("read %s root-isolation entrypoint: %v", packagePath, err)
			continue
		}
		if !hasCLITestRootIsolation(source, packagePath == "test/cli") {
			t.Errorf("%s/main_test.go must call testkit.RunWithIsolatedDefaultRoots from TestMain", packagePath)
		}
	}
}

func TestCLITestRootIsolationGuardRejectsMissingSetup(t *testing.T) {
	for name, source := range map[string]string{
		"no TestMain":       `package cli_test`,
		"direct m.Run":      `package cli_test; func TestMain(m *testing.M) { os.Exit(m.Run()) }`,
		"call outside main": `package cli_test; func helper(m *testing.M) { testkit.RunWithIsolatedDefaultRoots(m) }`,
	} {
		t.Run(name, func(t *testing.T) {
			if hasCLITestRootIsolation([]byte(source), false) {
				t.Fatal("guard admitted a CLI test package without TestMain root isolation")
			}
		})
	}
	if !hasCLITestRootIsolation([]byte(`package cli_test
func TestMain(m *testing.M) { os.Exit(testkit.RunWithIsolatedDefaultRoots(m)) }
`), false) {
		t.Fatal("guard rejected the canonical CLI test root-isolation entrypoint")
	}
	if hasCLITestRootIsolation([]byte(`package cli_test
func TestMain(m *testing.M) {
    if os.Getenv("GO_WANT_HELPER") == "1" { os.Exit(m.Run()) }
    os.Exit(testkit.RunWithIsolatedDefaultRoots(m))
}
`), true) {
		t.Fatal("guard admitted a direct m.Run bypass without the exact helper predicate")
	}
	if hasCLITestRootIsolation([]byte(`package cli_test
func TestMain(m *testing.M) {
    if false { testkit.RunWithIsolatedDefaultRoots(m) }
}
`), false) {
		t.Fatal("guard admitted an unreachable root-isolation call")
	}
	if !hasCLITestRootIsolation([]byte(`package cli_test
func TestMain(m *testing.M) {
    if isWorkspaceMutationCLIHelperInvocation() { os.Exit(m.Run()) }
    os.Exit(testkit.RunWithIsolatedDefaultRoots(m))
}
`), true) {
		t.Fatal("guard rejected the exact workspace helper inheritance branch")
	}
}

func hasCLITestRootIsolation(source []byte, allowWorkspaceHelper bool) bool {
	file, err := parser.ParseFile(token.NewFileSet(), "main_test.go", source, parser.SkipObjectResolution)
	if err != nil {
		return false
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "TestMain" || function.Body == nil {
			continue
		}
		if len(function.Body.List) == 0 || !isIsolatedRootExit(function.Body.List[len(function.Body.List)-1]) {
			return false
		}
		isolatedCalls := 0
		directRunCalls := 0
		guardedHelperRun := false
		ast.Inspect(function.Body, func(node ast.Node) bool {
			if statement, ok := node.(*ast.IfStmt); ok && isWorkspaceHelperGuard(statement.Cond) && containsDirectMRun(statement.Body) {
				guardedHelperRun = true
			}
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			receiver, receiverOK := selector.X.(*ast.Ident)
			if receiverOK && receiver.Name == "testkit" && selector.Sel.Name == "RunWithIsolatedDefaultRoots" {
				isolatedCalls++
			}
			if receiverOK && receiver.Name == "m" && selector.Sel.Name == "Run" {
				directRunCalls++
			}
			return true
		})
		if isolatedCalls != 1 {
			return false
		}
		if directRunCalls == 0 {
			return true
		}
		return allowWorkspaceHelper && directRunCalls == 1 && guardedHelperRun
	}
	return false
}

func isIsolatedRootExit(statement ast.Stmt) bool {
	expression, ok := statement.(*ast.ExprStmt)
	if !ok {
		return false
	}
	exitCall, ok := expression.X.(*ast.CallExpr)
	if !ok || len(exitCall.Args) != 1 {
		return false
	}
	exitSelector, ok := exitCall.Fun.(*ast.SelectorExpr)
	if !ok || exitSelector.Sel.Name != "Exit" {
		return false
	}
	exitReceiver, ok := exitSelector.X.(*ast.Ident)
	if !ok || exitReceiver.Name != "os" {
		return false
	}
	isolationCall, ok := exitCall.Args[0].(*ast.CallExpr)
	if !ok || len(isolationCall.Args) != 1 {
		return false
	}
	isolationSelector, ok := isolationCall.Fun.(*ast.SelectorExpr)
	if !ok || isolationSelector.Sel.Name != "RunWithIsolatedDefaultRoots" {
		return false
	}
	isolationReceiver, ok := isolationSelector.X.(*ast.Ident)
	if !ok || isolationReceiver.Name != "testkit" {
		return false
	}
	testMainArgument, ok := isolationCall.Args[0].(*ast.Ident)
	return ok && testMainArgument.Name == "m"
}

func isWorkspaceHelperGuard(expression ast.Expr) bool {
	call, ok := expression.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	function, ok := call.Fun.(*ast.Ident)
	return ok && function.Name == "isWorkspaceMutationCLIHelperInvocation"
}

func containsDirectMRun(node ast.Node) bool {
	found := false
	ast.Inspect(node, func(candidate ast.Node) bool {
		call, ok := candidate.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Run" {
			return true
		}
		receiver, ok := selector.X.(*ast.Ident)
		if ok && receiver.Name == "m" {
			found = true
			return false
		}
		return true
	})
	return found
}
