package normalize

import (
	"fmt"

	"github.com/isty2e/daem/internal/declaration"
	"github.com/isty2e/daem/internal/desired"
	desiredhostconfig "github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/target"
)

func normalizeHostConfigs(
	rawConfigs []declaration.HostConfig,
	defaultTargets []target.Target,
	defaults desired.Defaults,
) ([]desiredhostconfig.Config, error) {
	configs := make([]desiredhostconfig.Config, 0, len(rawConfigs))
	for index, raw := range rawConfigs {
		context := fmt.Sprintf("host_config[%d]", index)
		config, err := normalizeHostConfig(raw, defaultTargets, defaults, context)
		if err != nil {
			return nil, err
		}
		configs = append(configs, config)
	}
	return configs, nil
}

func normalizeHostConfig(
	raw declaration.HostConfig,
	defaultTargets []target.Target,
	defaults desired.Defaults,
	context string,
) (desiredhostconfig.Config, error) {
	if !raw.Source.Set {
		return desiredhostconfig.Config{}, fmt.Errorf("%s.source: required", context)
	}

	targets, err := targetsWithDefault(raw.Targets, defaultTargets, context+".targets")
	if err != nil {
		return desiredhostconfig.Config{}, err
	}
	if len(targets) != 1 {
		return desiredhostconfig.Config{}, fmt.Errorf("%s.targets: host configuration requires exactly one target", context)
	}

	scope, err := scopeWithDefault(raw.Scope, defaults.Scope(), context+".scope")
	if err != nil {
		return desiredhostconfig.Config{}, err
	}

	sourceSpec, err := normalizeRequiredSource(raw.Source.Source, context+".source")
	if err != nil {
		return desiredhostconfig.Config{}, err
	}

	ownership, err := desiredhostconfig.ParseOwnership(raw.Ownership)
	if err != nil {
		return desiredhostconfig.Config{}, fmt.Errorf("%s.ownership: %w", context, err)
	}
	syntax, err := desiredhostconfig.ParseSyntax(raw.Syntax)
	if err != nil {
		return desiredhostconfig.Config{}, fmt.Errorf("%s.syntax: %w", context, err)
	}

	config, err := desiredhostconfig.New(desiredhostconfig.Spec{
		Name: raw.ID, Source: sourceSpec, Target: targets[0], Scope: scope,
		Path: raw.Path, Ownership: ownership, Syntax: syntax,
	})
	if err != nil {
		return desiredhostconfig.Config{}, fmt.Errorf("%s: %w", context, err)
	}
	return config, nil
}
