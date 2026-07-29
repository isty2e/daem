// Package host dispatches provider-effective MCP observation to the private
// host adapter selected by each locked projection.
package host

import (
	"fmt"
	"os"

	mcpeffective "github.com/isty2e/daem/internal/assurance/observe/mcp/effective"
	"github.com/isty2e/daem/internal/output"
	pihostpath "github.com/isty2e/daem/internal/output/hostpath/pi"
	"github.com/isty2e/daem/internal/realization/aggregate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/topology"
)

// Input contains selected locked MCP projections and operation-local path
// facts. Direct-host projections without a provider contribution are ignored.
type Input struct {
	Contracts          []lock.LockedSubjectContract
	WorkDir            string
	ResolveDestination func(output.Destination) (string, error)
}

// Observe dispatches each provider-mediated projection to its admitted host
// observer without exposing host identities to the generic readiness workflow.
func Observe(input Input) ([]mcpeffective.Observation, error) {
	if input.ResolveDestination == nil {
		return nil, fmt.Errorf("provider-effective MCP destination resolver is required")
	}
	seen := make(map[topology.SubjectID]struct{}, len(input.Contracts))
	result := make([]mcpeffective.Observation, 0)
	var (
		piContext    piObservationContext
		piContextErr error
		piResolved   bool
	)
	for _, contract := range input.Contracts {
		subject := contract.SubjectID()
		if _, duplicate := seen[subject]; duplicate {
			return nil, fmt.Errorf(
				"duplicate provider-effective MCP contract for %q",
				subject,
			)
		}
		seen[subject] = struct{}{}

		provider, providerMediated := contract.MCPProviderContribution()
		if !providerMediated {
			continue
		}
		placement, ok := aggregate.MCPPlacementForSubject(subject)
		if !ok {
			return nil, fmt.Errorf(
				"provider-mediated subject %q is not an MCP projection",
				subject,
			)
		}
		switch placement.ID() {
		case aggregate.MCPPlacementPiProject, aggregate.MCPPlacementPiGlobal:
			if provider.Kind() != "mcp-client" || provider.Key() != "default" {
				return nil, fmt.Errorf(
					"Pi MCP subject %q has unsupported provider contribution %q/%q",
					subject,
					provider.Kind(),
					provider.Key(),
				)
			}
			if !piResolved {
				piContext, piContextErr = resolvePiObservationContext(input.WorkDir)
				piResolved = true
			}
			if piContextErr != nil {
				return nil, piContextErr
			}
			contribution, present, err := contract.ManagedAggregateContribution()
			if err != nil {
				return nil, err
			}
			if !present {
				return nil, fmt.Errorf(
					"Pi MCP subject %q has no managed aggregate contribution",
					subject,
				)
			}
			selectedPath, err := input.ResolveDestination(
				contribution.Contribution().AggregateRoot(),
			)
			if err != nil {
				return nil, fmt.Errorf(
					"resolve Pi MCP selected path for %q: %w",
					subject,
					err,
				)
			}
			observation, err := ObservePiAdapter(PiAdapterInput{
				Contract:     contract,
				HomeDir:      piContext.homeDir,
				WorkDir:      input.WorkDir,
				AgentRoot:    piContext.agentRoot,
				SelectedPath: selectedPath,
			})
			if err != nil {
				return nil, fmt.Errorf(
					"observe Pi MCP provider-effective state for %q: %w",
					subject,
					err,
				)
			}
			result = append(result, observation)
		default:
			return nil, fmt.Errorf(
				"provider-mediated MCP placement %q has no effective-state observer",
				placement.ID(),
			)
		}
	}
	return result, nil
}

type piObservationContext struct {
	homeDir   string
	agentRoot string
}

func resolvePiObservationContext(workDir string) (piObservationContext, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return piObservationContext{}, fmt.Errorf(
			"resolve home for Pi MCP effective observation: %w",
			err,
		)
	}
	agentRoot, err := pihostpath.ResolveAgentRoot(pihostpath.AgentRootInput{
		WorkDir: workDir,
	})
	if err != nil {
		return piObservationContext{}, fmt.Errorf(
			"resolve Pi agent root for MCP effective observation: %w",
			err,
		)
	}
	return piObservationContext{homeDir: homeDir, agentRoot: agentRoot}, nil
}
