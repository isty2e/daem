package build

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/skill"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
)

func TestBuildAcceptsMaximumSelectedSkillsAndLockfileRoundTrips(t *testing.T) {
	const skillCount = 4_096
	tempDir := t.TempDir()
	sharedSkillPath := writeSkill(t, tempDir, "shared-skill")
	rootSource := sourcetest.Local(t, "groups", source.LocalSourceModeVendor)
	childNames := make([]string, 0, skillCount)
	artifacts := make(map[string]resolutionFixture, skillCount)
	for index := range skillCount {
		name := fmt.Sprintf("skill-%04d", index)
		childNames = append(childNames, name)
		sourceID := artifact.SourceID("local:groups/" + name + "?mode=vendor")
		artifacts[string(sourceID)] = resolutionFixture{
			SourceID: sourceID, ContentPath: sharedSkillPath, Kind: artifact.ArtifactKindDirectory,
		}
	}
	resolver := &rootListingResolver{
		root:      mustRootListing(t, rootSource, "", artifact.ArtifactKindDirectory, childNames),
		artifacts: artifacts,
	}
	environment := lockEnvironment(t, desired.Spec{SkillSets: []skill.SkillSet{
		projectCopySkillSet(
			t,
			rootSource,
			[]skill.Selector{desiredtest.Selector(t, "glob:*")},
			[]target.Target{target.TargetCodex},
			false,
		),
	}})

	locked, err := buildWithTestOptions(t.Context(), environment, resolver, Options{})
	if err != nil {
		t.Fatalf("BuildWithOptions(%d selected skills): %v", skillCount, err)
	}
	content, err := lockfile.Marshal(locked)
	if err != nil {
		t.Fatalf("lockfile.Marshal(%d selected skills): %v", skillCount, err)
	}
	path := filepath.Join(t.TempDir(), "daem.lock.toml")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write lockfile: %v", err)
	}
	loaded, err := lockfile.Load(t.Context(), path)
	if err != nil {
		t.Fatalf("lockfile.Load(%d selected skills): %v", skillCount, err)
	}
	reencoded, err := lockfile.Marshal(loaded)
	if err != nil {
		t.Fatalf("lockfile.Marshal(loaded): %v", err)
	}
	if !bytes.Equal(reencoded, content) {
		t.Fatal("maximum selected-skill lockfile did not round trip byte-exactly")
	}
}

func TestBuildRejectsExpansionBudgetBeforeResolvingChildren(t *testing.T) {
	rootSource := sourcetest.Local(t, "groups", source.LocalSourceModeVendor)
	childNames := make([]string, 0, 4_097)
	for index := range 4_097 {
		childNames = append(childNames, fmt.Sprintf("skill-%04d", index))
	}
	listing := mustRootListing(
		t,
		rootSource,
		"",
		artifact.ArtifactKindDirectory,
		childNames,
	)
	input := desired.Spec{
		SkillSets: []skill.SkillSet{
			projectCopySkillSet(
				t,
				rootSource,
				[]skill.Selector{desiredtest.Selector(t, "glob:*")},
				[]target.Target{target.TargetCodex},
				false,
			),
		},
	}

	t.Run("sequential", func(t *testing.T) {
		resolver := &rootListingResolver{root: listing}
		locked, err := buildWithTestOptions(
			context.Background(),
			lockEnvironment(t, input),
			resolver,
			Options{},
		)
		assertSelectedSkillExpansionLimit(t, err)
		assertEmptyLockResult(t, locked)
		if len(resolver.resolved) != 0 {
			t.Fatalf("resolved children = %d, want none", len(resolver.resolved))
		}
	})

	t.Run("batch", func(t *testing.T) {
		resolver := &trackingBatchResolver{
			listings: map[string]source.RootListing{
				"local:groups?mode=vendor": listing,
			},
		}
		locked, err := buildWithTestOptions(
			context.Background(),
			lockEnvironment(t, input),
			resolver,
			Options{MaxParallelSourceOps: 4},
		)
		assertSelectedSkillExpansionLimit(t, err)
		assertEmptyLockResult(t, locked)
		if hasBatchOperation(resolver.batches, acquisition.OperationResolve) {
			t.Fatalf("source batches include child resolution after expansion failure: %#v", resolver.batches)
		}
	})
}

