package skill

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/supply/artifact"
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
	finalized := Finalize([]adopt.Skill{
		{InstallName: "alpha", Target: targetpkg.TargetPi, Scope: targetpkg.ScopeGlobal, ContentHash: contentHash},
		{InstallName: "alpha", Target: targetpkg.TargetCodex, Scope: targetpkg.ScopeGlobal, ContentHash: contentHash},
		{InstallName: "alpha", Target: targetpkg.TargetPi, Scope: targetpkg.ScopeGlobal, ContentHash: contentHash},
	})
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
}

func TestFinalizeMergesTargetPlacementRequestsWithoutChangingTargetOrder(t *testing.T) {
	contentHash := artifact.HashFileContent([]byte("same"))
	finalized := Finalize([]adopt.Skill{
		{
			InstallName: "alpha", Target: targetpkg.TargetOpenCode,
			Scope: targetpkg.ScopeProject, ContentHash: contentHash,
			Placements: map[targetpkg.Target]string{targetpkg.TargetOpenCode: ".agents/skills"},
		},
		{
			InstallName: "alpha", Target: targetpkg.TargetCodex,
			Scope: targetpkg.ScopeProject, ContentHash: contentHash,
		},
	})
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
