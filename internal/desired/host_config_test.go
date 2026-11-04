package desired_test

import (
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/desired/instructions"
	"github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/target"
)

func hostConfigEnvironmentSpec(t *testing.T) desired.Spec {
	t.Helper()
	defaults, err := desired.NewDefaults(target.ScopeProject, skill.InstallModeCopy)
	if err != nil {
		t.Fatal(err)
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
	return desired.Spec{Targets: []target.Target{target.TargetPi}, Defaults: defaults, HostConfigs: []hostconfig.Config{config}}
}

func TestHostConfigEnvironmentRetainsSourcesAndImmutableDeclarations(t *testing.T) {
	input := hostConfigEnvironmentSpec(t)
	config := input.HostConfigs[0]
	value, err := desired.New(input)
	if err != nil {
		t.Fatal(err)
	}
	input.HostConfigs[0] = hostconfig.Config{}
	returned := value.HostConfigs()
	returned[0] = hostconfig.Config{}
	if got := value.HostConfigs(); len(got) != 1 || got[0].ID() != config.ID() || got[0].Validate() != nil {
		t.Fatal("host configuration collection exposed mutable storage")
	}
	sources := value.EntityArtifactSources()
	if len(sources) != 1 {
		t.Fatalf("source-backed config missing from source census: %d", len(sources))
	}
	actual, err := source.SourceIDFor(sources[0])
	if err != nil {
		t.Fatal(err)
	}
	expected, err := source.SourceIDFor(config.Source())
	if err != nil || actual != expected {
		t.Fatal("config source identity changed")
	}
	rebuilt, err := value.WithGeneratedSkills(nil)
	if err != nil || len(rebuilt.HostConfigs()) != 1 || rebuilt.HostConfigs()[0].ID() != config.ID() {
		t.Fatalf("environment rebuild dropped host configuration: %v", err)
	}
}

func TestHostConfigEnvironmentRejectsInvalidOrDuplicateIdentity(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		input := hostConfigEnvironmentSpec(t)
		if invalid {
			input.HostConfigs[0] = hostconfig.Config{}
		} else {
			input.HostConfigs = append(input.HostConfigs, input.HostConfigs[0])
		}
		if _, err := desired.New(input); err == nil {
			t.Fatal("invalid or duplicate host configuration admitted")
		}
	}
	input := hostConfigEnvironmentSpec(t)
	instruction, err := instructions.New(instructions.Spec{
		Name: "review", Source: input.HostConfigs[0].Source(), Targets: []target.Target{target.TargetPi}, Scope: target.ScopeProject,
	})
	if err != nil {
		t.Fatal(err)
	}
	input.Instructions = []instructions.Instructions{instruction}
	if _, err := desired.New(input); err != nil {
		t.Fatalf("unrelated family-local ids collide: %v", err)
	}
}
