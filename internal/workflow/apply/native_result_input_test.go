package apply

import (
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

func TestNativeResultInputSurvivesPlanningAndRefusesLegacyExecution(t *testing.T) {
	for _, fragment := range []string{`{}`, `{"a":2}`} {
		input := nativeActionPreparationInput(t, configdocument.JSON, `{"a":1}`, `{"a":1}`, fragment, hostconfig.OwnershipKeys)
		action, err := BuildNativeNarrowingAction(t.Context(), input)
		if err != nil {
			t.Fatal(err)
		}
		result, err := reconcile.NewResult(reconcile.ResultInput{Context: reconcile.ContextApply, NativeNarrowings: []reconcile.NativeNarrowingAction{action}})
		if err != nil {
			t.Fatal(err)
		}
		assessment := readiness.Assessment{Reconciliation: result}
		current := commandPlan{assessment: assessment}
		prepared, err := applyEffectInput(current, nil)
		if err != nil || len(prepared.NativeNarrowings) != 1 || !prepared.NativeNarrowings[0].SourceContract().Equal(action.SourceContract()) || !prepared.NativeNarrowings[0].Baseline().Equal(action.Baseline()) {
			t.Fatal("planning conversion dropped native transition intent", err)
		}
		if _, err := execute.MaximumForwardEffectValidationCount(prepared); !errors.Is(err, execute.ErrNativeOperationRequired) {
			t.Fatal("transported native work became an ordinary zero-count schedule", err)
		}
		if _, err := applyOperationFingerprint(current, reconcile.ContextApply); !errors.Is(err, execute.ErrNativeOperationRequired) {
			t.Fatal("ordinary fingerprinting accepted an incomplete native projection", err)
		}
		_, err = runWithOptions(t.Context(), daempaths.Paths{}, desired.Environment{}, lock.File{}, targetselection.Selection{}, assessment, runOptions{})
		if !errors.Is(err, execute.ErrNativeOperationRequired) {
			t.Fatal("native-only Result reached the ordinary no-effects execution path", err)
		}
	}
}
