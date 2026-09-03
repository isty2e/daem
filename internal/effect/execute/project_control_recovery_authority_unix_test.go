//go:build darwin || linux

package execute

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestRecoveryResultReportsJournalRetiredAfterAcceptanceFailure(t *testing.T) {
	fixture := newProjectPathRecoveryFixture(t, projectInstructionRecoverySpec(
		"codex-project",
		target.TargetCodex,
		"AGENTS.md",
	))
	acceptanceFailure := errors.New("injected post-retirement acceptance failure")
	journalRetired := false
	filesystem := &rootSwapAfterProjectJournalGCCleanupStore{
		Store: testFilesystem(),
		swap: func() {
			journalRetired = true
		},
	}

	err := executeRecoveryPlanWithOptionsForTest(
		t.Context(),
		fixture.plan,
		fixture.paths,
		RecoveryOptions{
			Resolver:    destinationResolver(fixture.paths),
			StateCodec:  testStateCodec(),
			StateReader: testStateReader(fixture.paths.StatefilePath),
			Filesystem:  filesystem,
			AcceptVisibilityChanges: func(context.Context) error {
				if journalRetired {
					return acceptanceFailure
				}
				return nil
			},
		},
	)
	if !errors.Is(err, acceptanceFailure) ||
		!strings.Contains(err.Error(), "recovery journal retired") {
		t.Fatalf("ExecuteRecoveryPlanWithOptions error = %v, want post-retirement failure", err)
	}
	assertRecoveryTestContent(
		t,
		filepath.Join(fixture.projectRoot, "AGENTS.md"),
		[]byte("before:AGENTS.md\n"),
	)
	if _, statErr := os.Stat(fixture.plan.OperationDir()); !os.IsNotExist(statErr) {
		t.Fatalf("retired recovery journal stat error = %v, want absent", statErr)
	}
}

func TestRecoveryResultPreservesOwnedAuthorityCloseFailureAfterRetirement(t *testing.T) {
	fixture := newProjectPathRecoveryFixture(t, projectInstructionRecoverySpec(
		"codex-project",
		target.TargetCodex,
		"AGENTS.md",
	))
	closeFailure := errors.New("injected retained-authority close failure")

	err := executeRecoveryPlanWithOptionsForTest(
		t.Context(),
		fixture.plan,
		fixture.paths,
		RecoveryOptions{
			Resolver:    destinationResolver(fixture.paths),
			StateCodec:  testStateCodec(),
			StateReader: testStateReader(fixture.paths.StatefilePath),
			Filesystem:  testFilesystem(),
			afterAuthorityClose: func() error {
				return closeFailure
			},
		},
	)
	if !errors.Is(err, closeFailure) {
		t.Fatalf("ExecuteRecoveryPlanWithOptions error = %v, want close failure", err)
	}
	assertRecoveryTestContent(
		t,
		filepath.Join(fixture.projectRoot, "AGENTS.md"),
		[]byte("before:AGENTS.md\n"),
	)
	if _, statErr := os.Stat(fixture.plan.OperationDir()); !os.IsNotExist(statErr) {
		t.Fatalf("retired recovery journal stat error = %v, want absent", statErr)
	}
}

func TestRecoveryResultJoinsPrimaryAndOwnedAuthorityCloseFailures(t *testing.T) {
	fixture := newProjectPathRecoveryFixture(t, projectInstructionRecoverySpec(
		"codex-project",
		target.TargetCodex,
		"AGENTS.md",
	))
	primary := errors.New("injected before-retirement failure")
	closeFailure := errors.New("injected retained-authority close failure")

	err := executeRecoveryPlanWithOptionsForTest(
		t.Context(),
		fixture.plan,
		fixture.paths,
		RecoveryOptions{
			Resolver:    destinationResolver(fixture.paths),
			StateCodec:  testStateCodec(),
			StateReader: testStateReader(fixture.paths.StatefilePath),
			Filesystem:  testFilesystem(),
			beforeRetirement: func() error {
				return primary
			},
			afterAuthorityClose: func() error {
				return closeFailure
			},
		},
	)
	if !errors.Is(err, primary) || !errors.Is(err, closeFailure) {
		t.Fatalf("ExecuteRecoveryPlanWithOptions error = %v, want primary and close failures", err)
	}
	assertRecoveryTestContent(
		t,
		filepath.Join(fixture.projectRoot, "AGENTS.md"),
		[]byte("before:AGENTS.md\n"),
	)
	if _, statErr := os.Stat(fixture.plan.OperationDir()); statErr != nil {
		t.Fatalf("retained recovery journal stat error = %v, want present", statErr)
	}
}
