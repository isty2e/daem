//go:build darwin || linux

package execute

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/target"
)

func TestRecoveryCleanupRejectsProjectRootReplacementWithoutTouchingReplacement(t *testing.T) {
	fixture := newProjectPathRecoveryFixture(t, projectInstructionRecoverySpec(
		"codex-project",
		target.TargetCodex,
		"AGENTS.md",
	))
	movedRoot := fixture.projectRoot + "-moved"
	const replacementContent = "replacement\n"
	filesystem := &rootSwapOnJournalGCCleanupStore{
		Store: testFilesystem(),
		swap: func() {
			replaceSelectedRoot(t, fixture.projectRoot, movedRoot)
			writeRecoveryTestFile(
				t,
				filepath.Join(fixture.projectRoot, "AGENTS.md"),
				[]byte(replacementContent),
			)
		},
	}

	err := executeRecoveryPlanWithOptionsForTest(
		context.Background(),
		fixture.plan,
		fixture.paths,
		RecoveryOptions{
			Resolver:    destinationResolver(fixture.paths),
			StateCodec:  testStateCodec(),
			StateReader: testStateReader(fixture.paths.StatefilePath),
			Filesystem:  filesystem,
		},
	)
	if !hasRootedPathFailureKind(err, rootedpath.FailureRootReplaced) {
		t.Fatalf(
			"ExecuteRecoveryPlanWithOptions error = %v, want %s",
			err,
			rootedpath.FailureRootReplaced,
		)
	}
	if filesystem.swapCount() != 1 {
		t.Fatalf("project-root swaps = %d, want 1", filesystem.swapCount())
	}
	assertRecoveryTestContent(
		t,
		filepath.Join(fixture.projectRoot, "AGENTS.md"),
		[]byte(replacementContent),
	)
	assertRecoveryTestContent(
		t,
		filepath.Join(movedRoot, "AGENTS.md"),
		[]byte("before:AGENTS.md\n"),
	)
	relativeOperationDir, err := filepath.Rel(
		fixture.projectRoot,
		fixture.plan.OperationDir(),
	)
	if err != nil {
		t.Fatalf("derive moved recovery journal path: %v", err)
	}
	movedOperationDir := filepath.Join(movedRoot, relativeOperationDir)
	if _, statErr := os.Stat(movedOperationDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf(
			"moved recovery journal %q stat error = %v, want removed",
			movedOperationDir,
			statErr,
		)
	}
}
