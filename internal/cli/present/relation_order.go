package clipresent

import (
	"fmt"
	"io"
	"strings"

	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/topology"
)

const foreignPrecedenceChangeRisk = "foreign_precedence_change"

type relationOrderJSON struct {
	Target                string                    `json:"target"`
	Scope                 string                    `json:"scope"`
	ClassID               string                    `json:"class_id"`
	SequenceID            string                    `json:"sequence_id"`
	RuntimeMeaning        string                    `json:"runtime_meaning"`
	ConstraintFingerprint string                    `json:"constraint_fingerprint"`
	Authority             string                    `json:"authority,omitempty"`
	Revision              string                    `json:"revision,omitempty"`
	Kind                  string                    `json:"kind"`
	Reason                string                    `json:"reason,omitempty"`
	Detail                string                    `json:"detail,omitempty"`
	DesiredMembers        []relationOrderMemberJSON `json:"desired_members"`
	ObservedMembers       []relationOrderMemberJSON `json:"observed_members"`
	MissingMembers        []relationOrderMemberJSON `json:"missing_members,omitempty"`
	ForeignRowCount       int                       `json:"foreign_row_count"`
	Risks                 []relationOrderRiskJSON   `json:"risks,omitempty"`
	BlocksOrdinaryApply   bool                      `json:"blocks_ordinary_apply"`
	RequiresMutation      bool                      `json:"requires_mutation"`
}

type relationOrderMemberJSON struct {
	Subject          *planJSONSubject `json:"subject,omitempty"`
	HostLoadIdentity string           `json:"host_load_identity"`
}

type relationOrderRiskJSON struct {
	Code                string           `json:"code"`
	ManagedSubject      *planJSONSubject `json:"managed_subject,omitempty"`
	ForeignIdentity     string           `json:"foreign_identity"`
	ManagedWasBefore    bool             `json:"managed_was_before"`
	ManagedWillBeBefore bool             `json:"managed_will_be_before"`
}

func relationOrderJSONActions(
	decisions []reconcile.RelationOrderDecision,
) []relationOrderJSON {
	result := make([]relationOrderJSON, 0, len(decisions))
	for _, decision := range decisions {
		risks := make([]relationOrderRiskJSON, 0, len(decision.PrecedenceChanges()))
		for _, change := range decision.PrecedenceChanges() {
			risks = append(risks, relationOrderRiskJSON{
				Code:                foreignPrecedenceChangeRisk,
				ManagedSubject:      planJSONSubjectFor(change.ManagedSubject()),
				ForeignIdentity:     string(change.ForeignIdentity()),
				ManagedWasBefore:    change.ManagedWasBefore(),
				ManagedWillBeBefore: change.ManagedWillBeBefore(),
			})
		}
		result = append(result, relationOrderJSON{
			Target:                string(decision.Target()),
			Scope:                 string(decision.Scope()),
			ClassID:               string(decision.ClassID()),
			SequenceID:            string(decision.SequenceID()),
			RuntimeMeaning:        string(decision.RuntimeMeaning()),
			ConstraintFingerprint: decision.ConstraintFingerprint(),
			Authority:             string(decision.Authority()),
			Revision:              string(decision.Revision()),
			Kind:                  string(decision.Kind()),
			Reason:                string(decision.Reason()),
			Detail:                decision.Detail(),
			DesiredMembers:        relationOrderJSONMembers(decision.DesiredMembers()),
			ObservedMembers:       relationOrderJSONMembers(decision.ObservedMembers()),
			MissingMembers:        relationOrderJSONMembers(decision.MissingMembers()),
			ForeignRowCount:       decision.ForeignRowCount(),
			Risks:                 risks,
			BlocksOrdinaryApply:   decision.BlocksOrdinaryApply(),
			RequiresMutation:      decision.RequiresMutation(),
		})
	}
	return result
}

func relationOrderJSONMembers(
	members []hostrelation.RelationOrderMember,
) []relationOrderMemberJSON {
	result := make([]relationOrderMemberJSON, 0, len(members))
	for _, member := range members {
		result = append(result, relationOrderMemberJSON{
			Subject:          planJSONSubjectFor(member.Subject()),
			HostLoadIdentity: string(member.HostLoadIdentity()),
		})
	}
	return result
}

