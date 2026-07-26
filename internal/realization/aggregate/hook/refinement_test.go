package commandhook

import (
	"path/filepath"
	"strings"
	"testing"

	desiredhook "github.com/isty2e/daem/internal/desired/hook"
	desiredhookasset "github.com/isty2e/daem/internal/desired/hookasset"
	desiredfixture "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/topology"
	topologyhook "github.com/isty2e/daem/internal/topology/hook"
)

func TestPortableAndAvailableContributionsKeepPathResolutionOutOfTopology(t *testing.T) {
	assets := []desiredhookasset.HookAsset{testHookAsset(t, "guard", target.ScopeProject)}
	hooks := []desiredhook.Hook{
		testHook(t, "protect", target.ScopeProject, []target.Target{target.TargetCodex}, "python {hook_file:guard}"),
	}
	lowered := lowerHookRefinementFixture(t, assets, hooks)
	assetSubject := lowered.AssetProjections()[0].SubjectID()

	portable, err := PortableContributions(hooks, lowered, testContributionEncoder)
	if err != nil {
		t.Fatalf("PortableContributions returned error: %v", err)
	}
	if got := contributionContent(t, portable, lowered.Projections()[0].SubjectID()); !strings.Contains(got, "{hook_file:guard}") {
		t.Fatalf("portable contribution = %q, want unresolved HookAsset reference", got)
	}

	resolved, err := ContributionsWithAvailablePaths(
		hooks,
		lowered,
		map[topology.SubjectID]string{assetSubject: "/managed/guard"},
		testContributionEncoder,
	)
	if err != nil {
		t.Fatalf("ContributionsWithAvailablePaths returned error: %v", err)
	}
	got := contributionContent(t, resolved, lowered.Projections()[0].SubjectID())
	if !strings.Contains(got, "/managed/guard") || strings.Contains(got, "{hook_file:guard}") {
		t.Fatalf("resolved contribution = %q, want physical path without placeholder", got)
	}
	if lowered.AssetProjections()[0].SubjectID() != assetSubject {
		t.Fatal("path refinement changed canonical topology identity")
	}
}

func TestContributionsWithAvailablePathsResolvesOnlyCompleteConsumers(t *testing.T) {
	assets := []desiredhookasset.HookAsset{
		testHookAsset(t, "first", target.ScopeProject),
		testHookAsset(t, "second", target.ScopeProject),
	}
	hooks := []desiredhook.Hook{
		testHook(t, "complete", target.ScopeProject, []target.Target{target.TargetCodex}, "run {hook_file:first}"),
		testHook(t, "incomplete", target.ScopeProject, []target.Target{target.TargetCodex}, "run {hook_file:first} {hook_file:second}"),
	}
	lowered := lowerHookRefinementFixture(t, assets, hooks)
	assetSubjects := assetSubjectsByName(lowered)

	contributions, err := ContributionsWithAvailablePaths(
		hooks,
		lowered,
		map[topology.SubjectID]string{assetSubjects["first"]: "/managed/first"},
		testContributionEncoder,
	)
	if err != nil {
		t.Fatalf("ContributionsWithAvailablePaths returned error: %v", err)
	}
	projectionSubjects := projectionSubjectsByName(lowered)
	complete := contributionContent(t, contributions, projectionSubjects["complete"])
	if !strings.Contains(complete, "/managed/first") || strings.Contains(complete, "{hook_file:first}") {
		t.Fatalf("complete contribution = %q, want resolved path", complete)
	}
	incomplete := contributionContent(t, contributions, projectionSubjects["incomplete"])
	if !strings.Contains(incomplete, "{hook_file:first}") || !strings.Contains(incomplete, "{hook_file:second}") || strings.Contains(incomplete, "/managed/first") {
		t.Fatalf("incomplete contribution = %q, want wholly portable command", incomplete)
	}
}

