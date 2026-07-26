package build

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/instructions"
	"github.com/isty2e/daem/internal/desired/skill"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
)

func TestBuildBatchUsesListRootAndDeterministicConcreteRequests(t *testing.T) {
	tempDir := t.TempDir()
	alphaPath := writeSkill(t, tempDir, "skills/alpha")
	instructionsPath := writeFile(t, tempDir, "instructions/AGENTS.md", "project instructions\n")
	input := desired.Spec{
		SkillSets: []skill.SkillSet{
			projectCopySkillSet(t, sourcetest.Local(t, "skills", source.LocalSourceModeVendor), []skill.Selector{desiredtest.Selector(t, "glob:alpha")}, []target.Target{target.TargetCodex}, false),
		},
		Instructions: []instructions.Instructions{
			projectInstructions(t, "project", sourcetest.Local(t, "instructions/AGENTS.md", source.LocalSourceModeVendor), []target.Target{target.TargetCodex}),
		},
	}
	resolver := &trackingBatchResolver{
		listings: map[string]source.RootListing{
			"local:skills?mode=vendor": mustRootListing(
				t,
				sourcetest.Local(t, "skills", source.LocalSourceModeVendor),
				"",
				artifact.ArtifactKindDirectory,
				[]string{"alpha", "beta"},
			),
		},
		artifacts: map[string]resolutionFixture{
			"local:skills/alpha?mode=vendor": {
				SourceID:    "local:skills/alpha?mode=vendor",
				ContentPath: alphaPath,
				Kind:        artifact.ArtifactKindDirectory,
				ContentHash: "sha256:alpha",
			},
			"local:instructions/AGENTS.md?mode=vendor": {
				SourceID:    "local:instructions/AGENTS.md?mode=vendor",
				ContentPath: instructionsPath,
				Kind:        artifact.ArtifactKindFile,
				ContentHash: "sha256:instructions",
			},
		},
	}

	if _, err := buildWithTestOptions(context.Background(), lockEnvironment(t, input), resolver, Options{MaxParallelSourceOps: 4}); err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	if len(resolver.batches) != 2 {
		t.Fatalf("batch count = %d, want 2: %#v", len(resolver.batches), resolver.batches)
	}

	rootBatch := resolver.batches[0]
	if len(rootBatch) != 1 {
		t.Fatalf("root batch size = %d, want 1", len(rootBatch))
	}
	if rootBatch[0].Operation() != acquisition.OperationListRoot ||
		rootBatch[0].ID() != "skill_group_root:000000" ||
		rootBatch[0].Ordinal() != 0 {
		t.Fatalf("root request = %#v, want deterministic list-root request", rootBatch[0])
	}

	concreteBatch := resolver.batches[1]
	if len(concreteBatch) != 2 {
		t.Fatalf("concrete batch size = %d, want 2: %#v", len(concreteBatch), concreteBatch)
	}
	expected := []struct {
		id        acquisition.RequestID
		operation acquisition.Operation
		ordinal   int
	}{
		{id: "skill:000000", operation: acquisition.OperationResolve, ordinal: 0},
		{id: "instructions:000000", operation: acquisition.OperationResolve, ordinal: 0},
	}
	for index, want := range expected {
		got := concreteBatch[index]
		if got.ID() != want.id || got.Operation() != want.operation || got.Ordinal() != want.ordinal {
			t.Fatalf("concrete request[%d] = %#v, want id=%q operation=%q ordinal=%d", index, got, want.id, want.operation, want.ordinal)
		}
	}
}

func TestBuildBatchPinsGitGroupChildrenToListedRootRef(t *testing.T) {
	tempDir := t.TempDir()
	alphaPath := writeSkill(t, tempDir, "repo/alpha")
	resolvedRef := strings.Repeat("a", 40)
	rootSource := mustGitSource(t, "https://example.test/skills.git", ".", "main")
	rootSourceID := mustSourceID(t, rootSource)
	alphaSourceID := mustSourceID(t, mustGitSource(t, "https://example.test/skills.git", "alpha", resolvedRef))
	input := desired.Spec{
		SkillSets: []skill.SkillSet{
			projectCopySkillSet(t, rootSource, []skill.Selector{desiredtest.Selector(t, "glob:alpha")}, []target.Target{target.TargetCodex}, false),
		},
	}
	resolver := &trackingBatchResolver{
		listings: map[string]source.RootListing{
			rootSourceID: mustRootListing(
				t,
				rootSource,
				artifact.ResolvedRef(resolvedRef),
				artifact.ArtifactKindDirectory,
				[]string{"alpha"},
			),
		},
		artifacts: map[string]resolutionFixture{
			alphaSourceID: {
				SourceID:    artifact.SourceID(alphaSourceID),
				ContentPath: alphaPath,
				Kind:        artifact.ArtifactKindDirectory,
				ContentHash: "sha256:alpha",
				ResolvedRef: artifact.ResolvedRef(resolvedRef),
			},
		},
	}

	locked, err := buildWithTestOptions(context.Background(), lockEnvironment(t, input), resolver, Options{MaxParallelSourceOps: 4})
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	lockedSkill := mustLockedSubject(t, locked, entity.KindSkill, "alpha")
	if sourceID := mustExactSupply(t, lockedSkill).SourceID(); string(sourceID) != alphaSourceID {
		t.Fatalf("locked skill source_id = %q, want child pinned to listed resolved ref", sourceID)
	}
}

