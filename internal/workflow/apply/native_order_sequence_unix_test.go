//go:build unix

package apply

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/effect/execute/configrelation"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

func TestNativeOrderCompletionAndDeferralPartitionPhysicalDocuments(t *testing.T) {
	for _, completed := range []bool{false, true} {
		name := "deferred"
		if completed {
			name = "completed"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			paths := relationOrderTestPaths(t, root)
			locked := relationOrderTestLock(t, root, desiredextension.CarrierOpenCodePlugin, target.TargetOpenCode, []string{"alpha@1", "beta@1"})
			server := filepath.Join(root, ".opencode", "opencode.json")
			tui := filepath.Join(root, ".opencode", "tui.json")
			original := []byte(`{"plugin":["beta@1","foreign@1","alpha@1"]}`)
			writeRelationOrderTestFile(t, server, string(original))
			writeRelationOrderTestFile(t, tui, string(original))
			initial := relationOrderTestReconciliation(t, paths, locked, nil)
			observation, err := relationhost.ObserveOrder(relationhost.OrderInput{Paths: paths, Lockfile: locked, Constraint: locked.Locked.OrderConstraints()[0]})
			if err != nil {
				t.Fatal(err)
			}
			edits, err := observation.SelectedEdits(nil)
			if err != nil {
				t.Fatal(err)
			}
			var native relationhost.SelectedDocumentEdit
			for _, edit := range edits {
				if edit.SelectedPath() == server {
					native = edit
				}
			}
			if err := native.Validate(); err != nil {
				t.Fatal(err)
			}
			planned := applyAuthorityTestPlan(t)
			planned.context.Lockfile = locked
			planned.context.Selection, err = targetselection.ForDiagnostics([]string{string(target.TargetOpenCode)})
			if err != nil {
				t.Fatal(err)
			}
			planned.assessment = readiness.Assessment{Reconciliation: initial}
			planned.nativePlan = &nativePlanInputs{orders: []relationhost.SelectedDocumentEdit{native}}
			classes, err := admittedOrderClassFacts(planned)
			if err != nil || len(classes) != 1 || !classes[0].requiresMutation || !applyOrderScheduleFacts(classes)[0].requiresMutation {
				t.Fatal("mixed class lost its ordinary mutation reservation", classes, err)
			}
			projectRoot := captureRelationOrderTestRoot(t, root)
			defer projectRoot.Close()
			options := runOptions{
				projectRoot: projectRoot, executionGuard: testApplyExecutionGuard(t, paths),
				orderRiskBaseline: newRelationOrderRiskBaseline(initial.RelationOrders()),
			}
			prepared, err := prepareNativeOrderConvergence(t.Context(), paths, locked, initial, []relationhost.SelectedDocumentEdit{native}, options)
			if err != nil || len(prepared.results) != 1 {
				t.Fatal("native preparation required the unrelated ordinary actor", prepared, err)
			}
			image, err := relationhost.CompleteSelectedImage(t.Context(), native.Original(), configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}, nil, []relationhost.SelectedDocumentEdit{native})
			if err != nil {
				t.Fatal(err)
			}
			if completed {
				// The lower native publisher is tested separately; this fixture feeds
				// its completed image and outcome into the mixed-channel continuation.
				if err := os.WriteFile(server, image.Expected().Content(), 0o600); err != nil {
					t.Fatal(err)
				}
				recordNativeOrderEvents(&prepared, configrelation.OrderSequenceDone, true, nil, options)
				options.nativeOrderResult = &prepared
			} else {
				options.deferredNativeOrders = []relationOrderSequenceKey{selectedOrderSequenceKey(native)}
			}
			expected, err := mutation.NewPhysicalAuthoritySet(mutation.PhysicalAuthorityRequest{Path: tui, Target: string(target.TargetOpenCode), Scope: string(target.ScopeProject)})
			if err != nil {
				t.Fatal(err)
			}
			expectedNamespaces, err := expected.Namespaces()
			if err != nil || len(expectedNamespaces) == 0 {
				t.Fatal(err)
			}
			checks := 0
			options.validateBeforeEffects = func(_ context.Context, authority mutation.PhysicalAuthoritySet) error {
				checks++
				namespaces, err := authority.Namespaces()
				if err != nil || len(namespaces) != len(expectedNamespaces) {
					t.Fatal("ordinary authority included the native document", namespaces, err)
				}
				for _, expected := range expectedNamespaces {
					found := false
					for _, actual := range namespaces {
						found = found || expected.Equal(actual)
					}
					if !found {
						t.Fatal("ordinary authority did not match its selected document")
					}
				}
				return nil
			}
			result, err := runRelationOrderConvergence(t.Context(), paths, locked, initial, options)
			if err != nil || result.actionCount != 1 || checks != 1 || len(result.results) != 2 {
				t.Fatal("mixed physical sequences did not converge independently", result, checks, err)
			}
			wantServer := original
			if completed {
				wantServer = image.Expected().Content()
			}
			if !bytes.Equal(readRelationOrderTestFile(t, server), wantServer) ||
				!bytes.Equal(readRelationOrderTestFile(t, tui), image.Expected().Content()) {
				t.Fatal("ordinary continuation wrote a native document or skipped its own document")
			}
		})
	}
}

