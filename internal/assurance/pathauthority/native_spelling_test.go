//go:build darwin || linux

package pathauthority_test

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
)

func TestDarwinNativeSpellingAuthorityDoesNotReinterpretLegacyKeys(t *testing.T) {
	current, err := pathauthority.NewExact("/root/İ/point.json", "darwin-case-v2:iui")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := pathauthority.NewExact("/root/i/point.json", "darwin-case-v1:iii")
	if err != nil {
		t.Fatal(err)
	}
	if current.Equal(legacy) || current.Key() == legacy.Key() {
		t.Fatal("retained native spelling collapsed into its lossy legacy key")
	}
	if !current.MatchesLegacyComparison(legacy) || legacy.MatchesLegacyComparison(current) {
		t.Fatal("qualified legacy comparison changed direction or lost its encoding")
	}
	for _, scenario := range []struct{ key, witness string }{
		{"/root/İ/point.json", "darwin-case-v1:iii"},
		{"/root/i/point.json", "darwin-case-v2:iui"},
		{"/root/i/point.json", "darwin-case-v2:iii"},
	} {
		if _, err := pathauthority.NewExact(scenario.key, scenario.witness); err == nil {
			t.Fatal("noncanonical versioned spelling was accepted", scenario)
		}
	}
}

func TestDarwinNativeProvisionalPromotionRetainsItsObservedNamespace(t *testing.T) {
	missing, err := pathauthority.NewProvisional("/root/İ/point.json", "darwin-case-v2:iui", "/root", "darwin-case-v1:i")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := pathauthority.NewExact("/root/i/point.json", "darwin-case-v1:iii")
	if err != nil || !missing.MatchesMissingExact(legacy) {
		t.Fatal("qualified missing history lost the legacy comparison grammar", err)
	}
	for _, scenario := range []struct{ key, witness string }{
		{"/root/İ/point.json", "darwin-case-v2:iui"},
		{"/root/native-folded/point.json", "darwin-case-v1:iii"},
	} {
		exact, err := pathauthority.NewExact(scenario.key, scenario.witness)
		if err != nil || missing.AdmitsExact(exact) != nil {
			t.Fatal("native observation could not promote within its retained namespace", scenario, err)
		}
	}
	for _, scenario := range []struct{ key, witness string }{
		{"/root/İ/point.json", "darwin-case-v2:ius"},
		{"/outside/İ/point.json", "darwin-case-v2:iui"},
		{"/root/İ/deeper/point.json", "darwin-case-v2:iuii"},
	} {
		exact, err := pathauthority.NewExact(scenario.key, scenario.witness)
		if err != nil {
			t.Fatal(err)
		}
		if missing.AdmitsExact(exact) == nil {
			t.Fatal("promotion escaped its filesystem, namespace or depth", scenario)
		}
	}
}
