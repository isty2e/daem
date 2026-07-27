package refine

import (
	"fmt"
	"sort"
	"strings"

	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/delegate"
	mcpdelegate "github.com/isty2e/daem/internal/realization/delegate/mcp"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

// MCPContributionEncoder renders one binding through its selected native placement.
type MCPContributionEncoder func(
	desiredmcp.Server,
	desiredmcp.Binding,
	aggregate.MCPPlacement,
) ([]byte, error)

// MCPSubjects refines MCP server intent into canonical locked subjects.
func MCPSubjects(
	servers []desiredmcp.Server,
	encoder MCPContributionEncoder,
) ([]lock.LockedSubjectContract, error) {
	if len(servers) == 0 {
		return nil, nil
	}
	if encoder == nil {
		return nil, fmt.Errorf("MCP contribution encoder is required")
	}

	graph, err := topologymcp.Servers(servers)
	if err != nil {
		return nil, err
	}

	contracts := make([]lock.LockedSubjectContract, 0, len(servers))
	for index, server := range servers {
		for bindingIndex, binding := range server.Bindings() {
			contract, err := mcpLockedSubjectContract(graph, server, binding, encoder)
			if err != nil {
				return nil, fmt.Errorf("mcp_server[%d].binding[%d]: %w", index, bindingIndex, err)
			}
			contracts = append(contracts, contract)
		}
	}
	sortLockedSubjectContracts(contracts)
	return contracts, nil
}

// MCPBindingSubject refines one selected binding into a canonical locked subject.
func MCPBindingSubject(
	server desiredmcp.Server,
	binding desiredmcp.Binding,
	encoder MCPContributionEncoder,
) (lock.LockedSubjectContract, error) {
	if encoder == nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("MCP contribution encoder is required")
	}
	graph, err := topologymcp.Binding(server, binding)
	if err != nil {
		return lock.LockedSubjectContract{}, err
	}
	return mcpLockedSubjectContract(graph, server, binding, encoder)
}

func mcpLockedSubjectContract(
	graph topology.Graph,
	server desiredmcp.Server,
	binding desiredmcp.Binding,
	encoder MCPContributionEncoder,
) (lock.LockedSubjectContract, error) {
	placement, err := aggregate.MCPPlacementForBinding(binding)
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf(
			"unsupported MCP projection target/scope %s/%s: %w",
			binding.Target(),
			binding.Scope(),
			err,
		)
	}
	stdio, ok := binding.Transport().Stdio()
	if !ok {
		return lock.LockedSubjectContract{}, fmt.Errorf("unsupported MCP transport %q", binding.Transport().Kind())
	}
	canonical, err := encoder(server, binding, placement)
	if err != nil {
		return lock.LockedSubjectContract{}, err
	}
	credentialReferences, err := mcpCredentialReferences(stdio.Env())
	if err != nil {
		return lock.LockedSubjectContract{}, err
	}
	delegatePlan, hasDelegatePlan, err := mcpdelegate.MCPBindingDelegatePlanIfSupported(server, binding)
	if err != nil {
		return lock.LockedSubjectContract{}, err
	}
	var lockedDelegatePlan *delegate.DelegatePlan
	if hasDelegatePlan {
		lockedDelegatePlan = &delegatePlan
	}

	return lock.NewMCPProjectionSubjectContract(lock.MCPProjectionSubjectInput{
		Graph:                graph,
		EntityID:             server.ID(),
		PlacementID:          placement.ID(),
		ServerID:             server.ID().Name(),
		RequestedOnAbsent:    binding.OnAbsent(),
		LauncherCommand:      stdio.Command().Executable(),
		LauncherArgs:         stdio.Args(),
		CanonicalProjection:  string(canonical),
		DelegatePlan:         lockedDelegatePlan,
		CredentialReferences: credentialReferences,
	})
}

func mcpCredentialReferences(values map[string]desiredmcp.EnvReference) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	references := make([]string, 0, len(values))
	for name, value := range values {
		reference := strings.TrimSpace(value.FromEnv())
		if reference == "" {
			return nil, fmt.Errorf("env.%s.from_env: required", name)
		}
		if _, ok := seen[reference]; ok {
			continue
		}
		seen[reference] = struct{}{}
		references = append(references, reference)
	}
	sort.Strings(references)
	return references, nil
}

func sortLockedSubjectContracts(contracts []lock.LockedSubjectContract) {
	sort.SliceStable(contracts, func(left int, right int) bool {
		return contracts[left].CompareIdentity(contracts[right]) < 0
	})
}
