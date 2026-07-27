package skill

import (
	"path/filepath"
	"reflect"
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
