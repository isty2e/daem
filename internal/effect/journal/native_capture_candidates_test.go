package journal

import (
	"fmt"
	"os"
	"testing"

	journalobservation "github.com/isty2e/daem/internal/effect/journal/observation"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestNativeCaptureCandidatesAdmitExactCombinedDebtAfterRealReads(t *testing.T) {
	paths := &nativeCapturePathCounter{}
	budget := physicalwork.NewCaptureBudget(paths)
	whole := wholeGroupFixture(t, target.TargetCodex, target.ScopeProject, "remove", []byte("original\n"), nil)
	first := prepareWholeCaptureCandidate(t, &whole, budget)
	selected := missingSelectedCapture(t)
	second := prepareSelectedCaptureCandidate(t, &selected, budget)
	if whole.reader.imageReads != 1 || selected.reader.imageReads != 1 || paths.calls == 0 || budget.AdmitObservation() == nil {
		t.Fatal("actual image/namespace preparation did not remain before removal admission")
	}
	remaining := budget.RemainingBytes()
	native, err := first.comparison.RemovalDemands(t.Context(), first.relations)
	if err != nil {
		t.Fatal(err)
	}
	destination, _ := output.Parse("old/ordinary")
	ordinaryDemand, err := recovery.NewRemovalDemand(target.ScopeProject, destination, native.Demands()[0].States())
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := recovery.NewRemovalDemandSet([]recovery.RemovalDemand{ordinaryDemand})
	if err != nil {
		t.Fatal(err)
	}
	groups, comparisons, complete, err := captureNativeDocumentCandidates(t.Context(), []nativeDocumentCaptureCandidate{first, second}, ordinary, budget)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 || len(comparisons) != 2 || complete.Len() != 3 || groups[0].Removal == nil || groups[1].Removal == nil {
		t.Fatal("capture lost a native/ordinary logical relation or exposed incomplete facts")
	}
	if budget.RemainingBytes() > remaining {
		t.Fatal("complete admission replenished actual preimage read work")
	}
	content, err := os.ReadFile(whole.path)
	if err != nil || string(content) != "original\n" {
		t.Fatal("capture changed the existing host document", err)
	}
	if _, err := os.Stat(selected.path); !os.IsNotExist(err) {
		t.Fatal("capture published the candidate new host document", err)
	}
}

func TestNativeCaptureCandidatesDoNotInventRemovalForRetainedRoots(t *testing.T) {
	budget := physicalwork.NewCaptureBudget(nil)
	whole := wholeGroupFixture(t, target.TargetCodex, target.ScopeProject, "record", []byte("unchanged\n"), nil)
	first := prepareWholeCaptureCandidate(t, &whole, budget)
	fixture := nativeJournalFixture(t)
	before := fixture.before.ManagedAggregates()[0]
	fixture.after = durable.EmptySnapshot()
	original := aggregate.ExistingDocument([]byte(`{"model":true,"foreign":1}`))
	edits := selectedCaptureEdits(t, fixture, original, []aggregate.ProjectionContract{before.Contribution().Contract()}, nil)
	selected := selectedCaptureFixture(t, fixture, original, edits, nil, journalnamespace.Roots{})
	second := prepareSelectedCaptureCandidate(t, &selected, budget)
	groups, comparisons, demands, err := captureNativeDocumentCandidates(t.Context(), []nativeDocumentCaptureCandidate{first, second}, recovery.RemovalDemandSet{}, budget)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 || len(comparisons) != 2 || demands.Len() != 0 || groups[0].Removal != nil || groups[1].Removal != nil || !comparisons[1].ExpectedDocument().Exists() {
		t.Fatal("retained root or logical key release gained physical removal debt")
	}
	if budget.AdmitRemovalDemands(recovery.RemovalDemandSet{}) == nil {
		t.Fatal("zero-debt batch left admission open")
	}
}

func TestNativeCaptureCandidatesRefuseWholeUnionBeforeRemovalObservation(t *testing.T) {
	for _, failure := range []string{"duplicate", "over-limit", "invalid-later", "different-budget"} {
		t.Run(failure, func(t *testing.T) {
			paths := &nativeCapturePathCounter{}
			budget := physicalwork.NewCaptureBudget(paths)
			fixture := wholeGroupFixture(t, target.TargetCodex, target.ScopeProject, "remove", []byte("original\n"), nil)
			candidate := prepareWholeCaptureCandidate(t, &fixture, budget)
			candidates := []nativeDocumentCaptureCandidate{candidate}
			ordinary := recovery.RemovalDemandSet{}
			switch failure {
			case "duplicate":
				var err error
				ordinary, err = candidate.comparison.RemovalDemands(t.Context(), candidate.relations)
				if err != nil {
					t.Fatal(err)
				}
			case "over-limit":
				native, err := candidate.comparison.RemovalDemands(t.Context(), candidate.relations)
				if err != nil {
					t.Fatal(err)
				}
				var demands []recovery.RemovalDemand
				for index := range recovery.MaximumRemovalIntents {
					destination, _ := output.Parse(fmt.Sprintf("ordinary/%d", index))
					demand, err := recovery.NewRemovalDemand(target.ScopeProject, destination, native.Demands()[0].States())
					if err != nil {
						t.Fatal(err)
					}
					demands = append(demands, demand)
				}
				ordinary, err = recovery.NewRemovalDemandSet(demands)
				if err != nil {
					t.Fatal(err)
				}
			case "invalid-later":
				bad := candidate
				bad.comparison = journaldocument.Comparison{}
				candidates = append(candidates, bad)
			case "different-budget":
				budget = physicalwork.NewCaptureBudget(paths)
			}
			before := paths.calls
			groups, comparisons, demands, err := captureNativeDocumentCandidates(t.Context(), candidates, ordinary, budget)
			if err == nil || groups != nil || comparisons != nil || demands.Len() != 0 || paths.calls != before {
				t.Fatal("invalid complete union reached removal observation or returned prefix facts", err)
			}
			if budget.AdmitObservation() == nil {
				t.Fatal("failed complete union admitted prefix cardinality")
			}
			if err := budget.AdmitRemovalDemands(recovery.RemovalDemandSet{}); err != nil {
				t.Fatal("failed complete union consumed admission, including an empty prefix", err)
			}
		})
	}
}

func TestNativeCaptureCandidatesDiscardPrefixAfterLaterNamespaceFailure(t *testing.T) {
	budget := physicalwork.NewCaptureBudget(nil)
	whole := wholeGroupFixture(t, target.TargetCodex, target.ScopeProject, "remove", []byte("original\n"), nil)
	first := prepareWholeCaptureCandidate(t, &whole, budget)
	selected := missingSelectedCapture(t)
	second := prepareSelectedCaptureCandidate(t, &selected, budget)
	if err := selected.roots.Manifest.Root.Close(); err != nil {
		t.Fatal(err)
	}
	groups, comparisons, demands, err := captureNativeDocumentCandidates(t.Context(), []nativeDocumentCaptureCandidate{first, second}, recovery.RemovalDemandSet{}, budget)
	if err == nil || groups != nil || comparisons != nil || demands.Len() != 0 {
		t.Fatal("later qualified-root failure returned a completed prefix", err)
	}
}

func prepareWholeCaptureCandidate(t *testing.T, fixture *wholeGroupTestFixture, budget *physicalwork.Budget) nativeDocumentCaptureCandidate {
	t.Helper()
	var err error
	fixture.namespace, err = journalnamespace.Observe(t.Context(), fixture.namespace.Addresses(), fixture.roots, fixture.reader, budget)
	if err != nil {
		t.Fatal(err)
	}
	fixture.input.CaptureInput = reviseCaptureInput(fixture.input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) { spec.Budget = budget })
	candidate, err := prepareWholeDocumentCandidate(t.Context(), fixture.namespace, fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	return candidate
}

func prepareSelectedCaptureCandidate(t *testing.T, fixture *selectedGroupCaptureFixture, budget *physicalwork.Budget) nativeDocumentCaptureCandidate {
	t.Helper()
	var err error
	fixture.namespace, err = journalnamespace.Observe(t.Context(), fixture.namespace.Addresses(), fixture.roots, fixture.reader, budget)
	if err != nil {
		t.Fatal(err)
	}
	fixture.input.CaptureInput = reviseCaptureInput(fixture.input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) { spec.Budget = budget })
	candidate, err := prepareSelectedDocumentCandidate(t.Context(), fixture.namespace, fixture.input, fixture.subjects)
	if err != nil {
		t.Fatal(err)
	}
	return candidate
}

func missingSelectedCapture(t *testing.T) selectedGroupCaptureFixture {
	t.Helper()
	fixture := nativeJournalFixture(t)
	fixture.before = durable.EmptySnapshot()
	selected := fixture.after.ManagedAggregates()[0]
	original := aggregate.AbsentDocument()
	edits := selectedCaptureEdits(t, fixture, original, []aggregate.ProjectionContract{selected.Contribution().Contract()}, []durable.ManagedAggregateState{selected})
	return selectedCaptureFixture(t, fixture, original, edits, nil, journalnamespace.Roots{})
}

type nativeCapturePathCounter struct{ calls int }

func (counter *nativeCapturePathCounter) AdmitPathComponents(int) error {
	counter.calls++
	return nil
}
