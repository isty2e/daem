package profile

import (
	"fmt"
	"sort"

	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

// SelectMCPProviderContribution selects one explicit provider contribution for
// a provider-mediated MCP projection. Candidates must already have passed the
// target profile's identity and contribution admission.
func SelectMCPProviderContribution(
	selectedTarget target.Target,
	selectedScope target.Scope,
	candidates []extensiontopology.Contribution,
) (extensiontopology.Contribution, error) {
	if _, err := target.ParseTarget(string(selectedTarget)); err != nil {
		return extensiontopology.Contribution{}, fmt.Errorf("MCP provider selection target: %w", err)
	}
	if _, err := target.ParseScope(string(selectedScope)); err != nil {
		return extensiontopology.Contribution{}, fmt.Errorf("MCP provider selection scope: %w", err)
	}

	ordered := append([]extensiontopology.Contribution(nil), candidates...)
	sort.Slice(ordered, func(left int, right int) bool {
		return ordered[left].SubjectID().String() < ordered[right].SubjectID().String()
	})

	project := make([]extensiontopology.Contribution, 0, len(ordered))
	global := make([]extensiontopology.Contribution, 0, len(ordered))
	seen := make(map[string]struct{}, len(ordered))
	for index, candidate := range ordered {
		if err := candidate.Validate(); err != nil {
			return extensiontopology.Contribution{}, fmt.Errorf(
				"MCP provider candidate[%d]: %w",
				index,
				err,
			)
		}
		provider := candidate.Provider()
		if provider.Key().Target() != selectedTarget {
			return extensiontopology.Contribution{}, fmt.Errorf(
				"MCP provider %q targets %q, not binding target %q",
				candidate.SubjectID(),
				provider.Key().Target(),
				selectedTarget,
			)
		}
		identity := candidate.SubjectID().String()
		if _, duplicate := seen[identity]; duplicate {
			return extensiontopology.Contribution{}, fmt.Errorf(
				"duplicate MCP provider contribution %q",
				candidate.SubjectID(),
			)
		}
		seen[identity] = struct{}{}

		switch provider.Key().Scope() {
		case target.ScopeProject:
			project = append(project, candidate)
		case target.ScopeGlobal:
			global = append(global, candidate)
		default:
			return extensiontopology.Contribution{}, fmt.Errorf(
				"MCP provider %q has unsupported scope %q",
				candidate.SubjectID(),
				provider.Key().Scope(),
			)
		}
	}

	switch selectedScope {
	case target.ScopeProject:
		if len(project) != 0 {
			return requireUniqueMCPProviderContribution(selectedTarget, selectedScope, project)
		}
		return requireUniqueMCPProviderContribution(selectedTarget, selectedScope, global)
	case target.ScopeGlobal:
		if len(global) == 0 && len(project) != 0 {
			return extensiontopology.Contribution{}, fmt.Errorf(
				"global MCP binding for target %q cannot select a project-scoped provider",
				selectedTarget,
			)
		}
		return requireUniqueMCPProviderContribution(selectedTarget, selectedScope, global)
	default:
		return extensiontopology.Contribution{}, fmt.Errorf(
			"MCP binding has unsupported scope %q",
			selectedScope,
		)
	}
}

func requireUniqueMCPProviderContribution(
	selectedTarget target.Target,
	selectedScope target.Scope,
	candidates []extensiontopology.Contribution,
) (extensiontopology.Contribution, error) {
	switch len(candidates) {
	case 0:
		return extensiontopology.Contribution{}, fmt.Errorf(
			"MCP binding for target=%q scope=%q requires one explicit compatible provider",
			selectedTarget,
			selectedScope,
		)
	case 1:
		return candidates[0], nil
	default:
		identities := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			identities = append(identities, candidate.SubjectID().String())
		}
		return extensiontopology.Contribution{}, fmt.Errorf(
			"MCP binding for target=%q scope=%q has ambiguous provider contributions %q",
			selectedTarget,
			selectedScope,
			identities,
		)
	}
}
