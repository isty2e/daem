package status

import (
	"github.com/isty2e/daem/internal/output/hostpath"
	managedhostpath "github.com/isty2e/daem/internal/output/hostpath/managed"
	daempaths "github.com/isty2e/daem/internal/paths"
)

func destinationResolver(paths daempaths.Paths) hostpath.Resolver {
	return managedhostpath.Resolver(paths)
}
