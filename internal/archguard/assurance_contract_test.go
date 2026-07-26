package archguard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	mcpobserve "github.com/isty2e/daem/internal/assurance/observe/mcp"
	"github.com/isty2e/daem/internal/assurance/statefile"
)

func TestStatefileAttemptHistoryDoesNotExposeCurrentnessOrDecisionAuthority(t *testing.T) {
	root := findRepoRoot(t)
	for _, relativePath := range []string{
		"internal/assurance/durable/attempt/attempt_history.go",
		"internal/assurance/durable/attempt/delegate_attempt.go",
		"internal/assurance/durable/attempt/host_route_attempt.go",
	} {
		path := filepath.Join(root, filepath.FromSlash(relativePath))
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", relativePath, err)
		}
		for _, declaration := range file.Decls {
			method, ok := declaration.(*ast.FuncDecl)
			if !ok || method.Recv == nil || !method.Name.IsExported() {
				continue
			}
			for _, forbidden := range []string{
				"FreshFor",
				"IsCurrent",
				"IsReady",
				"Converged",
				"CanSkip",
				"CanDelete",
				"CanRetry",
				"CanRecover",
				"GrantsApplySkip",
			} {
				if strings.Contains(method.Name.Name, forbidden) {
					t.Errorf("durable history method %s.%s claims currentness or decision authority", relativePath, method.Name.Name)
				}
			}
		}
	}
}

func TestReconciliationDoesNotConsumeDurableAttemptHistory(t *testing.T) {
	root := findRepoRoot(t)
	reconcileDir := filepath.Join(root, "internal", "reconcile")
	forbidden := map[string]struct{}{
		"AttemptHistory":        {},
		"DelegateAttempt":       {},
		"DelegateAttempts":      {},
		"HostRouteAttempt":      {},
		"HostRouteAttempts":     {},
		"PriorDelegateAttempt":  {},
		"PriorDelegateAttempts": {},
	}

	err := filepath.Walk(reconcileDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if !ok {
				return true
			}
			if _, blocked := forbidden[identifier.Name]; blocked {
				t.Errorf(
					"reconciliation file %s consumes history identifier %q",
					strings.TrimPrefix(path, root+string(filepath.Separator)),
					identifier.Name,
				)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("scan reconciliation production files: %v", err)
	}
}

func TestRelationObservationDoesNotMintAuthorityFromWorkflowHints(t *testing.T) {
	root := findRepoRoot(t)
	for relativePath, forbidden := range map[string][]string{
		"internal/assurance/observe/relation/host/observe.go": {
			"durable.Snapshot",
			"DelegateAttempts",
			"HostRouteAttempts",
			"PostAttemptSubject",
			"StateObservation",
		},
		"internal/assurance/observe/relation/host/claude_adapter.go": {
			"DelegateAttempts",
			"HostRouteAttempts",
			"PostAttemptSubject",
		},
		"internal/workflow/apply/host_route_observer.go": {
			"durable.Snapshot",
			"DelegateAttempts",
			"HostRouteAttempts",
		},
		"internal/assurance/observe/model.go": {
			"StateObservation",
		},
	} {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relativePath)))
		if err != nil {
			t.Fatalf("read %s: %v", relativePath, err)
		}
		for _, marker := range forbidden {
			if strings.Contains(string(content), marker) {
				t.Errorf("authority boundary %s regained forbidden marker %q", relativePath, marker)
			}
		}
	}
}

func TestHostRouteResultAlgebraContainsOnlyProductionReachableStates(t *testing.T) {
	root := findRepoRoot(t)
	for relativePath, forbidden := range map[string]map[string]struct{}{
		"internal/assurance/hostroute/result.go": {
			"NoAttempt":                                  {},
			"PriorDelegateAttemptDisclosure":             {},
			"CurrentObservationWithPriorDelegateAttempt": {},
			"ResultHistoryOnly":                          {},
			"ResultUnsupported":                          {},
			"classifyWithoutCurrentAttempt":              {},
		},
		"internal/assurance/durable/attempt/host_route_attempt.go": {
			"HostRouteResultHistoryOnly":             {},
			"HostRouteResultUnsupported":             {},
			"HostRouteReasonPriorAttemptOnly":        {},
			"HostRouteReasonAttemptMissing":          {},
			"HostRouteReasonUnsupportedObservation":  {},
			"HostRouteReasonAttemptTimestampMissing": {},
		},
		"internal/workflow/apply/host_route_attempt.go": {
			"HostRouteResultHistoryOnly": {},
			"HostRouteResultUnsupported": {},
			"ResultHistoryOnly":          {},
			"ResultUnsupported":          {},
		},
	} {
		path := filepath.Join(root, filepath.FromSlash(relativePath))
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", relativePath, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if !ok {
				return true
			}
			if _, blocked := forbidden[identifier.Name]; blocked {
				t.Errorf(
					"host-route result owner %s retained unreachable identifier %q",
					relativePath,
					identifier.Name,
				)
			}
			return true
		})
	}
}

