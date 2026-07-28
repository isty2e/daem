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

func TestMCPTopologyOwnsProjectionStructuralIdentity(t *testing.T) {
	root := findRepoRoot(t)
	assertProductionPackageOmits(t, root, "internal/realization/aggregate", []string{
		"MCPSubjectNamespace",
		"MCPSubjectKey",
		"topology.NewSubjectID",
	})
	assertProductionPackageOmits(t, root, "internal/realization/lock", []string{
		"MCPProjectionSubjectNamespace",
	})

	lowerPath := filepath.Join(root, "internal", "topology", "mcp", "lower.go")
	content, err := os.ReadFile(lowerPath)
	if err != nil {
		t.Fatalf("read MCP topology lowerer: %v", err)
	}
	if strings.Contains(string(content), "internal/realization/aggregate") {
		t.Fatal("MCP topology lowerer imports Realization placement authority")
	}
}

func TestMCPTopologyProjectionNamespaceCatalogIsDataOnly(t *testing.T) {
	root := findRepoRoot(t)
	path := filepath.Join(root, "internal", "topology", "mcp", "projection_namespace_catalog.go")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read MCP projection namespace catalog: %v", err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, content, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse MCP projection namespace catalog: %v", err)
	}

	ast.Inspect(file, func(node ast.Node) bool {
		switch node.(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			t.Errorf("MCP projection namespace catalog must contain data rows only, found %T", node)
			return false
		default:
			return true
		}
	})
}

func TestMCPPlacementCatalogOwnsTargetSpecificAdmissionPolicy(t *testing.T) {
	root := findRepoRoot(t)
	path := filepath.Join(root, "internal", "declaration", "normalize", "mcp_server.go")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read MCP declaration normalizer: %v", err)
	}
	source := string(content)

	if strings.Contains(source, "internal/realization") {
		t.Error("MCP declaration normalizer imports Realization placement authority")
	}
	for _, forbidden := range []string{
		"target.TargetAntigravityCLI",
		"target.TargetClaudeCode",
		"target.TargetCodex",
		"target.TargetOpenCode",
		"target.TargetPi",
	} {
		if strings.Contains(source, forbidden) {
			t.Errorf("MCP declaration normalizer retains target-specific admission policy %q", forbidden)
		}
	}
}

func TestMCPRuntimeProbeAdmissionIsProfileOwned(t *testing.T) {
	root := findRepoRoot(t)
	assertProductionPackageOmits(
		t,
		root,
		"internal/realization/aggregate/codec/mcp",
		[]string{
			"SupportsRuntimeProbe",
			"RuntimeProbeRequiresDelegatePlan",
			"probeRequiresDelegate",
		},
	)
	assertProductionPackageOmits(
		t,
		root,
		"internal/realization/aggregate/codec",
		[]string{"MCPRuntimeProbePlacements"},
	)

	commandPath := filepath.Join(root, "internal", "workflow", "probe", "command.go")
	content, err := os.ReadFile(commandPath)
	if err != nil {
		t.Fatalf("read MCP runtime-probe workflow: %v", err)
	}
	source := string(content)
	if !strings.Contains(source, "MCPRuntimeProbeCapability") {
		t.Fatal("MCP runtime-probe workflow does not consume the profile capability")
	}
	for _, forbidden := range []string{
		"ImplementedMCPPlacementOperationsForID",
		"RuntimeProbePlacements",
		"SupportsRuntimeProbe",
	} {
		if strings.Contains(source, forbidden) {
			t.Errorf(
				"MCP runtime-probe workflow still derives admission from codec operation %q",
				forbidden,
			)
		}
	}
}

func TestMCPNoEnvProjectionInputsHaveOneConcreteRepresentation(t *testing.T) {
	root := findRepoRoot(t)
	assertProductionPackageOmits(
		t,
		root,
		"internal/realization/aggregate/codec/mcp",
		[]string{
			"type ClaudeGlobalMCPServerProjection ",
			"type AntigravityGlobalMCPServerProjection ",
			"type OpenCodeProjectMCPServerProjection ",
			"type OpenCodeGlobalMCPServerProjection ",
			"type CodexProjectMCPServerProjection ",
		},
	)
}

func assertProductionPackageOmits(t *testing.T, root string, packagePath string, forbidden []string) {
	t.Helper()
	directory := filepath.Join(root, filepath.FromSlash(packagePath))
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read %s: %v", packagePath, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, text := range forbidden {
			if strings.Contains(string(content), text) {
				t.Errorf("%s retains forbidden production authority %q", path, text)
			}
		}
	}
}
