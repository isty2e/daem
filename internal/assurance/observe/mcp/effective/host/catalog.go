// Package host dispatches provider-effective MCP observation to the private
// host adapter selected by each locked projection.
package host

import (
	"fmt"
	"os"
	"sort"

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
	Retiring           []aggregate.SubjectContribution
	Codecs             aggregate.CodecCatalog
	WorkDir            string
	ResolveDestination func(output.Destination) (string, error)
}

// ObservationSet separates current desired projections, which may constrain
// writes, from retiring managed projections, which are diagnostic only.
type ObservationSet struct {
	Current  []mcpeffective.Observation
	Retiring []mcpeffective.Observation
}

// Observe dispatches current and retiring provider-mediated projections to
// their admitted host observer without exposing host identities to readiness.
func Observe(input Input) (ObservationSet, error) {
	if input.ResolveDestination == nil {
		return ObservationSet{}, fmt.Errorf("provider-effective MCP destination resolver is required")
	}
	seen := make(map[topology.SubjectID]struct{}, len(input.Contracts)+len(input.Retiring))
	var (
		piContext    piObservationContext
		piContextErr error
		piResolved   bool
	)
	observeProjection := func(
		projection aggregate.SubjectContribution,
	) (mcpeffective.Observation, error) {
		subject := projection.SubjectID()
		if _, duplicate := seen[subject]; duplicate {
			return mcpeffective.Observation{}, fmt.Errorf(
				"duplicate provider-effective MCP projection for %q",
				subject,
			)
		}
		seen[subject] = struct{}{}
		placement, ok := aggregate.MCPPlacementForSubject(subject)
		if !ok {
			return mcpeffective.Observation{}, fmt.Errorf(
				"provider-mediated subject %q is not an MCP projection",
				subject,
			)
		}
		switch placement.ID() {
		case aggregate.MCPPlacementPiProject, aggregate.MCPPlacementPiGlobal:
			if !piResolved {
				piContext, piContextErr = resolvePiObservationContext(input.WorkDir)
				piResolved = true
			}
			if piContextErr != nil {
				return mcpeffective.Observation{}, piContextErr
			}
			selectedPath, err := input.ResolveDestination(
				projection.Contribution().AggregateRoot(),
			)
			if err != nil {
				return mcpeffective.Observation{}, fmt.Errorf(
					"resolve Pi MCP selected path for %q: %w",
					subject,
					err,
				)
			}
			observation, err := ObservePiAdapter(PiAdapterInput{
				Projection:   projection,
				Codecs:       input.Codecs,
				HomeDir:      piContext.homeDir,
				WorkDir:      input.WorkDir,
				AgentRoot:    piContext.agentRoot,
				SelectedPath: selectedPath,
			})
			if err != nil {
				return mcpeffective.Observation{}, fmt.Errorf(
					"observe Pi MCP provider-effective state for %q: %w",
					subject,
					err,
				)
			}
			return observation, nil
		default:
			return mcpeffective.Observation{}, fmt.Errorf(
				"provider-mediated MCP placement %q has no effective-state observer",
				placement.ID(),
			)
		}
	}

	result := ObservationSet{
		Current:  make([]mcpeffective.Observation, 0),
		Retiring: make([]mcpeffective.Observation, 0, len(input.Retiring)),
	}
	for _, contract := range input.Contracts {
		subject := contract.SubjectID()
		provider, providerMediated := contract.MCPProviderContribution()
		if !providerMediated {
			continue
		}
		if provider.Kind() != "mcp-client" || provider.Key() != "default" {
			return ObservationSet{}, fmt.Errorf(
				"provider-mediated MCP subject %q has unsupported contribution %q/%q",
				subject,
				provider.Kind(),
				provider.Key(),
			)
		}
		projection, present, err := contract.ManagedAggregateContribution()
		if err != nil {
			return ObservationSet{}, err
		}
		if !present {
			return ObservationSet{}, fmt.Errorf(
				"provider-mediated MCP subject %q has no managed aggregate contribution",
				subject,
			)
		}
		observation, err := observeProjection(projection)
		if err != nil {
			return ObservationSet{}, err
		}
		result.Current = append(result.Current, observation)
	}
	for _, projection := range input.Retiring {
		placement, ok := aggregate.MCPPlacementForSubject(projection.SubjectID())
		if !ok {
			return ObservationSet{}, fmt.Errorf(
				"retiring provider-effective subject %q is not an MCP projection",
				projection.SubjectID(),
			)
		}
		switch placement.ID() {
		case aggregate.MCPPlacementPiProject, aggregate.MCPPlacementPiGlobal:
		default:
			continue
		}
		observation, err := observeProjection(projection)
		if err != nil {
			return ObservationSet{}, err
		}
		result.Retiring = append(result.Retiring, observation)
	}
	sort.Slice(result.Current, func(left int, right int) bool {
		return topology.CompareSubjectID(
			result.Current[left].Subject(),
			result.Current[right].Subject(),
		) < 0
	})
	sort.Slice(result.Retiring, func(left int, right int) bool {
		return topology.CompareSubjectID(
			result.Retiring[left].Subject(),
			result.Retiring[right].Subject(),
		) < 0
	})
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
