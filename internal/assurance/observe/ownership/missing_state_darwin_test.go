//go:build darwin

package ownership

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

func TestMissingStateLegacyWitnessIsNeitherFreshNorReleaseAuthority(t *testing.T) {
	root := canonicalRoot(t)
	workspace := filepath.Join(root, "\u0130")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	path, manifest := filepath.Join(workspace, "state.json"), filepath.Join(workspace, "daem.toml")
	missing, err := statefile.ObserveMissingState(t.Context(), path, physicalwork.NewCaptureBudget(nil))
	if err != nil {
		t.Fatal(err)
	}
	exact, present := missing.OwnerPath().Exact()
	if !present || !strings.HasPrefix(exact.Witness(), "darwin-case-v2:") {
		t.Skip("requires a Darwin case-insensitive volume with retained native spelling")
	}
	legacyKey, _ := exact.LegacyComparisonKey()
	legacyWitness := "darwin-case-v1:" + strings.ReplaceAll(strings.TrimPrefix(exact.Witness(), "darwin-case-v2:"), "u", "i")
	legacy, err := pathauthority.NewExact(legacyKey, legacyWitness)
	if err != nil || !exact.MatchesLegacyComparison(legacy) {
		t.Fatalf("invalid comparison-only fixture: %v", err)
	}
	for _, name := range []string{"existing parent", "missing parent"} {
		t.Run(name, func(t *testing.T) {
			for _, recorded := range []pathauthority.Exact{legacy, exact} {
				owner := missingStateOwner(t, recorded, filepath.Join(workspace, "another-manifest.toml"))
				registry := missingStateClaims(t, root, owner)
				want := ErrMissingStateWithOutputs
				if recorded == legacy {
					want = ErrAmbiguousStateOwner
				}
				if err := RequireMissingStateRegistryClear(t.Context(), missing, manifest, registry); !errors.Is(err, want) {
					t.Fatalf("recorded witness %s: %v, want %v", recorded.Witness(), err, want)
				}
				outputs, err := MatchMissingStateOutputs(t.Context(), missing, manifest, registry)
				if recorded == legacy && (!outputs.Empty() || !errors.Is(err, want)) {
					t.Fatalf("legacy evidence could authorize release: %v (%v)", outputs.Claims(), err)
				}
				if recorded == exact && (err != nil || len(outputs.Claims()) != 1) {
					t.Fatalf("strict evidence did not select the owner: %v (%v)", outputs.Claims(), err)
				}
			}
		})
		if name == "existing parent" {
			if err := os.Remove(workspace); err != nil {
				t.Fatal(err)
			}
			missing, err = statefile.ObserveMissingState(t.Context(), path, physicalwork.NewCaptureBudget(nil))
			if err != nil {
				t.Fatal(err)
			}
			if _, known := missing.OwnerPath().Provisional(); !known {
				t.Fatal("a missing Unicode parent did not produce provisional authority")
			}
		}
	}
}
