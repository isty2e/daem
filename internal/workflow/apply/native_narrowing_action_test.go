package apply

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	lockobserve "github.com/isty2e/daem/internal/assurance/observe/lock"
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
	lockbuild "github.com/isty2e/daem/internal/realization/lock/build"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/build/documenttransition"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	sourceresolution "github.com/isty2e/daem/internal/supply/source/resolution"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

func nativeActionPreparationInput(t *testing.T, syntax configdocument.Syntax, baseline, current, fragment string, ownership hostconfig.Ownership) readiness.NativeNarrowingActionInput {
	t.Helper()
	return nativeActionPreparationInputForScope(t, syntax, baseline, current, fragment, ownership, target.ScopeGlobal)
}

func nativeActionPreparationInputForScope(t *testing.T, syntax configdocument.Syntax, baseline, current, fragment string, ownership hostconfig.Ownership, scope target.Scope) readiness.NativeNarrowingActionInput {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "fragment")
	if err := os.WriteFile(path, []byte(fragment), 0o600); err != nil {
		t.Fatal(err)
	}
	spec, err := source.NewLocalSource(path, source.LocalSourceModeVendor)
	if err != nil {
		t.Fatal(err)
	}
	resource, err := hostconfig.New(hostconfig.Spec{Name: "settings", Source: spec, Target: target.TargetPi, Scope: scope, Path: "extension-settings/test.json", Ownership: ownership, Syntax: hostconfig.Syntax(syntax.String())})
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
	file, err := lockbuild.BuildNativeDocumentsWithOptions(t.Context(), environment, resolver, lockbuild.Options{}, narrowingLimits)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := targetselection.ForAvailableTargets(environment.Targets(), []string{"pi"})
	if err != nil {
		t.Fatal(err)
	}
	epoch, err := lockobserve.ResolveNativeDocumentSourceEpoch(t.Context(), paths, environment, file, selection, narrowingLimits)
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
	state, err := durable.NewManagedPathState(subject, []target.Target{placement.Target()}, placement.Scope(), placement.Destination(), artifact.HashFileContent([]byte(baseline)), realization.PathProjectionFile, realization.PathPermissionsExact, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	return readiness.NativeNarrowingActionInput{Source: observed, Baseline: state, Current: aggregate.ExistingDocument([]byte(current)), DocumentLimits: narrowingLimits, ValueLimits: narrowingLimits}
}

func TestNativeNarrowingActionUsesActualSourceAndRetainsItsBaseline(t *testing.T) {
	for _, syntax := range []configdocument.Syntax{configdocument.JSON, configdocument.JSONC, configdocument.JSONLineComments, configdocument.TOML} {
		t.Run(syntax.String(), func(t *testing.T) {
			current, fragment := `{"a":1,"released":9}`, `{"a":2,"new":true}`
			if syntax == configdocument.JSONC || syntax == configdocument.JSONLineComments {
				current = "// keep\n" + current
			} else if syntax == configdocument.TOML {
				current, fragment = "# keep\na=1\nreleased=9\n", "a=2\nnew=true\n"
			}
			input := nativeActionPreparationInput(t, syntax, current, current, fragment, hostconfig.OwnershipKeys)
			action, err := readiness.BuildNativeNarrowingAction(t.Context(), input)
			if err != nil || action.Validate() != nil {
				t.Fatal("real key source did not prepare its narrowing action", err)
			}
			if !action.SourceContract().Equal(input.Source.SourceContract()) || !action.Baseline().Equal(input.Baseline) || !bytes.Equal(action.CurrentDocument().Content(), []byte(current)) {
				t.Fatal("narrowing action lost causal source, prior whole ownership or current image")
			}
			plan, present := action.KeyPlan()
			if !present || len(plan.Intents()) != 2 || len(action.DesiredContributions()) != 2 {
				t.Fatal("retained plan differs from the complete actual source")
			}
			selected, err := plan.Before().Selection()
			if err != nil {
				t.Fatal(err)
			}
			codec, err := nativecodec.New(t.Context(), selected.CodecContractID(), narrowingLimits, narrowingLimits)
			if err != nil {
				t.Fatal(err)
			}
			rendered, failure := codec.Render(action.CurrentDocument(), plan)
			if failure != nil {
				t.Fatal(failure)
			}
			after := narrowingParse(t, syntax, rendered.Document().Content())
			wanted := narrowingParse(t, syntax, []byte(fragment))
			for _, key := range []string{"a", "new"} {
				if !narrowingLookup(t, after, key).Equal(narrowingLookup(t, wanted, key)) {
					t.Fatal("retained values were not derived from the actual source")
				}
			}
			if !narrowingLookup(t, after, "released").Equal(narrowingLookup(t, narrowingParse(t, syntax, []byte(current)), "released")) {
				t.Fatal("narrowing plan acquired a released-key mutation")
			}
			rows := action.DesiredContributions()
			subject := rows[0].SubjectID()
			rows[0] = aggregate.SubjectContribution{}
			if action.DesiredContributions()[0].SubjectID() != subject {
				t.Fatal("returned desired rows changed the immutable action")
			}
		})
	}
}

