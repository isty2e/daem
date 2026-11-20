//go:build darwin

package ownership

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	outputownership "github.com/isty2e/daem/internal/output/ownership"
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
	outputPath, err := mutation.ObservePersistedDirectoryEntryAuthority(filepath.Join(root, "output"))
	if err != nil {
		t.Fatal(err)
	}
	address, err := outputownership.NewManagedAddressFromLegacy(outputPath.Exact(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, recorded := range []pathauthority.Exact{legacy, exact} {
		owner, err := stateauthority.New(recorded, filepath.Join(workspace, "another-manifest.toml"))
		if err != nil {
			t.Fatal(err)
		}
		claim, err := outputownership.NewActiveClaim(address, owner)
		if err != nil {
			t.Fatal(err)
		}
		registry, err := outputownership.NewRegistry([]outputownership.Claim{claim})
		if err != nil {
			t.Fatal(err)
		}
		err = RequireMissingStateRegistryClear(t.Context(), missing, manifest, registry)
		want := ErrMissingStateWithOutputs
		if recorded == legacy {
			want = ErrAmbiguousStateOwner
		}
		if !errors.Is(err, want) {
			t.Fatalf("recorded witness %s: %v, want %v", recorded.Witness(), err, want)
		}
	}
}
