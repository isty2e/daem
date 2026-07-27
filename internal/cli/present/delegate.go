package clipresent

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/topology"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

// DelegateAttemptInput is the presentation projection of one history-only
// delegate attempt result.
type DelegateAttemptInput struct {
	Attempt       delegate.AttemptRecord
	Observation   observerelation.ObservationSummary
	Postcondition observerelation.PostconditionSummary
}

// DelegateAttemptInputsFrom projects canonical apply results into bounded presentation inputs.
func DelegateAttemptInputsFrom(results []applyworkflow.DelegateAttemptResult) []DelegateAttemptInput {
	inputs := make([]DelegateAttemptInput, 0, len(results))
	for _, result := range results {
		inputs = append(inputs, DelegateAttemptInput{
			Attempt:       result.Attempt(),
			Observation:   result.ObservationSummary(),
			Postcondition: result.PostconditionSummary(),
		})
	}
	return inputs
}

type delegateActionJSON struct {
	Subject          planJSONSubject          `json:"subject"`
	Target           string                   `json:"target"`
	Scope            string                   `json:"scope"`
	Status           string                   `json:"status"`
	PolicyOutcome    string                   `json:"policy_outcome"`
	SchedulesAttempt bool                     `json:"schedules_attempt"`
	PlanIdentityKey  string                   `json:"plan_identity_key"`
	RunnerKind       string                   `json:"runner_kind"`
	Command          string                   `json:"command"`
	Args             []string                 `json:"args"`
	EnvBindings      []delegateEnvBindingJSON `json:"env_bindings"`
	Environment      string                   `json:"environment"`
	Package          *delegatePackageJSON     `json:"package,omitempty"`
	PinPolicy        string                   `json:"pin_policy"`
	TimeoutSeconds   int                      `json:"timeout_seconds"`
	Risks            []delegateRiskJSON       `json:"risks"`
	Dependencies     []delegateDependencyJSON `json:"dependencies"`
}

type delegateEnvBindingJSON struct {
	Name       string `json:"name"`
	SourceName string `json:"source_name"`
}

type delegatePackageJSON struct {
	Ecosystem string `json:"ecosystem"`
	Name      string `json:"name"`
	Selector  string `json:"selector,omitempty"`
}

type delegateRiskJSON struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Subject  string `json:"subject,omitempty"`
}

type delegateDependencyJSON struct {
	Kind    string          `json:"kind"`
	Subject planJSONSubject `json:"subject"`
}

type delegateAttemptJSON struct {
	EvidenceKind        string          `json:"evidence_kind"`
	Authority           string          `json:"authority"`
	Subject             planJSONSubject `json:"subject"`
	Target              string          `json:"target"`
	Scope               string          `json:"scope"`
	PlanIdentityKey     string          `json:"plan_identity_key"`
	Status              string          `json:"status"`
	Reason              string          `json:"reason"`
	Observation         string          `json:"observation"`
	Postcondition       string          `json:"postcondition"`
	ExitCode            *int            `json:"exit_code,omitempty"`
	TimedOut            bool            `json:"timed_out,omitempty"`
	OutputObserved      bool            `json:"output_observed,omitempty"`
	OutputTruncated     bool            `json:"output_truncated,omitempty"`
	RunnerErrorObserved bool            `json:"runner_error_observed,omitempty"`
	Redacted            bool            `json:"redacted,omitempty"`
}

const (
	delegateAttemptEvidenceKind = "last_attempt_diagnostics"
	delegateAttemptAuthority    = "history_only"
)

// PrintDelegateActions writes planned delegate disclosure rows without implying execution.
func PrintDelegateActionsWithOptions(output io.Writer, actions []reconcile.DelegateAction, options HumanOptions) {
	if len(actions) == 0 {
		return
	}
	fmt.Fprintf(output, "delegate actions: %d plans\n", len(actions))
	for _, action := range actions {
		plan := action.Plan()
		command := plan.Command()
		env := plan.Env().Bindings()
		if !options.Verbose {
			fmt.Fprintf(output, "  - run MCP command attempt subject=%q target=%s scope=%s command=%q args=%s env_bindings=%s environment=%s\n", subjectStringFromID(action.Subject()), action.Target(), action.Scope(), command.Executable(), quotedList(command.Args()), delegateEnvBindingList(env), subprocess.ChildEnvironmentInheritancePolicy)
			fmt.Fprintln(output, "    package, cache, server, auth, and future readiness are not guaranteed")
			continue
		}
		fmt.Fprintf(
			output,
			"  - subject=%q target=%s scope=%s status=%s outcome=%s schedules_attempt=%t runner=%s command=%q args=%s env_bindings=%s environment=%s pin=%s timeout=%s",
			subjectStringFromID(action.Subject()),
			action.Target(),
			action.Scope(),
			action.Disposition(),
			action.PolicyOutcome(),
			action.SchedulesAttempt(),
			plan.Runner().Kind(),
			command.Executable(),
			quotedList(command.Args()),
			delegateEnvBindingList(env),
			subprocess.ChildEnvironmentInheritancePolicy,
			plan.PinPolicy(),
			delegateTimeoutPolicy(),
		)
		if packageRef, present := plan.PackageRef(); present {
			fmt.Fprintf(output, " package=%q", delegatePackageString(packageRef))
		}
		if risks := delegateRiskList(action.Risks()); risks != "" {
			fmt.Fprintf(output, " risks=%s", risks)
		}
		if dependencies := delegateDependencyList(action.Dependencies()); dependencies != "" {
			fmt.Fprintf(output, " dependencies=%s", dependencies)
		}
		fmt.Fprintln(output)
	}
}

