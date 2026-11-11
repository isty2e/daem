package qualification

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
)

func TestSourceRetirementRequiresAllExactRecordedFactsOrNone(t *testing.T) {
	base := t.TempDir()
	owner, err := stateauthority.New(pathtest.Exact(filepath.Join(base, "state.json")), filepath.Join(base, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := stateauthority.New(pathtest.Exact(filepath.Join(base, "other-state.json")), filepath.Join(base, "other.toml"))
	if err != nil {
		t.Fatal(err)
	}
	claim := func(name string, selected stateauthority.Authority, reservation string) ownership.Claim {
		t.Helper()
		key, err := output.NewKeyPath("mcpServers", name)
		if err != nil {
			t.Fatal(err)
		}
		footprint, err := output.NewKeyFootprint(key)
		if err != nil {
			t.Fatal(err)
		}
		address, err := ownership.NewManagedAddress(pathtest.Exact(filepath.Join(base, "source.json")), footprint)
		if err != nil {
			t.Fatal(err)
		}
		var value ownership.Claim
		if reservation == "" {
			value, err = ownership.NewActiveClaim(address, selected)
		} else {
			value, err = ownership.NewReservedClaim(address, selected, reservation)
		}
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	a, b := claim("a", owner, ""), claim("b", owner, "")
	bar := claim("bar", owner, "")
	for _, test := range []struct {
		name    string
		actual  []ownership.Claim
		after   []ownership.Claim
		retired bool
		refuse  bool
	}{
		{"pending with disjoint own row", []ownership.Claim{a, b, bar}, nil, false, false},
		{"settled", nil, nil, true, false},
		{"settled with disjoint own row", []ownership.Claim{bar}, nil, true, false},
		{"new foreign replacement", []ownership.Claim{claim("a", foreign, ""), claim("b", foreign, "")}, nil, true, false},
		{"partial", []ownership.Claim{a}, nil, false, true},
		{"third own reservation", []ownership.Claim{claim("a", owner, "another-operation")}, nil, false, true},
		{"pending survivor", []ownership.Claim{a, b, bar}, []ownership.Claim{b}, false, false},
		{"settled survivor with foreign replacement", []ownership.Claim{claim("a", foreign, ""), b, bar}, []ownership.Claim{b}, true, false},
		{"missing survivor", nil, []ownership.Claim{b}, false, true},
		{"third survivor reservation", []ownership.Claim{claim("b", owner, "another-operation")}, []ownership.Claim{b}, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			registry, err := ownership.NewRegistry(test.actual)
			if err != nil {
				t.Fatal(err)
			}
			retired, err := sourceClaimFinalizationApplied(registry, []ownership.Claim{a, b}, test.after)
			if (err != nil) != test.refuse || err == nil && retired != test.retired {
				t.Fatal("unexpected source retirement classification", retired, err)
			}
		})
	}
}
