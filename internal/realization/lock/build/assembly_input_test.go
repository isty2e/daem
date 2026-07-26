package build

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/skill"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
)

func TestSkillLockAssemblyInputPreservesCanonicalSkillAndClonesCorrelation(t *testing.T) {
	targets := []target.Target{target.TargetCodex, target.TargetOpenCode}
	sourceSpec := sourcetest.Local(t, "skills/review", source.LocalSourceModeVendor)
	set := desiredtest.SkillSet(t, skill.SkillSetSpec{
		Source:      sourcetest.Local(t, "skills", source.LocalSourceModeVendor),
		Include:     []skill.Selector{desiredtest.Selector(t, "glob:review")},
		Targets:     targets,
		Scope:       target.ScopeProject,
		InstallMode: skill.InstallModeSymlink,
		Portable:    true,
	})
	declarationIdentity, err := set.DeclarationIdentity()
	if err != nil {
		t.Fatal(err)
	}
	lockable := lockableSkill{
		Resource: desiredtest.Skill(t, skill.Spec{
			Name:         "review",
			InstallName:  "agent-review",
			Source:       sourceSpec,
			Targets:      targets,
			Scope:        target.ScopeProject,
			InstallMode:  skill.InstallModeSymlink,
			Portable:     true,
			CompatRepair: true,
		}),
		SkillSetDeclaration: &declarationIdentity,
	}
	entityID := lockable.Resource.ID()
	resolved := mustResolvedArtifactInput(t, entityID, sourceSpec, artifact.ArtifactKindDirectory)
	input, err := newSkillLockAssemblyInput(lockable, resolved, assemblyTaskRef{
		taskID: "skill:000000", stage: EventStageSkill, ordinal: 0, entityID: entityID,
	})
	if err != nil {
		t.Fatalf("newSkillLockAssemblyInput returned error: %v", err)
	}

	targets[0] = target.TargetPi
	if input.value.ID().Name() != "review" || input.value.InstallName() != "agent-review" ||
		input.value.Scope() != target.ScopeProject || input.value.InstallMode() != skill.InstallModeSymlink ||
		!input.value.Portable() || !input.value.CompatRepair() {
		t.Fatalf("assembly skill = %#v, want all canonical skill facts preserved", input.value)
	}
	if got := input.value.Targets(); !reflect.DeepEqual(got, []target.Target{target.TargetCodex, target.TargetOpenCode}) {
		t.Fatalf("assembly targets = %#v, want canonical desired targets", got)
	}
	if input.skillSetDeclaration == nil || !input.skillSetDeclaration.Equal(declarationIdentity) {
		t.Fatalf("assembly declaration identity = %#v, want selector declaration", input.skillSetDeclaration)
	}
	if input.skillSetDeclaration == lockable.SkillSetDeclaration {
		t.Fatal("assembly input reused caller-owned skill set declaration pointer")
	}
}

func TestLockAssemblyRejectsMismatchedSubjectArtifactAndTask(t *testing.T) {
	lockable := lockableSkill{
		Resource: desiredtest.Skill(t, skill.Spec{
			Name: "alpha", Source: sourcetest.Local(t, "skills/alpha", source.LocalSourceModeVendor),
			Targets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject, InstallMode: skill.InstallModeCopy,
		}),
	}
	resolved := mustResolvedArtifactInput(
		t,
		mustEntityID(t, entity.KindSkill, "beta"),
		sourcetest.Local(t, "skills/beta", source.LocalSourceModeVendor),
		artifact.ArtifactKindDirectory,
	)

	_, err := newSkillLockAssemblyInput(lockable, resolved, assemblyTaskRef{
		taskID:   "skill:000000",
		stage:    EventStageSkill,
		ordinal:  0,
		entityID: mustEntityID(t, entity.KindSkill, "alpha"),
	})
	if err == nil {
		t.Fatal("newSkillLockAssemblyInput returned nil error")
	}
	if !strings.Contains(err.Error(), "does not match candidate skill/alpha") {
		t.Fatalf("error = %q, want subject mismatch", err)
	}

	matching := mustResolvedArtifactInput(
		t,
		mustEntityID(t, entity.KindSkill, "alpha"),
		sourcetest.Local(t, "skills/alpha", source.LocalSourceModeVendor),
		artifact.ArtifactKindDirectory,
	)
	_, err = newSkillLockAssemblyInput(lockable, matching, assemblyTaskRef{
		taskID:   "skill:000001",
		stage:    EventStageSkill,
		ordinal:  1,
		entityID: mustEntityID(t, entity.KindSkill, "beta"),
	})
	if err == nil {
		t.Fatal("newSkillLockAssemblyInput returned nil error for task mismatch")
	}
	if !strings.Contains(err.Error(), "task id skill/beta does not match subject skill/alpha") {
		t.Fatalf("error = %q, want task mismatch", err)
	}
}

