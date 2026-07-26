package build

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/instructions"
	"github.com/isty2e/daem/internal/desired/skill"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
)

func TestBuildWithOptionsSequentialAndParallelMatchBuild(t *testing.T) {
	tempDir := t.TempDir()
	alphaPath := writeSkill(t, tempDir, "skills/alpha")
	zetaPath := writeSkill(t, tempDir, "skills/zeta")
	instructionsPath := writeFile(t, tempDir, "instructions/AGENTS.md", "project instructions\n")
	input := desired.Spec{
		Skills: []skill.Skill{
			projectCopySkill(t, "zeta", sourcetest.Local(t, "skills/zeta", source.LocalSourceModeVendor), []target.Target{target.TargetCodex}, false),
			projectCopySkill(t, "alpha", sourcetest.Local(t, "skills/alpha", source.LocalSourceModeVendor), []target.Target{target.TargetCodex}, false),
		},
		Instructions: []instructions.Instructions{
			projectInstructions(t, "project", sourcetest.Local(t, "instructions/AGENTS.md", source.LocalSourceModeVendor), []target.Target{target.TargetCodex}),
		},
	}
	artifacts := map[string]resolutionFixture{
		"local:skills/alpha?mode=vendor": {
			SourceID:    "local:skills/alpha?mode=vendor",
			ContentPath: alphaPath,
			Kind:        artifact.ArtifactKindDirectory,
			ContentHash: "sha256:alpha",
		},
		"local:skills/zeta?mode=vendor": {
			SourceID:    "local:skills/zeta?mode=vendor",
			ContentPath: zetaPath,
			Kind:        artifact.ArtifactKindDirectory,
			ContentHash: "sha256:zeta",
		},
		"local:instructions/AGENTS.md?mode=vendor": {
			SourceID:    "local:instructions/AGENTS.md?mode=vendor",
			ContentPath: instructionsPath,
			Kind:        artifact.ArtifactKindFile,
			ContentHash: "sha256:instructions",
		},
	}

	wrapperSnapshot, err := buildWithTestOptions(context.Background(), lockEnvironment(t, input), &trackingBatchResolver{artifacts: artifacts}, Options{})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	sequentialSnapshot, err := buildWithTestOptions(
		context.Background(),
		lockEnvironment(t, input),
		&trackingBatchResolver{artifacts: artifacts},
		Options{MaxParallelSourceOps: 1},
	)
	if err != nil {
		t.Fatalf("BuildWithOptions sequential returned error: %v", err)
	}
	parallelSnapshot, err := buildWithTestOptions(
		context.Background(),
		lockEnvironment(t, input),
		&trackingBatchResolver{artifacts: artifacts},
		Options{MaxParallelSourceOps: 4},
	)
	if err != nil {
		t.Fatalf("BuildWithOptions parallel returned error: %v", err)
	}

	if !reflect.DeepEqual(wrapperSnapshot, sequentialSnapshot) {
		t.Fatalf("BuildWithOptions sequential snapshot differs from Build:\nBuild=%#v\nSequential=%#v", wrapperSnapshot, sequentialSnapshot)
	}
	if !reflect.DeepEqual(wrapperSnapshot, parallelSnapshot) {
		t.Fatalf("BuildWithOptions parallel snapshot differs from Build:\nBuild=%#v\nParallel=%#v", wrapperSnapshot, parallelSnapshot)
	}

	wrapperBytes, err := lockfile.Marshal(wrapperSnapshot)
	if err != nil {
		t.Fatalf("Marshal wrapper snapshot returned error: %v", err)
	}
	parallelBytes, err := lockfile.Marshal(parallelSnapshot)
	if err != nil {
		t.Fatalf("Marshal parallel snapshot returned error: %v", err)
	}
	if !bytes.Equal(wrapperBytes, parallelBytes) {
		t.Fatalf("parallel lockfile bytes differ from wrapper:\n%s\n---\n%s", wrapperBytes, parallelBytes)
	}
}

