package build

import (
	"context"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
)

func TestBuildDoesNotLockHooks(t *testing.T) {
	lockfile, err := buildWithTestOptions(context.Background(), lockEnvironment(t, desired.Spec{}), stubResolver{artifacts: map[string]resolutionFixture{}}, Options{})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if hooks := lockedSubjectsOfKind(lockfile, entity.KindHook); len(hooks) != 0 {
		t.Fatalf("locked hooks = %#v, want none", hooks)
	}
}

func TestBuildHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	config := desired.Spec{
		Skills: []skill.Skill{
			projectCopySkill(t, "demo", sourcetest.Local(t, "skills/demo", source.LocalSourceModeVendor), []target.Target{target.TargetCodex}, false),
		},
	}

	_, err := buildWithTestOptions(ctx, lockEnvironment(t, config), stubResolver{artifacts: map[string]resolutionFixture{}}, Options{})
	if err == nil {
		t.Fatal("Build returned nil error")
	}

	if err != context.Canceled && !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestBuildReportsMissingSkillSource(t *testing.T) {
	config := desired.Spec{
		Skills: []skill.Skill{
			projectCopySkill(t, "demo", sourcetest.Local(t, "skills/demo", source.LocalSourceModeVendor), []target.Target{target.TargetCodex}, false),
		},
	}

	_, err := buildWithTestOptions(context.Background(), lockEnvironment(t, config), stubResolver{artifacts: map[string]resolutionFixture{}}, Options{})
	if err == nil {
		t.Fatal("Build returned nil error")
	}
	if !strings.Contains(err.Error(), `resolve skill "demo"`) ||
		!strings.Contains(err.Error(), "missing source local:skills/demo?mode=vendor") {
		t.Fatalf("error = %q, want missing source diagnostic", err)
	}
}

func TestBuildRejectsInvalidSourceResolution(t *testing.T) {
	config := desired.Spec{
		Skills: []skill.Skill{
			projectCopySkill(t, "demo", sourcetest.Local(t, "skills/demo", source.LocalSourceModeVendor), []target.Target{target.TargetCodex}, false),
		},
	}

	_, err := buildWithTestOptions(context.Background(), lockEnvironment(t, config), invalidResolutionResolver{}, Options{})
	if err == nil {
		t.Fatal("Build returned nil error")
	}

	if !strings.Contains(err.Error(), "exact artifact source id is required") {
		t.Fatalf("error = %q, want invalid resolution diagnostic", err)
	}
}

type invalidResolutionResolver struct{}

func (invalidResolutionResolver) Resolve(
	context.Context,
	source.Source,
	acquisition.OperationOptions,
) (acquisition.Resolution, error) {
	return acquisition.Resolution{}, nil
}