func TestNativeNarrowingActionDistinguishesEmptyReleaseAndCaptureRequired(t *testing.T) {
	for _, test := range []struct {
		name, current, fragment string
		empty, captureRequired  bool
	}{
		{name: "empty release", current: `{"a":7,"released":9}`, fragment: `{}`, empty: true},
		{name: "already desired after drift", current: `{ "a":2,"released":9 }`, fragment: `{"a":2}`},
		{name: "capture required", current: `{"a":1,"released":9}`, fragment: `{"a":2}`, captureRequired: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := nativeActionPreparationInput(t, configdocument.JSON, `{"a":1}`, test.current, test.fragment, hostconfig.OwnershipKeys)
			action, err := readiness.BuildNativeNarrowingAction(t.Context(), input)
			if test.captureRequired {
				if !errors.Is(err, documenttransition.ErrNativeNarrowingNeedsCapture) || action.Validate() == nil {
					t.Fatal("capture-required source prepared an executable narrowing prefix", err)
				}
				return
			}
			if err != nil || action.Validate() != nil || !action.CurrentDocument().Exists() || !bytes.Equal(action.CurrentDocument().Content(), input.Current.Content()) {
				t.Fatal("scope release lost current file or its causal action", err)
			}
			_, hasPlan := action.KeyPlan()
			if hasPlan == test.empty || (len(action.DesiredContributions()) == 0) != test.empty {
				t.Fatal("empty source was confused with absent action or a deletion plan")
			}
		})
	}
}

func TestNativeNarrowingActionEmptyScopeStillRefusesUnsafeInputs(t *testing.T) {
	input := nativeActionPreparationInput(t, configdocument.JSON, `{"a":1}`, `{"local":9}`, `{}`, hostconfig.OwnershipKeys)
	for _, test := range []struct {
		name string
		edit func(*readiness.NativeNarrowingActionInput)
	}{
		{name: "missing source", edit: func(i *readiness.NativeNarrowingActionInput) { i.Source = lockobserve.NativeSourceObservation{} }},
		{name: "missing prior ownership", edit: func(i *readiness.NativeNarrowingActionInput) { i.Baseline = durable.ManagedPathState{} }},
		{name: "missing current", edit: func(i *readiness.NativeNarrowingActionInput) { i.Current = aggregate.AbsentDocument() }},
		{name: "malformed current", edit: func(i *readiness.NativeNarrowingActionInput) { i.Current = aggregate.ExistingDocument([]byte("{")) }},
		{name: "non object current", edit: func(i *readiness.NativeNarrowingActionInput) { i.Current = aggregate.ExistingDocument([]byte("[]")) }},
		{name: "missing limits", edit: func(i *readiness.NativeNarrowingActionInput) { i.DocumentLimits = configdocument.Limits{} }},
		{name: "different placement", edit: func(i *readiness.NativeNarrowingActionInput) {
			destination, err := output.Parse("@pi-agent/extension-settings/other.json")
			if err != nil {
				t.Fatal(err)
			}
			old := i.Baseline
			i.Baseline, err = durable.NewManagedPathState(old.Subject(), old.ConsumerTargets(), old.Scope(), destination, old.ContentHash(), old.ContentKind(), old.PermissionPolicy(), old.FileMode())
			if err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := input
			test.edit(&changed)
			if action, err := readiness.BuildNativeNarrowingAction(t.Context(), changed); err == nil || action.Validate() == nil {
				t.Fatal("empty release bypassed source/current/prior-ownership admission")
			}
		})
	}
	whole := nativeActionPreparationInput(t, configdocument.JSON, `{"a":1}`, `{"a":1}`, `{}`, hostconfig.OwnershipDocument)
	if action, err := readiness.BuildNativeNarrowingAction(t.Context(), whole); err == nil || action.Validate() == nil {
		t.Fatal("whole-mode source was inferred to be empty key-mode intent")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if action, err := readiness.BuildNativeNarrowingAction(ctx, input); !errors.Is(err, context.Canceled) || action.Validate() == nil {
		t.Fatal("cancellation returned a usable scope release", err)
	}
}

func TestNativeNarrowingActionCanonicalPlanMustEqualItsWholeSource(t *testing.T) {
	input := nativeActionPreparationInput(t, configdocument.JSON, `{"a":1}`, `{"a":1}`, `{"a":2}`, hostconfig.OwnershipKeys)
	action, err := readiness.BuildNativeNarrowingAction(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := action.KeyPlan()
	otherInput := nativeActionPreparationInput(t, configdocument.JSON, `{"a":1}`, `{"a":1}`, `{"a":99}`, hostconfig.OwnershipKeys)
	other, err := readiness.BuildNativeNarrowingAction(t.Context(), otherInput)
	if err != nil {
		t.Fatal(err)
	}
	otherPlan, _ := other.KeyPlan()
	remove, err := aggregate.NewProjectionIntent(plan.Before().States()[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	removal, err := aggregate.NewPlan(plan.Before(), []aggregate.ProjectionIntent{remove})
	if err != nil {
		t.Fatal(err)
	}
	for _, changed := range []reconcile.NativeNarrowingActionInput{
		{Source: input.Source.SourceContract(), Projections: input.Source.Projections(), Baseline: input.Baseline, Current: input.Current},
		{Source: input.Source.SourceContract(), Baseline: input.Baseline, Current: input.Current, Plan: &plan},
		{Source: input.Source.SourceContract(), Projections: input.Source.Projections(), Baseline: input.Baseline, Current: input.Current, Plan: &otherPlan},
		{Source: input.Source.SourceContract(), Projections: input.Source.Projections(), Baseline: input.Baseline, Current: input.Current, Plan: &removal},
	} {
		if result, err := reconcile.NewNativeNarrowingAction(changed); err == nil || result.Validate() == nil {
			t.Fatal("source/plan mismatch or deletion intent became a canonical narrowing action")
		}
	}
	if (reconcile.NativeNarrowingAction{}).Validate() == nil {
		t.Fatal("zero action carries narrowing authority")
	}
}
