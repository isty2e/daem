package hook

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/encoding/hookdocument"
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

	hooks, skipped, err := Candidates(context.Background(), target.TargetCodex, target.ScopeProject)
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

	hooks, skipped, err := Candidates(context.Background(), target.TargetCodex, target.ScopeProject)
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

func TestImportHooksProjectionRejectsAmbiguousDocumentWithoutPartialHooks(t *testing.T) {
	validHook := `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo ok"}]}]}`
	for _, test := range []struct {
		name    string
		content string
		reason  string
	}{
		{name: "duplicate root", content: `{"hooks":` + validHook + `,"hooks":{}}`, reason: importHookSkipDuplicateJSONKey},
		{name: "duplicate nested", content: `{"meta":{"x":1,"x":2},"hooks":` + validHook + `}`, reason: importHookSkipDuplicateJSONKey},
		{name: "excessive depth", content: strings.Repeat("[", 66) + "0" + strings.Repeat("]", 66), reason: importHookSkipJSONDepth},
		{name: "JSON comments not admitted", content: `{"hooks":{} // comment\n}`, reason: "malformed_json"},
	} {
		t.Run(test.name, func(t *testing.T) {
			hooks, skipped := parseImportHooks(
				[]byte(test.content),
				target.TargetCodex,
				target.ScopeProject,
				".codex/hooks.json",
			)
			if len(hooks) != 0 || len(skipped) != 1 || skipped[0].Reason != test.reason {
				t.Fatalf("parseImportHooks = (%#v, %#v), want one %q skip", hooks, skipped, test.reason)
			}
		})
	}
}

func TestCandidatesRejectsUnsafeHookFileShapes(t *testing.T) {
	root := t.TempDir()
	withHookWorkingDirectory(t, root)
	if err := os.MkdirAll(".codex", 0o700); err != nil {
		t.Fatal(err)
	}
	livePath := filepath.Join(".codex", "hooks.json")

	assertSkip := func(reason string) {
		t.Helper()
		hooks, skipped, err := Candidates(context.Background(), target.TargetCodex, target.ScopeProject)
		if err != nil {
			t.Fatalf("Candidates returned error: %v", err)
		}
		if len(hooks) != 0 || !hasHookSkip(skipped, ".codex/hooks.json", reason) {
			t.Fatalf("Candidates = (%#v, %#v), want %q skip", hooks, skipped, reason)
		}
	}

	if err := os.Mkdir(livePath, 0o700); err != nil {
		t.Fatal(err)
	}
	assertSkip(importHookSkipNotRegular)
	if err := os.Remove(livePath); err != nil {
		t.Fatal(err)
	}

	targetPath := filepath.Join(root, "actual-hooks.json")
	if err := os.WriteFile(targetPath, []byte(`{"hooks":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(targetPath, livePath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	assertSkip(importHookSkipSymlink)
	if err := os.Remove(livePath); err != nil {
		t.Fatal(err)
	}

	file, err := os.OpenFile(livePath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(hookdocument.MaximumBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	assertSkip(importHookSkipTooLarge)
}

func TestCandidatesStopsWhenHookImportContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	hooks, skipped, err := Candidates(ctx, target.TargetCodex, target.ScopeProject)
	if !errors.Is(err, context.Canceled) || hooks != nil || skipped != nil {
		t.Fatalf("Candidates = (%#v, %#v, %v), want context cancellation", hooks, skipped, err)
	}
}

func withHookWorkingDirectory(t *testing.T, directory string) {
	t.Helper()

	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
}

func hasHookSkip(skipped []adopt.Skipped, livePath string, reason string) bool {
	for _, skip := range skipped {
		if skip.LivePath == livePath && skip.Reason == reason {
			return true
		}
	}
	return false
}
