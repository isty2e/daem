//go:build darwin || linux

package fileset

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

func TestPublicationIgnoresInactiveSiblingsWithoutChangingLocalFence(t *testing.T) {
	root := mustCanonicalStateDir(t, t.TempDir())
	for index := range maximumStateDirFenceEntries + 1 {
		name := filepath.Join(root, fmt.Sprintf("unrelated-%04d", index))
		if err := os.Mkdir(name, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	stage := filepath.Join(root, ".daem-tmp-"+strings.Repeat("a", 32))
	if err := os.Mkdir(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ObserveClearPublicationFenceAt(t.Context(), root, mutationfs.MaximumPhysicalPathDepth, &fenceObservationBudget{}); err != nil {
		t.Fatal("inactive siblings affected publication admission", err)
	}
	if err := ObserveClearFenceAt(t.Context(), root, mutationfs.MaximumPhysicalPathDepth, &fenceObservationBudget{}); err == nil {
		t.Fatal("changed the local FileSet fence")
	}
	if _, err := os.Stat(stage); err != nil {
		t.Fatal("observation mutated inactive evidence", err)
	}
}

func TestPublicationRecoveryWithAnOlderInactiveStage(t *testing.T) {
	input, _ := publicationInputForTest(t)
	if err := os.MkdirAll(input.EvidenceRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(input.EvidenceRoot, ".daem-tmp-"+strings.Repeat("b", 32))
	if err := os.Mkdir(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	stopped := errors.New("materializer stopped")
	input.Materializers[input.Intent.targets[1].path] = func(_ context.Context, _ []byte) ([]byte, error) {
		return nil, stopped
	}
	if err := CommitFreshFileSet5(t.Context(), input); !errors.Is(err, stopped) {
		t.Fatal("lost the materializer failure")
	}
	assertPublicationBeforeForTest(t, input.Intent)
	assertMissing(t, transactionDir(input.EvidenceRoot))
	if _, err := os.Stat(stage); err != nil {
		t.Fatal("publication swept an older stage", err)
	}
}

func TestPublicationRetirementRejectsReplacedActiveDirectory(t *testing.T) {
	for _, complete := range []bool{false, true} {
		t.Run(fmt.Sprintf("complete=%t", complete), func(t *testing.T) {
			input, revision := interruptedPublicationForTest(t, true)
			plan, err := loadQualifiedPublication(t.Context(), input.EvidenceRoot, input.Intent, revision, &fenceObservationBudget{})
			if err != nil {
				t.Fatal(err)
			}
			defer plan.rootWitness.Close()
			active := transactionDir(input.EvidenceRoot)
			saved := active + ".saved"
			if err := os.Rename(active, saved); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(active, 0o700); err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(saved)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if !complete && entry.Name() != transactionMarkerFile {
					continue
				}
				content, err := os.ReadFile(filepath.Join(saved, entry.Name()))
				if err != nil {
					t.Fatal(err)
				}
				writeFixture(t, filepath.Join(active, entry.Name()), string(content), 0o600)
			}
			if err := plan.finish(t.Context(), RecoveryFinalize, revision); err == nil {
				t.Fatal("replacement active evidence inherited retirement authority")
			}
			for _, path := range []string{active, saved} {
				if _, err := os.Stat(path); err != nil {
					t.Fatal("replacement refusal moved or collected evidence", err)
				}
			}
		})
	}
}
