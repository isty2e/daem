//go:build darwin || linux

package fileset

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

func TestPublicationCleanupProgressExceedsRootInventoryBound(t *testing.T) {
	root := mustCanonicalStateDir(t, t.TempDir())
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for index := range 2*maximumStateDirFenceEntries + 1 {
		if err := os.Mkdir(filepath.Join(root, fmt.Sprintf("%s%032x", fileSetTemporaryPrefix, index)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		plan, err := InspectPublicationCleanup(t.Context(), root, &fenceObservationBudget{})
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.Paths()) == 0 || len(plan.Deferred()) == 0 {
			plan.Close()
			t.Fatal("large root made no progress or claimed exhaustive coverage")
		}
		if err := plan.Execute(t.Context()); err != nil {
			plan.Close()
			t.Fatal("execution reintroduced a whole-root inventory bound", err)
		}
		for _, path := range plan.Paths() {
			assertMissing(t, path)
		}
		plan.Close()
	}

	name := publicationRetiredPrefix + strings.Repeat("f", 32)
	retired := filepath.Join(root, name)
	if err := os.Mkdir(retired, 0o700); err != nil {
		t.Fatal(err)
	}
	identity, err := storagecommit.ObserveEntryIdentity(t.Context(), retired)
	if err != nil {
		t.Fatal(err)
	}
	collectRetiredPublication(t.Context(), root, name, identity)
	assertMissing(t, retired)
}
