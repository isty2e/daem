package adopt

import targetpkg "github.com/isty2e/daem/internal/target"

type importSummaryCounts struct {
	Instructions int
	Skills       int
	Hooks        int
	MCPServers   int
}

type importSummaryKey struct {
	Target targetpkg.Target
	Scope  targetpkg.Scope
}

// HasMergeConflicts reports whether a merge plan contains conflicts.
func (plan Plan) HasMergeConflicts() bool {
	for _, result := range plan.mergeResults {
		if result.Status == MergeStatusConflict {
			return true
		}
	}
	return false
}

// ResourceCount returns the number of resources that would be imported or written.
func (plan Plan) ResourceCount() int {
	return plan.candidates.ResourceCount()
}

// SummaryRows returns per target/scope resource counts in target order.
func (plan Plan) SummaryRows() []SummaryRow {
	rows := importSummaryRows(plan)
	ordered := make([]SummaryRow, 0, len(rows))
	for _, target := range targetpkg.SupportedTargets() {
		for _, scope := range []targetpkg.Scope{targetpkg.ScopeProject, targetpkg.ScopeGlobal} {
			key := importSummaryKey{Target: target, Scope: scope}
			counts, ok := rows[key]
			if !ok {
				continue
			}
			ordered = append(ordered, SummaryRow{
				Target:       key.Target,
				Scope:        key.Scope,
				Instructions: counts.Instructions,
				Skills:       counts.Skills,
				Hooks:        counts.Hooks,
				MCPServers:   counts.MCPServers,
			})
		}
	}
	return ordered
}

func importSummaryRows(plan Plan) map[importSummaryKey]importSummaryCounts {
	rows := make(map[importSummaryKey]importSummaryCounts)
	for _, source := range plan.candidates.sources {
		key := importSummaryKey{Target: source.Target, Scope: source.Scope}
		counts := rows[key]
		counts.Instructions++
		rows[key] = counts
	}
	for _, skill := range plan.candidates.skills {
		targets := skill.Targets
		if len(targets) == 0 {
			targets = []targetpkg.Target{skill.Target}
		}
		for _, target := range UniqueTargets(targets) {
			key := importSummaryKey{Target: target, Scope: skill.Scope}
			counts := rows[key]
			counts.Skills++
			rows[key] = counts
		}
	}
	for _, hook := range plan.candidates.hooks {
		key := importSummaryKey{Target: hook.Target, Scope: hook.Scope}
		counts := rows[key]
		counts.Hooks++
		rows[key] = counts
	}
	for _, server := range plan.candidates.mcpServers {
		key := importSummaryKey{Target: server.Target, Scope: server.Scope}
		counts := rows[key]
		counts.MCPServers++
		rows[key] = counts
	}
	return rows
}
