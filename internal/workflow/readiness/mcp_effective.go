package readiness

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	liveobserve "github.com/isty2e/daem/internal/assurance/observe/live"
	mcpeffective "github.com/isty2e/daem/internal/assurance/observe/mcp/effective"
	mcpeffectivehost "github.com/isty2e/daem/internal/assurance/observe/mcp/effective/host"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	reconcileprojection "github.com/isty2e/daem/internal/reconcile/build/projection"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/topology"
)

func observeProviderEffectiveMCP(
	paths daempaths.Paths,
	resolver liveobserve.DestinationResolver,
	locked lock.File,
	currentState durable.Snapshot,
	selection targetselection.Selection,
	codecs aggregate.CodecCatalog,
) (mcpeffectivehost.ObservationSet, error) {
	contracts, err := selectedMCPProjectionContracts(locked, selection)
	if err != nil {
		return mcpeffectivehost.ObservationSet{}, err
	}
	retiring, err := retiringMCPProjections(
		contracts,
		currentState,
		selection,
	)
	if err != nil {
		return mcpeffectivehost.ObservationSet{}, err
	}
	return mcpeffectivehost.Observe(mcpeffectivehost.Input{
		Contracts:          contracts,
		Retiring:           retiring,
		Codecs:             codecs,
		WorkDir:            paths.ManifestRoot,
		ResolveDestination: resolver,
	})
}

func retiringMCPProjections(
	current []lock.LockedSubjectContract,
	currentState durable.Snapshot,
	selection targetselection.Selection,
) ([]aggregate.SubjectContribution, error) {
	currentSubjects := make(map[topology.SubjectID]struct{}, len(current))
	for _, contract := range current {
		currentSubjects[contract.SubjectID()] = struct{}{}
	}
	result := make([]aggregate.SubjectContribution, 0)
	for _, state := range currentState.ManagedAggregates() {
		if !selection.Includes(state.Contribution().Target()) {
			continue
		}
		if _, stillDesired := currentSubjects[state.Subject()]; stillDesired {
			continue
		}
		if _, admitted := aggregate.MCPPlacementForSubject(state.Subject()); !admitted {
			continue
		}
		projection, err := aggregate.NewSubjectContribution(
			state.Subject(),
			state.Contribution(),
		)
		if err != nil {
			return nil, fmt.Errorf(
				"retiring MCP projection %q: %w",
				state.Subject(),
				err,
			)
		}
		result = append(result, projection)
	}
	return result, nil
}

func providerEffectiveConstraints(
	observations []mcpeffective.Observation,
) ([]reconcileprojection.AggregateSubjectConstraint, error) {
	result := make([]reconcileprojection.AggregateSubjectConstraint, 0)
	for _, observation := range observations {
		var (
			reason reconcile.ActionReason
			detail string
		)
		switch observation.State() {
		case mcpeffective.StateExact:
			continue
		case mcpeffective.StateConflicting:
			reason = reconcile.ReasonEffectiveStateConflict
			detail = effectiveBlockDetail(observation, "also defines the selected MCP name")
		case mcpeffective.StateUnobservable:
			reason = reconcile.ReasonEffectiveStateUnobserved
			detail = effectiveBlockDetail(observation, "cannot be observed exactly")
		default:
			return nil, fmt.Errorf(
				"provider-effective observation for %q has unsupported state %q",
				observation.Subject(),
				observation.State(),
			)
		}
		constraint, err := reconcileprojection.NewAggregateSubjectConstraint(
			observation.Subject(),
			reason,
			detail,
		)
		if err != nil {
			return nil, err
		}
		result = append(result, constraint)
	}
	return result, nil
}

func providerEffectiveRemovalNotices(
	observations []mcpeffective.Observation,
) ([]reconcileprojection.AggregateRemovalNotice, error) {
	result := make(
		[]reconcileprojection.AggregateRemovalNotice,
		0,
		len(observations),
	)
	for _, observation := range observations {
		detail := "managed MCP config entry will be removed; "
		switch observation.State() {
		case mcpeffective.StateExact:
			detail += "no other same-name definition was observed"
		case mcpeffective.StateConflicting:
			switch {
			case observation.HigherConflictPresent() &&
				observation.LowerFallbackPresent():
				detail += "an unowned higher-precedence same-name definition remains effective while lower same-name definitions remain shadowed"
				detail += effectiveSourceLocation(
					observation.HigherConflictSources(),
				)
			case observation.HigherConflictPresent():
				detail += "an unowned higher-precedence same-name definition remains effective"
				detail += effectiveSourceLocation(
					observation.HigherConflictSources(),
				)
			case observation.LowerFallbackPresent():
				equivalence, _ := observation.LowerFallbackEquivalence()
				switch equivalence {
				case mcpeffective.DefinitionEquivalenceEquivalent:
					detail += "an equivalent unowned lower-precedence same-name definition may become effective"
				case mcpeffective.DefinitionEquivalenceDifferent:
					detail += "a materially different unowned lower-precedence same-name definition may become effective"
				default:
					detail += "an unowned lower-precedence same-name definition with incomparable semantics may become effective"
				}
				detail += effectiveSourceLocation(
					observation.LowerFallbackSources(),
				)
			default:
				return nil, fmt.Errorf(
					"conflicting provider-effective removal observation for %q has no classified conflict",
					observation.Subject(),
				)
			}
		case mcpeffective.StateUnobservable:
			detail += "the effective definition after removal cannot be determined because an active source was not observable exactly"
			detail += effectiveSourceLocation(observation.BlockingSources())
		default:
			return nil, fmt.Errorf(
				"provider-effective removal observation for %q has unsupported state %q",
				observation.Subject(),
				observation.State(),
			)
		}
		detail += "; provider package removal is separate; runtime absence is not claimed"
		notice, err := reconcileprojection.NewAggregateRemovalNotice(
			observation.Subject(),
			detail,
		)
		if err != nil {
			return nil, err
		}
		result = append(result, notice)
	}
	return result, nil
}

func effectiveSourceLocation(
	sources []mcpeffective.SourceObservation,
) string {
	if len(sources) == 0 {
		return ""
	}
	detail := fmt.Sprintf(
		` (source %q at %q`,
		sources[0].ID(),
		sources[0].Path(),
	)
	if len(sources) > 1 {
		detail += fmt.Sprintf(", %d additional sources", len(sources)-1)
	}
	return detail + ")"
}

func effectiveBlockDetail(
	observation mcpeffective.Observation,
	summary string,
) string {
	blocking := observation.BlockingSources()
	if len(blocking) == 0 {
		return "provider-effective MCP state " + summary
	}
	first := blocking[0]
	detail := fmt.Sprintf(
		"provider-effective MCP source %q at %q %s",
		first.ID(),
		first.Path(),
		summary,
	)
	if first.Detail() != "" {
		detail += ": " + first.Detail()
	}
	if len(blocking) > 1 {
		detail += fmt.Sprintf(" (%d additional blocking sources)", len(blocking)-1)
	}
	return detail
}