func TestLockAssemblyRejectsDuplicateSubjectIdentity(t *testing.T) {
	sourceSpec := sourcetest.Local(t, "skills/alpha", source.LocalSourceModeVendor)
	lockable := lockableSkill{
		Resource: desiredtest.Skill(t, skill.Spec{
			Name: "alpha", Source: sourceSpec,
			Targets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject, InstallMode: skill.InstallModeCopy,
		}),
	}
	entityID := lockable.Resource.ID()
	resolved := mustResolvedArtifactInput(
		t,
		entityID,
		sourceSpec,
		artifact.ArtifactKindDirectory,
	)
	input, err := newSkillLockAssemblyInput(lockable, resolved, assemblyTaskRef{
		taskID:   "skill:000000",
		stage:    EventStageSkill,
		ordinal:  0,
		entityID: entityID,
	})
	if err != nil {
		t.Fatalf("newSkillLockAssemblyInput returned error: %v", err)
	}

	err = (lockAssemblyInput{Skills: []skillLockAssemblyInput{input, input}}).validate()
	if err == nil {
		t.Fatal("lockAssemblyInput.validate returned nil error")
	}
	if !strings.Contains(err.Error(), `duplicate lock assembly subject "skill:alpha"`) {
		t.Fatalf("error = %q, want duplicate subject diagnostic", err)
	}
}

func TestLockAssemblyRejectsSkillArtifactKindBeforeSnapshotPersistence(t *testing.T) {
	sourceSpec := sourcetest.Local(t, "skills/not-a-skill.md", source.LocalSourceModeVendor)
	lockable := lockableSkill{
		Resource: desiredtest.Skill(t, skill.Spec{
			Name: "bad-skill", Source: sourceSpec,
			Targets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject, InstallMode: skill.InstallModeCopy,
		}),
	}
	entityID := lockable.Resource.ID()
	resolved := mustResolvedArtifactInput(
		t,
		entityID,
		sourceSpec,
		artifact.ArtifactKindFile,
	)
	input, err := newSkillLockAssemblyInput(lockable, resolved, assemblyTaskRef{
		taskID:   "skill:000000",
		stage:    EventStageSkill,
		ordinal:  0,
		entityID: entityID,
	})
	if err != nil {
		t.Fatalf("newSkillLockAssemblyInput returned error: %v", err)
	}

	_, err = lockResolvedSkills(context.Background(), []skillLockAssemblyInput{input}, Options{})
	if err == nil {
		t.Fatal("lockResolvedSkills returned nil error")
	}
	if !strings.Contains(err.Error(), `validate skill "bad-skill"`) ||
		!strings.Contains(err.Error(), "must resolve to a directory") {
		t.Fatalf("error = %q, want artifact kind validation before snapshot persistence", err)
	}
}

func mustResolvedArtifactInput(
	t *testing.T,
	subjectID entity.ID,
	sourceSpec source.Source,
	kind artifact.ArtifactKind,
) resolvedArtifactInput {
	t.Helper()

	root := filepath.Join(t.TempDir(), "artifact")
	switch kind {
	case artifact.ArtifactKindDirectory:
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatalf("create test artifact directory: %v", err)
		}
		content := "---\nname: " + subjectID.Name() + "\ndescription: Test skill\n---\n"
		if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(content), 0o600); err != nil {
			t.Fatalf("write test skill artifact: %v", err)
		}
	case artifact.ArtifactKindFile:
		if err := os.WriteFile(root, []byte("not a skill directory\n"), 0o600); err != nil {
			t.Fatalf("write test file artifact: %v", err)
		}
	default:
		t.Fatalf("unsupported test artifact kind %q", kind)
	}

	view, err := access.OpenView(root)
	if err != nil {
		t.Fatalf("OpenView returned error: %v", err)
	}
	contentHash, err := view.Hash(context.Background())
	if err != nil {
		t.Fatalf("View.Hash returned error: %v", err)
	}
	sourceID, err := source.SourceIDFor(sourceSpec)
	if err != nil {
		t.Fatalf("SourceIDFor returned error: %v", err)
	}
	identity, err := artifact.NewExactIdentity(sourceID, "", kind, contentHash)
	if err != nil {
		t.Fatalf("NewExactIdentity returned error: %v", err)
	}
	resolution, err := acquisition.NewResolution(sourceSpec, identity, view)
	if err != nil {
		t.Fatalf("NewResolution returned error: %v", err)
	}
	input, err := newResolvedArtifactInput(subjectID, sourceSpec, resolution)
	if err != nil {
		t.Fatalf("newResolvedArtifactInput returned error: %v", err)
	}
	return input
}
