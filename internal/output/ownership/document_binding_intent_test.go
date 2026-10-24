package ownership

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/output"
)

func TestDocumentBindingIntentExactVariantsAndProvenance(t *testing.T) {
	root := t.TempDir()
	binding := testDocumentBinding(t, root, "owner", ".pi/settings.json", filepath.Join(root, "settings.json"))
	present := mustBindingValue(t, binding)
	caller, err := stateauthority.New(binding.Owner().StatefileAuthority(), filepath.Join(root, "alias.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name          string
		before, after DocumentBindingValue
		changes       bool
	}{
		{"acquire", NoDocumentBinding(), present, true},
		{"retain", present, present, false},
		{"retire", present, NoDocumentBinding(), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			intent, err := NewExactDocumentBindingIntent(caller, binding.Destination(), test.before, test.after)
			if err != nil {
				t.Fatal(err)
			}
			if !intent.Owner().Equal(caller) || !intent.Owner().ExactEqual(binding.Owner()) || intent.Destination() != binding.Destination() || !intent.Before().Equal(test.before) {
				t.Fatal("intent changed logical identity or its actual before record")
			}
			after, known := intent.KnownAfter()
			if !known || !after.Equal(test.after) {
				t.Fatal("exact or absent target became unresolved")
			}
			if _, pending := intent.ProvisionalAfter(); pending {
				t.Fatal("exact intent fabricated a provisional target")
			}
			if changed, err := intent.ChangesBinding(); err != nil || changed != test.changes {
				t.Fatal("intent confused retained identity with binding publication")
			}
			copy, err := NewExactDocumentBindingIntent(caller, binding.Destination(), test.before, test.after)
			if err != nil || !intent.Equal(copy) {
				t.Fatal("equivalent canonical intent changed equality")
			}
			if _, err := intent.Promote(binding.PathAuthority()); err == nil {
				t.Fatal("exact/absent intent became a provisional promotion substitute")
			}
			if actual, found := after.Get(); found && !actual.Owner().ExactEqual(binding.Owner()) {
				t.Fatal("caller manifest alias refreshed the stored record provenance")
			}
		})
	}
}

func TestDocumentBindingIntentRefusesInvalidAndRetargetedFacts(t *testing.T) {
	root := t.TempDir()
	binding := testDocumentBinding(t, root, "owner", ".pi/settings.json", filepath.Join(root, "settings.json"))
	present := mustBindingValue(t, binding)
	foreign := testDocumentBinding(t, root, "foreign", ".pi/settings.json", filepath.Join(root, "settings.json"))
	otherDestination := testDocumentBinding(t, root, "owner", ".pi/other.json", filepath.Join(root, "settings.json"))
	otherNamespace := testDocumentBinding(t, root, "owner", ".pi/settings.json", filepath.Join(root, "other.json"))
	aliasOwner, err := stateauthority.New(binding.Owner().StatefileAuthority(), filepath.Join(root, "alias.toml"))
	if err != nil {
		t.Fatal(err)
	}
	aliasRecord, err := NewDocumentBinding(aliasOwner, binding.Destination(), binding.PathAuthority())
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name          string
		owner         stateauthority.Authority
		destination   output.Destination
		before, after DocumentBindingValue
	}{
		{"zero owner", stateauthority.Authority{}, binding.Destination(), NoDocumentBinding(), present},
		{"zero destination", binding.Owner(), output.Destination{}, NoDocumentBinding(), present},
		{"absence to absence", binding.Owner(), binding.Destination(), NoDocumentBinding(), NoDocumentBinding()},
		{"foreign before", binding.Owner(), binding.Destination(), mustBindingValue(t, foreign), NoDocumentBinding()},
		{"foreign after", binding.Owner(), binding.Destination(), NoDocumentBinding(), mustBindingValue(t, foreign)},
		{"different destination", binding.Owner(), binding.Destination(), NoDocumentBinding(), mustBindingValue(t, otherDestination)},
		{"implicit rebind", binding.Owner(), binding.Destination(), present, mustBindingValue(t, otherNamespace)},
		{"provenance refresh", binding.Owner(), binding.Destination(), present, mustBindingValue(t, aliasRecord)},
	} {
		t.Run(test.name, func(t *testing.T) {
			intent, err := NewExactDocumentBindingIntent(test.owner, test.destination, test.before, test.after)
			if err == nil || intent.Validate() == nil {
				t.Fatal("invalid intent returned an admitted value")
			}
		})
	}

	var zero DocumentBindingIntent
	if zero.Validate() == nil || zero.Equal(zero) {
		t.Fatal("zero intent became comparison evidence")
	}
	if _, known := zero.KnownAfter(); known {
		t.Fatal("uninitialized intent became a known absent target")
	}
	if _, pending := zero.ProvisionalAfter(); pending {
		t.Fatal("uninitialized intent became a provisional target")
	}
	if _, err := zero.ChangesBinding(); err == nil {
		t.Fatal("invalid intent silently became a no-op publication")
	}
	if _, err := NewProvisionalDocumentBindingIntent(binding.Owner(), binding.Destination(), pathauthority.Provisional{}); err == nil {
		t.Fatal("missing provisional observation was admitted")
	}
}

