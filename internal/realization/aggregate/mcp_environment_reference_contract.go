package aggregate

import "fmt"

// MCPEnvReferenceMapping identifies which child-to-source environment mappings
// one MCP placement can represent without changing launch semantics.
type MCPEnvReferenceMapping string

const (
	MCPEnvMappingUnsupported MCPEnvReferenceMapping = "unsupported"
	MCPEnvMappingSameName    MCPEnvReferenceMapping = "same_name"
	MCPEnvMappingAliased     MCPEnvReferenceMapping = "aliased"
)

// MCPEnvReferenceResolution identifies when the selected host resolves a
// symbolic environment reference.
type MCPEnvReferenceResolution string

const (
	MCPEnvResolutionUnavailable MCPEnvReferenceResolution = "unavailable"
	MCPEnvResolutionHostRuntime MCPEnvReferenceResolution = "host_runtime"
)

// MCPEnvReferenceContract is the exact environment-reference capability of one
// MCP placement. Native field syntax remains private to the selected codec.
type MCPEnvReferenceContract struct {
	mapping    MCPEnvReferenceMapping
	resolution MCPEnvReferenceResolution
}

// MCPEnvReferenceAdmissionError reports that one placement cannot represent a
// canonical child/source environment-reference pair.
type MCPEnvReferenceAdmissionError struct {
	placementID MCPPlacementID
	mapping     MCPEnvReferenceMapping
	childName   string
	sourceName  string
}

func (failure MCPEnvReferenceAdmissionError) Error() string {
	switch failure.mapping {
	case MCPEnvMappingUnsupported:
		return fmt.Sprintf("MCP placement %q does not support environment references", failure.placementID)
	case MCPEnvMappingSameName:
		return fmt.Sprintf(
			"MCP placement %q supports only same-name environment references; child %q selects source %q",
			failure.placementID,
			failure.childName,
			failure.sourceName,
		)
	default:
		return fmt.Sprintf(
			"MCP placement %q rejected environment reference child %q source %q for mapping %q",
			failure.placementID,
			failure.childName,
			failure.sourceName,
			failure.mapping,
		)
	}
}

// PlacementID returns the rejecting placement identity.
func (failure MCPEnvReferenceAdmissionError) PlacementID() MCPPlacementID {
	return failure.placementID
}

// Mapping returns the mapping capability that rejected the reference.
func (failure MCPEnvReferenceAdmissionError) Mapping() MCPEnvReferenceMapping {
	return failure.mapping
}

// NewMCPEnvReferenceContract constructs a validated placement capability.
func NewMCPEnvReferenceContract(
	mapping MCPEnvReferenceMapping,
	resolution MCPEnvReferenceResolution,
) (MCPEnvReferenceContract, error) {
	contract := MCPEnvReferenceContract{
		mapping:    mapping,
		resolution: resolution,
	}
	if err := contract.Validate(); err != nil {
		return MCPEnvReferenceContract{}, err
	}
	return contract, nil
}

// Mapping returns the admitted child-to-source mapping capability.
func (contract MCPEnvReferenceContract) Mapping() MCPEnvReferenceMapping {
	return contract.mapping
}

// Resolution returns when the host resolves admitted symbolic references.
func (contract MCPEnvReferenceContract) Resolution() MCPEnvReferenceResolution {
	return contract.resolution
}

// Supported reports whether the placement admits any environment reference.
func (contract MCPEnvReferenceContract) Supported() bool {
	return contract.mapping == MCPEnvMappingSameName ||
		contract.mapping == MCPEnvMappingAliased
}

// AdmitsReference reports whether one canonical child/source pair is
// representable. Desired owns environment-name syntax; this contract owns only
// placement-level mapping semantics.
func (contract MCPEnvReferenceContract) AdmitsReference(childName string, sourceName string) bool {
	switch contract.mapping {
	case MCPEnvMappingSameName:
		return childName == sourceName
	case MCPEnvMappingAliased:
		return true
	default:
		return false
	}
}

// Validate rejects unsupported mapping/resolution cross-products.
func (contract MCPEnvReferenceContract) Validate() error {
	switch contract.mapping {
	case MCPEnvMappingUnsupported:
		if contract.resolution != MCPEnvResolutionUnavailable {
			return fmt.Errorf(
				"unsupported MCP environment references require unavailable resolution, got %q",
				contract.resolution,
			)
		}
	case MCPEnvMappingSameName, MCPEnvMappingAliased:
		if contract.resolution != MCPEnvResolutionHostRuntime {
			return fmt.Errorf(
				"MCP environment mapping %q requires host-runtime resolution, got %q",
				contract.mapping,
				contract.resolution,
			)
		}
	default:
		return fmt.Errorf("unsupported MCP environment-reference mapping %q", contract.mapping)
	}
	return nil
}
