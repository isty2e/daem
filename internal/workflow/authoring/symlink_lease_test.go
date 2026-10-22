package authoring

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/operationplan"
)

func TestDeclarationAliasesContendAcrossStateDirectories(t *testing.T) {
	for _, sharedName := range []string{"daem.toml", "daem.lock.toml"} {
		t.Run(sharedName, func(t *testing.T) {
			root := t.TempDir()
			shared := filepath.Join(root, "shared")
			if err := os.WriteFile(shared, []byte("shared"), 0o600); err != nil {
				t.Fatal(err)
			}
			domains := make([][]mutation.Domain, 0, 2)
			for _, name := range []string{"one", "two"} {
				project := filepath.Join(root, name)
				if err := os.Mkdir(project, 0o700); err != nil {
					t.Fatal(err)
				}
				for _, document := range []string{"daem.toml", "daem.lock.toml"} {
					path := filepath.Join(project, document)
					if document == sharedName {
						if err := os.Symlink("../shared", path); err != nil {
							t.Fatal(err)
						}
					} else if err := os.WriteFile(path, []byte("other"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				program := operationplan.CompileAuthoring(operationplan.AuthoringInput{
					ManifestPath: filepath.Join(project, "daem.toml"),
					LockfilePath: filepath.Join(project, "daem.lock.toml"),
					MarkerPath:   filepath.Join(project, ".daem", "metadata-transaction"),
				})
				for _, step := range program.DomainSteps() {
					request, ok := step.Path()
					if !ok {
						continue
					}
					logical, ok := request.Logical()
					if ok && logical.Path == filepath.Join(project, sharedName) && logical.Effect == mutation.PathEffectReferent && logical.Access != mutation.AccessExclusive {
						t.Fatal("physical declaration write target is not exclusive")
					}
				}
				lowered, err := lowerAuthoringDomainSteps(program.DomainSteps())
				if err != nil {
					t.Fatal(err)
				}
				domains = append(domains, lowered)
			}
			store, err := mutation.NewStore(filepath.Join(root, "user-data"))
			if err != nil {
				t.Fatal(err)
			}
			first, err := store.Acquire(t.Context(), domains[0]...)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = first.Release() })
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			second, err := store.Acquire(ctx, domains[1]...)
			if second != nil {
				_ = second.Release()
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("second alias acquired shared physical writer: %v", err)
			}
			if err := first.Release(); err != nil {
				t.Fatal(err)
			}
			second, err = store.Acquire(t.Context(), domains[1]...)
			if err != nil {
				t.Fatalf("second alias remained blocked after release: %v", err)
			}
			if err := second.Release(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
