package commandhook

import (
	"fmt"
	pathpkg "path"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

const (
	codexProjectHookConfig = ".codex/config.toml"
	codexGlobalHookConfig  = "~/.codex/config.toml"
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

func Destination(selectedTarget target.Target, scope target.Scope) (string, bool) {
	placement, ok := aggregate.HookPlacementFor(selectedTarget, scope)
	if !ok {
		return "", false
	}
	return pathpkg.Clean(placement.AggregateRoot()), true
}

func CodexInlineConfigDestination(hookDestination string) (string, bool) {
	for _, candidate := range []struct {
		scope  target.Scope
		config string
	}{
		{scope: target.ScopeProject, config: codexProjectHookConfig},
		{scope: target.ScopeGlobal, config: codexGlobalHookConfig},
	} {
		placement, ok := aggregate.HookPlacementFor(target.TargetCodex, candidate.scope)
		if ok && pathpkg.Clean(hookDestination) == pathpkg.Clean(placement.AggregateRoot()) {
			return pathpkg.Clean(candidate.config), true
		}
	}
	return "", false
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
