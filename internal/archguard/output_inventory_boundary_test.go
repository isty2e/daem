package archguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOutputInventoryAssessmentHasNarrowObservationGraph(t *testing.T) {
	root := findRepoRoot(t)
	assessmentPath := filepath.Join(
		root,
		"internal",
		"workflow",
		"readiness",
		"output_inventory.go",
	)
	content, err := os.ReadFile(assessmentPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"assurance/observe/lock",
		"assurance/observe/mcp/effective",
		"assurance/observe/mcp/provider",
		"assurance/observe/relation",
		"reconcile/build/hostroute",
		"ResolveSourceEpoch",
		"observeMCPProviders",
		"observeProviderEffectiveMCP",
		"resolveCarrierObservations",
		"BuildRelationActions",
	} {
		if strings.Contains(string(content), forbidden) {
			t.Errorf("output inventory assessment regained unrelated probe dependency %q", forbidden)
		}
	}

	listPath := filepath.Join(root, "internal", "workflow", "list", "output.go")
	importsStatus, err := sourceImports(listPath, "github.com/isty2e/daem/internal/workflow/status")
	if err != nil {
		t.Fatal(err)
	}
	if importsStatus {
		t.Fatal("list outputs regained full status workflow dependency")
	}
	listContent, err := os.ReadFile(listPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"ValidateCurrentExtensionOrder",
		"ExtensionOrderIdentityResolver",
	} {
		if strings.Contains(string(listContent), forbidden) {
			t.Errorf("list outputs regained unrelated extension-order dependency %q", forbidden)
		}
	}
}
