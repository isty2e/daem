package hostroute

import (
	"slices"
	"testing"
)

func TestDisclosureCanonicalizesBoundedEffectFacts(t *testing.T) {
	disclosure, err := NewDisclosure(DisclosureInput{
		ExecutionSubject:      "selected relation",
		InvocationKind:        InvocationKindCommand,
		CWDPolicy:             CWDPolicySelectedRoot,
		TimeoutSeconds:        30,
		EffectClasses:         []string{"network", "cache", "network"},
		RetainedEffectClasses: []string{"old cache"},
		NonClaims:             []string{"runtime readiness", "exact artifact"},
	})
	if err != nil {
		t.Fatalf("NewDisclosure returned error: %v", err)
	}
	if !slices.Equal(disclosure.EffectClasses(), []string{"cache", "network"}) {
		t.Fatalf("effect classes = %v", disclosure.EffectClasses())
	}
	if !slices.Equal(disclosure.NonClaims(), []string{"exact artifact", "runtime readiness"}) {
		t.Fatalf("non-claims = %v", disclosure.NonClaims())
	}
}

func TestDisclosureRejectsIncompleteExecutionEnvelope(t *testing.T) {
	base := DisclosureInput{
		ExecutionSubject:      "selected relation",
		InvocationKind:        InvocationKindCommand,
		CWDPolicy:             CWDPolicySelectedRoot,
		TimeoutSeconds:        30,
		EffectClasses:         []string{"network"},
		RetainedEffectClasses: []string{"host cache"},
		NonClaims:             []string{"exact artifact"},
	}
	cases := []struct {
		name   string
		mutate func(*DisclosureInput)
	}{
		{name: "subject", mutate: func(input *DisclosureInput) { input.ExecutionSubject = "" }},
		{name: "kind", mutate: func(input *DisclosureInput) { input.InvocationKind = "shell" }},
		{name: "cwd", mutate: func(input *DisclosureInput) { input.CWDPolicy = "ambient" }},
		{name: "timeout", mutate: func(input *DisclosureInput) { input.TimeoutSeconds = 0 }},
		{name: "effects", mutate: func(input *DisclosureInput) { input.EffectClasses = nil }},
		{name: "retained", mutate: func(input *DisclosureInput) { input.RetainedEffectClasses = nil }},
		{name: "nonclaims", mutate: func(input *DisclosureInput) { input.NonClaims = nil }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			input := base
			input.EffectClasses = append([]string(nil), base.EffectClasses...)
			input.RetainedEffectClasses = append([]string(nil), base.RetainedEffectClasses...)
			input.NonClaims = append([]string(nil), base.NonClaims...)
			test.mutate(&input)
			if _, err := NewDisclosure(input); err == nil {
				t.Fatal("NewDisclosure unexpectedly succeeded")
			}
		})
	}
}
