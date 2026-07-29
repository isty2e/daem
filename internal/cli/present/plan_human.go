package clipresent

import (
	"fmt"
	"io"
	"strings"

	"github.com/isty2e/daem/internal/reconcile"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func PrintActionPlanWithOptions(output io.Writer, label string, actionPlan reconcile.Result, options HumanOptions) {
	decisions := actionPlan.Decisions()
	if options.Verbose {
		fmt.Fprintf(output, "%s: %d actions\n", label, actionPlan.ProjectionDecisionCount())
		printVerboseDecisionRows(output, decisions)
		return
	}
	current := 0
	pending := make([]reconcile.Decision, 0, len(decisions))
	for _, decision := range decisions {
		if decision.IsNoOp() {
			current++
			continue
		}
		pending = append(pending, decision)
	}
	fmt.Fprintf(output, "%s: %d resources (up to date=%d changes=%d)\n", label, actionPlan.ProjectionDecisionCount(), current, len(pending))
	printDecisionRows(output, pending, options)
}

// PrintPlanResultWithOptions renders every applied reconciliation decision.
func PrintPlanResultWithOptions(output io.Writer, planResult reconcile.Result, options HumanOptions) {
	decisions := planResult.MutatingDecisions()
	if options.Verbose {
		printVerboseDecisionRows(output, decisions)
		return
	}
	counts := make(map[string]int)
	for _, decision := range decisions {
		counts[decisionLabel(decision)]++
	}
	for _, label := range []string{
		"add managed output", "update managed output", "remove managed output",
		"manage existing matching output", "update managed ownership", "record managed output", "blocked",
	} {
		if counts[label] != 0 {
			fmt.Fprintf(output, "  %s: %d\n", label, counts[label])
		}
	}
	for _, decision := range decisions {
		if managedPath, ok := decision.ManagedPath(); ok {
			printManagedPathRows(output, []reconcile.ManagedPathDecision{managedPath}, options)
			continue
		}
		aggregate, _ := decision.Aggregate()
		printAggregateRows(output, []reconcile.AggregateSubjectDecision{aggregate}, options)
	}
}

func decisionLabel(decision reconcile.Decision) string {
	if managedPath, ok := decision.ManagedPath(); ok {
		return managedPathLabel(managedPath)
	}
	aggregate, _ := decision.Aggregate()
	return aggregateLabel(aggregate)
}

func printDecisionRows(output io.Writer, decisions []reconcile.Decision, options HumanOptions) {
	for _, decision := range decisions {
		if managedPath, ok := decision.ManagedPath(); ok {
			printManagedPathRows(output, []reconcile.ManagedPathDecision{managedPath}, options)
			continue
		}
		aggregate, _ := decision.Aggregate()
		printAggregateRows(output, []reconcile.AggregateSubjectDecision{aggregate}, options)
	}
}

func printVerboseDecisionRows(output io.Writer, decisions []reconcile.Decision) {
	for _, decision := range decisions {
		if managedPath, ok := decision.ManagedPath(); ok {
			printVerboseManagedPathRows(output, []reconcile.ManagedPathDecision{managedPath})
			continue
		}
		aggregate, _ := decision.Aggregate()
		printVerboseAggregateRows(output, []reconcile.AggregateSubjectDecision{aggregate})
	}
}

func printAggregateRows(output io.Writer, decisions []reconcile.AggregateSubjectDecision, options HumanOptions) {
	if options.Verbose {
		printVerboseAggregateRows(output, decisions)
		return
	}
	for _, decision := range decisions {
		identityLabel, identityValue := aggregateIdentityLabel(decision)
		fmt.Fprintf(
			output,
			"  %s %s=%q target=%s scope=%s\n",
			aggregateLabel(decision), identityLabel, identityValue, decision.Target(), decision.Scope(),
		)
		fmt.Fprintf(output, "    path: %q%s\n", decision.Destination(), decision.ContentPath())
		if decision.IsBlocked() {
			fmt.Fprintf(output, "    blocked: %s\n", actionReasonLabel(decision.Reason()))
			if decision.Detail() != "" {
				fmt.Fprintf(output, "    detail: %s\n", decision.Detail())
			}
		} else if decision.Kind() == reconcile.AggregateRemove &&
			decision.Detail() != "" {
			fmt.Fprintf(output, "    detail: %s\n", decision.Detail())
		}
	}
}

func printVerboseAggregateRows(output io.Writer, decisions []reconcile.AggregateSubjectDecision) {
	for _, decision := range decisions {
		identityLabel, identityValue := aggregateIdentityLabel(decision)
		fmt.Fprintf(
			output,
			"%s %s=%q target=%s scope=%s destination=%q content_path=%q reason=%s",
			aggregatePublicKind(decision), identityLabel, identityValue,
			decision.Target(), decision.Scope(), decision.Destination(), decision.ContentPath(), decision.Reason(),
		)
		if decision.Detail() != "" {
			fmt.Fprintf(output, " detail=%q", decision.Detail())
		}
		fmt.Fprintln(output)
	}
}

func aggregatePublicKind(decision reconcile.AggregateSubjectDecision) string {
	switch decision.Kind() {
	case reconcile.AggregateBlocked:
		return "error"
	case reconcile.AggregateReplace:
		return "update"
	case reconcile.AggregateRemove:
		return "delete"
	default:
		return string(decision.Kind())
	}
}

func aggregateIdentityLabel(decision reconcile.AggregateSubjectDecision) (string, string) {
	if entityID, ok := topologyprojection.EntityID(decision.Subject()); ok {
		return "resource", string(entityID.Kind()) + "/" + entityID.Name()
	}
	return "subject", subjectString(*planJSONSubjectFor(decision.Subject()))
}

func aggregateLabel(decision reconcile.AggregateSubjectDecision) string {
	if decision.Kind() == reconcile.AggregateRecord {
		switch decision.Reason() {
		case reconcile.ReasonManagedExisting:
			return "manage existing matching output"
		case reconcile.ReasonStateStale:
			return "update managed ownership"
		}
	}
	switch decision.Kind() {
	case reconcile.AggregateCreate:
		return "add managed output"
	case reconcile.AggregateReplace:
		return "update managed output"
	case reconcile.AggregateRemove:
		return "remove managed output"
	case reconcile.AggregateRecord:
		return "record managed output"
	case reconcile.AggregateNoOp:
		return "up to date"
	case reconcile.AggregateBlocked:
		return "blocked"
	default:
		return string(decision.Kind())
	}
}

func printManagedPathRows(output io.Writer, decisions []reconcile.ManagedPathDecision, options HumanOptions) {
	if options.Verbose {
		printVerboseManagedPathRows(output, decisions)
		return
	}
	for _, decision := range decisions {
		identityLabel, identityValue := managedPathIdentityLabel(decision)
		targetLabel, targetValue := managedPathTargetLabel(decision)
		fmt.Fprintf(
			output,
			"  %s %s=%q %s=%s scope=%s\n",
			managedPathLabel(decision),
			identityLabel,
			identityValue,
			targetLabel,
			targetValue,
			decision.Scope(),
		)
		fmt.Fprintf(output, "    path: %q\n", decision.Destination())
		if decision.IsBlocked() {
			fmt.Fprintf(output, "    blocked: %s\n", actionReasonLabel(decision.Reason()))
			if decision.Detail() != "" {
				fmt.Fprintf(output, "    detail: %s\n", decision.Detail())
			}
		}
	}
}

func printVerboseManagedPathRows(output io.Writer, decisions []reconcile.ManagedPathDecision) {
	for _, decision := range decisions {
		identityLabel, identityValue := managedPathIdentityLabel(decision)
		targetLabel, targetValue := managedPathTargetLabel(decision)
		fmt.Fprintf(
			output,
			"%s %s=%q %s=%s scope=%s destination=%q mode=%s reason=%s",
			managedPathPublicKind(decision),
			identityLabel,
			identityValue,
			targetLabel,
			targetValue,
			decision.Scope(),
			decision.Destination(),
			decision.PlacementMode(),
			decision.Reason(),
		)
		if decision.Detail() != "" {
			fmt.Fprintf(output, " detail=%q", decision.Detail())
		}
		if safety, ok := managedPathSafetyState(decision); ok {
			fmt.Fprintf(output, " safety=%s", safety)
		}
		if decision.PermissionPolicy() != "" {
			fmt.Fprintf(output, " permission_policy=%s", decision.PermissionPolicy())
			if decision.PermissionPolicy() != "none" {
				fmt.Fprintf(
					output,
					" desired_file_mode=%04o live_file_mode=%04o",
					decision.DesiredFileMode().Perm(),
					decision.LiveFileMode().Perm(),
				)
			}
		}
		fmt.Fprintln(output)
	}
}

func managedPathPublicKind(decision reconcile.ManagedPathDecision) string {
	switch decision.Kind() {
	case reconcile.ManagedPathBlocked:
		return "error"
	case reconcile.ManagedPathReplace:
		return "update"
	case reconcile.ManagedPathRemove:
		return "delete"
	default:
		return string(decision.Kind())
	}
}

func managedPathIdentityLabel(decision reconcile.ManagedPathDecision) (string, string) {
	if entityID, ok := topologyprojection.EntityID(decision.Subject()); ok {
		return "resource", string(entityID.Kind()) + "/" + entityID.Name()
	}
	return "subject", subjectString(*planJSONSubjectFor(decision.Subject()))
}

func managedPathTargetLabel(decision reconcile.ManagedPathDecision) (string, string) {
	consumers := decision.ConsumerTargets()
	if len(consumers) == 0 {
		if previous, present := decision.PreviousState(); present {
			consumers = previous.ConsumerTargets()
		}
	}
	values := targetStrings(consumers)
	if len(values) == 1 {
		return "target", values[0]
	}
	return "targets", strings.Join(values, ",")
}

func managedPathLabel(decision reconcile.ManagedPathDecision) string {
	if decision.Kind() == reconcile.ManagedPathRecord {
		switch decision.Reason() {
		case reconcile.ReasonManagedExisting:
			return "manage existing matching output"
		case reconcile.ReasonStateStale:
			return "update managed ownership"
		}
	}
	switch decision.Kind() {
	case reconcile.ManagedPathCreate:
		return "add managed output"
	case reconcile.ManagedPathReplace:
		return "update managed output"
	case reconcile.ManagedPathRemove:
		return "remove managed output"
	case reconcile.ManagedPathRecord:
		return "record managed output"
	case reconcile.ManagedPathNoOp:
		return "up to date"
	case reconcile.ManagedPathBlocked:
		return "blocked"
	default:
		return string(decision.Kind())
	}
}

func actionReasonLabel(reason reconcile.ActionReason) string {
	return strings.ReplaceAll(string(reason), "_", " ")
}
