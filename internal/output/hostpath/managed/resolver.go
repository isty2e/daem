// Package managed composes host-specific physical-root policies into the
// generic managed-output destination resolver.
package managed

import (
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/hostpath"
	codexhostpath "github.com/isty2e/daem/internal/output/hostpath/codex"
	pihostpath "github.com/isty2e/daem/internal/output/hostpath/pi"
	daempaths "github.com/isty2e/daem/internal/paths"
)

// Resolver returns the operation's managed-output destination resolver.
func Resolver(paths daempaths.Paths, resolveRoot func(string) (string, error)) hostpath.Resolver {
	piOverride := pihostpath.DestinationOverride(paths.ManifestRoot)
	codexOverride := codexhostpath.DestinationOverride(paths.ManifestRoot, resolveRoot)
	return hostpath.NewResolverWithManagedDataRoot(
		paths.ManifestRoot,
		paths.DataDir,
	).WithDestinationOverride(func(destination output.Destination) (string, bool, error) {
		path, matched, err := piOverride(destination)
		if err != nil || matched {
			return path, matched, err
		}
		return codexOverride(destination)
	})
}