// PrintDelegateAttempts writes sanitized attempt summaries without dumping command output.
func PrintDelegateAttemptsWithOptions(output io.Writer, results []DelegateAttemptInput, options HumanOptions) {
	if len(results) == 0 {
		return
	}
	fmt.Fprintf(output, "delegate attempts: %d history-only diagnostics\n", len(results))
	for _, result := range results {
		attempt := result.Attempt
		if !options.Verbose {
			fmt.Fprintf(output, "  - subject=%q target=%s scope=%s: %s; %s\n", subjectString(subjectIDJSON(attempt.Subject())), attempt.Target(), attempt.Scope(), result.Observation, result.Postcondition)
			continue
		}
		fmt.Fprintf(
			output,
			"  - evidence=%s authority=%s subject=%q target=%s scope=%s status=%s reason=%s observation=%s postcondition=%s",
			delegateAttemptEvidenceKind,
			delegateAttemptAuthority,
			subjectString(subjectIDJSON(attempt.Subject())),
			attempt.Target(),
			attempt.Scope(),
			attempt.Status(),
			delegateAttemptReason(attempt.Reason()),
			result.Observation,
			result.Postcondition,
		)
		if exitCode, ok := attempt.ExitCode(); ok {
			fmt.Fprintf(output, " exit_code=%d", exitCode)
		}
		if attempt.TimedOut() {
			fmt.Fprint(output, " timed_out=true")
		}
		if attempt.Stdout() != "" || attempt.Stderr() != "" {
			fmt.Fprint(output, " output_observed=true")
		}
		if attempt.StdoutTruncated() || attempt.StderrTruncated() {
			fmt.Fprint(output, " output_truncated=true")
		}
		if attempt.ErrorDetail() != "" {
			fmt.Fprint(output, " runner_error_observed=true")
		}
		if attempt.Redacted() {
			fmt.Fprint(output, " redacted=true")
		}
		fmt.Fprintln(output)
	}
}

func delegateJSONActions(actions []reconcile.DelegateAction) []delegateActionJSON {
	result := make([]delegateActionJSON, 0, len(actions))
	for _, action := range actions {
		plan := action.Plan()
		command := plan.Command()
		packageRef, hasPackage := plan.PackageRef()
		result = append(result, delegateActionJSON{
			Subject:          subjectIDJSON(action.Subject()),
			Target:           string(action.Target()),
			Scope:            string(action.Scope()),
			Status:           string(action.Disposition()),
			PolicyOutcome:    string(action.PolicyOutcome()),
			SchedulesAttempt: action.SchedulesAttempt(),
			PlanIdentityKey:  plan.IdentityKey(),
			RunnerKind:       string(plan.Runner().Kind()),
			Command:          command.Executable(),
			Args:             command.Args(),
			EnvBindings:      delegateJSONEnvBindings(plan.Env().Bindings()),
			Environment:      subprocess.ChildEnvironmentInheritancePolicy,
			Package:          delegateJSONPackage(packageRef, hasPackage),
			PinPolicy:        string(plan.PinPolicy()),
			TimeoutSeconds:   int(delegate.DefaultTimeout / time.Second),
			Risks:            delegateJSONRisks(action.Risks()),
			Dependencies:     delegateJSONDependencies(action.Dependencies()),
		})
	}
	return result
}

func delegateJSONEnvBindings(values []realizationdelegate.EnvBinding) []delegateEnvBindingJSON {
	result := make([]delegateEnvBindingJSON, 0, len(values))
	for _, value := range values {
		result = append(result, delegateEnvBindingJSON{
			Name:       value.Name(),
			SourceName: value.SourceName(),
		})
	}
	return result
}

