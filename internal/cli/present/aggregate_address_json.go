package clipresent

import (
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func aggregateJSONAddress(subject topology.SubjectID, address aggregate.ProjectionAddress) (contentPath string, keyPath []string, err error) {
	if err := address.Validate(); err != nil {
		return "", nil, err
	}
	if _, _, native := topologyprojection.HostConfigKeyIdentity(subject); native {
		return "", address.KeyPath().Segments(), nil
	}

	legacy, err := address.LegacyContentPath()
	return string(legacy), nil, err
}
