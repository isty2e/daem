package hook

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

func TestCandidatesImportsRepresentableCodexHook(t *testing.T) {
	tempDir := t.TempDir()
	oldWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWorkingDirectory); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	if err := os.MkdirAll(".codex", 0o700); err != nil {
		t.Fatal(err)
	}
	content := []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo ok","timeout":3,"statusMessage":"checking"}]}]}}`)
	if err := os.WriteFile(filepath.Join(".codex", "hooks.json"), content, 0o600); err != nil {
		t.Fatal(err)
	}

	hooks, skipped, err := Candidates(target.TargetCodex, target.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 {
		t.Fatalf("skipped = %#v, want none", skipped)
	}
	if len(hooks) != 1 {
		t.Fatalf("hooks = %#v, want one", hooks)
	}
	hook := hooks[0]
	if hook.Event != "PreToolUse" || hook.Matcher != "Bash" || hook.Command != "echo ok" {
		t.Fatalf("hook = %#v", hook)
	}
	if hook.Target != target.TargetCodex || hook.Scope != target.ScopeProject {
		t.Fatalf("target/scope = %s/%s", hook.Target, hook.Scope)
	}
}

func TestCandidatesSkipsCodexHookShapeThatSurfaceCannotRepresent(t *testing.T) {
	tempDir := t.TempDir()
	oldWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWorkingDirectory); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	if err := os.MkdirAll(".codex", 0o700); err != nil {
		t.Fatal(err)
	}
	content := []byte(`{"hooks":{"Stop":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo no"}]}]}}`)
	if err := os.WriteFile(filepath.Join(".codex", "hooks.json"), content, 0o600); err != nil {
		t.Fatal(err)
	}

	hooks, skipped, err := Candidates(target.TargetCodex, target.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(hooks) != 0 {
		t.Fatalf("hooks = %#v, want none", hooks)
	}
	if len(skipped) != 1 || skipped[0].Reason == "" {
		t.Fatalf("skipped = %#v, want one reason", skipped)
	}
}

func TestHookDestinationPathUsesCanonicalOutputGrammar(t *testing.T) {
	projectDestination, err := output.Parse(".codex/hooks.json")
	if err != nil {
		t.Fatal(err)
	}
	projectPath, err := hookDestinationPath(projectDestination, target.ScopeProject)
	if err != nil {
		t.Fatalf("hookDestinationPath(project) error = %v", err)
	}
	if want := filepath.FromSlash(".codex/hooks.json"); projectPath != want {
		t.Fatalf("hookDestinationPath(project) = %q, want %q", projectPath, want)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	homeDestination, err := output.Parse("~/.claude/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	homePath, err := hookDestinationPath(homeDestination, target.ScopeGlobal)
	if err != nil {
		t.Fatalf("hookDestinationPath(home) error = %v", err)
	}
	if want := filepath.Join(home, ".claude", "settings.json"); homePath != want {
		t.Fatalf("hookDestinationPath(home) = %q, want %q", homePath, want)
	}

	if _, err := hookDestinationPath(homeDestination, target.ScopeProject); err == nil {
		t.Fatal("hookDestinationPath(home, project scope) succeeded, want scope error")
	}
}
