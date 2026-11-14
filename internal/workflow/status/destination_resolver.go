package status

import (
	physicalhostpath "github.com/isty2e/daem/internal/assurance/observe/hostpath"
	daempaths "github.com/isty2e/daem/internal/paths"
)

func destinationResolver(paths daempaths.Paths) physicalhostpath.Resolver {
	return physicalhostpath.NewResolver(paths)
}