func TestStatefileDoesNotPersistRuntimeProbeEvidence(t *testing.T) {
	content, err := statefile.Marshal(durable.EmptySnapshot())
	if err != nil {
		t.Fatalf("marshal empty durable snapshot: %v", err)
	}
	encoded := strings.ToLower(string(content))
	for _, forbidden := range []string{"runtime", "probe", "readiness"} {
		if strings.Contains(encoded, forbidden) {
			t.Errorf("statefile persists operation-local runtime probe evidence through %q", forbidden)
		}
	}
}

func TestPassiveRelationEvidenceDoesNotRegainHistoryOrLifecycleMirrors(t *testing.T) {
	root := findRepoRoot(t)
	for relativePath, forbidden := range map[string][]string{
		"internal/realization/relation/relation.go": {
			"RelationObservation",
		},
		"internal/assurance/observe/relation/model.go": {
			"RelationObservation",
			"SameSubjectIndexes",
			"ManagedKeyIndexes",
		},
		"internal/assurance/observe/claudeplugin/model.go": {
			"PriorDelegateAttempt",
			"DelegateAttemptDisclosure",
			"type InventoryAvailability =",
			"type EvidenceFreshness =",
			"type CorrelationState =",
			"type ReasonCode =",
			"type Watchpoint =",
		},
	} {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relativePath)))
		if err != nil {
			t.Fatalf("read %s: %v", relativePath, err)
		}
		for _, marker := range forbidden {
			if strings.Contains(string(content), marker) {
				t.Errorf("passive relation owner %s regained forbidden marker %q", relativePath, marker)
			}
		}
	}
}

func TestPassiveMCPProjectionEvidenceDoesNotRegainOtherAssuranceAxes(t *testing.T) {
	observationType := reflect.TypeFor[mcpobserve.AggregateProjectionObservation]()
	wantFields := []string{"Subject", "Projection", "Ownership", "Shadowing"}
	if observationType.NumField() != len(wantFields) {
		t.Fatalf(
			"AggregateProjectionObservation has %d fields, want exact passive field set %v",
			observationType.NumField(),
			wantFields,
		)
	}
	for index, want := range wantFields {
		if got := observationType.Field(index).Name; got != want {
			t.Errorf("AggregateProjectionObservation field[%d] = %q, want %q", index, got, want)
		}
	}

	root := findRepoRoot(t)
	for relativePath, forbidden := range map[string][]string{
		"internal/assurance/observe/mcp/model.go": {
			"type Observation struct",
			"type StatusDimension struct",
			"type Runtime",
			"ReasonRuntime",
			"ConfigObservationInput",
			"LastDelegateAttempt        LastDelegateAttemptInput",
			"type ApprovalState",
			"type ApprovalEvidence",
			"type ApprovalObservation",
			"ReasonApprovalState",
		},
		"internal/assurance/observe/mcp/observe.go": {
			"func ObserveConfig(",
			"func ObserveProjectFile(",
			"func (observation AggregateProjectionObservation) StatusDimensions(",
		},
	} {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relativePath)))
		if err != nil {
			t.Fatalf("read %s: %v", relativePath, err)
		}
		for _, marker := range forbidden {
			if strings.Contains(string(content), marker) {
				t.Errorf("passive MCP owner %s regained forbidden marker %q", relativePath, marker)
			}
		}
	}
}

func TestRuntimeProbeModelStaysOperationLocalAndIndependent(t *testing.T) {
	root := findRepoRoot(t)
	runtimeDir := filepath.Join(root, "internal", "assurance", "runtimeprobe")
	entries, err := os.ReadDir(runtimeDir)
	if err != nil {
		t.Fatalf("read runtime probe package: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(runtimeDir, entry.Name())
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, imported := range file.Imports {
			importPath, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatalf("unquote import %s in %s: %v", imported.Path.Value, path, err)
			}
			for _, forbidden := range []string{
				"/internal/assurance/observe",
				"/internal/assurance/statefile",
				"/internal/workflow",
				"/internal/cli/present",
				"/internal/effect/execute",
				"/internal/diagnose",
			} {
				if strings.Contains(importPath, forbidden) {
					t.Errorf("runtime probe model imports forbidden owner %q", importPath)
				}
			}
		}
	}

	if _, err := os.Stat(filepath.Join(root, "internal", "observe", "mcp", "runtime.go")); !os.IsNotExist(err) {
		t.Errorf("passive MCP package retains runtime model file: %v", err)
	}
}
