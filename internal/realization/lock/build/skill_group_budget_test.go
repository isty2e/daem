package build

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/skill"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
)

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

func TestSequentialSkillGroupListingsCountOneReusedSourceRoot(t *testing.T) {
	rootSource := sourcetest.Local(t, "groups", source.LocalSourceModeVendor)
	set := projectCopySkillSet(
		t,
		rootSource,
		[]skill.Selector{desiredtest.Selector(t, "glob:*")},
		[]target.Target{target.TargetCodex},
		false,
	)
	tasks := make([]sourceTask, 0, 1_025)
	for index := range 1_025 {
		tasks = append(tasks, newSkillGroupListTask(index, set))
	}
	resolver := &rootListingResolver{
		root: mustRootListing(
			t,
			rootSource,
			"",
			artifact.ArtifactKindDirectory,
			[]string{"alpha"},
		),
	}

	results, err := sourceTaskResults(context.Background(), resolver, tasks, Options{})
	if err != nil {
		t.Fatalf("sourceTaskResults returned error: %v", err)
	}
	if len(results) != len(tasks) || resolver.listed != 1 {
		t.Fatalf("results/list calls = %d/%d, want %d/1", len(results), resolver.listed, len(tasks))
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
