//go:build darwin || linux

package operationplan

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
)

func TestBuilderDoesNotCanonicalizePathFacts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	loop := filepath.Join(root, "loop")
	if err := os.Symlink("loop", loop); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(loop, "state.json")
	builder := NewBuilder(RevisionsFirstEffect, nil, 0)
	if err := builder.AddLogical(
		path,
		mutation.AccessExclusive,
		mutation.PathEffectDirectoryEntry,
	); err != nil {
		t.Fatalf("pure builder observed path state: %v", err)
	}
	steps := builder.Compile().DomainSteps()
	if len(steps) != 1 {
		t.Fatalf("domain steps = %d, want 1", len(steps))
	}
	request, ok := steps[0].Path()
	if !ok {
		t.Fatal("compiled path fact became a precompiled domain")
	}
	logical, ok := request.Logical()
	if !ok || logical.Path != path {
		t.Fatalf("logical request = %#v, want path %q", request, path)
	}
}
