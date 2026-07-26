package clipresent

import (
	"fmt"
	"io"
	"time"

	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	assurancepostcondition "github.com/isty2e/daem/internal/assurance/postcondition"
	"github.com/isty2e/daem/internal/topology"
)

const (
	hostRouteAttemptEvidenceKind = "host_route_attempt_diagnostics"
	hostRouteAttemptAuthority    = "history_only"
)

var hostRouteAttemptNonClaims = []string{
	"exact_artifact_replay",
	"current_contribution_inventory",
	"runtime_readiness",
	"tool_inventory",
	"auth_trust_state",
	"package_cache_convergence",
	"carrier_removal",
	"destructive_cleanup",
	"future_skip_authority",
}

type hostRouteAttemptJSON struct {
	EvidenceKind             string                           `json:"evidence_kind"`
	Authority                string                           `json:"authority"`
	Subject                  planJSONSubject                  `json:"subject"`
	Target                   string                           `json:"target"`
	Scope                    string                           `json:"scope"`
	Operation                string                           `json:"operation"`
	RouteID                  string                           `json:"route_id"`
	RouteRequestHash         string                           `json:"route_request_hash"`
	ObservedAt               string                           `json:"observed_at"`
	ResultClass              string                           `json:"result_class"`
	Reason                   string                           `json:"reason"`
	AttemptObserved          bool                             `json:"attempt_observed"`
	AttemptReason            string                           `json:"attempt_reason,omitempty"`
	Observation              string                           `json:"observation"`
	Postcondition            string                           `json:"postcondition"`
	EffectPostconditions     []effectPostconditionSummaryJSON `json:"effect_postconditions"`
	ExitCode                 *int                             `json:"exit_code,omitempty"`
	TimedOut                 bool                             `json:"timed_out,omitempty"`
	Redacted                 bool                             `json:"redacted,omitempty"`
	GrantsApplySkipAuthority bool                             `json:"grants_apply_skip_authority"`
	NonClaims                []string                         `json:"non_claims"`
}

type effectPostconditionSummaryJSON struct {
	Requirement string `json:"requirement"`
	State       string `json:"state"`
}

// PrintHostRouteAttemptsWithOptions writes bounded host-route attempt diagnostics without
// implying install convergence, package-cache ownership, readiness, or future
// skip authority.
func PrintHostRouteAttemptsWithOptions(output io.Writer, attempts []durableattempt.HostRouteAttempt, options HumanOptions) error {
	if len(attempts) == 0 {
		return nil
	}
	fmt.Fprintf(output, "host route attempts: %d history-only diagnostics\n", len(attempts))
	for _, attempt := range attempts {
		if !options.Verbose {
			fmt.Fprintf(output, "  - last host %s attempt subject=%q target=%s scope=%s: %s; %s", attempt.Operation(), hostRouteAttemptSubjectString(attempt.Subject()), attempt.Target(), attempt.Scope(), attempt.ObservationSummary(), attempt.PostconditionSummary())
			printEffectPostconditionSummaries(output, attempt.EffectPostconditions())
			fmt.Fprintln(output)
			fmt.Fprintln(output, "    host may retain packages, caches, credentials, trust data, or logs")
			continue
		}
		fmt.Fprintf(
			output,
			"  - evidence=%s authority=%s subject=%q target=%s scope=%s operation=%s route_id=%q route_request_hash=%q result_class=%s reason=%s attempt_observed=%t observation=%s postcondition=%s grants_apply_skip_authority=%t non_claims=%q",
			hostRouteAttemptEvidenceKind,
			hostRouteAttemptAuthority,
			hostRouteAttemptSubjectString(attempt.Subject()),
			attempt.Target(),
			attempt.Scope(),
			attempt.Operation(),
			attempt.RouteID(),
			attempt.RouteRequestHash(),
			attempt.ResultClass(),
			attempt.Reason(),
			attempt.AttemptObserved(),
			attempt.ObservationSummary(),
			attempt.PostconditionSummary(),
			false,
			hostRouteNonClaims(),
		)
		printEffectPostconditionSummaries(output, attempt.EffectPostconditions())
		if attempt.AttemptReason() != durableattempt.HostRouteAttemptReasonNone {
			fmt.Fprintf(output, " attempt_reason=%s", attempt.AttemptReason())
		}
		if exitCode, present := attempt.ExitCode(); present {
			fmt.Fprintf(output, " exit_code=%d", exitCode)
		}
		if attempt.TimedOut() {
			fmt.Fprint(output, " timed_out=true")
		}
		if attempt.Redacted() {
			fmt.Fprint(output, " redacted=true")
		}
		fmt.Fprintln(output)
	}
	return nil
}

