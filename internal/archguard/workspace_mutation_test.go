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

func TestMutationPackageRejectsPhaseImports(t *testing.T) {
	report := FormatReport(AnalyzeRecords([]PackageRecord{
		{
			ImportPath: "example.com/project/internal/effect/mutation",
			Imports:    []string{"example.com/project/internal/workflow/apply"},
		},
	}))
	if !strings.Contains(report, "mutation package imports forbidden phase: workflow: internal/effect/mutation -> internal/workflow/apply") {
		t.Fatalf("report missing mutation boundary violation:\n%s", report)
	}
}

func TestOwnershipMutationImportsOnlyExactStableOwnershipValues(t *testing.T) {
	const module = "example.com/project/"
	allowed := AnalyzeRecords([]PackageRecord{{
		ImportPath: module + "internal/effect/mutation/ownership",
		Imports:    []string{module + "internal/output/ownership"},
	}})
	if len(allowed) != 0 {
		t.Fatalf("exact stable ownership import produced violations:\n%s", FormatReport(allowed))
	}

	for _, importPath := range []string{
		"internal/output",
		"internal/output/hostpath",
		"internal/output/ownership/store",
	} {
		t.Run(importPath, func(t *testing.T) {
			violations := AnalyzeRecords([]PackageRecord{{
				ImportPath: module + "internal/effect/mutation/ownership",
				Imports:    []string{module + importPath},
			}})
			if len(violations) == 0 {
				t.Fatalf("%s import was admitted", importPath)
			}
		})
	}
}

func TestCLIProcessContextGuard(t *testing.T) {
	root := findRepoRoot(t)
	mainContent, err := os.ReadFile(filepath.Join(root, "cmd", "daem", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	mainText := string(mainContent)
	if !strings.Contains(mainText, "runWithSignalLifecycle") {
		t.Fatal("cmd/daem/main.go missing runWithSignalLifecycle")
	}
	mainFile, err := parser.ParseFile(token.NewFileSet(), "cmd/daem/main.go", mainContent, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse cmd/daem/main.go: %v", err)
	}
	if !hasRunOptionsContextBinding(mainFile) {
		t.Fatal("cmd/daem/main.go does not bind cli.RunOptions.Context to ctx")
	}
	signalContent, err := os.ReadFile(filepath.Join(root, "cmd", "daem", "signals.go"))
	if err != nil {
		t.Fatal(err)
	}
	signalText := string(signalContent)
	for _, required := range []string{"signal.Notify", "signal.Stop", "os.Interrupt", "syscall.SIGTERM", "signalExitCode(first)"} {
		if !strings.Contains(signalText, required) {
			t.Fatalf("cmd/daem/signals.go missing %q", required)
		}
	}

	cliFiles, err := filepath.Glob(filepath.Join(root, "internal", "cli", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range cliFiles {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(content), "/internal/effect/mutation\"") {
			t.Fatalf("%s imports mutation primitives instead of a workflow owner", filepath.Base(path))
		}
		if filepath.Base(path) != "run.go" && strings.Contains(string(content), "context.Background()") {
			t.Fatalf("%s creates a detached CLI context", filepath.Base(path))
		}
	}
}

func hasRunOptionsContextBinding(file *ast.File) bool {
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		literal, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		selector, ok := literal.Type.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "RunOptions" {
			return true
		}
		owner, ok := selector.X.(*ast.Ident)
		if !ok || owner.Name != "cli" {
			return true
		}
		for _, element := range literal.Elts {
			field, ok := element.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, keyOK := field.Key.(*ast.Ident)
			value, valueOK := field.Value.(*ast.Ident)
			if keyOK && valueOK && key.Name == "Context" && value.Name == "ctx" {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func TestWorkspaceWritersRevalidateAcquiredPathDomains(t *testing.T) {
	root := findRepoRoot(t)
	for _, relativePath := range []string{
		"internal/workflow/init/init.go",
		"internal/workflow/lock/mutation.go",
		"internal/workflow/authoring/mutation.go",
		"internal/workflow/adopt/mutation.go",
	} {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relativePath)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), "DomainsMatchCurrent") {
			t.Fatalf("%s does not revalidate the path domains it acquired", relativePath)
		}
	}
}