func TestNativeClasslessRetirementMatchesAnActorWithoutOrderDecision(t *testing.T) {
	fixture := newOpenCodeDirectWorkflowFixture(t, target.ScopeProject)
	paths := relationOrderTestPaths(t, fixture.root)
	edits, err := relationhost.ObserveOpenCodeRetirementEdits(paths, lock.File{}, target.ScopeProject, []durablecarrier.ManagedCarrierClaim{fixture.claim})
	if err != nil || len(edits) == 0 {
		t.Fatal(err)
	}
	result, err := reconcile.NewResult(reconcile.ResultInput{Context: reconcile.ContextApply, CarrierAbsences: []carrierabsence.Action{fixture.action}})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := prepareNativeOrderConvergence(t.Context(), paths, lock.File{}, result, edits, runOptions{})
	if err != nil || len(prepared.results) != 0 || len(prepared.reconciliation.RelationOrders()) != 0 || fixture.executorCalls != 0 {
		t.Fatal("classless actor needed a fabricated order decision or host work", prepared, err)
	}
	for _, edit := range edits {
		if !edit.Changed() {
			t.Fatal("fixture did not select its exact retiring row")
		}
	}
	writeRelationOrderTestFile(t, edits[0].SelectedPath(), `{"plugin":["changed-after-planning@1"]}`)
	if _, err := prepareNativeOrderConvergence(t.Context(), paths, lock.File{}, result, edits, runOptions{}); err == nil {
		t.Fatal("changed classless CAS evidence kept its reserved actor")
	}
}

