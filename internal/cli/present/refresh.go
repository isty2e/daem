package clipresent

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	refreshworkflow "github.com/isty2e/daem/internal/workflow/refresh"
)

const refreshJSONSchemaVersion = 2

// RefreshReport is the schema-versioned public projection of one explicit
// carrier refresh plan or result.
type RefreshReport struct {
	SchemaVersion int               `json:"schema_version"`
	Command       string            `json:"command"`
	Mode          string            `json:"mode"`
	Selection     RefreshSelection  `json:"selection"`
	Route         RefreshRoute      `json:"route"`
	Disclosure    RefreshDisclosure `json:"disclosure"`
	Result        RefreshResult     `json:"result"`
	HasErrors     bool              `json:"has_errors"`
}

type RefreshSelection struct {
	ID            string `json:"id"`
	Target        string `json:"target"`
	Scope         string `json:"scope"`
	CarrierFamily string `json:"carrier_family"`
}

type RefreshRoute struct {
	Operation              string `json:"operation"`
	RouteID                string `json:"route_id"`
	AdapterContractVersion string `json:"adapter_contract_version"`
	RequestHash            string `json:"request_hash"`
	ExecutionSubject       string `json:"execution_subject"`
	ObservationPosture     string `json:"observation_posture"`
}

type RefreshDisclosure struct {
	InvocationKind        string   `json:"invocation_kind"`
	Command               string   `json:"command"`
	Args                  []string `json:"args"`
	EnvNames              []string `json:"env_names"`
	CWDPolicy             string   `json:"cwd_policy"`
	TimeoutSeconds        int      `json:"timeout_seconds"`
	EffectClasses         []string `json:"effect_classes"`
	RetainedEffectClasses []string `json:"retained_effect_classes"`
	NonClaims             []string `json:"non_claims"`
}

type RefreshResult struct {
	Class          string                 `json:"class"`
	ReasonCode     string                 `json:"reason_code,omitempty"`
	Detail         string                 `json:"detail"`
	Attempted      bool                   `json:"attempted"`
	ProcessOutcome *RefreshProcessOutcome `json:"process_outcome,omitempty"`
	Observation    *RefreshObservation    `json:"observation,omitempty"`
	AttemptHistory RefreshAttemptHistory  `json:"attempt_history"`
	Remediation    []string               `json:"remediation"`
}

type RefreshProcessOutcome struct {
	Started   bool   `json:"started"`
	Reason    string `json:"reason,omitempty"`
	ExitCode  *int   `json:"exit_code,omitempty"`
	TimedOut  bool   `json:"timed_out"`
	Cancelled bool   `json:"cancelled"`
	Signaled  bool   `json:"signaled"`
	Redacted  bool   `json:"redacted"`
}

type RefreshAttemptHistory struct {
	Persisted bool `json:"persisted"`
}

type RefreshObservation struct {
	State        string `json:"state"`
	Reason       string `json:"reason,omitempty"`
	Availability string `json:"availability"`
	Freshness    string `json:"freshness"`
}

// RefreshReportFrom projects one canonical workflow result without adding
// host output, filesystem paths, or secret values.
func RefreshReportFrom(result refreshworkflow.CommandResult) RefreshReport {
	var observation *RefreshObservation
	if result.Observation != nil {
		observation = &RefreshObservation{
			State:        string(result.Observation.State),
			Reason:       string(result.Observation.Reason),
			Availability: string(result.Observation.Availability),
			Freshness:    string(result.Observation.Freshness),
		}
	}
	return RefreshReport{
		SchemaVersion: refreshJSONSchemaVersion,
		Command:       "refresh extension",
		Mode:          string(result.Mode),
		Selection: RefreshSelection{
			ID:            result.Selection.ID,
			Target:        string(result.Selection.Target),
			Scope:         string(result.Selection.Scope),
			CarrierFamily: result.Selection.Carrier,
		},
		Route: RefreshRoute{
			Operation:              string(result.Route.Operation),
			RouteID:                result.Route.RouteID,
			AdapterContractVersion: result.Route.AdapterContractVersion,
			RequestHash:            result.Route.RequestHash,
			ExecutionSubject:       result.Route.ExecutionSubject,
			ObservationPosture:     string(result.Route.ObservationPosture),
		},
		Disclosure: RefreshDisclosure{
			InvocationKind:        result.Disclosure.Invocation.Kind,
			Command:               result.Disclosure.Invocation.Command,
			Args:                  append([]string{}, result.Disclosure.Invocation.Args...),
			EnvNames:              append([]string{}, result.Disclosure.Invocation.EnvNames...),
			CWDPolicy:             result.Disclosure.Invocation.CWDPolicy,
			TimeoutSeconds:        result.Disclosure.Invocation.TimeoutSeconds,
			EffectClasses:         append([]string{}, result.Disclosure.EffectClasses...),
			RetainedEffectClasses: append([]string{}, result.Disclosure.RetainedEffectClasses...),
			NonClaims:             append([]string{}, result.Disclosure.NonClaims...),
		},
		Result: RefreshResult{
			Class:          string(result.ResultClass),
			ReasonCode:     string(result.ReasonCode),
			Detail:         result.FailureDetail(),
			Attempted:      result.Attempted,
			ProcessOutcome: refreshProcessOutcome(result.ProcessOutcome),
			Observation:    observation,
			AttemptHistory: RefreshAttemptHistory{
				Persisted: result.AttemptHistory.Persisted,
			},
			Remediation: append([]string{}, result.Remediation...),
		},
		HasErrors: result.HasErrors(),
	}
}

