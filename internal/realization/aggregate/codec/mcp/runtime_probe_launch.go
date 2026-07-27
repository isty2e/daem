package mcpcodec

import (
	"fmt"
	"maps"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
)

type runtimeProbeLaunchDecoder struct {
	placementID aggregate.MCPPlacementID
	decode      func(string) (string, []string, map[string]string, error)
}

var runtimeProbeLaunchDecoderCatalog = []runtimeProbeLaunchDecoder{
	{
		placementID: aggregate.MCPPlacementClaudeProject,
		decode:      claudeProjectMCPRuntimeProbeLaunch,
	},
	{
		placementID: aggregate.MCPPlacementOpenCodeProject,
		decode:      openCodeProjectMCPRuntimeProbeLaunch,
	},
}

// DecodeRuntimeProbeLaunch lowers one canonical host entry into secret-free
// executable launch facts. Product probe admission belongs to the target
// profile and must be checked before calling this syntax operation.
func DecodeRuntimeProbeLaunch(
	placementID aggregate.MCPPlacementID,
	canonicalProjection string,
) (string, []string, map[string]string, error) {
	for _, decoder := range runtimeProbeLaunchDecoderCatalog {
		if decoder.placementID != placementID {
			continue
		}
		command, args, env, err := decoder.decode(canonicalProjection)
		if err != nil {
			return "", nil, nil, err
		}
		clonedEnv := make(map[string]string, len(env))
		maps.Copy(clonedEnv, env)
		return command, append([]string(nil), args...), clonedEnv, nil
	}
	return "", nil, nil, fmt.Errorf(
		"MCP placement %q does not support runtime probes",
		placementID,
	)
}

func validateRuntimeProbeLaunchDecoderCatalog(
	decoders []runtimeProbeLaunchDecoder,
	capabilities []profile.MCPRuntimeProbeCapability,
) error {
	capabilityIDs := make(map[aggregate.MCPPlacementID]struct{}, len(capabilities))
	for _, capability := range capabilities {
		if err := capability.Validate(); err != nil {
			return err
		}
		capabilityIDs[capability.Placement().ID()] = struct{}{}
	}

	decoderIDs := make(map[aggregate.MCPPlacementID]struct{}, len(decoders))
	for _, decoder := range decoders {
		if _, ok := aggregate.MCPPlacementForID(decoder.placementID); !ok {
			return fmt.Errorf(
				"MCP runtime-probe launch decoder placement %q is not implemented",
				decoder.placementID,
			)
		}
		if decoder.decode == nil {
			return fmt.Errorf(
				"MCP runtime-probe launch decoder %q is nil",
				decoder.placementID,
			)
		}
		if _, duplicate := decoderIDs[decoder.placementID]; duplicate {
			return fmt.Errorf(
				"MCP runtime-probe launch decoders share placement %q",
				decoder.placementID,
			)
		}
		if _, admitted := capabilityIDs[decoder.placementID]; !admitted {
			return fmt.Errorf(
				"MCP runtime-probe launch decoder %q has no profile capability",
				decoder.placementID,
			)
		}
		decoderIDs[decoder.placementID] = struct{}{}
	}
	for placementID := range capabilityIDs {
		if _, implemented := decoderIDs[placementID]; !implemented {
			return fmt.Errorf(
				"MCP runtime-probe capability %q has no launch decoder",
				placementID,
			)
		}
	}
	return nil
}

func init() {
	if err := validateRuntimeProbeLaunchDecoderCatalog(
		runtimeProbeLaunchDecoderCatalog,
		profile.MCPRuntimeProbeCapabilities(),
	); err != nil {
		panic(err)
	}
}