func TestSequentialSkillGroupListingsDedupeLikeBatchListings(t *testing.T) {
	rootSource := sourcetest.Local(t, "groups", source.LocalSourceModeVendor)
	listing := mustRootListing(
		t,
		rootSource,
		"",
		artifact.ArtifactKindDirectory,
		[]string{"alpha", "beta"},
	)
	first := projectCopySkillSet(
		t,
		rootSource,
		[]skill.Selector{desiredtest.Selector(t, "glob:alpha")},
		[]target.Target{target.TargetCodex},
		false,
	)
	second := projectCopySkillSet(
		t,
		rootSource,
		[]skill.Selector{desiredtest.Selector(t, "glob:beta")},
		[]target.Target{target.TargetCodex},
		false,
	)
	resolver := &rootListingResolver{root: listing}

	results, err := sourceTaskResults(
		context.Background(),
		resolver,
		[]sourceTask{newSkillGroupListTask(0, first), newSkillGroupListTask(1, second)},
		Options{},
	)
	if err != nil {
		t.Fatalf("sourceTaskResults returned error: %v", err)
	}
	if len(results) != 2 || resolver.listed != 1 {
		t.Fatalf("results/list calls = %d/%d, want 2/1", len(results), resolver.listed)
	}
}

func TestSequentialSkillGroupListingsRejectRootCountBeforeBackendWork(t *testing.T) {
	tasks := make([]sourceTask, 0, 1_025)
	for index := range 1_025 {
		rootSource := sourcetest.Local(
			t,
			fmt.Sprintf("groups/%04d", index),
			source.LocalSourceModeVendor,
		)
		set := projectCopySkillSet(
			t,
			rootSource,
			[]skill.Selector{desiredtest.Selector(t, "glob:*")},
			[]target.Target{target.TargetCodex},
			false,
		)
		tasks = append(tasks, newSkillGroupListTask(index, set))
	}
	resolver := &rootListingResolver{}

	results, err := sourceTaskResults(context.Background(), resolver, tasks, Options{})
	if results != nil || !errors.Is(err, source.ErrRootListingLimitExceeded) {
		t.Fatalf("results/error = %#v/%v, want root listing limit", results, err)
	}
	if resolver.listed != 0 {
		t.Fatalf("backend list calls = %d, want none", resolver.listed)
	}
}

func TestSkillGroupCountRejectsBeforeSourceTaskConstruction(t *testing.T) {
	rootSource := sourcetest.Local(t, "groups", source.LocalSourceModeVendor)
	set := projectCopySkillSet(
		t,
		rootSource,
		[]skill.Selector{desiredtest.Selector(t, "glob:*")},
		[]target.Target{target.TargetCodex},
		false,
	)
	sets := make([]skill.SkillSet, 0, 1_025)
	for range 1_025 {
		sets = append(sets, set)
	}
	resolver := &rootListingResolver{}
	events := make([]Event, 0)

	resources, err := expandLockableSkillSetsFromListings(
		context.Background(),
		sets,
		resolver,
		Options{Events: func(event Event) { events = append(events, event) }},
	)
	if resources != nil {
		t.Fatalf("expanded resources = %#v, want none", resources)
	}
	var limitErr *skill.ExpansionLimitError
	if !errors.As(err, &limitErr) || limitErr.Kind() != skill.ExpansionLimitGroups ||
		limitErr.Limit() != 1_024 || limitErr.Observed() != 1_025 {
		t.Fatalf("expansion error = %#v, want groups 1025/1024", err)
	}
	if resolver.listed != 0 {
		t.Fatalf("backend list calls = %d, want none", resolver.listed)
	}
	if len(events) != 0 {
		t.Fatalf("events = %#v, want none before group admission", events)
	}
}

func assertSelectedSkillExpansionLimit(t *testing.T, err error) {
	t.Helper()
	var limitErr *skill.ExpansionLimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("build error = %v, want ExpansionLimitError", err)
	}
	if limitErr.Kind() != skill.ExpansionLimitSelectedSkills ||
		limitErr.Limit() != 4_096 || limitErr.Observed() != 4_097 {
		t.Fatalf("limit error = %#v, want selected_skills 4097/4096", limitErr)
	}
}

func assertEmptyLockResult(t *testing.T, locked lock.File) {
	t.Helper()
	if locked.Version != 0 || len(locked.Locked.Subjects()) != 0 ||
		len(locked.Locked.OrderConstraints()) != 0 {
		t.Fatalf("failed build returned partial lock: %#v", locked)
	}
}
