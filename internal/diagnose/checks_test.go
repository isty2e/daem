package diagnose

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/findings"
	sourcepkg "github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/backend/localfs"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	targetpkg "github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func TestSelectedSkillTargetsFiltersBySelection(t *testing.T) {
	selection, err := targetselection.ForDiagnostics([]string{"codex"})
	if err != nil {
		t.Fatalf("build selection: %v", err)
	}
	skill := testfixture.Skill(t, desiredskill.Spec{
		Name:   "demo",
		Source: sourcetest.Local(t, "skills/demo", sourcepkg.LocalSourceModeVendor),
		Targets: []targetpkg.Target{
			targetpkg.TargetCodex,
			targetpkg.TargetClaudeCode,
		},
		Scope: targetpkg.ScopeProject, InstallMode: desiredskill.InstallModeCopy, Portable: true,
	})

	targets := SelectedSkillTargets(skill, selection)
	if len(targets) != 1 || targets[0] != targetpkg.TargetCodex {
		t.Fatalf("expected only codex target, got %#v", targets)
	}
}

func TestSkillChecksClassifiesMissingLocalSourceAsWarning(t *testing.T) {
	selection, err := targetselection.ForDiagnostics([]string{"codex"})
	if err != nil {
		t.Fatalf("build selection: %v", err)
	}
	skill := testfixture.Skill(t, desiredskill.Spec{
		Name:        "missing",
		Source:      sourcetest.Local(t, "skills/missing", sourcepkg.LocalSourceModeVendor),
		Targets:     []targetpkg.Target{targetpkg.TargetCodex},
		Scope:       targetpkg.ScopeProject,
		InstallMode: desiredskill.InstallModeCopy,
		Portable:    true,
	})

	resolver, err := localfs.NewResolver(t.TempDir())
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	checks := skillChecks(context.Background(), resolver, skill, selection)
	if len(checks) != 1 {
		t.Fatalf("checks = %#v, want one missing-source check", checks)
	}
	if checks[0].Status != findings.CheckWarn {
		t.Fatalf("severity = %s, want %s: %s", checks[0].Status, findings.CheckWarn, checks[0].Detail)
	}
	if !strings.Contains(checks[0].Detail, "local skill source skills/missing is missing") {
		t.Fatalf("detail = %q, want missing local source diagnostic", checks[0].Detail)
	}
}

func TestConfigFileCheckRejectsMultipleJSONValues(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte("{}\n{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	check := configFileCheck("target=claude-code config_file", doctorConfigFile{
		Path:                configPath,
		Format:              ConfigFormatJSON,
		SyntaxErrorSeverity: findings.SeverityError,
	})
	if check.Status != findings.CheckError {
		t.Fatalf("expected config syntax error, got %s: %s", check.Status, check.Detail)
	}
	if !strings.Contains(check.Detail, "multiple JSON values") {
		t.Fatalf("expected multiple JSON values detail, got %q", check.Detail)
	}
}

func TestDirectoryCheckReportsCreatableMissingDirectory(t *testing.T) {
	check := directoryCheck("cache", filepath.Join(t.TempDir(), "missing", "cache"))
	if check.Status != findings.CheckOK {
		t.Fatalf("expected creatable directory to be ok, got %s: %s", check.Status, check.Detail)
	}
	if !strings.Contains(check.Detail, "can be created") {
		t.Fatalf("expected creatable detail, got %q", check.Detail)
	}
}

func TestTargetSpecForCodexConfigFile(t *testing.T) {
	homeDirectory := filepath.Join(t.TempDir(), "home")
	spec := targetSpecFor(homeDirectory, targetpkg.TargetCodex)
	if spec.ConfigRoot != filepath.Join(homeDirectory, ".codex") {
		t.Fatalf("unexpected config root: %q", spec.ConfigRoot)
	}
	if len(spec.ConfigFiles) != 1 {
		t.Fatalf("expected one codex config file, got %d", len(spec.ConfigFiles))
	}
	if spec.ConfigFiles[0].Format != ConfigFormatTOML {
		t.Fatalf("expected codex config format TOML, got %s", spec.ConfigFiles[0].Format)
	}
}

func TestTargetSpecForAntigravityCLIConfigFile(t *testing.T) {
	homeDirectory := filepath.Join(t.TempDir(), "home")
	spec := targetSpecFor(homeDirectory, targetpkg.TargetAntigravityCLI)
	if spec.ConfigRoot != filepath.Join(homeDirectory, ".gemini", "antigravity-cli") {
		t.Fatalf("unexpected config root: %q", spec.ConfigRoot)
	}
	if len(spec.ConfigFiles) != 1 {
		t.Fatalf("expected one antigravity cli config file, got %d", len(spec.ConfigFiles))
	}
	if spec.ConfigFiles[0].Path != filepath.Join(homeDirectory, ".gemini", "antigravity-cli", "settings.json") {
		t.Fatalf("unexpected config file path: %q", spec.ConfigFiles[0].Path)
	}
	if spec.ConfigFiles[0].Format != ConfigFormatJSON {
		t.Fatalf("expected antigravity cli config format JSON, got %s", spec.ConfigFiles[0].Format)
	}
	if spec.ConfigFiles[0].SyntaxErrorSeverity != findings.SeverityWarn {
		t.Fatalf("expected antigravity cli config syntax warning, got %s", spec.ConfigFiles[0].SyntaxErrorSeverity)
	}
}
