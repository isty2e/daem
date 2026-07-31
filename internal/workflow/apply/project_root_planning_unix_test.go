//go:build darwin || linux

package apply

import (
	"os"
	"path/filepath"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

func TestProjectRelationOrderRequiresRetainedProjectRoot(t *testing.T) {
	root := t.TempDir()
	locked := relationOrderTestLock(
		t,
		root,
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		[]string{"npm:a@1", "npm:b@1"},
	)
	writeRelationOrderTestFile(
		t,
		filepath.Join(root, ".pi", "settings.json"),
		`{"packages":["npm:b@1","npm:a@1"]}`,
	)
	reconciliation := relationOrderTestReconciliation(
		t,
		daempaths.Paths{ManifestRoot: root},
		locked,
		nil,
	)
	if !requiresProjectRootAuthority(commandPlan{
		assessment: readiness.Assessment{Reconciliation: reconciliation},
	}) {
		t.Fatal("project extension order did not retain project-root authority")
	}
}

func TestGlobalRelationOrderRequiresRetainedSelectedRoot(t *testing.T) {
	root := t.TempDir()
	agentRoot := filepath.Join(root, "pi-agent")
	t.Setenv("PI_CODING_AGENT_DIR", agentRoot)
	locked := relationOrderTestLockAtScope(
		t,
		root,
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		target.ScopeGlobal,
		[]string{"npm:a@1", "npm:b@1"},
	)
	writeRelationOrderTestFile(
		t,
		filepath.Join(agentRoot, "settings.json"),
		`{"packages":["npm:b@1","npm:a@1"]}`,
	)
	reconciliation := relationOrderTestReconciliation(
		t,
		daempaths.Paths{ManifestRoot: root},
		locked,
		nil,
	)
	if !requiresProjectRootAuthority(commandPlan{
		assessment: readiness.Assessment{Reconciliation: reconciliation},
	}) {
		t.Fatal("global extension order did not retain selected-root authority")
	}
}

func TestPlanWriteRetainsProjectRootAlongsideDeclarationWitness(t *testing.T) {
	root, manifestPath, lockfilePath, missingInventory, _, _ := writeApplyCodexPluginCarrierCommandFixture(t)
	planned, err := PlanWrite(t.Context(), CommandInput{
		ManifestPath:         manifestPath,
		LockfilePath:         lockfilePath,
		TargetValues:         []string{"codex"},
		RelationObservations: &missingInventory,
	})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	defer planned.Close()
	if planned.lifecycle == nil || planned.lifecycle.planned.projectRoot == nil {
		t.Fatalf("prepared write did not retain declaration and project-root witnesses")
	}
	matches, err := planned.lifecycle.declarationRevisions.MatchesCurrent(t.Context())
	if err != nil || !matches {
		t.Fatalf("prepared declaration witness current = %t, %v", matches, err)
	}

	moved := root + "-captured"
	t.Cleanup(func() { _ = os.RemoveAll(moved) })
	if err := os.Rename(root, moved); err != nil {
		t.Fatalf("move selected root after planning: %v", err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create replacement root: %v", err)
	}
	if err := planned.lifecycle.planned.projectRoot.ValidateSelection(root); !hasRootedPathFailureKind(
		err,
		rootedpath.FailureRootReplaced,
	) {
		t.Fatalf("project-root validation error = %v, want %s", err, rootedpath.FailureRootReplaced)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".daem", "state.json")); !os.IsNotExist(statErr) {
		t.Fatalf("replacement statefile stat error = %v, want absent", statErr)
	}
}
