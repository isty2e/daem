package nativeconfig

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/target"
)

func TestOpaqueNamespacesRequirePhysicalDisjointness(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	native := filepath.Join(root, "native")
	if err := os.Mkdir(native, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(native, "config.toml")
	if err := os.WriteFile(file, []byte("model='retained'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	budget := physicalwork.NewCaptureBudget(nil)
	observed, err := mutation.ObserveDirectoryEntryAuthorityBounded(file, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "host-alias")
	if err := os.Symlink(native, alias); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, path string
		overlap    bool
	}{
		{"sibling", filepath.Join(root, "host"), false},
		{"ancestor", root, true},
		{"same directory", native, true},
		{"symlinked directory", alias, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			path, err := relationobserve.NewAuthorityPath(test.path, target.TargetClaudeCode, target.ScopeGlobal)
			if err != nil {
				t.Fatal(err)
			}
			err = RequireDisjointOpaqueNamespaces(t.Context(), []mutation.DirectoryEntryAuthorityObservation{observed}, []relationobserve.AuthorityPath{path}, physicalwork.NewCaptureBudget(nil))
			if test.overlap != errors.Is(err, ErrOpaqueNamespaceOverlap) || (!test.overlap && err != nil) {
				t.Fatal("incorrect physical namespace admission", err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := RequireDisjointOpaqueNamespaces(ctx, nil, nil, budget); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled namespace check succeeded", err)
	}
	if err := RequireDisjointOpaqueNamespaces(t.Context(), []mutation.DirectoryEntryAuthorityObservation{{}}, nil, budget); err == nil {
		t.Fatal("zero namespace became evidence of disjointness")
	}
}
