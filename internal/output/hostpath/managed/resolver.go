// Package managed composes host-specific physical-root policies into the
// generic managed-output destination resolver.
package managed

import (
	"github.com/isty2e/daem/internal/output/hostpath"
	pihostpath "github.com/isty2e/daem/internal/output/hostpath/pi"
	daempaths "github.com/isty2e/daem/internal/paths"
)

// Resolver returns the operation's managed-output destination resolver.
func Resolver(paths daempaths.Paths) hostpath.Resolver {
	return hostpath.NewResolverWithManagedDataRoot(
		paths.ManifestRoot,
		paths.DataDir,
	).WithDestinationOverride(pihostpath.DestinationOverride(paths.ManifestRoot))
}
