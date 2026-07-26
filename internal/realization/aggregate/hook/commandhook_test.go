package commandhook

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/target"
)

func TestDestination(t *testing.T) {
	tests := []struct {
		name   string
		target target.Target
		scope  target.Scope
		want   string
		ok     bool
	}{
		{name: "codex project", target: target.TargetCodex, scope: target.ScopeProject, want: ".codex/hooks.json", ok: true},
		{name: "codex global", target: target.TargetCodex, scope: target.ScopeGlobal, want: "~/.codex/hooks.json", ok: true},
		{name: "claude project", target: target.TargetClaudeCode, scope: target.ScopeProject, want: ".claude/settings.json", ok: true},
		{name: "claude global", target: target.TargetClaudeCode, scope: target.ScopeGlobal, want: "~/.claude/settings.json", ok: true},
		{name: "unsupported", target: target.TargetOpenCode, scope: target.ScopeProject, ok: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := Destination(test.target, test.scope)
			if got != test.want || ok != test.ok {
				t.Fatalf("Destination(%q, %q) = %q, %t; want %q, %t", test.target, test.scope, got, ok, test.want, test.ok)
			}
		})
	}
}

func TestCodexInlineConfigDestination(t *testing.T) {
	if got, ok := CodexInlineConfigDestination(".codex/hooks.json"); got != ".codex/config.toml" || !ok {
		t.Fatalf("project inline config = %q, %t", got, ok)
	}
	if got, ok := CodexInlineConfigDestination("~/.codex/hooks.json"); got != "~/.codex/config.toml" || !ok {
		t.Fatalf("global inline config = %q, %t", got, ok)
	}
	if got, ok := CodexInlineConfigDestination(".claude/settings.json"); got != "" || ok {
		t.Fatalf("non-codex inline config = %q, %t", got, ok)
	}
}

func TestValidateShape(t *testing.T) {
	if err := ValidateShape("format", target.TargetClaudeCode, "AnyFutureEvent", "Matcher", "tool_name == 'Bash'"); err != nil {
		t.Fatalf("ValidateShape claude returned error: %v", err)
	}
	if err := ValidateShape("format", target.TargetCodex, "PreToolUse", "Bash", ""); err != nil {
		t.Fatalf("ValidateShape codex returned error: %v", err)
	}

	tests := []struct {
		name    string
		event   string
		matcher string
		ifValue string
		want    string
	}{
		{name: "condition", event: "PreToolUse", ifValue: "tool_name == 'Bash'", want: "target_override.if is not supported"},
		{name: "unsupported event", event: "Unknown", want: `unsupported event "Unknown"`},
		{name: "ignored matcher", event: "Stop", matcher: "Bash", want: `matcher is not supported for event "Stop"`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateShape("format", target.TargetCodex, test.event, test.matcher, test.ifValue)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err = %v, want %q", err, test.want)
			}
		})
	}
}
