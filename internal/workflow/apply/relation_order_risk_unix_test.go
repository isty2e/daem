//go:build unix

package apply

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
)

func TestRelationOrderRiskBaselineRejectsUninitializedZeroValue(t *testing.T) {
	if err := (relationOrderRiskBaseline{}).validate(); err == nil ||
		!strings.Contains(err.Error(), "baseline is required") {
		t.Fatalf("zero baseline validation error = %v", err)
	}
	if err := newRelationOrderRiskBaseline(nil).validate(); err != nil {
		t.Fatalf("constructed empty baseline validation error = %v", err)
	}
}

func TestRelationOrderRiskBaselineComparesExactRiskIdentity(t *testing.T) {
	root := t.TempDir()
	paths := daempaths.Paths{ManifestRoot: root}
	locked := relationOrderTestLock(
		t,
		root,
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		[]string{"npm:a@1", "npm:b@1"},
	)
	settingsPath := filepath.Join(root, ".pi", "settings.json")
	decisionsForContent := func(content string) []reconcile.RelationOrderDecision {
		t.Helper()
		writeRelationOrderTestFile(t, settingsPath, content)
		return relationOrderTestReconciliation(t, paths, locked, nil).RelationOrders()
	}

	authorized := decisionsForContent(`{"packages":["npm:b@1","npm:foreign-a@1","npm:a@1"]}`)
	baseline := newRelationOrderRiskBaseline(authorized)
	if got := baseline.expansion(authorized).AddedRiskCount(); got != 0 {
		t.Fatalf("unchanged risk expansion = %d, want 0", got)
	}

	contraction := decisionsForContent(`{"packages":["npm:a@1","npm:b@1"]}`)
	if got := baseline.expansion(contraction).AddedRiskCount(); got != 0 {
		t.Fatalf("risk contraction expansion = %d, want 0", got)
	}

	changedIdentity := decisionsForContent(
		`{"packages":["npm:b@1","npm:foreign-b@1","npm:a@1"]}`,
	)
	expansion := baseline.expansion(changedIdentity)
	if expansion.AddedRiskCount() != 2 || len(expansion.Decisions()) != 1 {
		t.Fatalf(
			"changed foreign identity expansion = %#v, want two risks in one decision",
			expansion,
		)
	}
}

func TestRelationOrderRiskBaselineIgnoresDecisionInputOrder(t *testing.T) {
	root := t.TempDir()
	paths := daempaths.Paths{ManifestRoot: root}
	locked := relationOrderTestLock(
		t,
		root,
		desiredextension.CarrierOpenCodePlugin,
		target.TargetOpenCode,
		[]string{"alpha@1", "beta@1"},
	)
	content := `{"plugin":["beta@1","foreign@1","alpha@1"]}`
	writeRelationOrderTestFile(t, filepath.Join(root, ".opencode", "opencode.json"), content)
	writeRelationOrderTestFile(t, filepath.Join(root, ".opencode", "tui.json"), content)
	decisions := relationOrderTestReconciliation(t, paths, locked, nil).RelationOrders()
	if len(decisions) != 2 {
		t.Fatalf("OpenCode decisions = %d, want 2", len(decisions))
	}

	reordered := slices.Clone(decisions)
	slices.Reverse(reordered)
	if got := newRelationOrderRiskBaseline(decisions).expansion(reordered); got.AddedRiskCount() != 0 {
		t.Fatalf("reordered decisions expanded risk: %#v", got)
	}
}
