package namespace

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
)

type CapabilityResolver func(output.Destination, rootedpath.PhysicalTraversalBudget) (rootedpath.CommitCapability, bool, error)

func AcquireMatchingCapability(destination output.Destination, resolvedPath string, acquire CapabilityResolver, budget rootedpath.PhysicalTraversalBudget) (rootedpath.CommitCapability, bool, error) {
	if acquire == nil {
		return nil, false, nil
	}
	capability, present, err := acquire(destination, budget)
	if err != nil {
		if capability != nil {
			err = errors.Join(err, capability.Close())
		}
		return nil, false, err
	}
	if !present {
		if capability == nil {
			return nil, false, nil
		}
		return nil, false, errors.Join(
			fmt.Errorf("destination %q returned retained root authority while reporting it absent", destination),
			capability.Close(),
		)
	}
	if capability == nil {
		return nil, false, fmt.Errorf("destination %q returned nil retained root authority", destination)
	}
	capabilityPath, err := capability.Destination().LexicalPath()
	if err != nil {
		return nil, false, errors.Join(
			fmt.Errorf("read destination %q retained authority path: %w", destination, err),
			capability.Close(),
		)
	}
	if filepath.Clean(capabilityPath) != filepath.Clean(resolvedPath) {
		return nil, false, errors.Join(
			fmt.Errorf(
				"destination %q resolver path %q does not match retained authority path %q",
				destination,
				resolvedPath,
				capabilityPath,
			),
			capability.Close(),
		)
	}
	return capability, true, nil
}
