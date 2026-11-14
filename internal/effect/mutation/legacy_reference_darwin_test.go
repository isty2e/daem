//go:build darwin

package mutation

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

func legacyReferenceFixture(t *testing.T) PersistedDirectoryEntryAuthority {
	t.Helper()
	original, _ := legacyReferenceNeighbours(t)
	if _, available := original.Exact().LegacyLocatorCandidate(); !available {
		t.Skip("filesystem did not expose a spelling-preserving or native-spelling reference")
	}
	return original
}

func legacyReferenceNeighbours(t *testing.T) (PersistedDirectoryEntryAuthority, PersistedDirectoryEntryAuthority) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"İstanbul", "istanbul"} {
		dir := filepath.Join(root, name)
		if err := os.Mkdir(dir, 0o700); errors.Is(err, os.ErrExist) {
			t.Skip("filesystem does not distinguish the normalization-sensitive neighbours")
		} else if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	original, err := ObservePersistedDirectoryEntryAuthority(filepath.Join(root, "İstanbul", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	neighbour, err := ObservePersistedDirectoryEntryAuthority(filepath.Join(root, "istanbul", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return original, neighbour
}

func TestObserveLegacyReferenceRefusesASCIIKeyEvenWhenNeighbourMatches(t *testing.T) {
	original, neighbour := legacyReferenceNeighbours(t)
	legacyKey, present := original.Exact().LegacyComparisonKey()
	if !present || !strings.HasPrefix(original.Exact().Witness(), "darwin-case-v2:") {
		t.Skip("filesystem did not expose native spelling in the original namespace")
	}
	modes := strings.TrimPrefix(original.Exact().Witness(), "darwin-case-v2:")
	legacy, err := pathauthority.NewExact(legacyKey, "darwin-case-v1:"+strings.ReplaceAll(modes, "u", "i"))
	if err != nil {
		t.Fatal(err)
	}
	if !neighbour.Exact().Equal(legacy) {
		t.Skip("filesystem did not expose the exact legacy folded-neighbour collision")
	}
	if original.Exact().Equal(legacy) || !original.Exact().MatchesLegacyComparison(legacy) {
		t.Fatal("fixture does not separate exact equality from legacy overlap")
	}
	budget := &legacyReferenceBudget{deny: errors.New("ineligible access must not reach I/O")}
	actual, err := ObserveLegacyReferenceBounded(t.Context(), legacy, mutationfs.MaximumPhysicalPathDepth, budget)
	if !errors.Is(err, ErrLegacyReferenceUnprovable) || !actual.Reference().IsZero() || budget.calls != 0 {
		t.Fatal("an already-ambiguous key was qualified through its folded neighbour", actual, budget, err)
	}
}
