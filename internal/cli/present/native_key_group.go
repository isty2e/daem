package clipresent

import (
	"fmt"
	"io"
	"strings"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

type nativeHumanGroupKey struct {
	declaration entity.ID
	document    aggregate.DocumentAddress
}

type humanDecisionGroup struct {
	native    bool
	decisions []reconcile.Decision
}

func nativeHumanGroupIdentity(decision reconcile.AggregateSubjectDecision) (nativeHumanGroupKey, bool) {
	id, _, native := topologyprojection.HostConfigKeyIdentity(decision.Subject())
	return nativeHumanGroupKey{id, decision.Contract().Address().Document()}, native
}

func groupHumanDecisions(decisions []reconcile.Decision, options HumanOptions) []humanDecisionGroup {
	var groups []humanDecisionGroup
	indices := make(map[nativeHumanGroupKey]int)
	for _, decision := range decisions {
		aggregate, aggregatePresent := decision.Aggregate()
		if options.Verbose || !aggregatePresent {
			groups = append(groups, humanDecisionGroup{decisions: []reconcile.Decision{decision}})
			continue
		}
		key, native := nativeHumanGroupIdentity(aggregate)
		if !native {
			groups = append(groups, humanDecisionGroup{decisions: []reconcile.Decision{decision}})
			continue
		}
		index, found := indices[key]
		if !found {
			index = len(groups)
			indices[key] = index
			groups = append(groups, humanDecisionGroup{native: true})
		}
		groups[index].decisions = append(groups[index].decisions, decision)
	}
	return groups
}

func printNativeGroupHeader(output io.Writer, decision reconcile.AggregateSubjectDecision) {
	label, reference := projectionIdentityLabel(decision.Subject())
	fmt.Fprintf(output, "  %s=%s target=%s scope=%s destination=%s\n", label, Quote(reference), Escape(string(decision.Target())), Escape(string(decision.Scope())), Quote(decision.Destination().String()))
}

func printNativeGroupKey(output io.Writer, decision reconcile.AggregateSubjectDecision) {
	_, key, _ := topologyprojection.HostConfigKeyIdentity(decision.Subject())
	fmt.Fprintf(output, "    key: %s: %s", nativeKeyDisplay(key), aggregateLabel(decision))
	if decision.IsBlocked() {
		fmt.Fprintf(output, " (%s)", actionReasonLabel(decision.Reason()))
	}
	fmt.Fprintln(output)
	if decision.Kind() == reconcile.AggregateRemove && decision.Detail() != "" {
		fmt.Fprintln(output, "      detail: removal may change the effective host definition; runtime absence is not claimed")
	}
}

func printNativeGroupPlan(output io.Writer, group humanDecisionGroup, facts resourcePlanFacts, matched map[resourcePlanKey]bool, options HumanOptions) {
	var visible []reconcile.AggregateSubjectDecision
	var block configurationBlock
	documentBlocked := false
	for _, decision := range group.decisions {
		row, _ := decision.Aggregate()
		key := resourcePlanKeyFrom(row.Subject(), row.Target(), row.Scope())
		matched[key] = true
		if observed, blocked := facts.blocks[row.Subject()]; blocked {
			block, documentBlocked = observed, true
		}
		_, rowBlocked := facts.blocks[row.Subject()]
		if row.IsNoOp() && !rowBlocked && len(facts.plans[key]) == 0 && !hasVisibleMCPStatuses(facts.statuses[key], options) {
			continue
		}
		visible = append(visible, row)
	}
	if len(visible) == 0 {
		return
	}
	printNativeGroupHeader(output, visible[0])
	for _, row := range visible {
		printNativeGroupKey(output, row)
		key := resourcePlanKeyFrom(row.Subject(), row.Target(), row.Scope())
		printResourcePlanExtras(output, facts.statuses[key], facts.plans[key], options)
	}
	if documentBlocked {
		printNativeGroupBlock(output, block, visible[0])
	}
}

func printNativeGroupBlock(output io.Writer, block configurationBlock, representative reconcile.AggregateSubjectDecision) {
	identity, _ := nativeHumanGroupIdentity(representative)
	var causes []string
	seen := make(map[topology.SubjectID]bool)
	for _, subject := range block.causes {
		if seen[subject] {
			continue
		}
		seen[subject] = true
		id, key, native := topologyprojection.HostConfigKeyIdentity(subject)
		if native && id == identity.declaration && subject.Namespace() == representative.Subject().Namespace() {
			causes = append(causes, "key "+nativeKeyDisplay(key))
		} else {
			causes = append(causes, configurationCauseDisplay(subject))
		}
	}
	fmt.Fprint(output, "    document: blocked")
	if len(causes) != 0 {
		fmt.Fprintf(output, " by %s", strings.Join(causes, ", "))
	} else {
		fmt.Fprintf(output, " (%s)", actionReasonLabel(block.reason))
	}
	fmt.Fprintln(output, "; apply will be refused until resolved")
}

func configurationCauseDisplay(subject topology.SubjectID) string {
	_, reference := projectionIdentityLabel(subject)
	if _, key, native := topologyprojection.HostConfigKeyIdentity(subject); native {
		return Quote(reference) + " key " + nativeKeyDisplay(key)
	}
	return Quote(reference)
}
