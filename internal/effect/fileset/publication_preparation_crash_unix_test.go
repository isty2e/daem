//go:build darwin || linux

package fileset

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

func TestPublicationPreparationProcessLossLeavesOnlyCollectibleInactiveWork(t *testing.T) {
	for _, phase := range []string{"empty", "partial"} {
		t.Run(phase, func(t *testing.T) {
			root := mustCanonicalStateDir(t, t.TempDir())
			evidence := filepath.Join(root, "metadata")
			canary := filepath.Join(root, "installed")
			writeFixture(t, canary, "unchanged", 0o600)
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			process := exec.CommandContext(t.Context(), executable, "-test.run=^TestPublicationPreparationCrashHelper$")
			process.Env = append(os.Environ(), "DAEM_PUBLICATION_STAGE_ROOT="+evidence, "DAEM_PUBLICATION_STAGE_PHASE="+phase)
			output, err := process.CombinedOutput()
			var stopped *exec.ExitError
			if !errors.As(err, &stopped) || stopped.ExitCode() != 86 {
				t.Fatal("stage helper did not stop at preparation", err, string(output))
			}
			assertMissing(t, transactionDir(evidence))
			if err := ObserveClearPublicationFenceAt(t.Context(), evidence, mutationfs.MaximumPhysicalPathDepth, &fenceObservationBudget{}); err != nil {
				t.Fatal("pre-effect process loss became a global fence", err)
			}
			cleanup, err := InspectPublicationCleanup(t.Context(), evidence, &fenceObservationBudget{})
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup.Close()
			if len(cleanup.Paths()) != 1 {
				t.Fatal("actual producer's abandoned stage was not admitted", cleanup.Paths(), cleanup.Deferred())
			}
			path := cleanup.Paths()[0]
			if err := cleanup.Execute(t.Context()); err != nil {
				t.Fatal(err)
			}
			assertMissing(t, path)
			content, err := os.ReadFile(canary)
			if err != nil || string(content) != "unchanged" {
				t.Fatal("preparation/GC changed an installed file", err)
			}
		})
	}
}

func TestPublicationPreparationCrashHelper(t *testing.T) {
	root := os.Getenv("DAEM_PUBLICATION_STAGE_ROOT")
	if root == "" {
		t.Skip("subprocess crash helper")
	}
	if os.Getenv("DAEM_TEST_HARNESS") != "1" {
		t.Fatal("crash helper requires the isolated repository harness")
	}
	budget := &fenceObservationBudget{}
	witness, destination, err := rootedpath.CaptureDestinationBounded(transactionDir(root), mutationfs.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	defer witness.Close()
	capability, err := witness.Acquire(destination)
	if err != nil {
		t.Fatal(err)
	}
	limits, err := mutationfs.NewTreeTraversalLimits(1, 0, 16)
	if err != nil {
		t.Fatal(err)
	}
	_, err = storagecommit.PrepareRootedTreeWithLimits(t.Context(), capability, limits, func(writer mutationfs.RootedTreeWriter) error {
		if os.Getenv("DAEM_PUBLICATION_STAGE_PHASE") == "partial" {
			path, err := mutationfs.NewTreeRelativePath(publicationIntentFile)
			if err != nil {
				return err
			}
			if err := writer.WriteFile(path, 0o600, strings.NewReader("{torn")); err != nil {
				return err
			}
		}
		os.Exit(86)
		return nil
	})
	t.Fatal("crash helper returned", err)
}
