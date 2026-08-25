package skill

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/adopt"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	targetpkg "github.com/isty2e/daem/internal/target"
)

func TestAssignImportedSkillGroupSourcesUsesSkillContentInGroupRoot(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "daem.d")
	sourceDirectory, err := adopt.NewSourceDirectory(filepath.Join(filepath.Dir(sourceDir), "daem.toml"), sourceDir)
	if err != nil {
		t.Fatalf("NewSourceDirectory returned error: %v", err)
	}
	skills := []adopt.Skill{
		{
			ResourceName: "alpha",
			InstallName:  "alpha",
			Targets:      []targetpkg.Target{targetpkg.TargetCodex},
			Scope:        targetpkg.ScopeGlobal,
			ContentHash:  artifact.HashFileContent([]byte("alpha-v1")),
		},
		{
			ResourceName: "beta",
			InstallName:  "beta",
			Targets:      []targetpkg.Target{targetpkg.TargetCodex},
			Scope:        targetpkg.ScopeGlobal,
			ContentHash:  artifact.HashFileContent([]byte("beta-v1")),
		},
	}

	grouped, err := AssignGroupSources(sourceDirectory, append([]adopt.Skill(nil), skills...))
	if err != nil {
		t.Fatalf("AssignGroupSources returned error: %v", err)
	}
	if grouped[0].GroupRoot == "" || grouped[0].GroupRoot != grouped[1].GroupRoot {
		t.Fatalf("group roots = %q and %q, want one non-empty group root", grouped[0].GroupRoot, grouped[1].GroupRoot)
	}
	if grouped[0].SourcePath != filepath.Join(grouped[0].GroupRoot, "alpha") {
		t.Fatalf("alpha sourcePath = %q, want child of group root", grouped[0].SourcePath)
	}
	if grouped[1].SourcePath != filepath.Join(grouped[1].GroupRoot, "beta") {
		t.Fatalf("beta sourcePath = %q, want child of group root", grouped[1].SourcePath)
	}

	changedSkills := append([]adopt.Skill(nil), skills...)
	changedSkills[1].ContentHash = artifact.HashFileContent([]byte("beta-v2"))
	changed, err := AssignGroupSources(sourceDirectory, changedSkills)
	if err != nil {
		t.Fatalf("AssignGroupSources returned error: %v", err)
	}
	if changed[0].GroupRoot == grouped[0].GroupRoot {
		t.Fatalf("groupRoot = %q after content change, want content-addressed root to change", changed[0].GroupRoot)
	}
}

