package namespace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestRemovedDocumentRulesDoNotIgnoreNewRoleAlias(t *testing.T) {
	base := t.TempDir()
	manifest := nativeNamespaceRoot(t, base)
	roots := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: manifest})
	pi := filepath.Join(base, "pi-global")
	protection := protectionRoots(t, pi, filepath.Join(base, "codex-global"))
	address := nativeNamespaceAddress(t, target.TargetCodex, target.ScopeProject, ".codex/Café.toml")
	child := filepath.Join(base, ".codex")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	const file = "Café.toml"
	path := filepath.Join(child, file)
	if err := os.WriteFile(path, []byte("model='original'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(pi, 0o700); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(child, "other.json")
	if err := os.WriteFile(other, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	role := filepath.Join(pi, "settings.json")
	if err := os.Symlink(other, role); err != nil {
		t.Fatal(err)
	}
	observe := func() DocumentNamespace {
		t.Helper()
		value, err := Observe(t.Context(), []aggregate.DocumentAddress{address}, roots, protection, journalTestFilesystem(), imageBudget(t))
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := observe()
	if err := before.RequireCompletePhysicalRules(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	missing := observe()
	if err := missing.RequirePhysicalRules(before); !errors.Is(err, ErrPhysicalRulesChanged) {
		t.Fatal("ordinary rule comparison discarded the uncertainty of the new absence", err)
	}
	if err := missing.RequireRemovedDocumentRules(before); err != nil {
		t.Fatal("a disjoint role endpoint blocked the recorded removal", err)
	}

	if err := os.Remove(role); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, role); err != nil {
		t.Fatal(err)
	}
	if err := observe().RequireRemovedDocumentRules(before); !errors.Is(err, ErrPhysicalRulesChanged) {
		t.Fatal("removed-file comparison missed a role endpoint newly aliasing the absent file", err)
	}
}