func TestBuildWithOptionsEmitsLockEventsAndPassesSourceEvents(t *testing.T) {
	tempDir := t.TempDir()
	skillPath := writeSkill(t, tempDir, "skills/demo")
	instructionsPath := writeFile(t, tempDir, "instructions/AGENTS.md", "project instructions\n")
	input := desired.Spec{
		Skills: []skill.Skill{
			projectCopySkill(t, "demo", sourcetest.Local(t, "skills/demo", source.LocalSourceModeVendor), []target.Target{target.TargetCodex}, false),
		},
		Instructions: []instructions.Instructions{
			projectInstructions(t, "project", sourcetest.Local(t, "instructions/AGENTS.md", source.LocalSourceModeVendor), []target.Target{target.TargetCodex}),
		},
	}
	resolver := &trackingBatchResolver{
		artifacts: map[string]resolutionFixture{
			"local:skills/demo?mode=vendor": {
				SourceID:    "local:skills/demo?mode=vendor",
				ContentPath: skillPath,
				Kind:        artifact.ArtifactKindDirectory,
				ContentHash: "sha256:demo",
			},
			"local:instructions/AGENTS.md?mode=vendor": {
				SourceID:    "local:instructions/AGENTS.md?mode=vendor",
				ContentPath: instructionsPath,
				Kind:        artifact.ArtifactKindFile,
				ContentHash: "sha256:instructions",
			},
		},
	}
	lockEvents := newLockEventRecorder()
	sourceEvents := func(acquisition.Event) {}

	_, err := buildWithTestOptions(context.Background(), lockEnvironment(t, input), resolver, Options{
		MaxParallelSourceOps: 4,
		Events:               lockEvents.sink,
		SourceEvents:         sourceEvents,
	})
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	if len(resolver.batchOptions) != 1 || resolver.batchOptions[0].Events() == nil {
		t.Fatalf("batch options = %#v, want source event sink forwarded", resolver.batchOptions)
	}

	events := lockEvents.snapshot()
	wantKinds := []EventKind{
		EventResourceResolveStarted,
		EventResourceResolveStarted,
		EventResourceResolved,
		EventResourceResolved,
		EventResourceLocked,
		EventResourceLocked,
		EventSnapshotValidated,
	}
	gotKinds := lockEventKinds(events)
	if !reflect.DeepEqual(gotKinds, wantKinds) {
		t.Fatalf("lock event kinds = %#v, want %#v; events=%#v", gotKinds, wantKinds, events)
	}
	if events[0].EntityID != mustEntityID(t, entity.KindSkill, "demo") ||
		events[1].EntityID != mustEntityID(t, entity.KindInstructions, "project") {
		t.Fatalf("resolve start events = %#v, want skill then instructions resource ids", events[:2])
	}
	if events[len(events)-1].Stage != EventStageSnapshot || events[len(events)-1].Count != 4 {
		t.Fatalf("snapshot event = %#v, want snapshot count 4", events[len(events)-1])
	}
}

func TestBuildWithOptionsEmitsSkillGroupExpansionEvents(t *testing.T) {
	tempDir := t.TempDir()
	alphaPath := writeSkill(t, tempDir, "skills/alpha")
	input := desired.Spec{
		SkillSets: []skill.SkillSet{
			projectCopySkillSet(t, sourcetest.Local(t, "skills", source.LocalSourceModeVendor), []skill.Selector{desiredtest.Selector(t, "glob:alpha")}, []target.Target{target.TargetCodex}, false),
		},
	}
	resolver := &trackingBatchResolver{
		listings: map[string]source.RootListing{
			"local:skills?mode=vendor": mustRootListing(
				t,
				sourcetest.Local(t, "skills", source.LocalSourceModeVendor),
				"",
				artifact.ArtifactKindDirectory,
				[]string{"alpha"},
			),
		},
		artifacts: map[string]resolutionFixture{
			"local:skills/alpha?mode=vendor": {
				SourceID:    "local:skills/alpha?mode=vendor",
				ContentPath: alphaPath,
				Kind:        artifact.ArtifactKindDirectory,
				ContentHash: "sha256:alpha",
			},
		},
	}
	lockEvents := newLockEventRecorder()

	_, err := buildWithTestOptions(context.Background(), lockEnvironment(t, input), resolver, Options{
		MaxParallelSourceOps: 4,
		Events:               lockEvents.sink,
	})
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}

	events := lockEvents.snapshot()
	listStarted := filterLockEvents(events, EventSkillGroupListStarted)
	if len(listStarted) != 1 ||
		listStarted[0].Stage != EventStageSkillGroupRoot ||
		listStarted[0].SkillGroupIndex == nil ||
		*listStarted[0].SkillGroupIndex != 0 ||
		listStarted[0].EntityID != (entity.ID{}) {
		t.Fatalf("skill group list event = %#v, want group-root index event without resource id", listStarted)
	}
	expanded := filterLockEvents(events, EventSkillGroupExpanded)
	if len(expanded) != 1 || expanded[0].Count != 1 {
		t.Fatalf("skill group expanded events = %#v, want one child count", expanded)
	}
}

