package refine

import (
	"fmt"
	"sort"
	"strings"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/delegate"
	mcpdelegate "github.com/isty2e/daem/internal/realization/delegate/mcp"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
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
	extensions []desiredextension.Extension,
	encoder MCPContributionEncoder,
) ([]lock.LockedSubjectContract, error) {
	if len(servers) == 0 {
		return nil, nil
	}
	if encoder == nil {
		return nil, fmt.Errorf("MCP contribution encoder is required")
	}

	providerCandidates, err := mcpProviderContributions(extensions)
	if err != nil {
		return nil, err
	}
	providerSelections, selectedProviders, err := selectMCPProviders(servers, providerCandidates)
	if err != nil {
		return nil, err
	}
	graph, err := topologymcp.ServersWithProviderSelections(servers, providerSelections)
	if err != nil {
		return nil, err
	}

	contracts := make([]lock.LockedSubjectContract, 0, len(servers))
	for index, server := range servers {
		for bindingIndex, binding := range server.Bindings() {
			projection, err := topologymcp.ProjectionSubject(
				binding.Target(),
				binding.Scope(),
				server.ID().Name(),
			)
			if err != nil {
				return nil, err
			}
			contract, err := mcpLockedSubjectContract(
				graph,
				server,
				binding,
				selectedProviders[projection],
				encoder,
			)
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
	return mcpLockedSubjectContract(graph, server, binding, nil, encoder)
}

func mcpLockedSubjectContract(
	graph topology.Graph,
	server desiredmcp.Server,
	binding desiredmcp.Binding,
	providerContribution *extensiontopology.ContributionReference,
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
		ProviderContribution: providerContribution,
	})
}

func mcpProviderContributions(
	extensions []desiredextension.Extension,
) ([]extensiontopology.Contribution, error) {
	contributions := make([]extensiontopology.Contribution, 0)
	for index, value := range extensions {
		if err := value.Validate(); err != nil {
			return nil, fmt.Errorf("extension[%d] MCP provider: %w", index, err)
		}
		carrier, err := extensiontopology.NewCarrier(value.CarrierKey())
		if err != nil {
			return nil, fmt.Errorf("extension[%d] MCP provider: %w", index, err)
		}
		contribution, admitted, err := profile.MCPProviderContributionForTarget(
			value.Target(),
			carrier,
		)
		if err != nil {
			return nil, fmt.Errorf("extension[%d] MCP provider: %w", index, err)
		}
		if admitted {
			contributions = append(contributions, contribution)
		}
	}
	return contributions, nil
}

func selectMCPProviders(
	servers []desiredmcp.Server,
	candidates []extensiontopology.Contribution,
) (
	map[topology.SubjectID]topologymcp.ProviderSelection,
	map[topology.SubjectID]*extensiontopology.ContributionReference,
	error,
) {
	selections := make(map[topology.SubjectID]topologymcp.ProviderSelection)
	references := make(map[topology.SubjectID]*extensiontopology.ContributionReference)
	for serverIndex, server := range servers {
		for bindingIndex, binding := range server.Bindings() {
			placement, err := aggregate.MCPPlacementForBinding(binding)
			if err != nil {
				return nil, nil, err
			}
			required, err := lock.MCPPlacementRequiresProviderContribution(placement.ID())
			if err != nil {
				return nil, nil, err
			}
			if !required {
				continue
			}
			selected, err := profile.SelectMCPProviderContribution(
				binding.Target(),
				binding.Scope(),
				candidates,
			)
			if err != nil {
				return nil, nil, fmt.Errorf(
					"mcp_server[%d].binding[%d]: %w",
					serverIndex,
					bindingIndex,
					err,
				)
			}
			projection, err := topologymcp.ProjectionSubject(
				binding.Target(),
				binding.Scope(),
				server.ID().Name(),
			)
			if err != nil {
				return nil, nil, err
			}
			selection, err := topologymcp.NewProviderSelection(
				selected.Provider().SubjectID(),
				selected.SubjectID(),
			)
			if err != nil {
				return nil, nil, err
			}
			reference := selected.Reference()
			selections[projection] = selection
			references[projection] = &reference
		}
	}
	return selections, references, nil
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
