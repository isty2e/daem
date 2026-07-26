package normalize

import (
	"fmt"
	"sort"

	"github.com/isty2e/daem/internal/declaration"
	"github.com/isty2e/daem/internal/desired"
	desiredinstructions "github.com/isty2e/daem/internal/desired/instructions"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/target"
)

func normalizeInstructions(rawInstructions map[string]declaration.Instructions, defaultTargets []target.Target, defaults desired.Defaults) ([]desiredinstructions.Instructions, error) {
	names := make([]string, 0, len(rawInstructions))
	for name := range rawInstructions {
		names = append(names, name)
	}
	sort.Strings(names)

	values := make([]desiredinstructions.Instructions, 0, len(names))
	for _, name := range names {
		rawInstruction := rawInstructions[name]
		context := "instructions." + name

		sourceSpec, err := normalizeInstructionSource(rawInstruction.Source, context+".source")
		if err != nil {
			return nil, err
		}
		targets, err := targetsWithDefault(rawInstruction.Targets, defaultTargets, context+".targets")
		if err != nil {
			return nil, err
		}

		defaultScope := defaults.Scope()
		if name == string(target.ScopeGlobal) || name == string(target.ScopeProject) {
			defaultScope, err = target.ParseScope(name)
			if err != nil {
				return nil, err
			}
		}
		scope, err := scopeWithDefault(rawInstruction.Scope, defaultScope, context+".scope")
		if err != nil {
			return nil, err
		}

		renderings, err := normalizeInstructionTargets(rawInstruction.Target, context)
		if err != nil {
			return nil, err
		}
		value, err := desiredinstructions.New(desiredinstructions.Spec{
			Name: name, Source: sourceSpec, Targets: targets, Scope: scope, Renderings: renderings,
		})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", context, err)
		}
		values = append(values, value)
	}
	return values, nil
}

func normalizeInstructionSource(raw declaration.InstructionSource, context string) (source.Source, error) {
	if !raw.Set {
		return source.Source{}, fmt.Errorf("%s: required", context)
	}

	sourceSpec, err := normalizeRequiredSource(raw.Source, context)
	if err != nil {
		return source.Source{}, err
	}
	return sourceSpec, nil
}

func normalizeInstructionTargets(rawTargets map[string]declaration.InstructionTarget, context string) (map[target.Target]desiredinstructions.Rendering, error) {
	renderings := make(map[target.Target]desiredinstructions.Rendering, len(rawTargets))
	for rawTarget, rawRendering := range rawTargets {
		parsedTarget, err := target.ParseTarget(rawTarget)
		if err != nil {
			return nil, fmt.Errorf("%s.target.%s: %w", context, rawTarget, err)
		}

		mode := rawRendering.Mode
		if mode == "" {
			mode = string(desiredinstructions.RenderModeCopy)
		}
		rendering, err := desiredinstructions.NewRendering(rawRendering.RenderTo, desiredinstructions.RenderMode(mode))
		if err != nil {
			return nil, fmt.Errorf("%s.target.%s: %w", context, rawTarget, err)
		}
		renderings[parsedTarget] = rendering
	}
	return renderings, nil
}
