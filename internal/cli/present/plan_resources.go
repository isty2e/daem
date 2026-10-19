package clipresent

import (
	"fmt"
	"io"
	"strings"

	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

type resourcePlanKey struct {
	subject MCPSubject
	target  string
	scope   string
}

type configurationBlock struct {
	subjectCount int
	reason       reconcile.ActionReason
	causes       []topology.SubjectID
}

type resourcePlanFacts struct {
	statuses map[resourcePlanKey][]MCPStatus
	plans    map[resourcePlanKey][]reconcile.DelegateAction
	blocks   map[topology.SubjectID]configurationBlock
}

func PrintActionPlanWithOptions(output io.Writer, label string, input HumanPlanInput, options HumanOptions) {
	decisions := sortedHumanRows(input.Reconciliation.Decisions(), humanDecisionOrder)
	current, pending, blocked := 0, 0, 0
	for _, decision := range decisions {
		switch {
		case decision.IsNoOp():
			current++
		case projectionDecisionBlocked(decision):
			blocked++
		default:
			pending++
		}
	}
	fmt.Fprintf(output, "%s: %s (current=%d pending=%d blocked=%d)\n", label,
		CountLabel(input.Reconciliation.ProjectionDecisionCount(), "output entry", "output entries"),
		current, pending, blocked)

	facts := resourcePlanFactsFrom(input)
	matched := make(map[resourcePlanKey]bool)
	for _, decision := range decisions {
		if managed, ok := decision.ManagedPath(); ok {
			if !managed.IsNoOp() || options.Verbose {
				printManagedResourcePlan(output, managed, options)
			}
			continue
		}
		aggregate, _ := decision.Aggregate()
		key := resourcePlanKeyFrom(aggregate.Subject(), aggregate.Target(), aggregate.Scope())
		matched[key] = true
		block, documentBlocked := facts.blocks[aggregate.Subject()]
		statuses, plans := facts.statuses[key], facts.plans[key]
		if aggregate.IsNoOp() && !options.Verbose && !documentBlocked && len(plans) == 0 && !hasVisibleMCPStatuses(statuses, options) {
			continue
		}

		printAggregateResourcePlan(output, aggregate, options)
		if documentBlocked {
			printConfigurationBlock(output, block, aggregate.Subject())
		}
		for _, status := range statuses {
			if options.Verbose {
				fmt.Fprintf(output, "    MCP observation: subject=%s target=%s scope=%s",
					Quote(mcpSubjectString(status.Subject)), Escape(status.Target), Escape(status.Scope))
				printMCPStatusMetadata(output, status)
				fmt.Fprintln(output)
			}
			printMCPStatusDimensions(output, status, options)
		}
		for _, plan := range plans {
			printDelegateActionBody(output, plan, options)
		}
	}

	var unmatchedStatuses []MCPStatus
	for _, status := range input.MCPStatuses {
		key := resourcePlanKey{subject: status.Subject, target: status.Target, scope: status.Scope}
		if !matched[key] {
			unmatchedStatuses = append(unmatchedStatuses, status)
		}
	}
	PrintMCPStatusesWithOptions(output, unmatchedStatuses, options)

	var unmatchedPlans []reconcile.DelegateAction
	for _, plan := range input.Reconciliation.Delegates() {
		if !matched[resourcePlanKeyFrom(plan.Subject(), plan.Target(), plan.Scope())] {
			unmatchedPlans = append(unmatchedPlans, plan)
		}
	}
	printDelegateActionRows(output, unmatchedPlans, options)
	if len(input.Reconciliation.Delegates()) != 0 {
		fmt.Fprintf(output, "MCP command plans: %s\n", CountLabel(len(input.Reconciliation.Delegates()), "plan", "plans"))
		printDelegatePlanLimitations(output, input.Reconciliation.Delegates())
	}
}

func resourcePlanFactsFrom(input HumanPlanInput) resourcePlanFacts {
	facts := resourcePlanFacts{
		statuses: make(map[resourcePlanKey][]MCPStatus),
		plans:    make(map[resourcePlanKey][]reconcile.DelegateAction),
		blocks:   make(map[topology.SubjectID]configurationBlock),
	}
	for _, status := range input.MCPStatuses {
		key := resourcePlanKey{subject: status.Subject, target: status.Target, scope: status.Scope}
		facts.statuses[key] = append(facts.statuses[key], status)
	}
	for _, plan := range input.Reconciliation.Delegates() {
		key := resourcePlanKeyFrom(plan.Subject(), plan.Target(), plan.Scope())
		facts.plans[key] = append(facts.plans[key], plan)
	}
	for _, document := range input.Reconciliation.Aggregates() {
		if !document.IsBlocked() {
			continue
		}
		block := configurationBlock{subjectCount: len(document.Subjects()), reason: document.Reason()}
		for _, projection := range document.Projections() {
			for _, decision := range projection.SubjectDecisions() {
				if decision.IsBlocked() {
					block.causes = append(block.causes, decision.Subject())
				}
			}
		}
		for _, subject := range document.BlockedSubjects() {
			facts.blocks[subject] = block
		}
	}
	return facts
}

func resourcePlanKeyFrom(subject topology.SubjectID, selectedTarget target.Target, scope target.Scope) resourcePlanKey {
	return resourcePlanKey{subject: mcpSubjectFromID(subject), target: string(selectedTarget), scope: string(scope)}
}

func hasVisibleMCPStatuses(statuses []MCPStatus, options HumanOptions) bool {
	for _, status := range statuses {
		if options.Verbose || len(selectedMCPDimensions(allMCPDimensions(status), options)) != 0 {
			return true
		}
	}
	return false
}

func projectionDecisionBlocked(decision reconcile.Decision) bool {
	if managed, ok := decision.ManagedPath(); ok {
		return managed.IsBlocked()
	}
	aggregate, _ := decision.Aggregate()
	return aggregate.IsBlocked()
}

func printManagedResourcePlan(output io.Writer, decision reconcile.ManagedPathDecision, options HumanOptions) {
	identityLabel, identityValue := projectionIdentityLabel(decision.Subject())
	targetLabel, targetValue := managedPathTargetLabel(decision)
	fmt.Fprintf(output, "  %s=%s %s=%s scope=%s\n", identityLabel, Quote(identityValue),
		targetLabel, Escape(targetValue), Escape(string(decision.Scope())))
	fmt.Fprint(output, "    output: ")
	if options.Verbose {
		printVerboseManagedPathRows(output, []reconcile.ManagedPathDecision{decision})
	} else {
		fmt.Fprintln(output, managedPathLabel(decision))
		if decision.IsBlocked() {
			fmt.Fprintf(output, "    blocked: %s\n", actionReasonLabel(decision.Reason()))
		}
	}
}

func printAggregateResourcePlan(output io.Writer, decision reconcile.AggregateSubjectDecision, options HumanOptions) {
	identityLabel, identityValue := projectionIdentityLabel(decision.Subject())
	fmt.Fprintf(output, "  %s=%s target=%s scope=%s\n", identityLabel, Quote(identityValue),
		Escape(string(decision.Target())), Escape(string(decision.Scope())))
	fmt.Fprint(output, "    config: ")
	if options.Verbose {
		printVerboseAggregateRows(output, []reconcile.AggregateSubjectDecision{decision})
		return
	}
	fmt.Fprintln(output, aggregateLabel(decision))
	if decision.IsBlocked() {
		fmt.Fprintf(output, "    blocked: %s\n", actionReasonLabel(decision.Reason()))
	} else if decision.Kind() == reconcile.AggregateRemove && decision.Detail() != "" {
		fmt.Fprintln(output, "    detail: removal may change the effective host definition; runtime absence is not claimed")
	}
}

func printConfigurationBlock(output io.Writer, block configurationBlock, current topology.SubjectID) {
	causes := make([]string, 0, len(block.causes))
	seen := make(map[string]bool)
	for _, subject := range block.causes {
		if subject == current {
			continue
		}
		_, reference := projectionIdentityLabel(subject)
		if !seen[reference] {
			seen[reference] = true
			causes = append(causes, Quote(reference))
		}
	}
	if len(block.causes) != 0 && len(causes) == 0 {
		return
	}

	label := "configuration document"
	if block.subjectCount > 1 {
		label = "shared configuration"
	}
	fmt.Fprintf(output, "    %s: blocked", label)
	if len(causes) != 0 {
		fmt.Fprintf(output, " by %s", strings.Join(causes, ", "))
	} else {
		fmt.Fprintf(output, " (%s)", actionReasonLabel(block.reason))
	}
	fmt.Fprintln(output, "; apply will be refused until resolved")
}
