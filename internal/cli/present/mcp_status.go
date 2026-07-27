package clipresent

import (
	"fmt"
	"io"

	mcpobserve "github.com/isty2e/daem/internal/assurance/observe/mcp"
	"github.com/isty2e/daem/internal/assurance/runtimeprobe"
	"github.com/isty2e/daem/internal/target"
)

// MCPStatus is the presentation DTO for passive MCP status rows grouped by evidence axis.
type MCPStatus struct {
	Subject                MCPSubject           `json:"subject"`
	Target                 string               `json:"target"`
	Scope                  string               `json:"scope"`
	ConfigPath             string               `json:"config_path"`
	ContentPath            string               `json:"content_path"`
	AdapterContractVersion string               `json:"adapter_contract_version"`
	Projection             []MCPStatusDimension `json:"projection_dimensions,omitempty"`
	Host                   []MCPStatusDimension `json:"host_dimensions,omitempty"`
	Delegate               []MCPStatusDimension `json:"delegate_dimensions,omitempty"`
	Runtime                []MCPStatusDimension `json:"runtime_dimensions,omitempty"`
	Residue                []MCPStatusDimension `json:"residue_dimensions,omitempty"`
	Other                  []MCPStatusDimension `json:"other_dimensions,omitempty"`
}

// MCPSubject is the public JSON identity for a locked MCP projection subject.
type MCPSubject struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// MCPStatusDimension is one public status feature-matrix row for an MCP subject.
type MCPStatusDimension struct {
	Dimension string `json:"dimension"`
	State     string `json:"state"`
	Reason    string `json:"reason,omitempty"`
}

