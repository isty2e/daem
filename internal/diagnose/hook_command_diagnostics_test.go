package diagnose

import (
	"testing"

	desiredhook "github.com/isty2e/daem/internal/desired/hook"
	"github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/findings"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func TestHookCommandDiagnosticsHandleQuotedPathsAndDoubleQuotedExpansion(t *testing.T) {
	windowsExecutable := commandExecutable(`"C:\Program Files\Tool\tool.exe" --flag`)
	if windowsExecutable != `C:\Program Files\Tool\tool.exe` {
		t.Fatalf("windowsExecutable = %q", windowsExecutable)
	}
	if isLookupAmbiguousExecutable(windowsExecutable) {
		t.Fatalf("quoted absolute executable was treated as PATH lookup: %q", windowsExecutable)
	}
	if !containsShellSyntax(`echo "$HOME"`) {
		t.Fatal("double-quoted shell expansion was not diagnosed")
	}
	if containsShellSyntax(`echo '$HOME'`) {
		t.Fatal("single-quoted literal was diagnosed as shell expansion")
	}
}

func TestHookCommandDiagnosticsTreatHookFilePlaceholderAsManagedExecutable(t *testing.T) {
	if !isLookupAmbiguousExecutable("{hook_file:guard}") {
		t.Fatal("placeholder executable should still look path-ambiguous before managed reference check")
	}
	managed := testfixture.Hook(t, desiredhook.Spec{
		Name:    "managed",
		Event:   "PreToolUse",
		Type:    desiredhook.TypeCommand,
		Command: "{hook_file:guard} --check",
		Targets: []target.Target{target.TargetClaudeCode},
		Scope:   target.ScopeProject,
	})
	if !isManagedHookAssetExecutable(managed, "{hook_file:guard}") {
		t.Fatal("valid hook_file placeholder was not classified as managed executable")
	}
	if isManagedHookAssetExecutable(managed, "{hook_file:other}") {
		t.Fatal("undeclared hook_file placeholder was classified as managed executable")
	}

	selection, err := targetselection.ForAvailableTargets([]target.Target{target.TargetClaudeCode}, nil)
	if err != nil {
		t.Fatalf("ForAvailableTargets returned error: %v", err)
	}
	diagnostics := HookCommandDiagnostics([]desiredhook.Hook{managed}, selection)

	if hasHookDiagnostic(diagnostics, hookDiagnosticLookup, target.TargetClaudeCode) {
		t.Fatalf("diagnostics = %#v, did not want PATH lookup warning for managed hook_file placeholder", diagnostics)
	}
}

func TestHookCommandDiagnosticsUseSurfaceSupportAndSelection(t *testing.T) {
	selection, err := targetselection.ForAvailableTargets(
		[]target.Target{target.TargetCodex, target.TargetOpenCode},
		nil,
	)
	if err != nil {
		t.Fatalf("ForAvailableTargets returned error: %v", err)
	}
	diagnostics := HookCommandDiagnostics([]desiredhook.Hook{testfixture.Hook(t, desiredhook.Spec{
		Name:    "protect-env",
		Event:   "PreToolUse",
		Type:    desiredhook.TypeCommand,
		Command: "echo $HOME",
		Targets: []target.Target{
			target.TargetCodex,
			target.TargetOpenCode,
		},
		Scope: target.ScopeProject,
	})}, selection)

	if !hasHookDiagnostic(diagnostics, hookDiagnosticCodexTrust, target.TargetCodex) {
		t.Fatalf("diagnostics = %#v, want Codex trust-review warning", diagnostics)
	}
	if hasHookDiagnosticForTarget(diagnostics, target.TargetOpenCode) {
		t.Fatalf("diagnostics = %#v, did not want unsupported OpenCode hook diagnostics", diagnostics)
	}
}

func TestHookCommandDiagnosticsSkipUnselectedSupportedTarget(t *testing.T) {
	selection, err := targetselection.ForAvailableTargets(
		[]target.Target{target.TargetCodex, target.TargetClaudeCode},
		[]string{string(target.TargetClaudeCode)},
	)
	if err != nil {
		t.Fatalf("ForAvailableTargets returned error: %v", err)
	}
	diagnostics := HookCommandDiagnostics([]desiredhook.Hook{testfixture.Hook(t, desiredhook.Spec{
		Name:    "protect-env",
		Event:   "PreToolUse",
		Type:    desiredhook.TypeCommand,
		Command: "echo $HOME",
		Targets: []target.Target{
			target.TargetCodex,
		},
		Scope: target.ScopeProject,
	})}, selection)

	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none for unselected Codex target", diagnostics)
	}
}

func hasHookDiagnostic(diagnostics []findings.Diagnostic, code string, target target.Target) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code && diagnostic.Target == target {
			return true
		}
	}

	return false
}

func hasHookDiagnosticForTarget(diagnostics []findings.Diagnostic, target target.Target) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Target == target {
			return true
		}
	}

	return false
}