func TestNativeExactActorRefusesDriftAndKeepsTypedObservationFailure(t *testing.T) {
	root := t.TempDir()
	paths := relationOrderTestPaths(t, root)
	locked := relationOrderTestLock(t, root, desiredextension.CarrierOpenCodePlugin, target.TargetOpenCode, []string{"alpha@1", "beta@1"})
	path := filepath.Join(root, ".opencode", "opencode.json")
	writeRelationOrderTestFile(t, path, `{"plugin":["alpha@1","beta@1"]}`)
	initial := relationOrderTestReconciliation(t, paths, locked, nil)
	observe := func() relationhost.SelectedDocumentEdit {
		t.Helper()
		observation, err := relationhost.ObserveOrder(relationhost.OrderInput{Paths: paths, Lockfile: locked, Constraint: locked.Locked.OrderConstraints()[0]})
		if err != nil {
			t.Fatal(err)
		}
		edits, err := observation.SelectedEdits(nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, edit := range edits {
			if edit.SelectedPath() == path {
				return edit
			}
		}
		t.Fatal("fixture has no selected document")
		return relationhost.SelectedDocumentEdit{}
	}
	exact := observe()
	options := runOptions{orderRiskBaseline: newRelationOrderRiskBaseline(initial.RelationOrders())}
	if err := requireNativeSelectedActors([]relationhost.SelectedDocumentEdit{exact}, nil); err != nil {
		t.Fatal("a checked exact native document invented a mutation actor", err)
	}
	writeRelationOrderTestFile(t, path, `{"plugin":["beta@1","alpha@1"]}`)
	changed := observe()
	if _, err := prepareNativeOrderConvergence(t.Context(), paths, locked, initial, []relationhost.SelectedDocumentEdit{exact}, options); err == nil {
		t.Fatal("an exact native document drifted into the ordinary channel")
	}
	if requireNativeSelectedActors([]relationhost.SelectedDocumentEdit{changed}, nil) == nil ||
		requireNativeSelectedActors(nil, []relationhost.SelectedDocumentEdit{changed}) == nil {
		t.Fatal("qualification and bound publication actor sets diverged")
	}
	if err := requireNativeSelectedActors([]relationhost.SelectedDocumentEdit{changed}, []relationhost.SelectedDocumentEdit{changed}); err != nil {
		t.Fatal("matched selected publication actor was refused", err)
	}
	writeRelationOrderTestFile(t, path, `{`)
	blocked, err := prepareNativeOrderConvergence(t.Context(), paths, locked, initial, []relationhost.SelectedDocumentEdit{exact}, options)
	if !errors.Is(err, ErrRelationOrderBlock) || strings.Contains(err.Error(), root) ||
		len(blocked.results) != 1 || blocked.results[0].Outcome() != RelationOrderNotAttempted {
		t.Fatal("native observation lost its typed, path-neutral refusal", blocked.results, err)
	}
}

func TestNativeSubsetWithoutDecisionIsNotTransferredToOrdinaryWriter(t *testing.T) {
	for _, deferred := range []bool{false, true} {
		name := "completed"
		if deferred {
			name = "deferred"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			paths := relationOrderTestPaths(t, root)
			locked := relationOrderTestLock(t, root, desiredextension.CarrierOpenCodePlugin, target.TargetOpenCode, []string{"alpha@1", "beta@1"})
			server := filepath.Join(root, ".opencode", "opencode.json")
			tui := filepath.Join(root, ".opencode", "tui.json")
			writeRelationOrderTestFile(t, server, `{"plugin":["alpha@1","beta@1"]}`)
			writeRelationOrderTestFile(t, tui, `{"plugin":["foreign@1"]}`)
			initial := relationOrderTestReconciliation(t, paths, locked, nil)
			observation, err := relationhost.ObserveOrder(relationhost.OrderInput{Paths: paths, Lockfile: locked, Constraint: locked.Locked.OrderConstraints()[0]})
			if err != nil {
				t.Fatal(err)
			}
			edits, err := observation.SelectedEdits(nil)
			if err != nil {
				t.Fatal(err)
			}
			var native relationhost.SelectedDocumentEdit
			for _, edit := range edits {
				if edit.SelectedPath() == tui {
					native = edit
				}
			}
			var serverDecisions []reconcile.RelationOrderDecision
			for _, decision := range initial.RelationOrders() {
				if decision.SequenceID() != native.Sequence().SequenceID() {
					serverDecisions = append(serverDecisions, decision)
				}
			}
			initial, err = initial.WithRelationOrders(serverDecisions)
			if err != nil {
				t.Fatal(err)
			}
			options := runOptions{orderRiskBaseline: newRelationOrderRiskBaseline(initial.RelationOrders())}
			prepared, err := prepareNativeOrderConvergence(t.Context(), paths, locked, initial, []relationhost.SelectedDocumentEdit{native}, options)
			if err != nil || len(prepared.results) != 0 {
				t.Fatal("empty native subset invented an order result", prepared, err)
			}
			if deferred {
				options.deferredNativeOrders = []relationOrderSequenceKey{selectedOrderSequenceKey(native)}
			} else {
				options.nativeOrderResult = &prepared
			}
			changed := `{"plugin":["beta@1","alpha@1"]}`
			writeRelationOrderTestFile(t, tui, changed)
			result, err := runRelationOrderConvergence(t.Context(), paths, locked, initial, options)
			if err != nil || result.actionCount != 0 || len(result.results) != 1 || string(readRelationOrderTestFile(t, tui)) != changed {
				t.Fatal("a decisionless native subset acquired an ordinary mutation owner", result, err)
			}
		})
	}
}