// MCPStatusesFrom projects canonical passive MCP evidence into the public status contract.
func MCPStatusesFrom(observations []mcpobserve.LockedProjectionObservation) ([]MCPStatus, error) {
	if len(observations) == 0 {
		return nil, nil
	}
	runtime, err := runtimeprobe.FoldFacts(nil)
	if err != nil {
		return nil, err
	}
	statuses := make([]MCPStatus, 0, len(observations))
	for _, observation := range observations {
		status, err := mcpStatusEvidence(
			observation.Scope(),
			observation.Current(),
			observation.LastDelegateAttempt(),
			runtime,
		)
		if err != nil {
			return nil, err
		}
		subject := observation.Subject()
		status.Subject = MCPSubject{
			Kind:      string(subject.Kind()),
			Namespace: subject.Namespace(),
			Name:      subject.Key(),
		}
		status.Target = string(observation.Target())
		status.Scope = string(observation.Scope())
		status.ConfigPath = observation.ConfigPath().String()
		status.ContentPath = string(observation.ContentPath())
		status.AdapterContractVersion = string(observation.AdapterContractVersion())
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func mcpStatusEvidence(
	scope target.Scope,
	current mcpobserve.AggregateProjectionObservation,
	lastDelegateAttempt mcpobserve.LastDelegateAttemptObservation,
	runtime runtimeprobe.Observation,
) (MCPStatus, error) {
	projectionDimension := ""
	switch scope {
	case target.ScopeProject:
		projectionDimension = "project_projection"
	case target.ScopeGlobal:
		projectionDimension = "global_projection"
	default:
		return MCPStatus{}, fmt.Errorf("MCP status projection has unsupported scope %q", scope)
	}
	return MCPStatus{
		Projection: []MCPStatusDimension{
			mcpStatusDimension(projectionDimension, string(current.Projection.State), string(current.Projection.Reason)),
		},
		Host: []MCPStatusDimension{
			mcpStatusDimension("same_scope_ownership", string(current.Ownership.State), string(current.Ownership.Reason)),
			mcpStatusDimension("effective_shadowing", string(current.Shadowing.State), string(current.Shadowing.Reason)),
		},
		Delegate: []MCPStatusDimension{
			mcpStatusDimension("delegate_last_attempt", string(lastDelegateAttempt.State), string(lastDelegateAttempt.Reason)),
		},
		Runtime: []MCPStatusDimension{
			runtimeStatusDimension(runtimeprobe.DimensionLauncher, runtime.Launcher()),
			runtimeStatusDimension(runtimeprobe.DimensionProtocolInitialize, runtime.ProtocolInitialize()),
			runtimeStatusDimension(runtimeprobe.DimensionAuthentication, runtime.Authentication()),
			runtimeStatusDimension(runtimeprobe.DimensionEndpointHealth, runtime.EndpointHealth()),
			runtimeStatusDimension(runtimeprobe.DimensionToolInventory, runtime.ToolInventory()),
		},
		Residue: []MCPStatusDimension{
			mcpStatusDimension("adoption_orphan_residue", string(mcpobserve.ResidueNotApplicable), ""),
		},
	}, nil
}

func runtimeStatusDimension(dimension runtimeprobe.Dimension, observation runtimeprobe.ReadinessObservation) MCPStatusDimension {
	return mcpStatusDimension(string(dimension), string(observation.State()), string(observation.Reason()))
}

func mcpStatusDimension(dimension string, state string, reason string) MCPStatusDimension {
	if state == "unknown" {
		state = "unobserved"
	}
	return MCPStatusDimension{
		Dimension: dimension,
		State:     state,
		Reason:    reason,
	}
}

// PrintMCPStatuses writes passive MCP status rows without implying runtime readiness.
func PrintMCPStatusesWithOptions(output io.Writer, statuses []MCPStatus, options HumanOptions) {
	if len(statuses) == 0 {
		return
	}
	if !options.Verbose {
		statuses = exceptionalMCPStatuses(statuses)
		if len(statuses) == 0 {
			return
		}
	}
	fmt.Fprintf(output, "mcp status: %d subjects\n", len(statuses))
	for _, status := range statuses {
		fmt.Fprintf(
			output, "  - subject=%q target=%s scope=%s",
			mcpSubjectString(status.Subject),
			status.Target,
			status.Scope,
		)
		if options.Verbose {
			fmt.Fprintf(output, " config_path=%q content_path=%q adapter_contract=%q", status.ConfigPath, status.ContentPath, status.AdapterContractVersion)
		}
		fmt.Fprintln(output)
		printMCPStatusDimensionGroup(output, "projection", selectedMCPDimensions(status.Projection, options))
		printMCPStatusDimensionGroup(output, "host", selectedMCPDimensions(status.Host, options))
		printMCPStatusDimensionGroup(output, "delegate", selectedMCPDimensions(status.Delegate, options))
		printMCPStatusDimensionGroup(output, "runtime", selectedMCPDimensions(status.Runtime, options))
		printMCPStatusDimensionGroup(output, "residue", selectedMCPDimensions(status.Residue, options))
		printMCPStatusDimensionGroup(output, "other", selectedMCPDimensions(status.Other, options))
	}
}

func exceptionalMCPStatuses(statuses []MCPStatus) []MCPStatus {
	result := make([]MCPStatus, 0, len(statuses))
	for _, status := range statuses {
		if len(selectedMCPDimensions(allMCPDimensions(status), HumanOptions{})) != 0 {
			result = append(result, status)
		}
	}
	return result
}

func allMCPDimensions(status MCPStatus) []MCPStatusDimension {
	result := make([]MCPStatusDimension, 0, len(status.Projection)+len(status.Host)+len(status.Delegate)+len(status.Runtime)+len(status.Residue)+len(status.Other))
	result = append(result, status.Projection...)
	result = append(result, status.Host...)
	result = append(result, status.Delegate...)
	result = append(result, status.Runtime...)
	result = append(result, status.Residue...)
	result = append(result, status.Other...)
	return result
}

func selectedMCPDimensions(dimensions []MCPStatusDimension, options HumanOptions) []MCPStatusDimension {
	if options.Verbose {
		return dimensions
	}
	result := make([]MCPStatusDimension, 0, len(dimensions))
	for _, dimension := range dimensions {
		switch dimension.State {
		case "projected", "managed", "current", "not_applicable", "not_probed", "observed_present":
			continue
		default:
			result = append(result, dimension)
		}
	}
	return result
}

func printMCPStatusDimensionGroup(output io.Writer, label string, dimensions []MCPStatusDimension) {
	if len(dimensions) == 0 {
		return
	}
	fmt.Fprintf(output, "    %s:\n", label)
	for _, dimension := range dimensions {
		fmt.Fprintf(output, "      %s: %s", dimension.Dimension, dimension.State)
		if dimension.Reason != "" {
			fmt.Fprintf(output, " reason=%s", dimension.Reason)
		}
		fmt.Fprintln(output)
	}
}

func mcpSubjectString(subject MCPSubject) string {
	return subject.Kind + "/" + subject.Namespace + "/" + subject.Name
}
