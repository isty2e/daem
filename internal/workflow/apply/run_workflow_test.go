package apply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/hook"
	"github.com/isty2e/daem/internal/desired/skill"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/mutation"
	hookcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	"github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/supply/artifact"
	targetpkg "github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	topologyhook "github.com/isty2e/daem/internal/topology/hook"
)

func TestRunWritesSelectedInstructionOutputAndStatefile(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	paths := isolatedApplyTestPaths(t, tempDir)
	writeApplyFile(t, filepath.Join(tempDir, "instructions", "AGENTS.md"), "shared instructions\n")
	instructionHash := hashApplyPath(t, filepath.Join(tempDir, "instructions", "AGENTS.md"))
	claudeOldHash := string(artifact.HashFileContent([]byte("claude-old")))
	writeApplyStatefile(t, paths.StatefilePath, applyStateSnapshot(t, durable.SnapshotInput{
		ManagedPaths: []durable.ManagedPathState{
			applyInstructionPathState(
				t, "project", []string{"claude-code"}, "project", "CLAUDE.md", claudeOldHash,
			),
		},
	}))
	resources := applyInstructionConfig(t, "project", "instructions/AGENTS.md", "", targetpkg.TargetCodex, targetpkg.TargetClaudeCode)
	locked := applyInstructionLockfile(
		t, "project", "local:instructions/AGENTS.md?mode=vendor", instructionHash,
		targetpkg.TargetCodex, targetpkg.TargetClaudeCode,
	)
	selection := applySelection(t, []string{"codex"})

	result, err := run(t, context.Background(), paths, resources, locked, selection, buildManagedApplyAssessment(t, paths, resources, locked, selection, false))
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.ActionCount != 1 {
		t.Fatalf("ActionCount = %d, want 1", result.ActionCount)
	}
	assertApplyFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "shared instructions\n")
	if _, err := os.Stat(filepath.Join(tempDir, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Fatalf("non-selected target output was written or stat failed: %v", err)
	}
	state, err := statefile.Load(t.Context(), paths.StatefilePath)
	if err != nil {
		t.Fatalf("load statefile: %v", err)
	}
	assertApplyStateResource(t, state, "project", "codex", "project", "AGENTS.md", instructionHash)
	assertApplyStateResource(t, state, "project", "claude-code", "project", "CLAUDE.md", claudeOldHash)
	assertNoApplyRecoveryArtifacts(t, paths)
}

func TestRunWithOptionsPassesExecuteEvents(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	paths := isolatedApplyTestPaths(t, tempDir)
	writeApplyFile(t, filepath.Join(tempDir, "instructions", "AGENTS.md"), "shared instructions\n")
	instructionHash := hashApplyPath(t, filepath.Join(tempDir, "instructions", "AGENTS.md"))
	resources := applyInstructionConfig(t, "project", "instructions/AGENTS.md", "", targetpkg.TargetCodex)
	locked := applyInstructionLockfile(t, "project", "local:instructions/AGENTS.md?mode=vendor", instructionHash)
	selection := applySelection(t, []string{"codex"})

	var events []execute.Event
	result, err := runWithOptions(
		context.Background(),
		paths,
		resources,
		locked,
		selection,
		buildManagedApplyAssessment(t, paths, resources, locked, selection, false),
		runOptions{ExecuteEvents: func(event execute.Event) {
			events = append(events, event)
		}},
	)
	if err != nil {
		t.Fatalf("runWithOptions returned error: %v", err)
	}
	if result.ActionCount != 1 {
		t.Fatalf("ActionCount = %d, want 1", result.ActionCount)
	}
	assertWorkflowApplyEventKinds(t, events, execute.EventJournalCaptureStarted, execute.EventActionStarted, execute.EventJournalCleaned)
}