// PrintRelationOrderActionsWithOptions writes one row per independently
// mutable physical extension sequence.
func PrintRelationOrderActionsWithOptions(
	output io.Writer,
	decisions []reconcile.RelationOrderDecision,
	options HumanOptions,
) {
	if len(decisions) == 0 {
		return
	}
	fmt.Fprintf(output, "extension order: %d sequences\n", len(decisions))
	for _, decision := range decisions {
		if options.Verbose {
			printVerboseRelationOrder(output, decision)
			continue
		}
		printRelationOrderSummary(output, decision)
	}
}

func printRelationOrderSummary(
	output io.Writer,
	decision reconcile.RelationOrderDecision,
) {
	meaning := relationOrderMeaning(decision.RuntimeMeaning())
	switch decision.Kind() {
	case reconcile.OrderExact:
		fmt.Fprintf(
			output,
			"  - %s is exact target=%s scope=%s sequence=%q\n",
			meaning,
			decision.Target(),
			decision.Scope(),
			decision.SequenceID(),
		)
	case reconcile.OrderNormalize:
		fmt.Fprintf(
			output,
			"  - normalize %s target=%s scope=%s sequence=%q\n",
			meaning,
			decision.Target(),
			decision.Scope(),
			decision.SequenceID(),
		)
		if count := len(decision.PrecedenceChanges()); count != 0 {
			fmt.Fprintf(
				output,
				"    includes %d managed/foreign precedence changes\n",
				count,
			)
		}
	case reconcile.OrderConditionalAfterCarrierChange:
		fmt.Fprintf(
			output,
			"  - recheck %s after %s target=%s scope=%s sequence=%q\n",
			meaning,
			strings.ReplaceAll(string(decision.Reason()), "_", " "),
			decision.Target(),
			decision.Scope(),
			decision.SequenceID(),
		)
	case reconcile.OrderBlocked:
		detail := decision.Detail()
		if detail == "" {
			detail = strings.ReplaceAll(string(decision.Reason()), "_", " ")
		}
		fmt.Fprintf(
			output,
			"  - blocked %s target=%s scope=%s sequence=%q: %s\n",
			meaning,
			decision.Target(),
			decision.Scope(),
			decision.SequenceID(),
			detail,
		)
	}
}

func printVerboseRelationOrder(
	output io.Writer,
	decision reconcile.RelationOrderDecision,
) {
	fmt.Fprintf(
		output,
		"  - kind=%s target=%s scope=%s class=%q sequence=%q runtime_meaning=%s authority=%q revision=%q constraint_fingerprint=%q reason=%s detail=%q desired=%q observed=%q missing=%q foreign_rows=%d risks=%q blocks_ordinary_apply=%t requires_mutation=%t\n",
		decision.Kind(),
		decision.Target(),
		decision.Scope(),
		decision.ClassID(),
		decision.SequenceID(),
		decision.RuntimeMeaning(),
		decision.Authority(),
		decision.Revision(),
		decision.ConstraintFingerprint(),
		decision.Reason(),
		decision.Detail(),
		relationOrderMemberIdentities(decision.DesiredMembers()),
		relationOrderMemberIdentities(decision.ObservedMembers()),
		relationOrderMemberIdentities(decision.MissingMembers()),
		decision.ForeignRowCount(),
		relationOrderRiskSummaries(decision),
		decision.BlocksOrdinaryApply(),
		decision.RequiresMutation(),
	)
}

func relationOrderMeaning(meaning hostrelation.RuntimeMeaning) string {
	switch meaning {
	case hostrelation.RuntimePrecedence:
		return "runtime extension precedence"
	case hostrelation.ConfigOrderOnly:
		return "extension config order"
	default:
		return "extension order"
	}
}

func relationOrderMemberIdentities(
	members []hostrelation.RelationOrderMember,
) []string {
	result := make([]string, 0, len(members))
	for _, member := range members {
		result = append(
			result,
			topologySubjectString(member.Subject())+"="+string(member.HostLoadIdentity()),
		)
	}
	return result
}

func relationOrderRiskSummaries(decision reconcile.RelationOrderDecision) []string {
	result := make([]string, 0, len(decision.PrecedenceChanges()))
	for _, change := range decision.PrecedenceChanges() {
		result = append(
			result,
			foreignPrecedenceChangeRisk+":"+
				topologySubjectString(change.ManagedSubject())+":"+
				string(change.ForeignIdentity()),
		)
	}
	return result
}

func topologySubjectString(subject topology.SubjectID) string {
	rendered := planJSONSubjectFor(subject)
	if rendered == nil {
		return ""
	}
	return subjectString(*rendered)
}
