package clipresent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/topology"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

const (
	foreignPrecedenceChangeRisk             = "foreign_precedence_change"
	maxRelationOrderIdentityDisclosureBytes = 256
)

var relationOrderAssignmentPattern = regexp.MustCompile(
	`(?i)(^|[:/?#&;,\s])[a-z][a-z0-9_-]{0,63}\s*=`,
)

var relationOrderSecretColonPattern = regexp.MustCompile(
	`(?i)(^|[:/?#&;,\s])(token|secret|password|auth|authorization|credential|api[-_]?key|access[-_]?key|client[-_]?secret|private[-_]?key)\s*:`,
)

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
	Code                    string           `json:"code"`
	ManagedSubject          *planJSONSubject `json:"managed_subject,omitempty"`
	ForeignIdentity         string           `json:"foreign_identity"`
	ForeignIdentityRedacted bool             `json:"foreign_identity_redacted,omitempty"`
	ManagedWasBefore        bool             `json:"managed_was_before"`
	ManagedWillBeBefore     bool             `json:"managed_will_be_before"`
}

type relationOrderResultJSON struct {
	Target     string `json:"target"`
	Scope      string `json:"scope"`
	ClassID    string `json:"class_id"`
	SequenceID string `json:"sequence_id"`
	Outcome    string `json:"outcome"`
	Changed    bool   `json:"changed"`
	Detail     string `json:"detail,omitempty"`
}

func relationOrderResultJSONRows(
	results []applyworkflow.RelationOrderExecutionResult,
) []relationOrderResultJSON {
	rows := make([]relationOrderResultJSON, 0, len(results))
	for _, result := range results {
		rows = append(rows, relationOrderResultJSON{
			Target:     string(result.Target()),
			Scope:      string(result.Scope()),
			ClassID:    string(result.ClassID()),
			SequenceID: string(result.SequenceID()),
			Outcome:    string(result.Outcome()),
			Changed:    result.Changed(),
			Detail:     result.Detail(),
		})
	}
	return rows
}

