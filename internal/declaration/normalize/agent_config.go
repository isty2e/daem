package normalize

import (
	"fmt"

	"github.com/isty2e/daem/internal/declaration"
	"github.com/isty2e/daem/internal/desired"
	desiredagentconfig "github.com/isty2e/daem/internal/desired/agentconfig"
	"github.com/isty2e/daem/internal/target"
)

func normalizeAgentConfigs(
	rawConfigs []declaration.AgentConfig,
	defaultTargets []target.Target,
	defaults desired.Defaults,
) ([]desiredagentconfig.Config, error) {
	configs := make([]desiredagentconfig.Config, 0, len(rawConfigs))
	for index, raw := range rawConfigs {
		context := fmt.Sprintf("host_config[%d]", index)
		config, err := normalizeAgentConfig(raw, defaultTargets, defaults, context)
		if err != nil {
			return nil, err
		}
		configs = append(configs, config)
	}
	return configs, nil
}

func normalizeAgentConfig(
	raw declaration.AgentConfig,
	defaultTargets []target.Target,
	defaults desired.Defaults,
	context string,
) (desiredagentconfig.Config, error) {
	if !raw.Source.Set {
		return desiredagentconfig.Config{}, fmt.Errorf("%s.source: required", context)
	}

	targets, err := targetsWithDefault(raw.Targets, defaultTargets, context+".targets")
	if err != nil {
		return desiredagentconfig.Config{}, err
	}
	if len(targets) != 1 {
		return desiredagentconfig.Config{}, fmt.Errorf("%s.targets: host configuration requires exactly one target", context)
	}

	scope, err := scopeWithDefault(raw.Scope, defaults.Scope(), context+".scope")
	if err != nil {
		return desiredagentconfig.Config{}, err
	}

	sourceSpec, err := normalizeRequiredSource(raw.Source.Source, context+".source")
	if err != nil {
		return desiredagentconfig.Config{}, err
	}

	ownership, err := desiredagentconfig.ParseOwnership(raw.Ownership)
	if err != nil {
		return desiredagentconfig.Config{}, fmt.Errorf("%s.ownership: %w", context, err)
	}
	syntax, err := desiredagentconfig.ParseSyntax(raw.Syntax)
	if err != nil {
		return desiredagentconfig.Config{}, fmt.Errorf("%s.syntax: %w", context, err)
	}

	config, err := desiredagentconfig.New(desiredagentconfig.Spec{
		Name: raw.ID, Source: sourceSpec, Target: targets[0], Scope: scope,
		Path: raw.Path, Ownership: ownership, Syntax: syntax,
	})
	if err != nil {
		return desiredagentconfig.Config{}, fmt.Errorf("%s: %w", context, err)
	}
	return config, nil
}
