package apply_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"

	"github.com/isty2e/daem/internal/assurance/durable"
	lockobserve "github.com/isty2e/daem/internal/assurance/observe/lock"
	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	lockbuild "github.com/isty2e/daem/internal/realization/lock/build"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	sourceresolution "github.com/isty2e/daem/internal/supply/source/resolution"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/workflow/readiness"
	statusworkflow "github.com/isty2e/daem/internal/workflow/status"
)

func nativeTransportAction(t *testing.T, name, fragment string) reconcile.NativeNarrowingAction {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "fragment.json")
	if err := os.WriteFile(path, []byte(fragment), 0o600); err != nil {
		t.Fatal(err)
	}
	spec, err := source.NewLocalSource(path, source.LocalSourceModeVendor)
	if err != nil {
		t.Fatal(err)
	}
	resource, err := hostconfig.New(hostconfig.Spec{Name: name, Source: spec, Target: target.TargetPi, Scope: target.ScopeGlobal, Path: "extension-settings/" + name + ".json", Ownership: hostconfig.OwnershipKeys, Syntax: hostconfig.SyntaxJSON})
	if err != nil {
		t.Fatal(err)
	}
	defaults, err := desired.NewDefaults(target.ScopeProject, desiredskill.InstallModeCopy)
	if err != nil {
		t.Fatal(err)
	}
	environment, err := desired.New(desired.Spec{Targets: []target.Target{target.TargetPi}, Defaults: defaults, HostConfigs: []hostconfig.Config{resource}})
	if err != nil {
		t.Fatal(err)
	}
	paths, err := daempaths.Resolve(filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := sourceresolution.NewResolver(paths)
	if err != nil {
		t.Fatal(err)
	}
	limits := configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64}
	locked, err := lockbuild.BuildNativeDocumentsWithOptions(t.Context(), environment, resolver, lockbuild.Options{}, limits)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := targetselection.ForAvailableTargets(environment.Targets(), []string{"pi"})
	if err != nil {
		t.Fatal(err)
	}
	epoch, err := lockobserve.ResolveNativeDocumentSourceEpoch(t.Context(), paths, environment, locked, selection, limits)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := epoch.NativeSource(t.Context(), resource)
	if err != nil {
		t.Fatal(err)
	}
	placement, _ := observed.SourceContract().HostConfigPlacement()
	subject, err := aggregate.HostConfigDocumentSubject(resource.ID(), placement.Target(), placement.Scope())
	if err != nil {
		t.Fatal(err)
	}
	current := []byte(`{"private-key-marker":"actual-secret-marker"}`)
	baseline, err := durable.NewManagedPathState(subject, []target.Target{placement.Target()}, placement.Scope(), placement.Destination(), artifact.HashFileContent(current), realization.PathProjectionFile, realization.PathPermissionsExact, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	action, err := readiness.BuildNativeNarrowingAction(t.Context(), readiness.NativeNarrowingActionInput{
		Source: observed, Baseline: baseline, Current: aggregate.ExistingDocument(current), DocumentLimits: limits, ValueLimits: limits,
	})
	if err != nil {
		t.Fatal(err)
	}
	return action
}

func assertNativeTransportPreserved(t *testing.T, actual, expected reconcile.NativeNarrowingAction) {
	t.Helper()
	if actual.Validate() != nil || !actual.SourceContract().Equal(expected.SourceContract()) || !actual.Baseline().Equal(expected.Baseline()) || !bytes.Equal(actual.CurrentDocument().Content(), expected.CurrentDocument().Content()) {
		t.Fatal("transport lost the source, baseline or current document relation")
	}
	actualPlan, actualPresent := actual.KeyPlan()
	expectedPlan, expectedPresent := expected.KeyPlan()
	if actualPresent != expectedPresent || actualPresent && !actualPlan.Before().Equal(expectedPlan.Before()) {
		t.Fatal("transport changed the selected before/Plan distinction")
	}
	actualItems, expectedItems := actual.DesiredContributions(), expected.DesiredContributions()
	if len(actualItems) != len(expectedItems) {
		t.Fatal("transport lost desired key membership")
	}
	for index, item := range actualItems {
		if item.SubjectID() != expectedItems[index].SubjectID() || !item.Contribution().Equal(expectedItems[index].Contribution()) {
			t.Fatal("transport changed the admitted desired key facts")
		}
	}
}