func TestContributionPathModesRejectInvalidPathSets(t *testing.T) {
	assets := []desiredhookasset.HookAsset{testHookAsset(t, "guard", target.ScopeProject)}
	hooks := []desiredhook.Hook{
		testHook(t, "protect", target.ScopeProject, []target.Target{target.TargetCodex}, "run {hook_file:guard}"),
	}
	lowered := lowerHookRefinementFixture(t, assets, hooks)
	assetSubject := lowered.AssetProjections()[0].SubjectID()
	extraSubject, err := topology.NewSubjectID(topology.SubjectProjection, "hook-asset.project.data", "hook-asset:extra")
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name    string
		refine  func() error
		wantErr string
	}{
		{
			name: "portable paths forbidden",
			refine: func() error {
				_, err := refineContributions(
					hooks,
					lowered,
					map[topology.SubjectID]string{assetSubject: "/managed/guard"},
					assetPathsPortable,
					testContributionEncoder,
				)
				return err
			},
			wantErr: "portable Hook refinement must not carry",
		},
		{
			name: "available path empty",
			refine: func() error {
				_, err := ContributionsWithAvailablePaths(
					hooks,
					lowered,
					map[topology.SubjectID]string{assetSubject: ""},
					testContributionEncoder,
				)
				return err
			},
			wantErr: "has an empty resolved path",
		},
		{
			name: "available path outside topology",
			refine: func() error {
				_, err := ContributionsWithAvailablePaths(
					hooks,
					lowered,
					map[topology.SubjectID]string{extraSubject: "/managed/extra"},
					testContributionEncoder,
				)
				return err
			},
			wantErr: "outside Hook topology",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := test.refine()
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("refinement error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func testContributionEncoder(input ContributionInput) (string, error) {
	return input.Command, nil
}

func lowerHookRefinementFixture(
	t *testing.T,
	assets []desiredhookasset.HookAsset,
	hooks []desiredhook.Hook,
) topologyhook.Model {
	t.Helper()
	lowered, err := topologyhook.Lower(assets, hooks)
	if err != nil {
		t.Fatalf("lower Hook topology: %v", err)
	}
	return lowered
}

func assetSubjectsByName(lowered topologyhook.Model) map[string]topology.SubjectID {
	result := make(map[string]topology.SubjectID, len(lowered.AssetProjections()))
	for _, projection := range lowered.AssetProjections() {
		result[projection.EntityID().Name()] = projection.SubjectID()
	}
	return result
}

func projectionSubjectsByName(lowered topologyhook.Model) map[string]topology.SubjectID {
	result := make(map[string]topology.SubjectID, len(lowered.Projections()))
	for _, projection := range lowered.Projections() {
		result[projection.EntityID().Name()] = projection.SubjectID()
	}
	return result
}

func contributionContent(
	t *testing.T,
	contributions []aggregate.SubjectContribution,
	subject topology.SubjectID,
) string {
	t.Helper()
	for _, contribution := range contributions {
		if contribution.SubjectID() == subject {
			return contribution.Contribution().CanonicalContribution()
		}
	}
	t.Fatalf("contribution for subject %q not found", subject)
	return ""
}

func testHookAsset(t *testing.T, name string, scope target.Scope) desiredhookasset.HookAsset {
	t.Helper()
	sourcePath := "hooks/" + name + ".sh"
	if scope == target.ScopeGlobal {
		sourcePath = filepath.Join(t.TempDir(), name+".sh")
	}
	return desiredfixture.HookAsset(t, desiredhookasset.Spec{
		Name: name, Source: sourcetest.Local(t, sourcePath, source.LocalSourceModeVendor),
		ArtifactKind: desiredhookasset.ArtifactKindFile, Scope: scope, Executable: true,
	})
}

func testHook(t *testing.T, name string, scope target.Scope, targets []target.Target, command string) desiredhook.Hook {
	t.Helper()
	return desiredfixture.Hook(t, desiredhook.Spec{
		Name: name, Event: "Stop", Type: desiredhook.TypeCommand, Command: command,
		Targets: targets, Scope: scope,
	})
}

func testSelection(t *testing.T, available []target.Target, requested ...string) targetselection.Selection {
	t.Helper()
	selection, err := targetselection.ForAvailableTargets(available, requested)
	if err != nil {
		t.Fatalf("ForAvailableTargets returned error: %v", err)
	}
	return selection
}
