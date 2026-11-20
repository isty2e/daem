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
	"github.com/isty2e/daem/internal/output"
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

func TestMissingStateBindingWithoutClaimsDoesNotProveLoss(t *testing.T) {
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
	selected := missingStateOwner(t, exact, manifest)
	conflict := missingStateOwner(t, pathtest.Exact(filepath.Join(root, "other", "state.json")), manifest)
	destination, err := output.NewDestination(output.RootProject, "settings.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name          string
		bindingOwner  stateauthority.Authority
		claims        []outputownership.Claim
		wantAdmission error
		wantRelease   error
	}{
		{name: "related binding only", bindingOwner: selected},
		{name: "ambiguous binding only", bindingOwner: conflict, wantRelease: ErrAmbiguousStateOwner},
		{
			name: "related binding with ambiguous claim", bindingOwner: selected,
			claims: missingStateClaims(t, root, conflict).Claims(), wantAdmission: ErrAmbiguousStateOwner, wantRelease: ErrAmbiguousStateOwner,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			binding, err := outputownership.NewDocumentBinding(test.bindingOwner, destination, pathtest.Exact(filepath.Join(root, "settings.json")))
			if err != nil {
				t.Fatal(err)
			}
			registry, err := outputownership.NewRegistryWithProtections(test.claims, []outputownership.DocumentBinding{binding}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := RequireMissingStateRegistryClear(t.Context(), missing, manifest, registry); !errors.Is(err, test.wantAdmission) {
				t.Fatalf("normal admission: %v, want %v", err, test.wantAdmission)
			}
			outputs, err := MatchMissingStateOutputs(t.Context(), missing, manifest, registry)
			if !errors.Is(err, test.wantRelease) {
				t.Fatalf("release selection: %v, want %v", err, test.wantRelease)
			}
			if test.wantRelease == nil && (len(outputs.Bindings()) != 1 || !outputs.Bindings()[0].Equal(binding) || len(outputs.Claims()) != 0) {
				t.Fatalf("did not select precisely the binding to release: %+v", outputs)
			}
			if test.wantRelease != nil && !outputs.Empty() {
				t.Fatal("ambiguous evidence selected releasable records")
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
