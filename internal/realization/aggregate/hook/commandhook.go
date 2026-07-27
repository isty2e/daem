package commandhook

import (
	"fmt"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

var (
	codexProjectHookConfig = mustHookConfigDestination(".codex/config.toml")
	codexGlobalHookConfig  = mustHookConfigDestination("~/.codex/config.toml")
)

var codexSupportedHookEvents = map[string]struct{}{
	"SessionStart":      {},
	"PreToolUse":        {},
	"PermissionRequest": {},
	"PostToolUse":       {},
	"PreCompact":        {},
	"PostCompact":       {},
	"UserPromptSubmit":  {},
	"SubagentStart":     {},
	"SubagentStop":      {},
	"Stop":              {},
}

var codexMatcherIgnoredHookEvents = map[string]struct{}{
	"Stop":             {},
	"UserPromptSubmit": {},
}

func Destination(selectedTarget target.Target, scope target.Scope) (output.Destination, bool) {
	placement, ok := aggregate.HookPlacementFor(selectedTarget, scope)
	if !ok {
		return output.Destination{}, false
	}
	return placement.AggregateRoot(), true
}

func CodexInlineConfigDestination(hookDestination output.Destination) (output.Destination, bool) {
	for _, candidate := range []struct {
		scope  target.Scope
		config output.Destination
	}{
		{scope: target.ScopeProject, config: codexProjectHookConfig},
		{scope: target.ScopeGlobal, config: codexGlobalHookConfig},
	} {
		placement, ok := aggregate.HookPlacementFor(target.TargetCodex, candidate.scope)
		if ok && hookDestination == placement.AggregateRoot() {
			return candidate.config, true
		}
	}
	return output.Destination{}, false
}

func mustHookConfigDestination(value string) output.Destination {
	destination, err := output.Parse(value)
	if err != nil {
		panic(err)
	}
	return destination
}

func ValidateShape(name string, selectedTarget target.Target, event string, matcher string, condition string) error {
	switch selectedTarget {
	case target.TargetClaudeCode:
		return nil
	case target.TargetCodex:
		return validateCodex(name, event, matcher, condition)
	default:
		return fmt.Errorf("hook %q target %q: command hook surface is not supported", name, selectedTarget)
	}
}

func validateCodex(name string, event string, matcher string, condition string) error {
	if condition != "" {
		return fmt.Errorf("hook %q target %q: target_override.if is not supported", name, target.TargetCodex)
	}
	if _, ok := codexSupportedHookEvents[event]; !ok {
		return fmt.Errorf("hook %q target %q: unsupported event %q", name, target.TargetCodex, event)
	}
	if _, ignored := codexMatcherIgnoredHookEvents[event]; ignored && matcher != "" {
		return fmt.Errorf("hook %q target %q: matcher is not supported for event %q", name, target.TargetCodex, event)
	}

	return nil
}