func TestBuildWithOptionsSequentialEventsDoNotStartUnreachedTasks(t *testing.T) {
	input := desired.Spec{
		Skills: []skill.Skill{
			projectCopySkill(t, "alpha", sourcetest.Local(t, "skills/alpha", source.LocalSourceModeVendor), []target.Target{target.TargetCodex}, false),
			projectCopySkill(t, "beta", sourcetest.Local(t, "skills/beta", source.LocalSourceModeVendor), []target.Target{target.TargetCodex}, false),
		},
	}
	resolver := failingSequentialResolver{
		errBySourceID: map[string]error{
			"local:skills/alpha?mode=vendor": errors.New("alpha failed"),
		},
	}
	lockEvents := newLockEventRecorder()

	_, err := buildWithTestOptions(context.Background(), lockEnvironment(t, input), resolver, Options{Events: lockEvents.sink})
	if err == nil {
		t.Fatal("BuildWithOptions returned nil error")
	}
	if !strings.Contains(err.Error(), `resolve skill "alpha"`) {
		t.Fatalf("error = %q, want alpha source diagnostic", err)
	}

	events := lockEvents.snapshot()
	wantKinds := []EventKind{EventResourceResolveStarted, EventResourceResolveFailed}
	if got := lockEventKinds(events); !reflect.DeepEqual(got, wantKinds) {
		t.Fatalf("events = %#v, want only alpha start/fail", events)
	}
	for _, event := range events {
		if event.EntityID != mustEntityID(t, entity.KindSkill, "alpha") {
			t.Fatalf("event = %#v, want no event for unreached beta task", event)
		}
	}
}

func TestBuildWithOptionsMaxParallelOneMatchesBuildDiagnostics(t *testing.T) {
	input := desired.Spec{
		Skills: []skill.Skill{
			projectCopySkill(t, "demo", sourcetest.Local(t, "skills/demo", source.LocalSourceModeVendor), []target.Target{target.TargetCodex}, false),
		},
	}

	_, wrapperErr := buildWithTestOptions(context.Background(), lockEnvironment(t, input), &trackingBatchResolver{}, Options{})
	if wrapperErr == nil {
		t.Fatal("Build returned nil error")
	}
	_, optionsErr := buildWithTestOptions(context.Background(), lockEnvironment(t, input), &trackingBatchResolver{}, Options{MaxParallelSourceOps: 1})
	if optionsErr == nil {
		t.Fatal("BuildWithOptions returned nil error")
	}
	if wrapperErr.Error() != optionsErr.Error() {
		t.Fatalf("BuildWithOptions MaxParallel=1 diagnostic = %q, Build diagnostic = %q", optionsErr, wrapperErr)
	}
}

func TestBuildWithOptionsFallsBackToRootListerWithoutBatchResolver(t *testing.T) {
	tempDir := t.TempDir()
	alphaPath := writeSkill(t, tempDir, "skills/alpha")
	input := desired.Spec{
		SkillSets: []skill.SkillSet{
			projectCopySkillSet(t, sourcetest.Local(t, "skills", source.LocalSourceModeVendor), []skill.Selector{desiredtest.Selector(t, "glob:alpha")}, []target.Target{target.TargetCodex}, false),
		},
	}
	resolver := &rootListingResolver{
		root: mustRootListing(
			t,
			sourcetest.Local(t, "skills", source.LocalSourceModeVendor),
			"",
			artifact.ArtifactKindDirectory,
			[]string{"alpha", "beta"},
		),
		artifacts: map[string]resolutionFixture{
			"local:skills/alpha?mode=vendor": {
				SourceID:    "local:skills/alpha?mode=vendor",
				ContentPath: alphaPath,
				Kind:        artifact.ArtifactKindDirectory,
				ContentHash: "sha256:alpha",
			},
		},
	}

	locked, err := buildWithTestOptions(context.Background(), lockEnvironment(t, input), resolver, Options{MaxParallelSourceOps: 4})
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	lockedSkills := lockedExactSupplySubjectsOfKind(locked, entity.KindSkill)
	if len(lockedSkills) != 1 || lockedSkills[0].EntityID().Name() != "alpha" {
		t.Fatalf("locked skills = %#v, want selected alpha", lockedSkills)
	}
	if len(resolver.resolved) != 1 || resolver.resolved[0] != "local:skills/alpha?mode=vendor" {
		t.Fatalf("resolved source IDs = %#v, want only selected child", resolver.resolved)
	}
}
