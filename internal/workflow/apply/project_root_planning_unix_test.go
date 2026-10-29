//go:build darwin || linux

package apply

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
	"github.com/isty2e/daem/internal/workflow/readiness"
	"github.com/isty2e/daem/test/ownershiptest"
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

func projectNativeGlobalMutationFixture(t *testing.T) daempaths.Paths {
	t.Helper()
	paths := documentProtectionCommandFixture(t, true)
	first, err := PlanWrite(t.Context(), CommandInput{ManifestPath: paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExecuteWithOptions(t.Context(), first, ExecuteOptions{}); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(paths.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	global := filepath.Join(paths.ManifestRoot, "global.md")
	manifest = append(manifest, []byte(fmt.Sprintf("\n[instructions.global]\nsource=%q\n", global))...)
	if err := os.WriteFile(paths.ManifestPath, manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(global, []byte("global instructions\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: paths.ManifestPath}); err != nil {
		t.Fatal(err)
	}
	return paths
}

func TestGlobalOnlyMutationRetainsUnchangedNativeProjectAuthority(t *testing.T) {
	for _, disjointProtection := range []bool{false, true} {
		t.Run(fmt.Sprintf("disjoint-protection=%t", disjointProtection), func(t *testing.T) {
			paths := projectNativeGlobalMutationFixture(t)
			nativePath := filepath.Join(paths.ManifestRoot, ".codex", "config.toml")
			before, err := os.ReadFile(nativePath)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: paths.ManifestPath})
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			planned := prepared.lifecycle.planned
			if planned.projectRoot == nil || len(planned.assessment.Reconciliation.MutatingAggregates()) != 0 {
				t.Fatal("unchanged native namespace lost its root or became a mutation")
			}
			expected := planned.nativePlan.state.AfterState()
			codec := planned.nativePlan.stateCodec
			store, err := ownershipstore.New(paths.OwnershipRegistryPath)
			if err != nil {
				t.Fatal(err)
			}
			if disjointProtection {
				ownershiptest.WritePendingDocumentProtection(t, paths.ManifestPath, filepath.Join(paths.ManifestRoot, "unrelated.json"))
			}
			registryBefore, err := store.Load(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ExecuteWithOptions(t.Context(), prepared, ExecuteOptions{}); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(nativePath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("global-only mutation changed native project content", err)
			}
			assertApplyFileContent(t, filepath.Join(os.Getenv("HOME"), ".codex", "AGENTS.md"), "global instructions\n")
			encoded, err := os.ReadFile(paths.StatefilePath)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := codec.Decode(encoded)
			if err != nil || !actual.Equal(expected) {
				t.Fatal("global-only mutation did not publish its complete native successor", err)
			}
			registryAfter, err := store.Load(t.Context())
			if err != nil || len(registryAfter.PendingProtections()) != len(registryBefore.PendingProtections()) {
				t.Fatal("global-only mutation lost disjoint safety records", err)
			}
			for index, pending := range registryBefore.PendingProtections() {
				if !pending.Equal(registryAfter.PendingProtections()[index]) {
					t.Fatal("global-only mutation changed an unrelated pending record")
				}
			}
		})
	}
}

func TestGlobalOnlyNativeMutationRefusesReplacementProjectRoot(t *testing.T) {
	paths := projectNativeGlobalMutationFixture(t)
	prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	moved := paths.ManifestRoot + "-captured"
	t.Cleanup(func() { _ = os.RemoveAll(moved) })
	if err := os.Rename(paths.ManifestRoot, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(paths.ManifestRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ExecuteWithOptions(t.Context(), prepared, ExecuteOptions{PlanWasDisclosed: true}); !hasRootedPathFailureKind(err, rootedpath.FailureRootReplaced) {
		t.Fatalf("replacement project root was not refused: %v", err)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".codex", "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatal("root refusal published a global output", err)
	}
	entries, err := os.ReadDir(paths.ManifestRoot)
	if err != nil || len(entries) != 0 {
		t.Fatal("root refusal wrote into the replacement", err)
	}
}

func TestNativeStateDocumentCohortUsesCompleteMembership(t *testing.T) {
	paths := projectNativeGlobalMutationFixture(t)
	prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	planned := prepared.lifecycle.planned
	assessment, after := planned.assessment, planned.nativePlan.state.AfterState()
	documents, err := nativeStateDocuments(t.Context(), assessment, after)
	if err != nil || len(documents) != 1 || documents[0].Scope() != target.ScopeProject {
		t.Fatal("native member disappeared from a changed complete successor", err)
	}
	noChange, err := nativeStateDocuments(t.Context(), assessment, assessment.CurrentState)
	if err != nil || len(noChange) != 0 {
		t.Fatal("unchanged full snapshot acquired native namespace obligations", err)
	}
	withoutMembers, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: after.ManagedPaths()})
	if err != nil {
		t.Fatal(err)
	}
	emptyAssessment := assessment
	emptyAssessment.CurrentState = durable.EmptySnapshot()
	empty, err := nativeStateDocuments(t.Context(), emptyAssessment, withoutMembers)
	if err != nil || len(empty) != 0 {
		t.Fatal("declaration without state members acquired native authority", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if documents, err := nativeStateDocuments(ctx, assessment, after); !errors.Is(err, context.Canceled) || documents != nil {
		t.Fatal("canceled membership projection returned a usable cohort", err)
	}
	invalid := assessment
	invalid.NativeApproval = []journal.NativeDocumentApprovalEvidence{{Aliases: []aggregate.DocumentAddress{{}}}}
	if documents, err := nativeStateDocuments(t.Context(), invalid, after); err == nil || documents != nil {
		t.Fatal("invalid namespace membership became an empty or partial authority", err)
	}
	root, err := output.Parse("@codex-home/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	global, err := aggregate.NewDocumentAddress(target.TargetCodex, target.ScopeGlobal, root)
	if err != nil {
		t.Fatal(err)
	}
	if requiresProjectRootAuthority(commandPlan{nativePlan: &nativePlanInputs{stateDocuments: []aggregate.DocumentAddress{global}}}) {
		t.Fatal("global-only namespace acquired project-root authority")
	}
}