func TestNativeResultTransportPreservesTransitionFamily(t *testing.T) {
	for _, fragment := range []string{`{}`, `{"private-key-marker":"changed-secret-marker"}`} {
		action := nativeTransportAction(t, "settings", fragment)
		for _, context := range []reconcile.OperationContext{reconcile.ContextInspect, reconcile.ContextDryRun, reconcile.ContextApply} {
			original, err := reconcile.NewResult(reconcile.ResultInput{Context: context, NativeNarrowings: []reconcile.NativeNarrowingAction{action}})
			if err != nil {
				t.Fatal(err)
			}
			withOrders, err := original.WithRelationOrders(nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, result := range []reconcile.Result{original, original.Clone(), withOrders} {
				if result.DecisionCount() != 1 || result.ProjectionDecisionCount() != 0 || len(result.Decisions()) != 0 || len(result.NativeNarrowings()) != 1 || result.HasErrors() {
					t.Fatal("ownership transition was lost or misrepresented as a projection row")
				}
				assertNativeTransportPreserved(t, result.NativeNarrowings()[0], action)
				copied := result.NativeNarrowings()
				copied[0] = reconcile.NativeNarrowingAction{}
				assertNativeTransportPreserved(t, result.NativeNarrowings()[0], action)
				if (statusworkflow.CommandResult{Reconciliation: result}).CheckClean() {
					t.Fatal("native-only pending work was classified as a clean status")
				}
			}
		}
	}
}

func TestNativeResultTransportRejectsDuplicatesAndWholeConflicts(t *testing.T) {
	a := nativeTransportAction(t, "alpha", `{}`)
	b := nativeTransportAction(t, "beta", `{}`)
	result, err := reconcile.NewResult(reconcile.ResultInput{Context: reconcile.ContextInspect, NativeNarrowings: []reconcile.NativeNarrowingAction{b, a}})
	if err != nil || len(result.NativeNarrowings()) != 2 {
		t.Fatal(err)
	}
	if result.NativeNarrowings()[0].SourceContract().EntityID().Name() != "alpha" {
		t.Fatal("native family order depended on producer order")
	}
	for _, actions := range [][]reconcile.NativeNarrowingAction{{a, a}, {a, {}}} {
		if result, err := reconcile.NewResult(reconcile.ResultInput{Context: reconcile.ContextInspect, NativeNarrowings: actions}); err == nil || result.DecisionCount() != 0 {
			t.Fatal("duplicate or invalid native action returned a partial Result")
		}
	}
	baseline := a.Baseline()
	remove, err := reconcile.NewManagedPathDecision(reconcile.ManagedPathDecisionInput{
		Kind: reconcile.ManagedPathRemove, Subject: baseline.Subject(), Scope: baseline.Scope(), Destination: baseline.Destination(),
		ContentKind: baseline.ContentKind(), PermissionPolicy: baseline.PermissionPolicy(), LiveHash: baseline.ContentHash(),
		LiveFileMode: baseline.FileMode(), Previous: &baseline, Reason: reconcile.ReasonRemovedFromManifest,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := reconcile.NewResult(reconcile.ResultInput{Context: reconcile.ContextApply, NativeNarrowings: []reconcile.NativeNarrowingAction{a}, ManagedPaths: []reconcile.ManagedPathDecision{remove}}); err == nil || result.DecisionCount() != 0 {
		t.Fatal("narrowing and whole-file deletion shared the same baseline")
	}
}

func TestNativeResultTransportProjectsBoundedMetadata(t *testing.T) {
	for _, fragment := range []string{`{}`, `{"private-key-marker":"changed-secret-marker"}`} {
		action := nativeTransportAction(t, "settings", fragment)
		result, err := reconcile.NewResult(reconcile.ResultInput{Context: reconcile.ContextInspect, NativeNarrowings: []reconcile.NativeNarrowingAction{action}})
		if err != nil {
			t.Fatal(err)
		}
		clean := (statusworkflow.CommandResult{Reconciliation: result}).CheckClean()
		var plan, applied bytes.Buffer
		if err := clipresent.PrintPlanJSON(&plan, clipresent.PlanJSONInput{Command: "status", Mode: "read", Reconciliation: result, CheckClean: &clean}); err != nil {
			t.Fatal(err)
		}
		if err := clipresent.PrintApplyResultJSON(&applied, clipresent.ApplyResultJSONInput{Reconciliation: result}); err != nil {
			t.Fatal(err)
		}
		for index, content := range [][]byte{plan.Bytes(), applied.Bytes()} {
			var payload struct {
				SchemaVersion int               `json:"schema_version"`
				Actions       []json.RawMessage `json:"actions"`
				Native        []struct {
					Name        string `json:"name"`
					Target      string `json:"target"`
					Scope       string `json:"scope"`
					Destination string `json:"destination"`
					KeyCount    int    `json:"desired_key_count"`
					Omitted     bool   `json:"values_omitted"`
				} `json:"native_narrowing_actions"`
			}
			if err := json.Unmarshal(content, &payload); err != nil {
				t.Fatal(err)
			}
			if payload.SchemaVersion != []int{18, 24}[index] || len(payload.Actions) != 0 || len(payload.Native) != 1 || payload.Native[0].Name != "settings" || payload.Native[0].Target != "pi" || payload.Native[0].Scope != "global" || payload.Native[0].Destination != action.Baseline().Destination().String() || payload.Native[0].KeyCount != len(action.DesiredContributions()) || !payload.Native[0].Omitted {
				t.Fatal("native output lost transition metadata or used an incompatible envelope")
			}
			for _, private := range []string{"private-key-marker", "actual-secret-marker", "changed-secret-marker", "fragment.json"} {
				if bytes.Contains(content, []byte(private)) {
					t.Fatal("native presentation exposed private values, selectors or source paths")
				}
			}
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(content, &raw); err != nil || !bytes.Contains(raw["native_narrowing_actions"], []byte(`"desired_key_count"`)) {
				t.Fatal("zero key intent was indistinguishable from omitted membership", err)
			}
		}
		for _, verbose := range []bool{false, true} {
			var human, results bytes.Buffer
			clipresent.PrintStatusPlanWithOptions(&human, clipresent.HumanPlanInput{Reconciliation: result}, clipresent.HumanOptions{Verbose: verbose})
			clipresent.PrintPlanResultWithOptions(&results, result, clipresent.HumanOptions{Verbose: verbose})
			for _, content := range []string{human.String(), results.String()} {
				if !strings.Contains(content, "document -> keys") || !strings.Contains(content, "values omitted") || strings.Contains(content, "secret-marker") || strings.Contains(content, "private-key-marker") {
					t.Fatal("human output omitted the transition or exposed native content")
				}
			}
		}
	}
}

func TestNativeResultTransportLegacyFrontiersRefuseBeforeEffects(t *testing.T) {
	for _, fragment := range []string{`{}`, `{"private-key-marker":"changed-secret-marker"}`} {
		action := nativeTransportAction(t, "settings", fragment)
		result, err := reconcile.NewResult(reconcile.ResultInput{Context: reconcile.ContextApply, NativeNarrowings: []reconcile.NativeNarrowingAction{action}})
		if err != nil {
			t.Fatal(err)
		}
		for _, guard := range []func(reconcile.Result) error{execute.RejectUnsupportedActions, execute.RejectUnsupportedExecutableActions} {
			if err := guard(result); !errors.Is(err, effectpreparation.ErrNativeOperationRequired) {
				t.Fatal("ordinary result admission silently accepted native work", err)
			}
		}
		root := t.TempDir()
		calls := 0
		input := execute.ApplyInput{
			NativeNarrowings: result.NativeNarrowings(), Paths: execute.Paths{StatefilePath: filepath.Join(root, "state.json"), RecoveryDir: filepath.Join(root, "recovery")},
			Resolver: func(output.Destination, rootedpath.PhysicalTraversalBudget) (string, error) {
				calls++
				return "", errors.New("unexpected resolution")
			},
		}
		if _, err := execute.MaximumForwardEffectValidationCount(input); !errors.Is(err, effectpreparation.ErrNativeOperationRequired) {
			t.Fatal("native work became a zero-count ordinary plan", err)
		}
		if _, err := execute.PrepareApplyEffectPlan(input); !errors.Is(err, effectpreparation.ErrNativeOperationRequired) {
			t.Fatal("native work became a usable ordinary structural plan", err)
		}
		ordinary, err := execute.PrepareApplyEffectPlan(execute.ApplyInput{})
		if err != nil {
			t.Fatal(err)
		}
		for _, apply := range []func(context.Context, execute.ApplyInput, execute.ApplyOptions) (execute.ApplyResult, error){
			execute.ApplyWithOptions,
			func(ctx context.Context, input execute.ApplyInput, options execute.ApplyOptions) (execute.ApplyResult, error) {
				return execute.ApplyWithEffectPlan(ctx, input, ordinary, options)
			},
		} {
			actual, err := apply(t.Context(), input, execute.ApplyOptions{})
			if !errors.Is(err, effectpreparation.ErrNativeOperationRequired) || actual.ExecutionAttempted {
				t.Fatal("native work reached the ordinary no-change execution branch", err)
			}
		}
		entries, err := os.ReadDir(root)
		if err != nil || len(entries) != 0 || calls != 0 {
			t.Fatal("native legacy refusal crossed a filesystem boundary", err)
		}
	}
}
