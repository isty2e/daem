package ownership

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/test/outputtest"
)

func TestNativeProvisionalIntentPromotionRetainsScopeAndNamespace(t *testing.T) {
	root := t.TempDir()
	physical := filepath.Join(root, "Café.json")
	exact := pathtest.DarwinCaseSensitive(physical)
	provisional, err := pathauthority.NewProvisional(physical, exact.Witness(), root, pathtest.DarwinCaseSensitive(root).Witness())
	if err != nil {
		t.Fatal(err)
	}
	owner := mustAuthority(t, filepath.Join(root, "state.json"), filepath.Join(root, "daem.toml"))
	destination := outputtest.Parse(t, "@pi-agent/settings.json")
	for _, footprint := range []output.Footprint{
		output.WholePathFootprint(), outputtest.KeyFootprint(t, ""),
		outputtest.KeyFootprint(t, "a/b"), outputtest.KeyFootprint(t, "a", "b"),
		outputtest.KeyFootprint(t, "~1"), outputtest.KeyFootprint(t, "."),
		outputtest.KeyFootprint(t, "line\n\x00key"),
	} {
		intent, err := NewProvisionalAcquireIntent(destination, footprint, provisional, owner, "operation-1")
		if err != nil {
			t.Fatal(err)
		}
		address, err := NewManagedAddress(exact, footprint)
		if err != nil {
			t.Fatal(err)
		}
		if !intent.Footprint().Equal(footprint) || intent.AdmitAddress(address) != nil {
			t.Fatal("matching exact visibility did not retain the provisional native scope")
		}
		otherScope, err := NewManagedAddress(exact, outputtest.KeyFootprint(t, "different"))
		if err != nil {
			t.Fatal(err)
		}
		otherPath, err := NewManagedAddress(pathtest.DarwinCaseSensitive(filepath.Join(filepath.Dir(root), filepath.Base(root)+"-outside", "Café.json")), footprint)
		if err != nil {
			t.Fatal(err)
		}
		if intent.AdmitAddress(otherScope) == nil || intent.AdmitAddress(otherPath) == nil {
			t.Fatal("promotion broadened native scope or namespace authority")
		}
	}
	if _, err := NewProvisionalAcquireIntent(destination, output.Footprint{}, provisional, owner, "operation-1"); !errors.Is(err, output.ErrInvalidFootprint) {
		t.Fatalf("zero footprint admission = %v", err)
	}

	legacy, err := NewProvisionalAcquireIntentFromLegacy(destination, "/literal~1", provisional, owner, "operation-1")
	if err != nil {
		t.Fatal(err)
	}
	spelling, err := legacy.LegacyContentPath()
	if err != nil || spelling != "/literal~1" || !legacy.Footprint().Equal(outputtest.KeyFootprint(t, "literal~1")) {
		t.Fatalf("legacy spelling was decoded or changed: %q, %v", spelling, err)
	}
	native, err := NewProvisionalAcquireIntent(destination, outputtest.KeyFootprint(t, "literal/"), provisional, owner, "operation-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := native.LegacyContentPath(); !errors.Is(err, output.ErrNotLegacyFootprint) {
		t.Fatalf("lossy legacy intent conversion = %v", err)
	}
}
