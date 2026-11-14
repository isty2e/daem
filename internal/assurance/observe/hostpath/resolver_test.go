package physicalhostpath

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/isty2e/daem/internal/output"
	daempaths "github.com/isty2e/daem/internal/paths"
)

type resolverBudget struct {
	visits int
	err    error
}

func (budget *resolverBudget) AdmitPathComponents(count int) error {
	budget.visits += count
	return budget.err
}

func TestResolverWalksLiteralParentsBeforeAppendingTheDocument(t *testing.T) {
	for _, missing := range []bool{false, true} {
		name := "existing"
		if missing {
			name = "future"
		}
		t.Run(name, func(t *testing.T) {
			base, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			inner := filepath.Join(base, "physical", "inner")
			if err := os.MkdirAll(inner, 0o700); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(base, "link")
			if err := os.Symlink(inner, link); err != nil {
				t.Skipf("directory symlinks are unavailable: %v", err)
			}
			selected := link + string(filepath.Separator) + ".." + string(filepath.Separator) + "home"
			expected := filepath.Join(base, "physical", "home")
			if runtime.GOOS == "windows" {
				expected = filepath.Join(base, "home")
			}
			if !missing {
				if err := os.Mkdir(expected, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("CODEX_HOME", selected)
			resolver := NewResolver(daempaths.Paths{ManifestRoot: base})
			destination, err := output.Parse("@codex-home/config.toml")
			if err != nil {
				t.Fatal(err)
			}
			budget := &resolverBudget{}
			path, err := resolver.Resolve(destination, budget)
			if err != nil || path != filepath.Join(expected, "config.toml") || budget.visits == 0 {
				t.Fatal("literal selector was cleaned or escaped its allocation", path, budget.visits, err)
			}
			if missing {
				if _, err := os.Stat(expected); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("resolution created a prospective root", err)
				}
			}
			limit := errors.New("fixture path allocation exhausted")
			if _, err := resolver.Resolve(destination, &resolverBudget{err: limit}); !errors.Is(err, limit) {
				t.Fatal("root resolution bypassed the supplied allocation", err)
			}
		})
	}
}

func TestResolverKeepsOrdinarySymlinkSpellingForRecordedBindings(t *testing.T) {
	base := t.TempDir()
	physical := filepath.Join(base, "physical")
	if err := os.Mkdir(physical, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(physical, alias); err != nil {
		t.Skipf("directory symlinks are unavailable: %v", err)
	}
	t.Setenv("CODEX_HOME", alias)
	destination, err := output.Parse("@codex-home/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	budget := &resolverBudget{err: errors.New("ordinary spelling must not add a selector walk")}
	path, err := NewResolver(daempaths.Paths{ManifestRoot: base}).Resolve(destination, budget)
	if err != nil || path != filepath.Join(alias, "config.toml") || budget.visits != 0 {
		t.Fatal("ordinary recorded spelling or traversal accounting changed", path, budget.visits, err)
	}
}
