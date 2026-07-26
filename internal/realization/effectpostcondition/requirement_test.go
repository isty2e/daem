package effectpostcondition

import "testing"

func TestSetValidatesClosedCanonicalRequirements(t *testing.T) {
	set, err := NewSet([]Requirement{LocalSourceUnchanged, CarrierArtifactsAbsent})
	if err != nil {
		t.Fatalf("NewSet returned error: %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if set.Empty() {
		t.Fatal("non-empty set reported empty")
	}
	requirements := set.Requirements()
	requirements[0] = Requirement("forged")
	if got := set.Requirements(); len(got) != 2 ||
		got[0] != CarrierArtifactsAbsent ||
		got[1] != LocalSourceUnchanged {
		t.Fatalf("caller mutation changed set: %#v", got)
	}
}

func TestSetRejectsUnknownAndDuplicateRequirements(t *testing.T) {
	tests := []struct {
		name   string
		values []Requirement
	}{
		{name: "unknown", values: []Requirement{"host_private_path_absent"}},
		{
			name: "duplicate",
			values: []Requirement{
				CarrierArtifactsAbsent,
				CarrierArtifactsAbsent,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewSet(test.values); err == nil {
				t.Fatalf("NewSet(%#v) succeeded", test.values)
			}
		})
	}
}

func TestZeroSetIsExplicitlyEmptyAndValid(t *testing.T) {
	var set Set
	if !set.Empty() {
		t.Fatal("zero set is not empty")
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("zero set Validate returned error: %v", err)
	}
	constructed, err := NewSet(nil)
	if err != nil {
		t.Fatalf("NewSet(nil) returned error: %v", err)
	}
	if !set.Equal(constructed) {
		t.Fatal("zero set differs from constructed empty set")
	}
}