func delegateJSONAttempts(results []DelegateAttemptInput) []delegateAttemptJSON {
	rows := make([]delegateAttemptJSON, 0, len(results))
	for _, result := range results {
		attempt := result.Attempt
		row := delegateAttemptJSON{
			EvidenceKind:        delegateAttemptEvidenceKind,
			Authority:           delegateAttemptAuthority,
			Subject:             subjectIDJSON(attempt.Subject()),
			Target:              attempt.Target(),
			Scope:               attempt.Scope(),
			PlanIdentityKey:     attempt.IdentityKey(),
			Status:              string(attempt.Status()),
			Reason:              delegateAttemptReason(attempt.Reason()),
			Observation:         string(result.Observation),
			Postcondition:       string(result.Postcondition),
			TimedOut:            attempt.TimedOut(),
			OutputObserved:      attempt.Stdout() != "" || attempt.Stderr() != "",
			OutputTruncated:     attempt.StdoutTruncated() || attempt.StderrTruncated(),
			RunnerErrorObserved: attempt.ErrorDetail() != "",
			Redacted:            attempt.Redacted(),
		}
		if exitCode, ok := attempt.ExitCode(); ok {
			row.ExitCode = &exitCode
		}
		rows = append(rows, row)
	}
	return rows
}

func delegateJSONPackage(
	pkg realizationdelegate.PackageRef,
	present bool,
) *delegatePackageJSON {
	if !present {
		return nil
	}
	return &delegatePackageJSON{
		Ecosystem: string(pkg.Ecosystem()),
		Name:      pkg.Name(),
		Selector:  pkg.Selector(),
	}
}

func delegateJSONRisks(risks []reconcile.DelegateRisk) []delegateRiskJSON {
	result := make([]delegateRiskJSON, 0, len(risks))
	for _, risk := range risks {
		result = append(result, delegateRiskJSON{
			Code:     string(risk.Code),
			Severity: string(risk.Severity),
			Subject:  risk.Subject,
		})
	}
	return result
}

func delegateJSONDependencies(dependencies []reconcile.DelegateDependency) []delegateDependencyJSON {
	result := make([]delegateDependencyJSON, 0, len(dependencies))
	for _, dependency := range dependencies {
		result = append(result, delegateDependencyJSON{
			Kind:    string(dependency.Kind),
			Subject: subjectIDJSON(dependency.Subject),
		})
	}
	return result
}

func subjectIDJSON(subject topology.SubjectID) planJSONSubject {
	return planJSONSubject{
		Kind:      string(subject.Kind()),
		Namespace: subject.Namespace(),
		Name:      subject.Key(),
	}
}

func subjectStringFromID(subject topology.SubjectID) string {
	return subject.String()
}

func delegateAttemptReason(reason delegate.Reason) string {
	if reason == "" {
		return "none"
	}
	return string(reason)
}

func delegateTimeoutPolicy() string {
	return delegate.DefaultTimeout.String()
}

func delegatePackageString(pkg realizationdelegate.PackageRef) string {
	value := string(pkg.Ecosystem()) + ":" + pkg.Name()
	if pkg.Selector() != "" {
		value += "@" + pkg.Selector()
	}
	return value
}

func delegateRiskList(risks []reconcile.DelegateRisk) string {
	if len(risks) == 0 {
		return ""
	}
	values := make([]string, 0, len(risks))
	for _, risk := range risks {
		value := string(risk.Severity) + ":" + string(risk.Code)
		if risk.Subject != "" {
			value += "(" + strconv.Quote(risk.Subject) + ")"
		}
		values = append(values, value)
	}
	return strings.Join(values, ",")
}

func delegateDependencyList(dependencies []reconcile.DelegateDependency) string {
	if len(dependencies) == 0 {
		return ""
	}
	values := make([]string, 0, len(dependencies))
	for _, dependency := range dependencies {
		values = append(values, string(dependency.Kind)+":"+strconv.Quote(subjectStringFromID(dependency.Subject)))
	}
	return strings.Join(values, ",")
}

func quotedList(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, strconv.Quote(value))
	}
	return "[" + strings.Join(quoted, ",") + "]"
}

func plainList(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	return "[" + strings.Join(values, ",") + "]"
}

func delegateEnvBindingList(values []realizationdelegate.EnvBinding) string {
	items := make([]string, 0, len(values))
	for _, value := range values {
		items = append(items, value.Name()+"<-"+value.SourceName())
	}
	return plainList(items)
}
