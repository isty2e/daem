package diagnose

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/supply/source/backend/localfs"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func TestSkillRepairDiagnosticsReusesMatchingSourceEpoch(t *testing.T) {
	sourceSpec, err := source.NewLocalSource("skills/oracle", source.LocalSourceModeVendor)
	if err != nil {
		t.Fatal(err)
	}
	resource := desiredtest.Skill(t, desiredskill.Spec{
		Name:        "oracle",
		Source:      sourceSpec,
		Targets:     []target.Target{target.TargetCodex},
		Scope:       target.ScopeProject,
		InstallMode: desiredskill.InstallModeCopy,
		Portable:    true,
	})
	selection, err := targetselection.ForAvailableTargets(
		[]target.Target{target.TargetCodex},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	manifestRoot := t.TempDir()
	paths, err := daempaths.Resolve(filepath.Join(manifestRoot, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	resolutionRoot := t.TempDir()
	skillRoot := filepath.Join(resolutionRoot, "skills", "oracle")
	if err := os.MkdirAll(skillRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(skillRoot, "skill.md"),
		[]byte("---\nname: oracle\ndescription: oracle\n---\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	resolver, err := localfs.NewResolver(resolutionRoot)
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := resolver.Resolve(context.Background(), sourceSpec)
	if err != nil {
		t.Fatal(err)
	}
	epoch := &fixedSkillSourceEpoch{resolution: resolution}

	diagnostics := SkillRepairDiagnosticsFromSourceEpoch(
		context.Background(),
		paths,
		[]desiredskill.Skill{resource},
		selection,
		epoch,
	)
	if epoch.calls != 1 {
		t.Fatalf("SkillResolution calls = %d, want 1", epoch.calls)
	}
	if len(diagnostics) != 1 || diagnostics[0].Code != skillDiagnosticRepairable {
		t.Fatalf("diagnostics = %#v, want one repairable diagnostic from epoch source", diagnostics)
	}
}

type fixedSkillSourceEpoch struct {
	resolution acquisition.Resolution
	calls      int
}

func (epoch *fixedSkillSourceEpoch) SkillResolution(
	desiredskill.Skill,
	targetselection.Selection,
) (acquisition.Resolution, bool) {
	epoch.calls++
	return epoch.resolution, true
}
