package profile

import (
	"path"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/target"
)

func TestResolveManagedFileRelativePathPreservesInstructionSelection(t *testing.T) {
	t.Parallel()

	for _, selectedTarget := range target.SupportedTargets() {
		owner := Profile(selectedTarget)
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			defaultPlacement, err := owner.DefaultPlacement(entity.KindInstructions, scope)
			if err != nil {
				continue
			}
			for _, placement := range owner.Placements(entity.KindInstructions, scope) {
				relativePath := placement.Root().String()
				if scope == target.ScopeGlobal {
					prefix := path.Dir(defaultPlacement.Root().String()) + "/"
					if !strings.HasPrefix(relativePath, prefix) {
						continue
					}
					relativePath = strings.TrimPrefix(relativePath, prefix)
				}
				destination, err := ResolveManagedFileRelativePath(
					scope,
					defaultPlacement.Root(),
					relativePath,
				)
				if err != nil {
					t.Fatalf("%s/%s/%q resolve error: %v", selectedTarget, scope, relativePath, err)
				}
				legacy, err := ManagedFilePlacementForRelativePath(
					entity.KindInstructions,
					selectedTarget,
					scope,
					relativePath,
				)
				if err != nil {
					t.Fatalf("%s/%s/%q legacy selection error: %v", selectedTarget, scope, relativePath, err)
				}
				if destination != placement.Root() || legacy.Root() != placement.Root() {
					t.Fatalf(
						"%s/%s/%q destination = %q, legacy = %q, want %q",
						selectedTarget,
						scope,
						relativePath,
						destination,
						legacy.Root(),
						placement.Root(),
					)
				}
			}
		}
	}
}
