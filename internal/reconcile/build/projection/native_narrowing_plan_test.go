package projection

import (
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func TestNativeNarrowingPlanRequiresUnchangedDocumentOrAllDesiredValues(t *testing.T) {
	for _, test := range []struct {
		name, current string
		unchanged     bool
		capture       bool
	}{
		{name: "unchanged permits changes", current: `{"a":1,"z":1}`, unchanged: true},
		{name: "desired values need no history", current: `{"a":2,"z":2}`},
		{name: "changed file does not prove old values", current: `{"a":1,"z":1}`, capture: true},
		{name: "desired then different", current: `{"a":2,"z":1}`, capture: true},
		{name: "different then desired", current: `{"a":1,"z":2}`, capture: true},
		{name: "missing without proof", current: `{"a":2}`, capture: true},
		{name: "unchanged permits creation", current: `{}`, unchanged: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			current, desired := nativeNarrowingPlanFixture(t, test.current, "@pi-agent/extension-settings/test.json")
			plan, err := BuildNativeNarrowingPlan(current, desired, test.unchanged)
			if test.capture {
				if !errors.Is(err, ErrNativeNarrowingNeedsCapture) {
					t.Fatalf("unproved value change did not request capture: %v", err)
				}
				if _, err := plan.Before().Selection(); err == nil {
					t.Fatal("refusal returned a usable partial plan")
				}
				return
			}
			if err != nil || len(plan.Intents()) != 2 {
				t.Fatalf("valid narrowing refused: %v", err)
			}
			for _, intent := range plan.Intents() {
				if _, present := intent.Desired(); !present {
					t.Fatal("narrowing manufactured a removal intent")
				}
			}
		})
	}
}

func TestNativeNarrowingPlanRefusesInvalidOrUncorrelatedInputs(t *testing.T) {
	current, desired := nativeNarrowingPlanFixture(t, `{"a":2,"z":2}`, "@pi-agent/extension-settings/test.json")
	_, foreign := nativeNarrowingPlanFixture(t, `{"a":2,"z":2}`, "@pi-agent/extension-settings/other.json")
	for _, test := range []struct {
		name    string
		current aggregate.Snapshot
		desired []aggregate.ContributionSet
	}{
		{name: "zero snapshot", desired: desired},
		{name: "missing desired", current: current},
		{name: "duplicate desired", current: current, desired: []aggregate.ContributionSet{desired[0], desired[0]}},
		{name: "foreign desired", current: current, desired: foreign},
		{name: "invalid desired", current: current, desired: []aggregate.ContributionSet{{}, desired[1]}},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan, err := BuildNativeNarrowingPlan(test.current, test.desired, true)
			if err == nil {
				t.Fatal("unchanged-document fact bypassed input correlation")
			}
			if _, err := plan.Before().Selection(); err == nil {
				t.Fatal("invalid input exposed a partial plan")
			}
		})
	}
}

func nativeNarrowingPlanFixture(t *testing.T, currentText, destination string) (aggregate.Snapshot, []aggregate.ContributionSet) {
	t.Helper()
	root, err := output.Parse(destination)
	if err != nil {
		t.Fatal(err)
	}
	codecID, _ := aggregate.HostConfigCodecForSyntax("json")
	limits := configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64}
	codec, err := nativecodec.New(t.Context(), codecID, limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	id, err := entity.New(entity.KindHostConfig, "settings")
	if err != nil {
		t.Fatal(err)
	}
	var contracts []aggregate.ProjectionContract
	for _, name := range []string{"a", "z"} {
		key, err := output.NewKeyPath(name)
		if err != nil {
			t.Fatal(err)
		}
		footprint, err := output.NewKeyFootprint(key)
		if err != nil {
			t.Fatal(err)
		}
		contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeGlobal, root, footprint, codecID)
		if err != nil {
			t.Fatal(err)
		}
		contracts = append(contracts, contract)
	}
	selection, err := aggregate.NewSelection(contracts)
	if err != nil {
		t.Fatal(err)
	}
	current, failure := codec.Read(aggregate.ExistingDocument([]byte(currentText)), selection)
	if failure != nil {
		t.Fatal(failure)
	}
	want, failure := codec.Read(aggregate.ExistingDocument([]byte(`{"a":2,"z":2}`)), selection)
	if failure != nil {
		t.Fatal(failure)
	}
	var desired []aggregate.ContributionSet
	for _, state := range want.States() {
		body, err := aggregate.NewManagedContribution(state.Contract(), state.CanonicalProjection())
		if err != nil {
			t.Fatal(err)
		}
		subject, err := topologyprojection.HostConfigKeySubject(id, target.TargetPi, target.ScopeGlobal, state.Contract().Address().KeyPath())
		if err != nil {
			t.Fatal(err)
		}
		item, err := aggregate.NewSubjectContribution(subject, body)
		if err != nil {
			t.Fatal(err)
		}
		set, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{item})
		if err != nil {
			t.Fatal(err)
		}
		desired = append(desired, set)
	}
	return current, desired
}