func TestBuildBatchRejectsMismatchedResultRequest(t *testing.T) {
	input := desired.Spec{
		Skills: []skill.Skill{
			projectCopySkill(t, "alpha", sourcetest.Local(t, "skills/alpha", source.LocalSourceModeVendor), []target.Target{target.TargetCodex}, false),
		},
	}
	_, err := buildWithTestOptions(
		context.Background(),
		lockEnvironment(t, input),
		&trackingBatchResolver{mismatchFirstRequest: true},
		Options{MaxParallelSourceOps: 2},
	)
	if err == nil {
		t.Fatal("BuildWithOptions returned nil error")
	}
	if !strings.Contains(err.Error(), "echoed request") {
		t.Fatalf("error = %q, want echoed request mismatch diagnostic", err)
	}
}

func TestBuildBatchRejectsShortResultList(t *testing.T) {
	input := desired.Spec{
		Skills: []skill.Skill{
			projectCopySkill(t, "alpha", sourcetest.Local(t, "skills/alpha", source.LocalSourceModeVendor), []target.Target{target.TargetCodex}, false),
		},
	}
	_, err := buildWithTestOptions(
		context.Background(),
		lockEnvironment(t, input),
		&trackingBatchResolver{dropLastResult: true},
		Options{MaxParallelSourceOps: 2},
	)
	if err == nil {
		t.Fatal("BuildWithOptions returned nil error")
	}
	if !strings.Contains(err.Error(), "source batch returned 0 results for 1 requests") {
		t.Fatalf("error = %q, want batch result length diagnostic", err)
	}
}

func TestBuildBatchErrorPriorityIgnoresCompletionOrder(t *testing.T) {
	input := desired.Spec{
		Skills: []skill.Skill{
			projectCopySkill(t, "alpha", sourcetest.Local(t, "skills/alpha", source.LocalSourceModeVendor), []target.Target{target.TargetCodex}, false),
			projectCopySkill(t, "beta", sourcetest.Local(t, "skills/beta", source.LocalSourceModeVendor), []target.Target{target.TargetCodex}, false),
		},
		Instructions: []instructions.Instructions{
			projectInstructions(t, "project", sourcetest.Local(t, "instructions/AGENTS.md", source.LocalSourceModeVendor), []target.Target{target.TargetCodex}),
		},
	}
	resolver := &trackingBatchResolver{
		errors: map[batchErrorKey]error{
			{operation: acquisition.OperationResolve, sourceID: "local:skills/alpha?mode=vendor"}:           errors.New("alpha failed after beta"),
			{operation: acquisition.OperationResolve, sourceID: "local:skills/beta?mode=vendor"}:            errors.New("beta failed first"),
			{operation: acquisition.OperationResolve, sourceID: "local:instructions/AGENTS.md?mode=vendor"}: errors.New("instructions failed first"),
		},
	}

	_, err := buildWithTestOptions(context.Background(), lockEnvironment(t, input), resolver, Options{MaxParallelSourceOps: 4})
	if err == nil {
		t.Fatal("BuildWithOptions returned nil error")
	}
	if !strings.Contains(err.Error(), `resolve skill "alpha"`) || strings.Contains(err.Error(), "beta failed first") {
		t.Fatalf("error = %q, want first skill ordinal to outrank later completions", err)
	}
}

