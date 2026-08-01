package journal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
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

	bindings, err := captureRecoveryGlobalPathBindings(actions, resolver, nil)
	if err != nil {
		t.Fatalf("captureRecoveryGlobalPathBindings returned error: %v", err)
	}
	want := filepath.Join(physicalRoot, ".codex", "AGENTS.md")
	persisted, err := bindings.persisted(target.ScopeGlobal, destination)
	if err != nil {
		t.Fatalf("persist global path binding: %v", err)
	}
	if persisted.ResolvedPath != want {
		t.Fatalf("captured global path = %q, want %q", persisted.ResolvedPath, want)
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
		Scope:             string(target.ScopeGlobal),
		Path:              destination.String(),
		GlobalPathBinding: persisted,
	}
	if err := validateRecoveryGlobalPathBindings(t.Context(), []recoveryEntry{entry}, resolver, nil); err != nil {
		t.Fatalf("same physical root through an alias was rejected: %v", err)
	}
	for _, test := range []struct {
		name string
		kind rootedpath.FailureKind
		edit func(*recoveryGlobalPathBinding)
	}{
		{
			name: "object",
			kind: rootedpath.FailureRootReplaced,
			edit: func(binding *recoveryGlobalPathBinding) {
				binding.RootProvenance.ObjectFingerprint = differentRecoveryFingerprint(
					binding.RootProvenance.ObjectFingerprint,
				)
			},
		},
		{
			name: "mount",
			kind: rootedpath.FailureMountChanged,
			edit: func(binding *recoveryGlobalPathBinding) {
				binding.RootProvenance.MountFingerprint = differentRecoveryFingerprint(
					binding.RootProvenance.MountFingerprint,
				)
			},
		},
	} {
		t.Run("rejects-forged-"+test.name+"-provenance", func(t *testing.T) {
			forged := entry
			forgedBinding := *persisted
			test.edit(&forgedBinding)
			forged.GlobalPathBinding = &forgedBinding
			err := validateRecoveryGlobalPathBindings(
				t.Context(),
				[]recoveryEntry{forged},
				resolver,
				nil,
			)
			if !hasRootedPathFailureKind(err, test.kind) {
				t.Fatalf("forged %s provenance error = %v, want %s", test.name, err, test.kind)
			}
		})
	}
	conflicting := entry
	conflictingBinding := *persisted
	conflictingBinding.ResolvedPath = filepath.Join(physicalRoot, ".codex", "OTHER.md")
	conflicting.GlobalPathBinding = &conflictingBinding
	if err := validateRecoveryGlobalPathBindings(
		t.Context(),
		[]recoveryEntry{entry, conflicting},
		resolver,
		nil,
	); err == nil || !strings.Contains(err.Error(), "inconsistent capture-time bindings") {
		t.Fatalf("inconsistent duplicate binding error = %v", err)
	}

	differentRoot := t.TempDir()
	err = validateRecoveryGlobalPathBindings(
		t.Context(),
		[]recoveryEntry{entry},
		func(output.Destination) (string, error) {
			return filepath.Join(differentRoot, ".codex", "AGENTS.md"), nil
		},
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "root selection changed") {
		t.Fatalf("different physical root error = %v, want root-selection drift refusal", err)
	}
}

func differentRecoveryFingerprint(value string) string {
	replacement := byte('0')
	if value[len(value)-1] == replacement {
		replacement = '1'
	}
	return value[:len(value)-1] + string(replacement)
}

func TestRecoveryGlobalPathBindingRejectsSamePathRootReplacement(t *testing.T) {
	base := t.TempDir()
	selectedRoot := filepath.Join(base, "global-root")
	if err := os.Mkdir(selectedRoot, 0o700); err != nil {
		t.Fatalf("create selected global root: %v", err)
	}
	destination, err := output.Parse("~/.codex/AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	resolvedPath := filepath.Join(selectedRoot, ".codex", "AGENTS.md")
	resolver := func(output.Destination) (string, error) { return resolvedPath, nil }
	bindings, err := captureRecoveryGlobalPathBindings(
		[]pathMutation{{Scope: target.ScopeGlobal, Destination: destination}},
		resolver,
		nil,
	)
	if err != nil {
		t.Fatalf("capture global root provenance: %v", err)
	}
	persisted, err := bindings.persisted(target.ScopeGlobal, destination)
	if err != nil {
		t.Fatalf("persist global root provenance: %v", err)
	}

	movedRoot := filepath.Join(base, "moved-global-root")
	if err := os.Rename(selectedRoot, movedRoot); err != nil {
		t.Fatalf("move captured global root: %v", err)
	}
	if err := os.Mkdir(selectedRoot, 0o700); err != nil {
		t.Fatalf("create replacement global root: %v", err)
	}

	entry := recoveryEntry{
		Scope:             string(target.ScopeGlobal),
		Path:              destination.String(),
		GlobalPathBinding: persisted,
	}
	err = validateRecoveryGlobalPathBindings(
		t.Context(),
		[]recoveryEntry{entry},
		resolver,
		nil,
	)
	if !hasRootedPathFailureKind(err, rootedpath.FailureRootReplaced) {
		t.Fatalf("same-path replacement error = %v, want %s", err, rootedpath.FailureRootReplaced)
	}
}

func TestRecoveryGlobalPathBindingAcceptsNewSameMountDescendantRoot(t *testing.T) {
	selectedRoot := filepath.Join(t.TempDir(), "global-root")
	if err := os.Mkdir(selectedRoot, 0o700); err != nil {
		t.Fatalf("create selected global root: %v", err)
	}
	destination, err := output.Parse("~/.config/daem/server.json")
	if err != nil {
		t.Fatal(err)
	}
	resolvedPath := filepath.Join(selectedRoot, "missing-parent", "server.json")
	resolver := func(output.Destination) (string, error) { return resolvedPath, nil }
	bindings, err := captureRecoveryGlobalPathBindings(
		[]pathMutation{{Scope: target.ScopeGlobal, Destination: destination}},
		resolver,
		nil,
	)
	if err != nil {
		t.Fatalf("capture global root provenance: %v", err)
	}
	persisted, err := bindings.persisted(target.ScopeGlobal, destination)
	if err != nil {
		t.Fatalf("persist global root provenance: %v", err)
	}
	physicalRoot, err := filepath.EvalSymlinks(selectedRoot)
	if err != nil {
		t.Fatalf("resolve selected global root: %v", err)
	}
	if persisted.RootProvenance.PhysicalRoot != physicalRoot {
		t.Fatalf(
			"capture root = %q, want nearest existing ancestor %q",
			persisted.RootProvenance.PhysicalRoot,
			physicalRoot,
		)
	}
	if err := os.Mkdir(filepath.Dir(resolvedPath), 0o700); err != nil {
		t.Fatalf("create same-mount destination parent: %v", err)
	}

	entry := recoveryEntry{
		Scope:             string(target.ScopeGlobal),
		Path:              destination.String(),
		GlobalPathBinding: persisted,
	}
	if err := validateRecoveryGlobalPathBindings(
		t.Context(),
		[]recoveryEntry{entry},
		resolver,
		nil,
	); err != nil {
		t.Fatalf("new same-mount descendant root rejected: %v", err)
	}
}
