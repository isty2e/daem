package readiness

import (
	"sort"

	mcpobserve "github.com/isty2e/daem/internal/assurance/observe/mcp"
	mcpeffective "github.com/isty2e/daem/internal/assurance/observe/mcp/effective"
	mcpeffectivehost "github.com/isty2e/daem/internal/assurance/observe/mcp/effective/host"
	"github.com/isty2e/daem/internal/reconcile"
	reconcileprojection "github.com/isty2e/daem/internal/reconcile/build/projection"
	"github.com/isty2e/daem/internal/topology"
)

func mcpPublicationConstraints(observations mcpeffectivehost.ObservationSet) ([]reconcileprojection.AggregateSubjectConstraint, error) {
	result := make([]reconcileprojection.AggregateSubjectConstraint, 0)
	blocked := make(map[topology.SubjectID]struct{})
	subjects := make([]topology.SubjectID, 0, len(observations.HostPrerequisites))
	for subject := range observations.HostPrerequisites {
		subjects = append(subjects, subject)
	}
	sort.Slice(subjects, func(i, j int) bool { return topology.CompareSubjectID(subjects[i], subjects[j]) < 0 })
	for _, subject := range subjects {
		host := observations.HostPrerequisites[subject]
		if host.State() != mcpobserve.HostUnqualified {
			continue
		}
		constraint, err := reconcileprojection.NewAggregateSubjectConstraint(subject, reconcile.ReasonHostPrerequisiteUnqualified,
			host.Detail()+"; use --target to select independent targets")
		if err != nil {
			return nil, err
		}
		result = append(result, constraint)
		blocked[subject] = struct{}{}
	}

	effective := make([]mcpeffective.Observation, 0, len(observations.Current))
	for _, observation := range observations.Current {
		if _, unqualified := blocked[observation.Subject()]; !unqualified {
			effective = append(effective, observation)
		}
	}
	constraints, err := providerEffectiveConstraints(effective)
	if err != nil {
		return nil, err
	}
	return append(result, constraints...), nil
}
