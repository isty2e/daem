package journal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

func TestRecoveryGlobalPathBindingCanonicalizesRootAliasesOnce(t *testing.T) {
	physicalRoot := t.TempDir()
	physicalRoot, err := filepath.EvalSymlinks(physicalRoot)
	if err != nil {
		t.Fatalf("canonicalize physical global root: %v", err)
	}
	aliasRoot := filepath.Join(t.TempDir(), "selected-home")
	if err := os.Symlink(physicalRoot, aliasRoot); err != nil {
		t.Fatalf("create global-root alias: %v", err)
	}
	destination, err := output.Parse("~/.codex/AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	resolverCalls := 0
	resolver := func(output.Destination) (string, error) {
		resolverCalls++
		return filepath.Join(aliasRoot, ".codex", "AGENTS.md"), nil
	}
	actions := []pathMutation{
		{Scope: target.ScopeGlobal, Destination: destination},
		{Scope: target.ScopeGlobal, Destination: destination, ContentPath: "/servers/context7"},
	}

	bindings, err := captureRecoveryGlobalPathBindings(actions, resolver)
	if err != nil {
		t.Fatalf("captureRecoveryGlobalPathBindings returned error: %v", err)
	}
	want := filepath.Join(physicalRoot, ".codex", "AGENTS.md")
	if got, err := bindings.path(target.ScopeGlobal, destination); err != nil || got != want {
		t.Fatalf("captured global path = %q, %v, want %q", got, err, want)
	}
	if resolverCalls != 1 {
		t.Fatalf("capture resolver calls = %d, want 1 per logical destination", resolverCalls)
	}
	if got, err := bindings.resolver(resolver)(destination); err != nil || got != want {
		t.Fatalf("bound resolver path = %q, %v, want %q", got, err, want)
	}
	if resolverCalls != 1 {
		t.Fatalf("bound resolver called ambient resolver; calls = %d, want 1", resolverCalls)
	}
	entry := recoveryEntry{
		Scope:              string(target.ScopeGlobal),
		Path:               destination.String(),
		ResolvedGlobalPath: want,
	}
	if err := validateRecoveryGlobalPathBindings(t.Context(), []recoveryEntry{entry}, resolver); err != nil {
		t.Fatalf("same physical root through an alias was rejected: %v", err)
	}

	differentRoot := t.TempDir()
	err = validateRecoveryGlobalPathBindings(
		context.Background(),
		[]recoveryEntry{entry},
		func(output.Destination) (string, error) {
			return filepath.Join(differentRoot, ".codex", "AGENTS.md"), nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "root selection changed") {
		t.Fatalf("different physical root error = %v, want root-selection drift refusal", err)
	}
}
