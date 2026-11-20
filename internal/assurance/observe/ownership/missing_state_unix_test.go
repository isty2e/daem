//go:build darwin || linux

package ownership

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	outputownership "github.com/isty2e/daem/internal/output/ownership"
)

func TestMissingStateOwnerComparison(t *testing.T) {
	root := canonicalRoot(t)
	manifest := filepath.Join(root, "daem.toml")
	missing, err := statefile.ObserveMissingState(t.Context(), filepath.Join(root, "state.json"), physicalwork.NewCaptureBudget(nil))
	if err != nil {
		t.Fatal(err)
	}
	exact, known := missing.OwnerPath().Exact()
	if !known {
		t.Fatal("a missing ASCII leaf must have exact comparison authority")
	}
	conflicting := pathtest.Exact(exact.Key())
	if conflicting.Equal(exact) {
		conflicting = pathtest.DarwinCaseSensitive(exact.Key())
	}
	other := pathtest.Exact(filepath.Join(root, "other", "state.json"))
	exactOwner := missingStateOwner(t, exact, filepath.Join(root, "other.toml"))
	foreign := missingStateOwner(t, other, filepath.Join(root, "other", "daem.toml"))
	wrongProvenance := missingStateOwner(t, other, manifest)
	wrongWitness := missingStateOwner(t, conflicting, exactOwner.ManifestPath())

	for _, scenario := range []struct {
		name   string
		owners []stateauthority.Authority
		want   error
	}{
		{"foreign", []stateauthority.Authority{foreign}, nil},
		{"shared State", []stateauthority.Authority{exactOwner, foreign}, ErrMissingStateWithOutputs},
		{"same manifest different State", []stateauthority.Authority{wrongProvenance}, ErrAmbiguousStateOwner},
		{"same path different witness", []stateauthority.Authority{wrongWitness}, ErrAmbiguousStateOwner},
		{"strict alongside a conflicting record", []stateauthority.Authority{exactOwner, wrongProvenance}, ErrAmbiguousStateOwner},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			registry := missingStateClaims(t, root, scenario.owners...)
			if err := RequireMissingStateRegistryClear(t.Context(), missing, manifest, registry); !errors.Is(err, scenario.want) {
				t.Fatalf("normal admission: %v, want %v", err, scenario.want)
			}
			outputs, err := MatchMissingStateOutputs(t.Context(), missing, manifest, registry)
			if errors.Is(scenario.want, ErrMissingStateWithOutputs) {
				if err != nil || len(outputs.Claims()) != 1 || !outputs.Claims()[0].OwnedBy(exactOwner) {
					t.Fatalf("release did not select the strictly matched owner: %v (%v)", outputs.Claims(), err)
				}
			} else if !errors.Is(err, scenario.want) || !outputs.Empty() {
				t.Fatalf("release authorizes unexpected rows: %v (%v), want %v", outputs.Claims(), err, scenario.want)
			}
		})
	}
}

func missingStateOwner(t *testing.T, exact pathauthority.Exact, manifest string) stateauthority.Authority {
	t.Helper()
	owner, err := stateauthority.New(exact, manifest)
	if err != nil {
		t.Fatal(err)
	}
	return owner
}

func missingStateClaims(t *testing.T, root string, owners ...stateauthority.Authority) outputownership.Registry {
	t.Helper()
	var claims []outputownership.Claim
	for index, owner := range owners {
		address, err := outputownership.NewManagedAddressFromLegacy(pathtest.Exact(filepath.Join(root, fmt.Sprintf("output-%d", index))), "")
		if err != nil {
			t.Fatal(err)
		}
		claim, err := outputownership.NewActiveClaim(address, owner)
		if err != nil {
			t.Fatal(err)
		}
		claims = append(claims, claim)
	}
	registry, err := outputownership.NewRegistry(claims)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}
