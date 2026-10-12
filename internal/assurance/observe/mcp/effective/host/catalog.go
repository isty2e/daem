// Package host dispatches provider-effective MCP observation to the private
// host adapter selected by each locked projection.
package host

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/realization/profile"

	mcpobserve "github.com/isty2e/daem/internal/assurance/observe/mcp"
	mcpeffective "github.com/isty2e/daem/internal/assurance/observe/mcp/effective"
	"github.com/isty2e/daem/internal/hostsurface/catalog"
	"github.com/isty2e/daem/internal/output"
	pihostpath "github.com/isty2e/daem/internal/output/hostpath/pi"
	"github.com/isty2e/daem/internal/realization/aggregate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

// Input contains selected locked MCP projections and operation-local path
// facts. Pi dispatch consumes recorded codecs rather than inferring a backend from provider presence.
type Input struct {
	Context            context.Context
	Contracts          []lock.LockedSubjectContract
	Previous           []aggregate.SubjectContribution
	Retiring           []aggregate.SubjectContribution
	Codecs             aggregate.CodecCatalog
	WorkDir            string
	ResolveDestination func(output.Destination) (string, error)
}

// ObservationSet separates current desired projections, which may constrain
// writes, from retiring managed projections, which are diagnostic only.
type ObservationSet struct {
	Current           []mcpeffective.Observation
	Retiring          []mcpeffective.Observation
	HostPrerequisites map[topology.SubjectID]mcpobserve.HostPrerequisiteObservation
}

// Observe dispatches current and retiring provider-mediated projections to
// their admitted host observer without exposing host identities to readiness.
func Observe(input Input) (ObservationSet, error) {
	if input.Context == nil {
		input.Context = context.Background()
	}
	if err := input.Context.Err(); err != nil {
		return ObservationSet{}, err
	}
	if input.ResolveDestination == nil {
		return ObservationSet{}, fmt.Errorf("provider-effective MCP destination resolver is required")
	}
	nativePeers, err := nativePeerProjections(input)
	if err != nil {
		return ObservationSet{}, err
	}
	seen := make(map[topology.SubjectID]struct{}, len(input.Contracts)+len(input.Retiring))
	var (
		piAgentRoot string
		piHomeDir   string
		piRootErr   error
		piResolved  bool
	)
	var nativeVersion profile.PiMCPVersion
	nativeVersionObserved := false
	nativeQualifiedScopes := make(map[target.Scope]mcpobserve.HostPrerequisiteObservation)
	hostPrerequisites := make(map[topology.SubjectID]mcpobserve.HostPrerequisiteObservation)

	observeProjection := func(
		projection aggregate.SubjectContribution,
		retiring bool,
	) (mcpeffective.Observation, error) {
		subject := projection.SubjectID()
		if _, duplicate := seen[subject]; duplicate {
			return mcpeffective.Observation{}, fmt.Errorf(
				"duplicate provider-effective MCP projection for %q",
				subject,
			)
		}
		seen[subject] = struct{}{}
		view, ok := catalog.Product().LookupMCPBySubject(subject)
		if !ok {
			return mcpeffective.Observation{}, fmt.Errorf(
				"provider-mediated subject %q is not an MCP projection",
				subject,
			)
		}
		placement := view.Placement()
		switch placement.ID() {
		case aggregate.MCPPlacementPiProject, aggregate.MCPPlacementPiGlobal:
			piContract, admitted := profile.PiMCPContractForCodec(projection.Contribution().CodecContractID())
			if !admitted {
				return mcpeffective.Observation{}, fmt.Errorf("recorded Pi MCP codec has no admitted contract")
			}
			if !piResolved {
				piAgentRoot, piRootErr = pihostpath.ResolveAgentRoot(pihostpath.AgentRootInput{WorkDir: input.WorkDir})
				piResolved = true
			}
			if piRootErr != nil {
				return mcpeffective.Observation{}, fmt.Errorf("resolve Pi agent root for MCP effective observation: %w", piRootErr)
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
			if piContract.Backend() == mcp.BackendNative {
				nativeInput := PiNativeInput{
					Projection: projection, Codecs: input.Codecs, WorkDir: input.WorkDir, AgentRoot: piAgentRoot,
					SelectedPath: selectedPath, Retiring: retiring, peers: nativePeers,
				}
				sourceContext, err := newPiNativeSourceContext(projection.Contribution().Scope(), input.WorkDir, piAgentRoot, selectedPath)
				if err != nil {
					return mcpeffective.Observation{}, err
				}
				if !retiring {
					if err := input.Context.Err(); err != nil {
						return mcpeffective.Observation{}, err
					}
					if !nativeVersionObserved {
						nativeVersion, err = ObservePiVersion(input.Context)
						if err != nil {
							return mcpeffective.Observation{}, err
						}
						nativeVersionObserved = true
					}
					scope := projection.Contribution().Scope()
					qualified, present := nativeQualifiedScopes[scope]
					if !present {
						qualificationErr := qualifyPiNativeSettings(input.Context, piContract, scope, input.WorkDir, piAgentRoot, nativeVersion)
						if err := input.Context.Err(); err != nil {
							return mcpeffective.Observation{}, err
						}
						qualified, err = nativeHostPrerequisite(qualificationErr)
						if err != nil {
							return mcpeffective.Observation{}, err
						}
						nativeQualifiedScopes[scope] = qualified
					}
					hostPrerequisites[subject] = qualified
				}
				return observePiNative(nativeInput, sourceContext)
			}
			if piHomeDir == "" {
				piHomeDir, err = os.UserHomeDir()
				if err != nil {
					return mcpeffective.Observation{}, fmt.Errorf("resolve home for Pi MCP effective observation: %w", err)
				}
			}
			observation, err := ObservePiAdapter(PiAdapterInput{
				Projection:   projection,
				Codecs:       input.Codecs,
				HomeDir:      piHomeDir,
				WorkDir:      input.WorkDir,
				AgentRoot:    piAgentRoot,
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
		Current:           make([]mcpeffective.Observation, 0),
		Retiring:          make([]mcpeffective.Observation, 0, len(input.Retiring)),
		HostPrerequisites: hostPrerequisites,
	}
	for _, contract := range input.Contracts {
		subject := contract.SubjectID()
		provider, providerMediated := contract.MCPProviderContribution()
		view, pi := catalog.Product().LookupMCPBySubject(subject)
		pi = pi && (view.Placement().ID() == aggregate.MCPPlacementPiProject || view.Placement().ID() == aggregate.MCPPlacementPiGlobal)
		if !providerMediated && !pi {
			continue
		}
		if providerMediated && (provider.Kind() != "mcp-client" || provider.Key() != "default") {
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
		observation, err := observeProjection(projection, false)
		if err != nil {
			return ObservationSet{}, err
		}
		result.Current = append(result.Current, observation)
	}
	for _, projection := range input.Retiring {
		view, ok := catalog.Product().LookupMCPBySubject(projection.SubjectID())
		if !ok {
			return ObservationSet{}, fmt.Errorf(
				"retiring provider-effective subject %q is not an MCP projection",
				projection.SubjectID(),
			)
		}
		switch view.Placement().ID() {
		case aggregate.MCPPlacementPiProject, aggregate.MCPPlacementPiGlobal:
		default:
			continue
		}
		observation, err := observeProjection(projection, true)
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
	if err := input.Context.Err(); err != nil {
		return ObservationSet{}, err
	}
	return result, nil
}