func TestDocumentBindingIntentPromotesOnlyBoundedPendingTargets(t *testing.T) {
	root := t.TempDir()
	namespace := filepath.Join(root, "configs")
	candidate := filepath.Join(namespace, "Caf\u00e9.json")
	provisional, err := pathauthority.NewProvisional(candidate, pathtest.DarwinCaseSensitive(candidate).Witness(), namespace, pathtest.DarwinCaseSensitive(namespace).Witness())
	if err != nil {
		t.Fatal(err)
	}
	owner := mustAuthority(t, filepath.Join(root, "state.json"), filepath.Join(root, "daem.toml"))
	for _, spelling := range []string{".pi/Caf\u00e9.json", "@pi-agent/Caf\u00e9.json"} {
		destination, err := output.Parse(spelling)
		if err != nil {
			t.Fatal(err)
		}
		intent, err := NewProvisionalDocumentBindingIntent(owner, destination, provisional)
		if err != nil {
			t.Fatal(err)
		}
		if _, known := intent.KnownAfter(); known {
			t.Fatal("pending target was flattened into known absence")
		}
		if actual, pending := intent.ProvisionalAfter(); !pending || !actual.Equal(provisional) {
			t.Fatal("pending target lost its complete comparison evidence")
		}
		if changed, err := intent.ChangesBinding(); err != nil || !changed {
			t.Fatal("pending acquisition lost its metadata demand")
		}
		if _, err := NewDocumentBindingConvergence([]DocumentBindingIntent{intent}); err == nil {
			t.Fatal("unpromoted target entered exact registry settlement")
		}

		exact := pathtest.DarwinCaseSensitive(filepath.Join(namespace, "Cafe\u0301.json"))
		promoted, err := intent.Promote(exact)
		if err != nil {
			t.Fatal(err)
		}
		after, known := promoted.KnownAfter()
		actual, present := after.Get()
		if !known || !present || !actual.Owner().ExactEqual(owner) || actual.Destination() != destination || !actual.PathAuthority().Equal(exact) {
			t.Fatal("promotion lost logical identity or exact observed namespace")
		}
		if _, pending := intent.ProvisionalAfter(); !pending {
			t.Fatal("promotion mutated the original pending evidence")
		}

		for _, bad := range []pathauthority.Exact{
			{},
			pathtest.DarwinCaseSensitive(filepath.Join(root, "elsewhere", "Cafe\u0301.json")),
			pathtest.DarwinCaseSensitive(filepath.Join(namespace, "extra", "Cafe\u0301.json")),
			mustPathAuthority(t, filepath.Join(namespace, "Cafe\u0301.json")),
		} {
			if result, err := intent.Promote(bad); err == nil || result.Validate() == nil {
				t.Fatal("invalid namespace/depth/semantics promotion returned usable evidence")
			}
		}
	}
}

func mustBindingValue(t *testing.T, binding DocumentBinding) DocumentBindingValue {
	t.Helper()
	value, err := PresentDocumentBinding(binding)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