func TestBuildBatchDuplicateSourceKeepsFirstTaskContext(t *testing.T) {
	input := desired.Spec{
		Skills: []skill.Skill{
			projectCopySkill(t, "alpha", sourcetest.Local(t, "shared", source.LocalSourceModeVendor), []target.Target{target.TargetCodex}, false),
			projectCopySkill(t, "beta", sourcetest.Local(t, "shared", source.LocalSourceModeVendor), []target.Target{target.TargetCodex}, false),
		},
	}
	resolver := &trackingBatchResolver{
		errors: map[batchErrorKey]error{
			{operation: acquisition.OperationResolve, sourceID: "local:shared?mode=vendor"}: errors.New("shared source failed"),
		},
	}

	_, err := buildWithTestOptions(context.Background(), lockEnvironment(t, input), resolver, Options{MaxParallelSourceOps: 4})
	if err == nil {
		t.Fatal("BuildWithOptions returned nil error")
	}
	if !strings.Contains(err.Error(), `resolve skill "alpha"`) || strings.Contains(err.Error(), `resolve skill "beta"`) {
		t.Fatalf("error = %q, want shared source error with first lock task context", err)
	}
}

func TestBuildBatchSkillArtifactErrorOutranksInstructionSourceError(t *testing.T) {
	tempDir := t.TempDir()
	notSkillPath := writeFile(t, tempDir, "skills/not-a-skill.md", "not a skill\n")
	input := desired.Spec{
		Skills: []skill.Skill{
			projectCopySkill(t, "bad-skill", sourcetest.Local(t, "skills/not-a-skill.md", source.LocalSourceModeVendor), []target.Target{target.TargetCodex}, false),
		},
		Instructions: []instructions.Instructions{
			projectInstructions(t, "project", sourcetest.Local(t, "instructions/AGENTS.md", source.LocalSourceModeVendor), []target.Target{target.TargetCodex}),
		},
	}
	resolver := &trackingBatchResolver{
		artifacts: map[string]resolutionFixture{
			"local:skills/not-a-skill.md?mode=vendor": {
				SourceID:    "local:skills/not-a-skill.md?mode=vendor",
				ContentPath: notSkillPath,
				Kind:        artifact.ArtifactKindFile,
				ContentHash: "sha256:not-skill",
			},
		},
		errors: map[batchErrorKey]error{
			{operation: acquisition.OperationResolve, sourceID: "local:instructions/AGENTS.md?mode=vendor"}: errors.New("instruction source failed first"),
		},
	}

	_, err := buildWithTestOptions(context.Background(), lockEnvironment(t, input), resolver, Options{MaxParallelSourceOps: 4})
	if err == nil {
		t.Fatal("BuildWithOptions returned nil error")
	}
	if !strings.Contains(err.Error(), `validate skill "bad-skill"`) || strings.Contains(err.Error(), "instruction source failed first") {
		t.Fatalf("error = %q, want skill artifact validation to outrank instruction source error", err)
	}
}

func TestBuildBatchInstructionSourceErrorsUseInstructionOrdinal(t *testing.T) {
	input := desired.Spec{
		Instructions: []instructions.Instructions{
			projectInstructions(t, "project", sourcetest.Local(t, "instructions/project.md", source.LocalSourceModeVendor), []target.Target{target.TargetCodex}),
			projectInstructions(t, "other", sourcetest.Local(t, "instructions/other.md", source.LocalSourceModeVendor), []target.Target{target.TargetCodex}),
		},
	}
	resolver := &trackingBatchResolver{
		errors: map[batchErrorKey]error{
			{operation: acquisition.OperationResolve, sourceID: "local:instructions/project.md?mode=vendor"}: errors.New("project failed after other"),
			{operation: acquisition.OperationResolve, sourceID: "local:instructions/other.md?mode=vendor"}:   errors.New("other failed first"),
		},
	}

	_, err := buildWithTestOptions(context.Background(), lockEnvironment(t, input), resolver, Options{MaxParallelSourceOps: 4})
	if err == nil {
		t.Fatal("BuildWithOptions returned nil error")
	}
	if !strings.Contains(err.Error(), `resolve instructions "project" source`) || strings.Contains(err.Error(), "other failed first") {
		t.Fatalf("error = %q, want first instructions ordinal to outrank later completion", err)
	}
}