func TestRunWithOptionsThreadsPreparedEffectPlanThroughBothPayloadBranches(t *testing.T) {
	t.Parallel()

	t.Run("payload-free", func(t *testing.T) {
		t.Parallel()
		tempDir := t.TempDir()
		paths := isolatedApplyTestPaths(t, tempDir)
		resources := applyEmptyEnvironment(t, targetpkg.TargetCodex)
		locked := snapshottest.File(t)
		selection := applySelection(t, []string{"codex"})
		assessment := buildManagedApplyAssessment(t, paths, resources, locked, selection, false)

		_, err := runWithOptions(
			context.Background(),
			paths,
			resources,
			locked,
			selection,
			assessment,
			runOptions{applyEffectPlan: &execute.ApplyEffectPlan{}},
		)
		if err == nil {
			t.Fatal("runWithOptions accepted an unavailable prepared plan")
		}
		for _, path := range []string{paths.StatefilePath, paths.RecoveryDir} {
			if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
				t.Fatalf("pre-effect plan failure created %q: %v", path, statErr)
			}
		}
	})

	t.Run("payload-bearing", func(t *testing.T) {
		t.Parallel()
		tempDir := t.TempDir()
		paths := isolatedApplyTestPaths(t, tempDir)
		writeApplyFile(t, filepath.Join(tempDir, "instructions", "AGENTS.md"), "shared instructions\n")
		instructionHash := hashApplyPath(t, filepath.Join(tempDir, "instructions", "AGENTS.md"))
		resources := applyInstructionConfig(t, "project", "instructions/AGENTS.md", "", targetpkg.TargetCodex)
		locked := applyInstructionLockfile(
			t,
			"project",
			"local:instructions/AGENTS.md?mode=vendor",
			instructionHash,
			targetpkg.TargetCodex,
		)
		selection := applySelection(t, []string{"codex"})
		assessment := buildManagedApplyAssessment(t, paths, resources, locked, selection, false)

		_, err := runWithOptions(
			context.Background(),
			paths,
			resources,
			locked,
			selection,
			assessment,
			runOptions{applyEffectPlan: &execute.ApplyEffectPlan{}},
		)
		if err == nil {
			t.Fatal("runWithOptions accepted an unavailable prepared plan")
		}
		for _, path := range []string{
			filepath.Join(tempDir, "AGENTS.md"),
			paths.StatefilePath,
			paths.RecoveryDir,
		} {
			if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
				t.Fatalf("pre-effect plan failure created %q: %v", path, statErr)
			}
		}
	})
}

func TestRunFinalValidationPrecedesJournalAndHostEffects(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	paths := isolatedApplyTestPaths(t, tempDir)
	writeApplyFile(t, filepath.Join(tempDir, "instructions", "AGENTS.md"), "content\n")
	contentHash := hashApplyPath(t, filepath.Join(tempDir, "instructions", "AGENTS.md"))
	resources := applyInstructionConfig(t, "project", "instructions/AGENTS.md", "", targetpkg.TargetCodex)
	locked := applyInstructionLockfile(t, "project", "local:instructions/AGENTS.md?mode=vendor", contentHash)
	selection := applySelection(t, []string{"codex"})
	wantErr := errors.New("final validation failed")
	validationCalls := 0
	_, err := runWithOptions(
		context.Background(),
		paths,
		resources,
		locked,
		selection,
		buildManagedApplyAssessment(t, paths, resources, locked, selection, false),
		runOptions{validateBeforeEffects: func(context.Context, mutation.PhysicalAuthoritySet) error {
			validationCalls++
			return wantErr
		}},
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("runWithOptions error = %v, want validation error", err)
	}
	if validationCalls != 1 {
		t.Fatalf("validation calls = %d, want 1", validationCalls)
	}
	for _, path := range []string{filepath.Join(tempDir, "AGENTS.md"), paths.StatefilePath, paths.RecoveryDir} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("%s stat error = %v, want absent", path, statErr)
		}
	}
}

