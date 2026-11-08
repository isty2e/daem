package namespace

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

func TestAbsentProtectionSelectorsRequireAnOptionalContract(t *testing.T) {
	contracts, err := configuration.NativeProtectionLocations()
	if err != nil {
		t.Fatal(err)
	}
	for _, contract := range contracts {
		reference := contract.Reference()
		absent, err := AbsentReservationSelector(reference, "not selected")
		if !contract.Optional() {
			if err == nil {
				t.Fatal("a required location silently lost protection", reference)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := NewReservationSelectors([]ReservationSelector{absent, absent}); err != nil {
			t.Fatal("identical absence was treated as a selector conflict", err)
		}
		located, err := NewReservationSelector(reference, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		unavailable, err := UnavailableReservationSelector(reference, "unavailable", errors.New("failed"))
		if err != nil {
			t.Fatal(err)
		}
		for _, other := range []ReservationSelector{located, unavailable} {
			if _, err := NewReservationSelectors([]ReservationSelector{absent, other}); err == nil {
				t.Fatal("absent and located/unavailable states were silently collapsed")
			}
		}
		for _, invalid := range []ReservationSelector{
			{reference: reference, absent: true},
			{reference: reference, source: "absent", absent: true, path: t.TempDir()},
			{reference: reference, source: "absent", absent: true, failure: errors.New("failed")},
		} {
			if _, err := NewReservationSelectors([]ReservationSelector{invalid}); err == nil {
				t.Fatal("contradictory absence bypassed selector validation")
			}
		}
	}
	foreign, _ := output.NewRootReference("unknown-protection-location", "unknown-location-v1")
	if _, err := AbsentReservationSelector(foreign, "not selected"); err == nil {
		t.Fatal("unknown coverage became a checked-empty location")
	}
	mandatory, _ := configuration.RootReferenceForScope(target.TargetCodex, target.ScopeGlobal)
	if _, err := NewReservationSelectors([]ReservationSelector{{reference: mandatory, absent: true, source: "not selected"}}); err == nil {
		t.Fatal("an aggregate admitted forged absence of mandatory Codex protection")
	}
}

func TestAdditionalOpenCodeLocationsRetainRulesWithoutGrantingWriters(t *testing.T) {
	contracts, err := configuration.NativeProtectionLocations()
	if err != nil {
		t.Fatal(err)
	}
	var additional output.RootReference
	for _, contract := range contracts {
		if contract.Reference().Identity() == "opencode-additional-configuration" {
			additional = contract.Reference()
		}
	}
	for _, inside := range []bool{true, false} {
		base := t.TempDir()
		manifest := nativeNamespaceRoot(t, base)
		extra := filepath.Join(base, "extra")
		if !inside {
			extra = t.TempDir()
		}
		if err := os.MkdirAll(extra, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(extra, "opencode.json"), []byte(`{"theme":"dark"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(base, ".pi"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(extra, "opencode.json"), filepath.Join(base, ".pi", "extra.json")); err != nil {
			t.Skipf("symlinks are unavailable: %v", err)
		}
		protection := protectionRoots(t, filepath.Join(base, "pi-global"), filepath.Join(base, "codex-global"))
		selector, err := NewReservationSelector(additional, extra)
		if err != nil {
			t.Fatal(err)
		}
		protection.selectors[additional] = selector
		roots := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: manifest})
		address := nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/extra.json")
		value, err := Observe(t.Context(), []aggregate.DocumentAddress{address}, roots, protection, journalTestFilesystem(), imageBudget(t))
		if !inside {
			var failure *ProtectionError
			if err == nil || errors.As(err, &failure) || !strings.Contains(err.Error(), "no explicitly supplied trusted writer root") {
				t.Fatal("an outside read-only configuration location became a writer or a false reservation", err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := value.RequireCompletePhysicalRules(); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(value.GlobalLocations(), []output.RootReference{additional}) {
			t.Fatal("additional global configuration lost its actual location", value.GlobalLocations())
		}
		roles := value.PhysicalRoles()
		if len(roles) != 1 || roles[0].Target() != target.TargetOpenCode || roles[0].Class() != configuration.SharedConfiguration || roles[0].Grammar() != configuration.JSONC {
			t.Fatal("additional configuration stopped using the canonical OpenCode rules", roles)
		}
		absent, err := AbsentReservationSelector(additional, "not selected")
		if err != nil {
			t.Fatal(err)
		}
		protection.selectors[additional] = absent
		without, err := Observe(t.Context(), []aggregate.DocumentAddress{address}, roots, protection, journalTestFilesystem(), imageBudget(t))
		if err != nil || len(without.GlobalLocations()) != 0 || len(without.PhysicalRoles()) != 0 {
			t.Fatal("explicit absence invented an unknown or a global configuration role", without, err)
		}
		if err := without.RequireCompletePhysicalRules(); err != nil {
			t.Fatal("explicit absence became incomplete physical coverage", err)
		}
	}
}