func relationOrderJSONActions(
	decisions []reconcile.RelationOrderDecision,
) []relationOrderJSON {
	result := make([]relationOrderJSON, 0, len(decisions))
	for _, decision := range decisions {
		precedenceChanges := decision.PrecedenceChanges()
		risks := make([]relationOrderRiskJSON, 0, len(precedenceChanges))
		for _, change := range precedenceChanges {
			foreignIdentity := relationOrderIdentityDisclosureFor(
				string(change.ForeignIdentity()),
			)
			risks = append(risks, relationOrderRiskJSON{
				Code:                    foreignPrecedenceChangeRisk,
				ManagedSubject:          planJSONSubjectFor(change.ManagedSubject()),
				ForeignIdentity:         foreignIdentity.Value(),
				ForeignIdentityRedacted: foreignIdentity.Redacted(),
				ManagedWasBefore:        change.ManagedWasBefore(),
				ManagedWillBeBefore:     change.ManagedWillBeBefore(),
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

// PrintRelationOrderRiskDeltasWithOptions discloses only precedence changes
// newly introduced after the original apply authorization.
func PrintRelationOrderRiskDeltasWithOptions(
	output io.Writer,
	deltas []applyworkflow.RelationOrderRiskDelta,
	options HumanOptions,
) {
	if len(deltas) == 0 {
		return
	}
	fmt.Fprintf(output, "extension order risk delta: %d sequences\n", len(deltas))
	for _, delta := range deltas {
		if options.Verbose {
			fmt.Fprintf(
				output,
				"  - target=%s scope=%s class=%q sequence=%q runtime_meaning=%s\n",
				delta.Target(),
				delta.Scope(),
				delta.ClassID(),
				delta.SequenceID(),
				delta.RuntimeMeaning(),
			)
		} else {
			fmt.Fprintf(
				output,
				"  - new %s risks target=%s scope=%s sequence=%q\n",
				relationOrderMeaning(delta.RuntimeMeaning()),
				delta.Target(),
				delta.Scope(),
				delta.SequenceID(),
			)
		}
		printRelationOrderRiskChanges(
			output,
			delta.PrecedenceChanges(),
			"adds",
		)
	}
}

// PrintRelationOrderResults writes final post-carrier outcomes per physical
// sequence, including honest partial failure and not-attempted states.
func PrintRelationOrderResults(
	output io.Writer,
	results []applyworkflow.RelationOrderExecutionResult,
) {
	if len(results) == 0 {
		return
	}
	fmt.Fprintf(output, "extension order results: %d sequences\n", len(results))
	for _, result := range results {
		fmt.Fprintf(
			output,
			"  - %s target=%s scope=%s sequence=%q",
			strings.ReplaceAll(string(result.Outcome()), "_", " "),
			result.Target(),
			result.Scope(),
			result.SequenceID(),
		)
		if result.Changed() {
			fmt.Fprint(output, " changed=true")
		}
		if result.Detail() != "" {
			fmt.Fprintf(output, ": %s", Escape(result.Detail()))
		}
		fmt.Fprintln(output)
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
	printRelationOrderRiskDetails(output, decision)
}

func printVerboseRelationOrder(
	output io.Writer,
	decision reconcile.RelationOrderDecision,
) {
	fmt.Fprintf(
		output,
		"  - kind=%s target=%s scope=%s class=%q sequence=%q runtime_meaning=%s authority=%q revision=%q constraint_fingerprint=%q reason=%s detail=%q desired=%q observed=%q missing=%q foreign_rows=%d blocks_ordinary_apply=%t requires_mutation=%t\n",
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
		decision.BlocksOrdinaryApply(),
		decision.RequiresMutation(),
	)
	printRelationOrderRiskDetails(output, decision)
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

func printRelationOrderRiskDetails(
	output io.Writer,
	decision reconcile.RelationOrderDecision,
) {
	printRelationOrderRiskChanges(
		output,
		decision.PrecedenceChanges(),
		"includes",
	)
}

func printRelationOrderRiskChanges(
	output io.Writer,
	changes []observerelation.PrecedenceChange,
	verb string,
) {
	if len(changes) == 0 {
		return
	}
	fmt.Fprintf(
		output,
		"    %s %d managed/foreign precedence changes:\n",
		verb,
		len(changes),
	)
	for _, change := range changes {
		foreignIdentity := relationOrderIdentityDisclosureFor(
			string(change.ForeignIdentity()),
		)
		fmt.Fprintf(
			output,
			"      - managed=%q foreign=%q managed_position=%s -> %s\n",
			Escape(topologySubjectString(change.ManagedSubject())),
			Escape(foreignIdentity.Value()),
			relationOrderPosition(change.ManagedWasBefore()),
			relationOrderPosition(change.ManagedWillBeBefore()),
		)
	}
}

type relationOrderIdentityDisclosure struct {
	value    string
	redacted bool
}

func relationOrderIdentityDisclosureFor(
	value string,
) relationOrderIdentityDisclosure {
	if !relationOrderIdentityRequiresRedaction(value) {
		return relationOrderIdentityDisclosure{value: value}
	}
	digest := sha256.Sum256([]byte(value))
	return relationOrderIdentityDisclosure{
		value:    "redacted:sha256:" + hex.EncodeToString(digest[:]),
		redacted: true,
	}
}

func (disclosure relationOrderIdentityDisclosure) Value() string {
	return disclosure.value
}

func (disclosure relationOrderIdentityDisclosure) Redacted() bool {
	return disclosure.redacted
}

func relationOrderIdentityRequiresRedaction(value string) bool {
	if len(value) > maxRelationOrderIdentityDisclosureBytes {
		return true
	}
	lowerValue := strings.ToLower(value)
	if filepath.IsAbs(value) ||
		strings.HasPrefix(lowerValue, "file:") ||
		strings.Contains(lowerValue, ":file:") ||
		strings.HasPrefix(lowerValue, "local:") ||
		hasLocalPathPrefix(value) ||
		isWindowsAbsolutePath(value) ||
		strings.Contains(value, "?") ||
		hasURLUserInfo(value) ||
		relationOrderAssignmentPattern.MatchString(value) ||
		relationOrderSecretColonPattern.MatchString(value) {
		return true
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return strings.Contains(value, "://")
	}
	return parsed.User != nil ||
		parsed.RawQuery != "" ||
		strings.Contains(parsed.Fragment, "=") ||
		strings.Contains(strings.ToLower(value), "%3d")
}

func hasLocalPathPrefix(value string) bool {
	for _, prefix := range []string{
		"./", "../", "~/", `.\`, `..\`, `~\`, `\\`,
	} {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func hasURLUserInfo(value string) bool {
	remaining := value
	for {
		marker := strings.Index(remaining, "://")
		if marker < 0 {
			return false
		}
		authority := remaining[marker+3:]
		if end := strings.IndexAny(authority, "/?#"); end >= 0 {
			authority = authority[:end]
		}
		if strings.Contains(authority, "@") {
			return true
		}
		remaining = remaining[marker+3:]
	}
}

func isWindowsAbsolutePath(value string) bool {
	if len(value) < 3 || value[1] != ':' ||
		(value[2] != '/' && value[2] != '\\') {
		return false
	}
	drive := value[0]
	return (drive >= 'A' && drive <= 'Z') || (drive >= 'a' && drive <= 'z')
}

func relationOrderPosition(managedBeforeForeign bool) string {
	if managedBeforeForeign {
		return "before"
	}
	return "after"
}

func topologySubjectString(subject topology.SubjectID) string {
	rendered := planJSONSubjectFor(subject)
	if rendered == nil {
		return ""
	}
	return subjectString(*rendered)
}