func TestRunWritesHookProjectionThroughWorkflowComposition(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	paths := isolatedApplyTestPaths(t, tempDir)
	resources := desiredtest.Environment(t, desired.Spec{
		Targets:  []targetpkg.Target{targetpkg.TargetCodex},
		Defaults: desiredtest.Defaults(t, targetpkg.ScopeProject, skill.InstallModeCopy),
		Hooks: []hook.Hook{
			desiredtest.Hook(t, hook.Spec{
				Name:    "protect-env",
				Event:   "PreToolUse",
				Matcher: "Write",
				Type:    hook.TypeCommand,
				Command: "python3 hooks/protect.py",
				Targets: []targetpkg.Target{targetpkg.TargetCodex},
				Scope:   targetpkg.ScopeProject,
			}),
		},
	})
	selection := applySelection(t, []string{"codex"})
	lowered, err := topologyhook.Lower(nil, resources.Hooks())
	if err != nil {
		t.Fatalf("lower Hook topology: %v", err)
	}
	contracts, err := refine.HookContributions(
		resources.Hooks(),
		lowered,
		hookcodec.CanonicalHookContribution,
	)
	if err != nil {
		t.Fatalf("lock Hook contributions: %v", err)
	}
	locked := snapshottest.File(t, contracts...)

	assessment := buildManagedApplyAssessment(t, paths, resources, locked, selection, false)
	result, err := run(
		t,
		context.Background(),
		paths,
		resources,
		locked,
		selection,
		assessment,
	)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.ActionCount != 1 {
		t.Fatalf("ActionCount = %d, want 1", result.ActionCount)
	}

	expectedConfig := "{\n  \"hooks\": {\n    \"PreToolUse\": [\n      {\n        \"matcher\": \"Write\",\n        \"hooks\": [\n          {\n            \"type\": \"command\",\n            \"command\": \"python3 hooks/protect.py\"\n          }\n        ]\n      }\n    ]\n  }\n}\n"
	assertApplyFileContent(t, filepath.Join(tempDir, ".codex", "hooks.json"), expectedConfig)

	state, err := statefile.Load(t.Context(), paths.StatefilePath)
	if err != nil {
		t.Fatalf("load statefile: %v", err)
	}
	assertHookAggregateState(t, state, lowered.Projections()[0].SubjectID())
	assertNoApplyRecoveryArtifacts(t, paths)
}

func TestRunDeletesRemovedManagedOutputAndStatefileRecord(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	paths := isolatedApplyTestPaths(t, tempDir)
	writeApplyFile(t, filepath.Join(tempDir, "AGENTS.md"), "removed managed\n")
	writeApplyFile(t, filepath.Join(tempDir, "UNMANAGED.md"), "leave alone\n")
	removedHash := hashApplyPath(t, filepath.Join(tempDir, "AGENTS.md"))
	writeApplyStatefile(t, paths.StatefilePath, applyStateSnapshot(t, durable.SnapshotInput{
		ManagedPaths: []durable.ManagedPathState{
			applyInstructionPathState(t, "removed", []string{"codex"}, "project", "AGENTS.md", removedHash),
		},
	}))
	resources := applyEmptyEnvironment(t, targetpkg.TargetCodex)
	locked := snapshottest.File(t)
	selection, err := targetselection.ForAvailableTargets(
		[]targetpkg.Target{targetpkg.TargetCodex},
		[]string{"codex"},
	)
	if err != nil {
		t.Fatalf("build state-only selection: %v", err)
	}

	result, err := run(t, context.Background(), paths, resources, locked, selection, buildManagedApplyAssessment(t, paths, resources, locked, selection, false))
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.ActionCount != 1 {
		t.Fatalf("ActionCount = %d, want 1", result.ActionCount)
	}
	if _, err := os.Stat(filepath.Join(tempDir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("removed output exists or stat failed: %v", err)
	}
	assertApplyFileContent(t, filepath.Join(tempDir, "UNMANAGED.md"), "leave alone\n")
	state, err := statefile.Load(t.Context(), paths.StatefilePath)
	if err != nil {
		t.Fatalf("load statefile: %v", err)
	}
	assertApplyStateResourceMissing(t, state, "removed", "codex", "project", "AGENTS.md")
	assertNoApplyRecoveryArtifacts(t, paths)
}
