package execute

import "testing"

func TestAggregateEffectTransitionLaw(t *testing.T) {
	for _, test := range []struct {
		name            string
		kind            AggregateEffectKind
		beforePresent   bool
		expectedPresent bool
		wantError       bool
	}{
		{name: "create", kind: AggregateEffectCreate, expectedPresent: true},
		{name: "replace", kind: AggregateEffectReplace, beforePresent: true, expectedPresent: true},
		{name: "record", kind: AggregateEffectRecord, beforePresent: true, expectedPresent: true},
		{name: "remove", kind: AggregateEffectRemove, beforePresent: true},
		{name: "create over present projection", kind: AggregateEffectCreate, beforePresent: true, expectedPresent: true, wantError: true},
		{name: "replace missing projection", kind: AggregateEffectReplace, expectedPresent: true, wantError: true},
		{name: "record missing projection", kind: AggregateEffectRecord, expectedPresent: true, wantError: true},
		{name: "remove missing projection", kind: AggregateEffectRemove, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateAggregateEffectTransition(test.kind, test.beforePresent, test.expectedPresent)
			if (err != nil) != test.wantError {
				t.Fatalf("validateAggregateEffectTransition(%q, %t, %t) error = %v", test.kind, test.beforePresent, test.expectedPresent, err)
			}
		})
	}
}