func TestFinalizePreservesFirstSeenRepresentativeTargetOrder(t *testing.T) {
	contentHash := artifact.HashFileContent([]byte("same"))
	piRoute := adopt.SkillSourceRoute{Target: targetpkg.TargetPi, LivePath: "/pi/alpha", ReadPath: "/pi/alpha"}
	codexRoute := adopt.SkillSourceRoute{Target: targetpkg.TargetCodex, LivePath: "/codex/alpha", ReadPath: "/codex/alpha"}
	finalized, err := Finalize([]adopt.Skill{
		{InstallName: "alpha", Target: targetpkg.TargetPi, Scope: targetpkg.ScopeGlobal, ContentHash: contentHash, SourceRoutes: []adopt.SkillSourceRoute{piRoute}},
		{InstallName: "alpha", Target: targetpkg.TargetCodex, Scope: targetpkg.ScopeGlobal, ContentHash: contentHash, SourceRoutes: []adopt.SkillSourceRoute{codexRoute}},
		{InstallName: "alpha", Target: targetpkg.TargetPi, Scope: targetpkg.ScopeGlobal, ContentHash: contentHash, SourceRoutes: []adopt.SkillSourceRoute{piRoute}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(finalized) != 1 {
		t.Fatalf("Finalize returned %#v, want one skill", finalized)
	}
	if finalized[0].Target != targetpkg.TargetPi {
		t.Fatalf("representative target = %q, want first-seen pi", finalized[0].Target)
	}
	want := []targetpkg.Target{targetpkg.TargetPi, targetpkg.TargetCodex}
	if !reflect.DeepEqual(finalized[0].Targets, want) {
		t.Fatalf("targets = %#v, want %#v", finalized[0].Targets, want)
	}
	wantRoutes := []adopt.SkillSourceRoute{codexRoute, piRoute}
	if !reflect.DeepEqual(finalized[0].SourceRoutes, wantRoutes) {
		t.Fatalf("source routes = %#v, want %#v", finalized[0].SourceRoutes, wantRoutes)
	}
	primaryRoute, err := finalized[0].PrimarySourceRoute()
	if err != nil {
		t.Fatal(err)
	}
	if primaryRoute != piRoute {
		t.Fatalf("primary source route = %#v, want representative-target route %#v", primaryRoute, piRoute)
	}
}

func TestFinalizeRejectsDistinctSourceRoutesForOneTarget(t *testing.T) {
	contentHash := artifact.HashFileContent([]byte("same"))
	_, err := Finalize([]adopt.Skill{
		{
			InstallName: "alpha", Target: targetpkg.TargetPi, Scope: targetpkg.ScopeGlobal, ContentHash: contentHash,
			SourceRoutes: []adopt.SkillSourceRoute{{Target: targetpkg.TargetPi, LivePath: "/pi/alpha", ReadPath: "/pi/alpha"}},
		},
		{
			InstallName: "alpha", Target: targetpkg.TargetPi, Scope: targetpkg.ScopeGlobal, ContentHash: contentHash,
			SourceRoutes: []adopt.SkillSourceRoute{{Target: targetpkg.TargetPi, LivePath: "/other/alpha", ReadPath: "/other/alpha"}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "conflicting source routes") {
		t.Fatalf("Finalize error = %v, want target route conflict", err)
	}
}

func TestFinalizeMergesTargetPlacementRequestsWithoutChangingTargetOrder(t *testing.T) {
	contentHash := artifact.HashFileContent([]byte("same"))
	finalized, err := Finalize([]adopt.Skill{
		{
			InstallName: "alpha", Target: targetpkg.TargetOpenCode,
			Scope: targetpkg.ScopeProject, ContentHash: contentHash,
			Placements: map[targetpkg.Target]string{targetpkg.TargetOpenCode: ".agents/skills"},
			SourceRoutes: []adopt.SkillSourceRoute{{
				Target: targetpkg.TargetOpenCode, LivePath: "/opencode/alpha", ReadPath: "/opencode/alpha",
			}},
		},
		{
			InstallName: "alpha", Target: targetpkg.TargetCodex,
			Scope: targetpkg.ScopeProject, ContentHash: contentHash,
			SourceRoutes: []adopt.SkillSourceRoute{{
				Target: targetpkg.TargetCodex, LivePath: "/codex/alpha", ReadPath: "/codex/alpha",
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(finalized) != 1 {
		t.Fatalf("Finalize returned %#v, want one skill", finalized)
	}
	if got := finalized[0].Placements[targetpkg.TargetOpenCode]; got != ".agents/skills" {
		t.Fatalf("placement = %q, want .agents/skills", got)
	}
}

func TestAssignGroupSourcesTreatsTargetOrderAsSetIdentity(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "daem.d")
	sourceDirectory, err := adopt.NewSourceDirectory(filepath.Join(filepath.Dir(sourceDir), "daem.toml"), sourceDir)
	if err != nil {
		t.Fatalf("NewSourceDirectory returned error: %v", err)
	}
	skills := []adopt.Skill{
		{
			ResourceName: "alpha",
			InstallName:  "alpha",
			Targets:      []targetpkg.Target{targetpkg.TargetPi, targetpkg.TargetCodex},
			Scope:        targetpkg.ScopeGlobal,
			ContentHash:  artifact.HashFileContent([]byte("alpha")),
		},
		{
			ResourceName: "beta",
			InstallName:  "beta",
			Targets:      []targetpkg.Target{targetpkg.TargetCodex, targetpkg.TargetPi},
			Scope:        targetpkg.ScopeGlobal,
			ContentHash:  artifact.HashFileContent([]byte("beta")),
		},
	}

	grouped, err := AssignGroupSources(sourceDirectory, skills)
	if err != nil {
		t.Fatalf("AssignGroupSources returned error: %v", err)
	}
	if grouped[0].GroupRoot == "" || grouped[0].GroupRoot != grouped[1].GroupRoot {
		t.Fatalf("group roots = %q and %q, want one order-independent target-set group", grouped[0].GroupRoot, grouped[1].GroupRoot)
	}
}

func TestAssignGroupSourcesDoesNotGroupDifferentPlacementPolicies(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "daem.d")
	sourceDirectory, err := adopt.NewSourceDirectory(filepath.Join(filepath.Dir(sourceDir), "daem.toml"), sourceDir)
	if err != nil {
		t.Fatal(err)
	}
	skills := []adopt.Skill{
		{
			ResourceName: "alpha", InstallName: "alpha",
			Targets: []targetpkg.Target{targetpkg.TargetOpenCode}, Scope: targetpkg.ScopeProject,
			Placements:  map[targetpkg.Target]string{targetpkg.TargetOpenCode: ".agents/skills"},
			ContentHash: artifact.HashFileContent([]byte("alpha")),
		},
		{
			ResourceName: "beta", InstallName: "beta",
			Targets: []targetpkg.Target{targetpkg.TargetOpenCode}, Scope: targetpkg.ScopeProject,
			Placements:  map[targetpkg.Target]string{targetpkg.TargetOpenCode: ".claude/skills"},
			ContentHash: artifact.HashFileContent([]byte("beta")),
		},
	}

	grouped, err := AssignGroupSources(sourceDirectory, skills)
	if err != nil {
		t.Fatal(err)
	}
	if grouped[0].GroupRoot != "" || grouped[1].GroupRoot != "" {
		t.Fatalf("different placement policies were grouped: %#v", grouped)
	}
}

func TestCandidatesPreservesNonDefaultAdmittedSkillRoot(t *testing.T) {
	projectRoot := t.TempDir()
	oldWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWorkingDirectory); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})

	skillRoot := filepath.Join(".agents", "skills", "review")
	if err := os.MkdirAll(skillRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte("---\nname: review\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceDirectory, err := adopt.NewSourceDirectory(
		filepath.Join(projectRoot, "daem.toml"),
		filepath.Join(projectRoot, "daem.d"),
	)
	if err != nil {
		t.Fatal(err)
	}

	candidates, _, _, err := Candidates(
		context.Background(),
		sourceDirectory,
		targetpkg.TargetOpenCode,
		targetpkg.ScopeProject,
		NewDestinationClaims(),
		NewSourceIdentityCache(skillTreeLimitsForTest(t)),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("Candidates = %#v, want one imported skill", candidates)
	}
	if got := candidates[0].Placements[targetpkg.TargetOpenCode]; got != ".agents/skills" {
		t.Fatalf("placement = %q, want .agents/skills", got)
	}
}

func TestImportSkillChargesDuplicateRouteBeforeClassification(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	for _, path := range []string{first, second} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	sourceDirectory, err := adopt.NewSourceDirectory(
		filepath.Join(root, "daem.toml"),
		filepath.Join(root, "daem.d"),
	)
	if err != nil {
		t.Fatal(err)
	}
	claims := NewDestinationClaims()
	identities := newSourceIdentityCacheWithLimits(
		func(_ context.Context, path string, _ access.TraversalLimit) (artifact.ContentHash, sourceIdentityMeasurement, error) {
			return artifact.HashFileContent([]byte(path)), sourceIdentityMeasurement{entries: 1, bytes: 3}, nil
		},
		mutationfs.DefaultTreeTraversalLimits(),
		2,
		5,
	)

	candidate, skipped, err := importSkillFromEntry(
		t.Context(), sourceDirectory, targetpkg.TargetCodex, targetpkg.ScopeGlobal,
		"", first, "review", claims, identities,
	)
	if err != nil || candidate.ResourceName != "review" || skipped.Reason != "" {
		t.Fatalf("first import = (%#v, %#v, %v), want retained candidate", candidate, skipped, err)
	}
	candidate, skipped, err = importSkillFromEntry(
		t.Context(), sourceDirectory, targetpkg.TargetCodex, targetpkg.ScopeGlobal,
		"", second, "review", claims, identities,
	)
	if !errors.Is(err, errSourceIdentityLimitExceeded) || candidate.ResourceName != "" || skipped.Reason != "" {
		t.Fatalf("duplicate import = (%#v, %#v, %v), want aggregate identity-budget failure before classification", candidate, skipped, err)
	}
}

func TestImportSkillClassifiesDuplicateDestinationByContentIdentity(t *testing.T) {
	for _, test := range []struct {
		name           string
		secondContent  string
		wantReason     adopt.SkipReason
		wantCategory   adopt.SkipCategory
		wantDetailLead string
	}{
		{
			name:           "identical content",
			secondContent:  "---\nname: review\ndescription: same\n---\n",
			wantReason:     importSkillSkipDuplicateName,
			wantCategory:   adopt.SkipCategoryInformational,
			wantDetailLead: "duplicates=",
		},
		{
			name:           "conflicting content",
			secondContent:  "---\nname: review\ndescription: different\n---\n",
			wantReason:     importSkillSkipConflictingName,
			wantCategory:   adopt.SkipCategoryActionRequired,
			wantDetailLead: "conflicts_with=",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			first := filepath.Join(root, "first", "review")
			second := filepath.Join(root, "second", "review")
			for _, path := range []string{first, second} {
				if err := os.MkdirAll(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			firstContent := "---\nname: review\ndescription: same\n---\n"
			if err := os.WriteFile(filepath.Join(first, "SKILL.md"), []byte(firstContent), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(second, "SKILL.md"), []byte(test.secondContent), 0o600); err != nil {
				t.Fatal(err)
			}
			sourceDirectory, err := adopt.NewSourceDirectory(
				filepath.Join(root, "daem.toml"),
				filepath.Join(root, "daem.d"),
			)
			if err != nil {
				t.Fatal(err)
			}
			claims := NewDestinationClaims()
			identities := NewSourceIdentityCache(skillTreeLimitsForTest(t))

			candidate, skipped, err := importSkillFromEntry(
				t.Context(), sourceDirectory, targetpkg.TargetCodex, targetpkg.ScopeGlobal,
				"", first, "review", claims, identities,
			)
			if err != nil || candidate.ResourceName != "review" || skipped.Reason != "" {
				t.Fatalf("first import = (%#v, %#v, %v), want retained candidate", candidate, skipped, err)
			}
			candidate, skipped, err = importSkillFromEntry(
				t.Context(), sourceDirectory, targetpkg.TargetCodex, targetpkg.ScopeGlobal,
				"", second, "review", claims, identities,
			)
			if err != nil || candidate.ResourceName != "" {
				t.Fatalf("second import = (%#v, %#v, %v), want skip", candidate, skipped, err)
			}
			if skipped.LivePath != second || skipped.Reason != test.wantReason ||
				skipped.Category() != test.wantCategory ||
				!strings.HasPrefix(skipped.Detail, test.wantDetailLead) ||
				!strings.Contains(skipped.Detail, first) {
				t.Fatalf("second skip = %#v, want reason=%q category=%q and both routes", skipped, test.wantReason, test.wantCategory)
			}
		})
	}
}

func TestImportSkillRequiresRegularSkillDocumentInIdentityObservation(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*testing.T, string)
	}{
		{name: "missing"},
		{
			name: "directory",
			setup: func(t *testing.T, root string) {
				t.Helper()
				if err := os.Mkdir(filepath.Join(root, "SKILL.md"), 0o700); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			skillRoot := filepath.Join(root, "review")
			if err := os.Mkdir(skillRoot, 0o700); err != nil {
				t.Fatal(err)
			}
			if test.setup != nil {
				test.setup(t, skillRoot)
			}
			sourceDirectory, err := adopt.NewSourceDirectory(
				filepath.Join(root, "daem.toml"),
				filepath.Join(root, "daem.d"),
			)
			if err != nil {
				t.Fatal(err)
			}

			candidate, skipped, err := importSkillFromEntry(
				context.Background(),
				sourceDirectory,
				targetpkg.TargetCodex,
				targetpkg.ScopeProject,
				"",
				skillRoot,
				"review",
				NewDestinationClaims(),
				NewSourceIdentityCache(skillTreeLimitsForTest(t)),
			)
			if err != nil {
				t.Fatal(err)
			}
			if candidate.ResourceName != "" || skipped.Reason != importSkillSkipMissingSkillMD {
				t.Fatalf("candidate = %#v, skip = %#v, want missing SKILL.md skip", candidate, skipped)
			}
		})
	}
}

func TestImportSkillSkipsMissingSkillMDBeforeTreeTraversal(t *testing.T) {
	root := t.TempDir()
	skillRoot := filepath.Join(root, "review")
	if err := os.MkdirAll(filepath.Join(skillRoot, "one", "two"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "one", "two", "payload"), []byte("nested"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceDirectory, err := adopt.NewSourceDirectory(
		filepath.Join(root, "daem.toml"),
		filepath.Join(root, "daem.d"),
	)
	if err != nil {
		t.Fatal(err)
	}
	limit, err := mutationfs.NewTreeTraversalLimits(2, 1, 1<<20)
	if err != nil {
		t.Fatal(err)
	}

	candidate, skipped, err := importSkillFromEntry(
		context.Background(),
		sourceDirectory,
		targetpkg.TargetCodex,
		targetpkg.ScopeProject,
		"",
		skillRoot,
		"review",
		NewDestinationClaims(),
		NewSourceIdentityCache(limit),
	)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.ResourceName != "" || skipped.Reason != importSkillSkipMissingSkillMD {
		t.Fatalf("candidate = %#v, skip = %#v, want missing SKILL.md skip before tree budget", candidate, skipped)
	}
}

func TestImportSkillFailsClosedOnRootBreadthOverflow(t *testing.T) {
	root := t.TempDir()
	skillRoot := filepath.Join(root, "review")
	if err := os.Mkdir(skillRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	for index := range 3 {
		if err := os.WriteFile(filepath.Join(skillRoot, fmt.Sprintf("entry-%d", index)), []byte("nested"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sourceDirectory, err := adopt.NewSourceDirectory(
		filepath.Join(root, "daem.toml"),
		filepath.Join(root, "daem.d"),
	)
	if err != nil {
		t.Fatal(err)
	}
	limit, err := mutationfs.NewTreeTraversalLimits(2, 1, 1<<20)
	if err != nil {
		t.Fatal(err)
	}

	candidate, skipped, err := importSkillFromEntry(
		context.Background(),
		sourceDirectory,
		targetpkg.TargetCodex,
		targetpkg.ScopeProject,
		"",
		skillRoot,
		"review",
		NewDestinationClaims(),
		NewSourceIdentityCache(limit),
	)
	if err == nil || candidate.ResourceName != "" || skipped.Reason != "" {
		t.Fatalf("candidate = %#v, skip = %#v, err = %v, want breadth overflow failure", candidate, skipped, err)
	}
	if !strings.Contains(err.Error(), "artifact tree exceeds 2 entries") {
		t.Fatalf("import error = %v, want structure-limit overflow", err)
	}
}

func TestImportSkillSkipsNestedSymlinkFromIdentityTraversal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires additional privileges on Windows")
	}
	root := t.TempDir()
	skillRoot := filepath.Join(root, "unsafe")
	if err := os.Mkdir(skillRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte("---\nname: unsafe\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "payload.txt"), []byte("payload\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(skillRoot, "payload.txt"), filepath.Join(skillRoot, "z-link")); err != nil {
		t.Fatal(err)
	}
	sourceDirectory, err := adopt.NewSourceDirectory(
		filepath.Join(root, "daem.toml"),
		filepath.Join(root, "daem.d"),
	)
	if err != nil {
		t.Fatal(err)
	}

	candidate, skipped, err := importSkillFromEntry(
		context.Background(),
		sourceDirectory,
		targetpkg.TargetCodex,
		targetpkg.ScopeProject,
		"",
		skillRoot,
		"unsafe",
		NewDestinationClaims(),
		NewSourceIdentityCache(skillTreeLimitsForTest(t)),
	)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.ResourceName != "" || skipped.Reason != importSkillSkipNestedSymlink {
		t.Fatalf("candidate = %#v, skip = %#v, want nested symlink skip", candidate, skipped)
	}
	resolvedRoot, err := filepath.EvalSymlinks(skillRoot)
	if err != nil {
		t.Fatal(err)
	}
	if skipped.LivePath != filepath.Join(resolvedRoot, "z-link") {
		t.Fatalf("nested symlink path = %q, want %q", skipped.LivePath, filepath.Join(resolvedRoot, "z-link"))
	}
}

func TestImportSkillTreatsSymlinkSkillDocumentAsMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires additional privileges on Windows")
	}
	root := t.TempDir()
	skillRoot := filepath.Join(root, "review")
	if err := os.Mkdir(skillRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing", filepath.Join(skillRoot, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	sourceDirectory, err := adopt.NewSourceDirectory(
		filepath.Join(root, "daem.toml"),
		filepath.Join(root, "daem.d"),
	)
	if err != nil {
		t.Fatal(err)
	}

	candidate, skipped, err := importSkillFromEntry(
		context.Background(),
		sourceDirectory,
		targetpkg.TargetCodex,
		targetpkg.ScopeProject,
		"",
		skillRoot,
		"review",
		NewDestinationClaims(),
		NewSourceIdentityCache(skillTreeLimitsForTest(t)),
	)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.ResourceName != "" || skipped.Reason != importSkillSkipMissingSkillMD {
		t.Fatalf("candidate = %#v, skip = %#v, want missing SKILL.md skip for nonregular document", candidate, skipped)
	}
}

func TestCandidatesHashSharedResolvedSkillRouteOnceAcrossTargets(t *testing.T) {
	projectRoot := t.TempDir()
	oldWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWorkingDirectory); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})

	skillRoot := filepath.Join(".agents", "skills", "review")
	if err := os.MkdirAll(skillRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte("---\nname: review\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceDirectory, err := adopt.NewSourceDirectory(
		filepath.Join(projectRoot, "daem.toml"),
		filepath.Join(projectRoot, "daem.d"),
	)
	if err != nil {
		t.Fatal(err)
	}

	observations := 0
	sourceIdentities := newSourceIdentityCache(func(
		ctx context.Context,
		readPath string,
		traversalLimit access.TraversalLimit,
	) (artifact.ContentHash, sourceIdentityMeasurement, error) {
		observations++
		return observeSkillDirectoryIdentity(ctx, readPath, traversalLimit, skillTreeLimitsForTest(t))
	})
	destinations := NewDestinationClaims()
	var imported []adopt.Skill
	for _, selectedTarget := range []targetpkg.Target{targetpkg.TargetCodex, targetpkg.TargetOpenCode} {
		candidates, _, _, err := Candidates(
			context.Background(),
			sourceDirectory,
			selectedTarget,
			targetpkg.ScopeProject,
			destinations,
			sourceIdentities,
		)
		if err != nil {
			t.Fatal(err)
		}
		if len(candidates) != 1 {
			t.Fatalf("%s candidates = %#v, want one", selectedTarget, candidates)
		}
		imported = append(imported, candidates[0])
	}

	if observations != 1 {
		t.Fatalf("shared skill identity observations = %d, want 1", observations)
	}
	firstRoute, err := imported[0].PrimarySourceRoute()
	if err != nil {
		t.Fatal(err)
	}
	secondRoute, err := imported[1].PrimarySourceRoute()
	if err != nil {
		t.Fatal(err)
	}
	if firstRoute.ReadPath != secondRoute.ReadPath || !filepath.IsAbs(firstRoute.ReadPath) {
		t.Fatalf("shared read paths = %q and %q, want one absolute route", firstRoute.ReadPath, secondRoute.ReadPath)
	}
	if imported[0].ContentHash != imported[1].ContentHash {
		t.Fatalf("shared content hashes = %q and %q", imported[0].ContentHash, imported[1].ContentHash)
	}
}

func TestSkillLocationPathPreservesProfilePathDomains(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	homePath, err := skillLocationPath("~/agent/skills")
	if err != nil {
		t.Fatalf("skillLocationPath(home) error = %v", err)
	}
	if want := filepath.Join(home, "agent", "skills"); homePath != want {
		t.Fatalf("skillLocationPath(home) = %q, want %q", homePath, want)
	}

	relativePath, err := skillLocationPath(".agents/skills")
	if err != nil {
		t.Fatalf("skillLocationPath(relative) error = %v", err)
	}
	if want := filepath.FromSlash(".agents/skills"); relativePath != want {
		t.Fatalf("skillLocationPath(relative) = %q, want %q", relativePath, want)
	}

	absoluteInput := filepath.Join(string(os.PathSeparator), "tmp", "nested", "..", "skills")
	absolutePath, err := skillLocationPath(absoluteInput)
	if err != nil {
		t.Fatalf("skillLocationPath(absolute) error = %v", err)
	}
	if want := filepath.Clean(absoluteInput); absolutePath != want {
		t.Fatalf("skillLocationPath(absolute) = %q, want %q", absolutePath, want)
	}
}

func TestSkillLocationPathReportsUnavailableHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("HOMEDRIVE", "")
	t.Setenv("HOMEPATH", "")
	if _, err := skillLocationPath("~/skills"); err == nil {
		t.Fatal("skillLocationPath() succeeded without a home directory")
	}
}

func TestSkillPathExistsUsesLstatForDanglingSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires additional privileges on Windows")
	}
	root := t.TempDir()
	link := filepath.Join(root, "skill")
	if err := os.Symlink(filepath.Join(root, "missing"), link); err != nil {
		t.Fatal(err)
	}

	exists, err := skillPathExists(link)
	if err != nil {
		t.Fatalf("skillPathExists() error = %v", err)
	}
	if !exists {
		t.Fatal("skillPathExists() = false, want dangling symlink to exist")
	}
}
