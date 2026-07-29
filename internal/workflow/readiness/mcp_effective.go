package readiness

import (
	"fmt"

	liveobserve "github.com/isty2e/daem/internal/assurance/observe/live"
	mcpeffective "github.com/isty2e/daem/internal/assurance/observe/mcp/effective"
	mcpeffectivehost "github.com/isty2e/daem/internal/assurance/observe/mcp/effective/host"
	daempaths "github.com/isty2e/daem/internal/paths"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	reconcileprojection "github.com/isty2e/daem/internal/reconcile/build/projection"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func observeProviderEffectiveMCP(
	paths daempaths.Paths,
	resolver liveobserve.DestinationResolver,
	locked lock.File,
	selection targetselection.Selection,
) ([]mcpeffective.Observation, error) {
	contracts, err := selectedMCPProjectionContracts(locked, selection)
	if err != nil {
		return nil, err
	}
	return mcpeffectivehost.Observe(mcpeffectivehost.Input{
		Contracts:          contracts,
		WorkDir:            paths.ManifestRoot,
		ResolveDestination: resolver,
	})
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