// PrintRefreshReport writes one bounded human-readable refresh result.
func PrintRefreshReport(
	output io.Writer,
	report RefreshReport,
	options HumanOptions,
) {
	fmt.Fprintf(output, "refresh extension: %s\n", report.Mode)
	fmt.Fprintf(
		output,
		"  selection: id=%q target=%s scope=%s carrier=%s\n",
		report.Selection.ID,
		report.Selection.Target,
		report.Selection.Scope,
		report.Selection.CarrierFamily,
	)
	fmt.Fprintf(
		output,
		"  route: operation=%s subject=%q posture=%s\n",
		report.Route.Operation,
		report.Route.ExecutionSubject,
		report.Route.ObservationPosture,
	)
	if options.Verbose {
		fmt.Fprintf(
			output,
			"  route identity: id=%s adapter=%s request=%s\n",
			report.Route.RouteID,
			report.Route.AdapterContractVersion,
			report.Route.RequestHash,
		)
	}
	fmt.Fprintf(
		output,
		"  invocation: kind=%s command=%q args=%s env_names=%s cwd=%s timeout=%ds\n",
		report.Disclosure.InvocationKind,
		report.Disclosure.Command,
		quotedList(report.Disclosure.Args),
		quotedList(report.Disclosure.EnvNames),
		report.Disclosure.CWDPolicy,
		report.Disclosure.TimeoutSeconds,
	)
	printRefreshValues(output, "effects", report.Disclosure.EffectClasses)
	printRefreshValues(
		output,
		"retained effects",
		report.Disclosure.RetainedEffectClasses,
	)
	printRefreshValues(output, "non-claims", report.Disclosure.NonClaims)
	PrintRefreshOutcome(output, report, options)
}

// PrintRefreshOutcome writes only the post-plan result fields after a plan has
// already been disclosed in human output.
func PrintRefreshOutcome(
	output io.Writer,
	report RefreshReport,
	options HumanOptions,
) {
	fmt.Fprintf(
		output,
		"  result: class=%s attempted=%t",
		report.Result.Class,
		report.Result.Attempted,
	)
	if report.Result.ReasonCode != "" {
		fmt.Fprintf(output, " reason=%s", report.Result.ReasonCode)
	}
	fmt.Fprintln(output)
	if report.Result.ProcessOutcome != nil {
		outcome := report.Result.ProcessOutcome
		fmt.Fprintf(
			output,
			"  process: started=%t reason=%s timed_out=%t cancelled=%t signaled=%t redacted=%t",
			outcome.Started,
			outcome.Reason,
			outcome.TimedOut,
			outcome.Cancelled,
			outcome.Signaled,
			outcome.Redacted,
		)
		if outcome.ExitCode != nil {
			fmt.Fprintf(output, " exit_code=%d", *outcome.ExitCode)
		}
		fmt.Fprintln(output)
	}
	if report.Result.Observation != nil {
		observation := report.Result.Observation
		fmt.Fprintf(
			output,
			"  observation: state=%s availability=%s freshness=%s",
			observation.State,
			observation.Availability,
			observation.Freshness,
		)
		if options.Verbose && observation.Reason != "" {
			fmt.Fprintf(output, " reason=%s", observation.Reason)
		}
		fmt.Fprintln(output)
	}
	fmt.Fprintf(
		output,
		"  attempt history: persisted=%t\n",
		report.Result.AttemptHistory.Persisted,
	)
	for _, remediation := range report.Result.Remediation {
		fmt.Fprintf(output, "  next: %s\n", remediation)
	}
}

// PrintRefreshJSON writes one JSON document with exactly the frozen top-level
// refresh fields.
func PrintRefreshJSON(output io.Writer, report RefreshReport) error {
	report.SchemaVersion = refreshJSONSchemaVersion
	report.Command = "refresh extension"
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func printRefreshValues(output io.Writer, label string, values []string) {
	fmt.Fprintf(output, "  %s: %s\n", label, strings.Join(values, ", "))
}

func refreshProcessOutcome(
	outcome *refreshworkflow.ProcessOutcome,
) *RefreshProcessOutcome {
	if outcome == nil {
		return nil
	}
	cloned := &RefreshProcessOutcome{
		Started:   outcome.Started,
		Reason:    string(outcome.Reason),
		TimedOut:  outcome.TimedOut,
		Cancelled: outcome.Cancelled,
		Signaled:  outcome.Signaled,
		Redacted:  outcome.Redacted,
	}
	if outcome.ExitCode != nil {
		exitCode := *outcome.ExitCode
		cloned.ExitCode = &exitCode
	}
	return cloned
}
