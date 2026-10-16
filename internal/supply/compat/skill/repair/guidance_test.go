package repair

import (
	"errors"
	"fmt"
	"slices"
	"testing"
)

func TestGuidanceRetainsPrimaryCauseAndStructuredClassification(t *testing.T) {
	cause := errors.New("invalid skill")
	for _, classification := range []Classification{
		{repairability: RepairabilityManual, manualReasons: []string{"missing name", "missing description"}},
		{repairability: RepairabilityMechanical, actions: []string{"rename entry"}},
	} {
		err := WithGuidance(cause, classification)
		if err.Error() != cause.Error() || !errors.Is(err, cause) {
			t.Fatalf("err=%v", err)
		}
		var guidance *GuidanceError
		if !errors.As(fmt.Errorf("outer: %w", err), &guidance) {
			t.Fatal("lost guidance")
		}
		observed := guidance.Classification()
		if reasons := observed.ManualReasons(); len(reasons) != 0 {
			reasons[0] = "changed"
		}
		if actions := observed.Actions(); len(actions) != 0 {
			actions[0] = "changed"
		}
		if !slices.Equal(guidance.Classification().Actions(), classification.Actions()) ||
			!slices.Equal(guidance.Classification().ManualReasons(), classification.ManualReasons()) ||
			guidance.Classification().Repairability() != classification.Repairability() {
			t.Fatal("caller mutated retained classification")
		}
	}
	if WithGuidance(cause, Classification{}) != cause {
		t.Fatal("empty classification changed cause")
	}
	if WithGuidance(nil, Classification{repairability: RepairabilityManual}) != nil {
		t.Fatal("nil cause became an error")
	}
}