func TestBuildBatchGroupErrorsUseGroupIndexAndBlockChildResolution(t *testing.T) {
	input := desired.Spec{
		SkillSets: []skill.SkillSet{
			projectCopySkillSet(t, sourcetest.Local(t, "groups/first", source.LocalSourceModeVendor), []skill.Selector{desiredtest.Selector(t, "glob:*")}, []target.Target{target.TargetCodex}, false),
			projectCopySkillSet(t, sourcetest.Local(t, "groups/second", source.LocalSourceModeVendor), []skill.Selector{desiredtest.Selector(t, "glob:*")}, []target.Target{target.TargetCodex}, false),
		},
	}
	resolver := &trackingBatchResolver{
		errors: map[batchErrorKey]error{
			{operation: acquisition.OperationListRoot, sourceID: "local:groups/first?mode=vendor"}:  errors.New("first failed after second"),
			{operation: acquisition.OperationListRoot, sourceID: "local:groups/second?mode=vendor"}: errors.New("second failed first"),
		},
	}

	_, err := buildWithTestOptions(context.Background(), lockEnvironment(t, input), resolver, Options{MaxParallelSourceOps: 4})
	if err == nil {
		t.Fatal("BuildWithOptions returned nil error")
	}
	if !strings.Contains(err.Error(), "list skill_group[0] source") || strings.Contains(err.Error(), "second failed first") {
		t.Fatalf("error = %q, want group index 0 diagnostic", err)
	}
	if hasBatchOperation(resolver.batches, acquisition.OperationResolve) {
		t.Fatalf("resolver batches include concrete resolve despite group listing failure: %#v", resolver.batches)
	}
}

func TestBuildBatchValidationBarriersBlockPrematureResolution(t *testing.T) {
	t.Run("duplicate skill id after group expansion", func(t *testing.T) {
		input := desired.Spec{
			Skills: []skill.Skill{
				projectCopySkill(t, "demo", sourcetest.Local(t, "direct/demo", source.LocalSourceModeVendor), []target.Target{target.TargetCodex}, false),
			},
			SkillSets: []skill.SkillSet{
				projectCopySkillSet(t, sourcetest.Local(t, "groups", source.LocalSourceModeVendor), []skill.Selector{desiredtest.Selector(t, "glob:demo")}, []target.Target{target.TargetCodex}, false),
			},
		}
		resolver := &trackingBatchResolver{
			listings: map[string]source.RootListing{
				"local:groups?mode=vendor": mustRootListing(
					t,
					sourcetest.Local(t, "groups", source.LocalSourceModeVendor),
					"",
					artifact.ArtifactKindDirectory,
					[]string{"demo"},
				),
			},
		}

		_, err := buildWithTestOptions(context.Background(), lockEnvironment(t, input), resolver, Options{MaxParallelSourceOps: 4})
		if err == nil {
			t.Fatal("BuildWithOptions returned nil error")
		}
		if !strings.Contains(err.Error(), `duplicate skill id "demo"`) {
			t.Fatalf("error = %q, want duplicate skill diagnostic", err)
		}
		if hasBatchOperation(resolver.batches, acquisition.OperationResolve) {
			t.Fatalf("resolver batches include concrete resolve despite duplicate skill: %#v", resolver.batches)
		}
	})

	t.Run("group expansion failure", func(t *testing.T) {
		input := desired.Spec{
			SkillSets: []skill.SkillSet{
				projectCopySkillSet(t, sourcetest.Local(t, "groups", source.LocalSourceModeVendor), []skill.Selector{desiredtest.Selector(t, "glob:*")}, []target.Target{target.TargetCodex}, false),
			},
		}
		resolver := &trackingBatchResolver{
			listings: map[string]source.RootListing{
				"local:groups?mode=vendor": mustRootListing(
					t,
					sourcetest.Local(t, "groups", source.LocalSourceModeVendor),
					"",
					artifact.ArtifactKindDirectory,
					nil,
				),
			},
		}

		_, err := buildWithTestOptions(context.Background(), lockEnvironment(t, input), resolver, Options{MaxParallelSourceOps: 4})
		if err == nil {
			t.Fatal("BuildWithOptions returned nil error")
		}
		if !strings.Contains(err.Error(), "matched no skill directories") {
			t.Fatalf("error = %q, want group expansion diagnostic", err)
		}
		if hasBatchOperation(resolver.batches, acquisition.OperationResolve) {
			t.Fatalf("resolver batches include concrete resolve despite group expansion failure: %#v", resolver.batches)
		}
	})
}

func TestBuildBatchExternalCancellationWinsOverOrdinaryErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	input := desired.Spec{
		Skills: []skill.Skill{
			projectCopySkill(t, "alpha", sourcetest.Local(t, "skills/alpha", source.LocalSourceModeVendor), []target.Target{target.TargetCodex}, false),
		},
	}
	resolver := &trackingBatchResolver{
		cancelAfterBatch: cancel,
		errors: map[batchErrorKey]error{
			{operation: acquisition.OperationResolve, sourceID: "local:skills/alpha?mode=vendor"}: errors.New("ordinary source failure"),
		},
	}

	_, err := buildWithTestOptions(ctx, lockEnvironment(t, input), resolver, Options{MaxParallelSourceOps: 4})
	if err == nil {
		t.Fatal("BuildWithOptions returned nil error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}