func hostRouteJSONAttempts(attempts []durableattempt.HostRouteAttempt) []hostRouteAttemptJSON {
	result := make([]hostRouteAttemptJSON, 0, len(attempts))
	for _, attempt := range attempts {
		var exitCode *int
		if value, present := attempt.ExitCode(); present {
			exitCode = &value
		}
		row := hostRouteAttemptJSON{
			EvidenceKind:             hostRouteAttemptEvidenceKind,
			Authority:                hostRouteAttemptAuthority,
			Subject:                  hostRouteAttemptSubjectJSON(attempt.Subject()),
			Target:                   string(attempt.Target()),
			Scope:                    string(attempt.Scope()),
			Operation:                string(attempt.Operation()),
			RouteID:                  attempt.RouteID(),
			RouteRequestHash:         attempt.RouteRequestHash(),
			ObservedAt:               attempt.ObservedAt().Format(time.RFC3339Nano),
			ResultClass:              string(attempt.ResultClass()),
			Reason:                   string(attempt.Reason()),
			AttemptObserved:          attempt.AttemptObserved(),
			AttemptReason:            string(attempt.AttemptReason()),
			Observation:              string(attempt.ObservationSummary()),
			Postcondition:            string(attempt.PostconditionSummary()),
			EffectPostconditions:     effectPostconditionSummariesJSON(attempt.EffectPostconditions()),
			ExitCode:                 exitCode,
			TimedOut:                 attempt.TimedOut(),
			Redacted:                 attempt.Redacted(),
			GrantsApplySkipAuthority: false,
			NonClaims:                hostRouteNonClaims(),
		}
		result = append(result, row)
	}
	return result
}

func printEffectPostconditionSummaries(
	output io.Writer,
	set assurancepostcondition.SummarySet,
) {
	summaries := set.Summaries()
	if len(summaries) == 0 {
		return
	}
	fmt.Fprintf(output, " effect_postconditions=%q", effectPostconditionSummaryStrings(summaries))
}

func effectPostconditionSummaryStrings(
	summaries []assurancepostcondition.Summary,
) []string {
	values := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		values = append(
			values,
			fmt.Sprintf("%s=%s", summary.Requirement(), summary.State()),
		)
	}
	return values
}

func effectPostconditionSummariesJSON(
	set assurancepostcondition.SummarySet,
) []effectPostconditionSummaryJSON {
	summaries := set.Summaries()
	values := make([]effectPostconditionSummaryJSON, 0, len(summaries))
	for _, summary := range summaries {
		values = append(values, effectPostconditionSummaryJSON{
			Requirement: string(summary.Requirement()),
			State:       string(summary.State()),
		})
	}
	return values
}

func hostRouteNonClaims() []string {
	return append([]string(nil), hostRouteAttemptNonClaims...)
}

func hostRouteAttemptSubjectJSON(subject topology.SubjectID) planJSONSubject {
	return planJSONSubject{
		Kind:      string(subject.Kind()),
		Namespace: subject.Namespace(),
		Name:      subject.Key(),
	}
}

func hostRouteAttemptSubjectString(subject topology.SubjectID) string {
	return subjectString(hostRouteAttemptSubjectJSON(subject))
}
