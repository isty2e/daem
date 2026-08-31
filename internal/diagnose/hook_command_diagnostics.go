package diagnose

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/isty2e/daem/internal/desired/hook"
	"github.com/isty2e/daem/internal/findings"
	hostsurfacecatalog "github.com/isty2e/daem/internal/hostsurface/catalog"
	targetpkg "github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

const (
	hookDiagnosticMissingTimeout = "hook.command.missing_timeout"
	hookDiagnosticShellSyntax    = "hook.command.shell_syntax"
	hookDiagnosticInterpreter    = "hook.command.broad_interpreter"
	hookDiagnosticLookup         = "hook.command.lookup_ambiguous"
	hookDiagnosticCodexTrust     = "hook.codex.trust_review_required"
)

func HookCommandDiagnostics(hooks []hook.Hook, selection targetselection.Selection) []findings.Diagnostic {
	diagnostics := make([]findings.Diagnostic, 0)
	for _, hook := range hooks {
		for _, target := range hook.Targets() {
			if !selection.Includes(target) || !hostsurfacecatalog.Product().HasHookTarget(target) {
				continue
			}
			diagnostics = append(diagnostics, diagnosticsForHookTarget(hook, target)...)
		}
	}

	return diagnostics
}

func diagnosticsForHookTarget(hook hook.Hook, target targetpkg.Target) []findings.Diagnostic {
	command := strings.TrimSpace(hook.Command())
	executable := commandExecutable(command)
	diagnostics := make([]findings.Diagnostic, 0, 5)

	if hook.TimeoutSeconds() == 0 {
		diagnostics = append(diagnostics, newHookCommandDiagnostic(
			hook,
			target,
			hookDiagnosticMissingTimeout,
			command,
			"command hook has no explicit timeout; a hung command can stall host hook execution",
		))
	}
	if containsShellSyntax(command) {
		diagnostics = append(diagnostics, newHookCommandDiagnostic(
			hook,
			target,
			hookDiagnosticShellSyntax,
			command,
			"command uses shell-like syntax such as expansion, pipes, redirects, or command separators; review host shell behavior before applying",
		))
	}
	if isBroadInterpreterExecutable(executable) {
		diagnostics = append(diagnostics, newHookCommandDiagnostic(
			hook,
			target,
			hookDiagnosticInterpreter,
			command,
			fmt.Sprintf("command starts with broad interpreter %q; review the script boundary and arguments", executable),
		))
	}
	if isLookupAmbiguousExecutable(executable) && !isManagedHookAssetExecutable(hook, executable) {
		diagnostics = append(diagnostics, newHookCommandDiagnostic(
			hook,
			target,
			hookDiagnosticLookup,
			command,
			fmt.Sprintf("command executable %q is resolved by host PATH lookup; prefer an explicit managed or absolute executable path when possible", executable),
		))
	}
	if target == targetpkg.TargetCodex {
		diagnostics = append(diagnostics, newHookCommandDiagnostic(
			hook,
			target,
			hookDiagnosticCodexTrust,
			command,
			"Codex may require host-level hook trust review before this hook runs; daem only writes configuration",
		))
	}

	return diagnostics
}

func newHookCommandDiagnostic(
	hook hook.Hook,
	target targetpkg.Target,
	code string,
	command string,
	detail string,
) findings.Diagnostic {
	return findings.Diagnostic{
		Severity: findings.SeverityWarn,
		Code:     code,
		EntityID: hook.ID(),
		Target:   target,
		Scope:    hook.Scope(),
		Event:    strings.TrimSpace(hook.Event()),
		Command:  command,
		Detail:   detail,
	}
}

func HookCommandChecks(hooks []hook.Hook, selection targetselection.Selection) []findings.Check {
	diagnostics := HookCommandDiagnostics(hooks, selection)
	checks := make([]findings.Check, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		checks = append(checks, warnCheck(
			fmt.Sprintf("target=%s resource=%s diagnostic=%s", diagnostic.Target, resourceIDString(diagnostic.EntityID), diagnostic.Code),
			fmt.Sprintf("event=%q command=%q %s", diagnostic.Event, diagnostic.Command, diagnostic.Detail),
		))
	}

	return checks
}

func commandExecutable(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}

	var builder strings.Builder
	var quote rune
	escaped := false
	for _, value := range command {
		if escaped {
			builder.WriteRune(value)
			escaped = false
			continue
		}
		if quote != 0 {
			if value == quote {
				quote = 0
				continue
			}
			builder.WriteRune(value)
			continue
		}
		if value == '\\' {
			escaped = true
			continue
		}
		if value == '\'' || value == '"' {
			quote = value
			continue
		}
		if unicode.IsSpace(value) {
			break
		}
		builder.WriteRune(value)
	}

	return builder.String()
}

func containsShellSyntax(command string) bool {
	var quote rune
	escaped := false
	for _, value := range command {
		if escaped {
			escaped = false
			continue
		}
		if quote != '\'' && value == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if quote == '"' && (value == '$' || value == '`') {
				return true
			}
			if value == quote {
				quote = 0
			}
			continue
		}
		switch value {
		case '\'', '"':
			quote = value
		case '|', '>', '<', ';', '&', '`', '$', '*', '?':
			return true
		}
	}

	return false
}

func isBroadInterpreterExecutable(executable string) bool {
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(filepath.ToSlash(executable))), ".exe")
	switch name {
	case "sh", "bash", "zsh", "ksh", "fish",
		"python", "python2", "python3",
		"node", "deno", "bun",
		"ruby", "perl", "php",
		"pwsh", "powershell", "cmd",
		"env":
		return true
	default:
		return false
	}
}

func isLookupAmbiguousExecutable(executable string) bool {
	if executable == "" {
		return false
	}

	return !strings.ContainsAny(executable, `/\`)
}

func isManagedHookAssetExecutable(value hook.Hook, executable string) bool {
	for _, reference := range value.AssetReferences() {
		if reference.Placeholder() == executable {
			return true
		}
	}
	return false
}
