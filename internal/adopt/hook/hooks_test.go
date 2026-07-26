package hook

import (
	"os"
	"path/filepath"
	"testing"

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
