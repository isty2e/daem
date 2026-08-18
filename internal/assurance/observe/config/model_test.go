package config

import "testing"

func TestEntryRejectsInvalidStateCombinations(t *testing.T) {
	cases := []struct {
		name string
		spec EntrySpec
	}{
		{
			name: "observed entry needs key",
			spec: EntrySpec{
				State:      EntryObserved,
				Activation: ActivationNotDeclared,
			},
		},
		{
			name: "observed entry cannot carry reason",
			spec: EntrySpec{
				Key:        "alpha@market",
				State:      EntryObserved,
				Activation: ActivationConfiguredTrue,
				Reason:     ReasonEntryNotTable,
			},
		},
		{
			name: "unsupported entry needs reason",
			spec: EntrySpec{
				Key:        "alpha@market",
				State:      EntryUnsupported,
				Activation: ActivationNotDeclared,
			},
		},
		{
			name: "empty unsupported key needs empty-key reason",
			spec: EntrySpec{
				State:      EntryUnsupported,
				Activation: ActivationNotDeclared,
				Reason:     ReasonEntryNotTable,
			},
		},
		{
			name: "activation reason needs unsupported activation",
			spec: EntrySpec{
				Key:        "alpha@market",
				State:      EntryUnsupported,
				Activation: ActivationNotDeclared,
				Reason:     ReasonActivationNotBoolean,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewEntry(tc.spec); err == nil {
				t.Fatalf("NewEntry accepted invalid spec %#v", tc.spec)
			}
		})
	}
}

func TestObservationRejectsInvalidEntrySetCombinations(t *testing.T) {
	entry := mustEntry(t, EntrySpec{
		Key:        "alpha@market",
		State:      EntryObserved,
		Activation: ActivationNotDeclared,
	})
	cases := []struct {
		name string
		spec ObservationSpec
	}{
		{
			name: "source path required",
			spec: ObservationSpec{
				ConfigExists:  true,
				EntrySetState: EntrySetObserved,
				Entries:       []Entry{entry},
			},
		},
		{
			name: "missing config cannot have unsupported entry set",
			spec: ObservationSpec{
				SourcePath:    "/tmp/config.toml",
				EntrySetState: EntrySetUnsupported,
			},
		},
		{
			name: "not declared cannot carry entries",
			spec: ObservationSpec{
				SourcePath:    "/tmp/config.toml",
				ConfigExists:  true,
				EntrySetState: EntrySetNotDeclared,
				Entries:       []Entry{entry},
			},
		},
		{
			name: "unsupported entry set cannot carry entries",
			spec: ObservationSpec{
				SourcePath:    "/tmp/config.toml",
				ConfigExists:  true,
				EntrySetState: EntrySetUnsupported,
				Entries:       []Entry{entry},
			},
		},
		{
			name: "budget-exceeded entry set cannot carry entries",
			spec: ObservationSpec{
				SourcePath:    "/tmp/config.toml",
				ConfigExists:  true,
				EntrySetState: EntrySetBudgetExceeded,
				Entries:       []Entry{entry},
			},
		},
		{
			name: "missing config cannot have budget-exceeded entry set",
			spec: ObservationSpec{
				SourcePath:    "/tmp/config.toml",
				EntrySetState: EntrySetBudgetExceeded,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewObservation(tc.spec); err == nil {
				t.Fatalf("NewObservation accepted invalid spec %#v", tc.spec)
			}
		})
	}
}

func TestObservationDefensivelyCopiesEntries(t *testing.T) {
	entry := mustEntry(t, EntrySpec{
		Key:        "alpha@market",
		State:      EntryObserved,
		Activation: ActivationNotDeclared,
	})
	entries := []Entry{entry}
	observation, err := NewObservation(ObservationSpec{
		SourcePath:    "/tmp/config.toml",
		ConfigExists:  true,
		EntrySetState: EntrySetObserved,
		Entries:       entries,
	})
	if err != nil {
		t.Fatalf("NewObservation: %v", err)
	}
	entries[0] = Entry{}
	got := observation.Entries()
	got[0] = Entry{}

	if !observation.Entries()[0].Observed() || observation.Entries()[0].Key() != "alpha@market" {
		t.Fatalf("Entries were not defensively copied")
	}
}

func mustEntry(t *testing.T, spec EntrySpec) Entry {
	t.Helper()
	entry, err := NewEntry(spec)
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}
	return entry
}
