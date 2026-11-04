package build_test

import (
	"context"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/realization/lock/build"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/target"
)

type unusedHostConfigResolver struct{ test *testing.T }

func (resolver unusedHostConfigResolver) Resolve(context.Context, source.Source, acquisition.OperationOptions) (acquisition.Resolution, error) {
	resolver.test.Fatal("unsupported resource admission began source effects")
	return acquisition.Resolution{}, nil
}

func TestHostConfigCannotBeSilentlyOmittedBeforeLowererAdmission(t *testing.T) {
	defaults, err := desired.NewDefaults(target.ScopeProject, skill.InstallModeCopy)
	if err != nil {
		t.Fatal(err)
	}
	input := desired.Spec{Targets: []target.Target{target.TargetPi}, Defaults: defaults}
	empty, err := desired.New(input)
	if err != nil {
		t.Fatal(err)
	}
	resolver := unusedHostConfigResolver{test: t}
	if _, err := build.BuildWithOptions(context.Background(), empty, resolver, build.Options{}); err != nil {
		t.Fatalf("empty baseline cannot distinguish resource admission: %v", err)
	}
	provenance, err := source.NewLocalSource("config/settings.json", source.LocalSourceModeVendor)
	if err != nil {
		t.Fatal(err)
	}
	config, err := hostconfig.New(hostconfig.Spec{
		Name: "review", Source: provenance, Target: target.TargetPi, Scope: target.ScopeProject, Path: "settings.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	input.HostConfigs = []hostconfig.Config{config}
	environment, err := desired.New(input)
	if err != nil {
		t.Fatal(err)
	}
	file, err := build.BuildWithOptions(context.Background(), environment, resolver, build.Options{})
	if err == nil || file.Version != 0 {
		t.Fatalf("unsupported desired config became an apparently successful lock: version=%d, err=%v", file.Version, err)
	}
}
