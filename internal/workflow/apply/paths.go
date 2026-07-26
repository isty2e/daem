package apply

import (
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/output/hostpath"
	daempaths "github.com/isty2e/daem/internal/paths"
)

func destinationResolver(paths daempaths.Paths) hostpath.Resolver {
	return hostpath.NewResolverWithManagedDataRoot(paths.ManifestRoot, paths.DataDir)
}

func executePaths(paths daempaths.Paths, statefilePath string) execute.Paths {
	return execute.Paths{
		RecoveryDir:           paths.RecoveryDir,
		StateDir:              paths.StateDir,
		StatefilePath:         statefilePath,
		ManifestRoot:          paths.ManifestRoot,
		DataDir:               paths.DataDir,
		OwnershipRegistryPath: paths.OwnershipRegistryPath,
	}
}
