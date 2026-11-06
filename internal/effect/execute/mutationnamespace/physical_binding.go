package mutationnamespace

import (
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

func (namespace Namespace) BindPhysicalAuthority(scope target.Scope, destination output.Destination, consumers []target.Target) ([]mutation.PhysicalAuthorityRequest, error) {
	if err := namespace.Bind(scope, destination); err != nil {
		return nil, err
	}
	bound, err := namespace.Resolve(scope, destination)
	if err != nil {
		return nil, err
	}

	var requests []mutation.PhysicalAuthorityRequest
	for _, consumer := range consumers {
		if consumer == "" {
			continue
		}
		request := mutation.PhysicalAuthorityRequest{Path: bound.HostPath(), Target: string(consumer), Scope: string(scope)}
		if _, err := mutation.NewPhysicalAuthoritySet(request); err != nil {
			return requests, err
		}
		requests = append(requests, request)
	}
	return requests, nil
}
