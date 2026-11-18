package desired_test

import (
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/agentconfig"
	"github.com/isty2e/daem/internal/desired/instructions"
	"github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/target"
)

func agentConfigEnvironmentSpec(t *testing.T) desired.Spec {
	t.Helper()
	defaults, err := desired.NewDefaults(target.ScopeProject, skill.InstallModeCopy)
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := source.NewLocalSource("config/settings.json", source.LocalSourceModeVendor)
	if err != nil {
		t.Fatal(err)
	}
	config, err := agentconfig.New(agentconfig.Spec{
		Name: "review", Source: provenance, Target: target.TargetPi, Scope: target.ScopeProject, Path: "settings.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	return desired.Spec{Targets: []target.Target{target.TargetPi}, Defaults: defaults, AgentConfigs: []agentconfig.Config{config}}
}

func TestAgentConfigEnvironmentRetainsSourcesAndImmutableDeclarations(t *testing.T) {
	input := agentConfigEnvironmentSpec(t)
	config := input.AgentConfigs[0]
	value, err := desired.New(input)
	if err != nil {
		t.Fatal(err)
	}
	input.AgentConfigs[0] = agentconfig.Config{}
	returned := value.AgentConfigs()
	returned[0] = agentconfig.Config{}
	if got := value.AgentConfigs(); len(got) != 1 || got[0].ID() != config.ID() || got[0].Validate() != nil {
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
	if err != nil || len(rebuilt.AgentConfigs()) != 1 || rebuilt.AgentConfigs()[0].ID() != config.ID() {
		t.Fatalf("environment rebuild dropped host configuration: %v", err)
	}
}

func TestAgentConfigEnvironmentRejectsInvalidOrDuplicateIdentity(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		input := agentConfigEnvironmentSpec(t)
		if invalid {
			input.AgentConfigs[0] = agentconfig.Config{}
		} else {
			input.AgentConfigs = append(input.AgentConfigs, input.AgentConfigs[0])
		}
		if _, err := desired.New(input); err == nil {
			t.Fatal("invalid or duplicate host configuration admitted")
		}
	}
	input := agentConfigEnvironmentSpec(t)
	instruction, err := instructions.New(instructions.Spec{
		Name: "review", Source: input.AgentConfigs[0].Source(), Targets: []target.Target{target.TargetPi}, Scope: target.ScopeProject,
	})
	if err != nil {
		t.Fatal(err)
	}
	input.Instructions = []instructions.Instructions{instruction}
	if _, err := desired.New(input); err != nil {
		t.Fatalf("unrelated family-local ids collide: %v", err)
	}
}
