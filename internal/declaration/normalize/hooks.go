package normalize

import (
	"fmt"

	"github.com/isty2e/daem/internal/declaration"
	"github.com/isty2e/daem/internal/desired"
	desiredhook "github.com/isty2e/daem/internal/desired/hook"
	"github.com/isty2e/daem/internal/target"
)

func normalizeHooks(rawHooks []declaration.Hook, defaultTargets []target.Target, defaults desired.Defaults) ([]desiredhook.Hook, error) {
	hooks := make([]desiredhook.Hook, 0, len(rawHooks))

	for index, rawHook := range rawHooks {
		context := fmt.Sprintf("hook[%d]", index)
		hookType := rawHook.Type
		if hookType == "" {
			hookType = string(desiredhook.TypeCommand)
		}

		targets, err := targetsWithDefault(rawHook.Targets, defaultTargets, context+".targets")
		if err != nil {
			return nil, err
		}

		scope, err := scopeWithDefault(rawHook.Scope, defaults.Scope(), context+".scope")
		if err != nil {
			return nil, err
		}

		overrides, err := normalizeHookOverrides(rawHook.TargetOverrides, context)
		if err != nil {
			return nil, err
		}

		hook, err := desiredhook.New(desiredhook.Spec{
			Name:            rawHook.Name,
			Event:           rawHook.Event,
			Matcher:         rawHook.Matcher,
			Type:            desiredhook.Type(hookType),
			Command:         rawHook.Command,
			TimeoutSeconds:  rawHook.TimeoutSeconds,
			StatusMessage:   rawHook.StatusMessage,
			Targets:         targets,
			Scope:           scope,
			TargetOverrides: overrides,
		})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", context, err)
		}
		hooks = append(hooks, hook)
	}

	return hooks, nil
}

func normalizeHookOverrides(rawOverrides []declaration.HookTargetOverride, context string) (map[target.Target]desiredhook.TargetOverride, error) {
	overrides := make(map[target.Target]desiredhook.TargetOverride, len(rawOverrides))
	for index, rawOverride := range rawOverrides {
		overrideContext := fmt.Sprintf("%s.target_override[%d]", context, index)

		parsedTarget, err := target.ParseTarget(rawOverride.Target)
		if err != nil {
			return nil, fmt.Errorf("%s.target: %w", overrideContext, err)
		}
		if _, exists := overrides[parsedTarget]; exists {
			return nil, fmt.Errorf("%s.target: duplicate override for target %q", overrideContext, parsedTarget)
		}
		overrides[parsedTarget] = desiredhook.NewTargetOverride(rawOverride.Condition, rawOverride.Matcher)
	}
	return overrides, nil
}
