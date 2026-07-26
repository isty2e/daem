package status

import (
	"github.com/isty2e/daem/internal/output/hostpath"
	daempaths "github.com/isty2e/daem/internal/paths"
)

func destinationResolver(paths daempaths.Paths) hostpath.Resolver {
	return hostpath.NewResolverWithManagedDataRoot(paths.ManifestRoot, paths.DataDir)
}
