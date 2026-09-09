package repair

import (
	"errors"
	"fmt"
	"testing"
)

func TestGuidanceRetainsRawDiagnosticAndCause(t *testing.T) {
	cause := errors.New("invalid skill")
	for _, test := range []struct {
		classification Classification
		want           string
	}{
		{Classification{repairability: RepairabilityManual, manualReasons: []string{"missing name", "missing description"}}, "invalid skill; repairability=manual; manual edit required: missing name; missing description"},
		{Classification{repairability: RepairabilityMechanical, actions: []string{"rename entry"}}, "invalid skill; repairability=mechanical; next: set compat_repair = true on this manifest resource and rerun daem lock; repair actions: rename entry"},
	} {
		err := WithGuidance(cause, test.classification)
		if err.Error() != test.want || !errors.Is(err, cause) {
			t.Fatalf("err=%v", err)
		}
		var guidance *GuidanceError
		if !errors.As(fmt.Errorf("outer: %w", err), &guidance) {
			t.Fatal("lost guidance")
		}
		classification := guidance.Classification()
		if reasons := classification.ManualReasons(); len(reasons) != 0 {
			reasons[0] = "changed"
		}
		if actions := classification.Actions(); len(actions) != 0 {
			actions[0] = "changed"
		}
		if err.Error() != test.want {
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
