package apply

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"

	"github.com/isty2e/daem/internal/effect/execute/hostroute"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/topology"
)

type applyRoutePreflightKind uint8

const (
	applyRoutePreflightNotApplicable applyRoutePreflightKind = iota
	applyRoutePreflightAccepted
	applyRoutePreflightRejected
	applyRoutePreflightOperationalFailure
)

const maximumApplyRoutePreflightDiagnosticRunes = 1024

type applyRoutePreflight struct {
	kind                   applyRoutePreflightKind
	command                hostroute.Command
	rejection              *hostroute.ValidationError
	operationalType        string
	operationalDiagnostic  string
	operationalFingerprint [sha256.Size]byte
}

func acceptedApplyRoutePreflight(command hostroute.Command) applyRoutePreflight {
	return applyRoutePreflight{kind: applyRoutePreflightAccepted, command: command}
}

func rejectedApplyRoutePreflight(err error) applyRoutePreflight {
	var rejection *hostroute.ValidationError
	if errors.As(err, &rejection) {
		return applyRoutePreflight{kind: applyRoutePreflightRejected, rejection: rejection}
	}
	diagnostic := err.Error()
	fingerprint := sha256.Sum256([]byte(diagnostic))
	diagnosticRunes := []rune(diagnostic)
	if len(diagnosticRunes) > maximumApplyRoutePreflightDiagnosticRunes {
		diagnostic = string(diagnosticRunes[:maximumApplyRoutePreflightDiagnosticRunes])
	}
	return applyRoutePreflight{
		kind:                   applyRoutePreflightOperationalFailure,
		operationalType:        reflect.TypeOf(err).String(),
		operationalDiagnostic:  diagnostic,
		operationalFingerprint: fingerprint,
	}
}

func (preflight applyRoutePreflight) rejected() bool {
	return preflight.kind == applyRoutePreflightRejected ||
		preflight.kind == applyRoutePreflightOperationalFailure
}

func (preflight applyRoutePreflight) equal(other applyRoutePreflight) bool {
	if preflight.kind != other.kind {
		return false
	}
	switch preflight.kind {
	case applyRoutePreflightNotApplicable:
		return true
	case applyRoutePreflightAccepted:
		return applyHostRouteCommandsEqual(preflight.command, other.command)
	case applyRoutePreflightRejected:
		return applyHostRouteValidationErrorsEqual(preflight.rejection, other.rejection)
	case applyRoutePreflightOperationalFailure:
		return preflight.operationalType == other.operationalType &&
			preflight.operationalFingerprint == other.operationalFingerprint
	default:
		return false
	}
}

type applyFinalRoutePlan struct {
	phaseEstablished        bool
	statefileInitiallyBound bool
	routes                  []applyRouteScheduleFact
	actionIndexes           map[mutation.OperationFingerprint][]int
	fingerprint             mutation.OperationFingerprint
	available               bool
}

func compileApplyFinalRoutePlan(
	routes []applyRouteScheduleFact,
	phaseEstablished bool,
	statefileInitiallyBound bool,
) (applyFinalRoutePlan, error) {
	retained := append([]applyRouteScheduleFact(nil), routes...)
	actionIndexes := make(map[mutation.OperationFingerprint][]int, len(retained))
	for index := range retained {
		fingerprint, err := applyRelationActionFingerprint(retained[index].action)
		if err != nil {
			return applyFinalRoutePlan{}, fmt.Errorf(
				"final host route action[%d] identity: %w",
				index,
				err,
			)
		}
		actionIndexes[fingerprint] = append(actionIndexes[fingerprint], index)
	}

	fingerprint, err := applyFinalRoutePlanFingerprint(
		retained,
		phaseEstablished,
		statefileInitiallyBound,
	)
	if err != nil {
		return applyFinalRoutePlan{}, err
	}
	return applyFinalRoutePlan{
		phaseEstablished:        phaseEstablished,
		statefileInitiallyBound: statefileInitiallyBound,
		routes:                  retained,
		actionIndexes:           actionIndexes,
		fingerprint:             fingerprint,
		available:               true,
	}, nil
}

func (plan applyFinalRoutePlan) equal(other applyFinalRoutePlan) bool {
	if plan.available != other.available {
		return false
	}
	if !plan.available {
		return true
	}
	if plan.phaseEstablished != other.phaseEstablished ||
		plan.statefileInitiallyBound != other.statefileInitiallyBound ||
		!plan.fingerprint.Equal(other.fingerprint) ||
		len(plan.routes) != len(other.routes) {
		return false
	}
	for index := range plan.routes {
		if !applyRouteScheduleFactsEqual(plan.routes[index], other.routes[index]) {
			return false
		}
	}
	return true
}

func bindApplyFinalRoutePlans(
	prepared applyContinuationPlan,
	current applyContinuationPlan,
) error {
	if !prepared.finalRoutePlan.available || !current.finalRoutePlan.available {
		return fmt.Errorf("final host route plan is unavailable")
	}
	if !prepared.finalRoutePlan.equal(current.finalRoutePlan) {
		return fmt.Errorf("prepared and current final host route plans differ")
	}
	return nil
}

func (plan applyFinalRoutePlan) routeFor(
	action reconcile.RelationAction,
) (applyRouteScheduleFact, error) {
	if !plan.available {
		return applyRouteScheduleFact{}, fmt.Errorf("final host route plan is unavailable")
	}
	fingerprint, err := applyRelationActionFingerprint(action)
	if err != nil {
		return applyRouteScheduleFact{}, fmt.Errorf("final host route action identity: %w", err)
	}
	indexes := plan.actionIndexes[fingerprint]
	matched := -1
	for _, index := range indexes {
		if !applyRelationActionsEqual(plan.routes[index].action, action) {
			continue
		}
		if matched >= 0 {
			return applyRouteScheduleFact{}, fmt.Errorf(
				"apply continuation final route action is ambiguous",
			)
		}
		matched = index
	}
	if matched < 0 {
		return applyRouteScheduleFact{}, fmt.Errorf("apply continuation final route is not scheduled")
	}
	return plan.routes[matched], nil
}

func applyRouteScheduleFactsEqual(left, right applyRouteScheduleFact) bool {
	return left.ref == right.ref &&
		left.work == right.work &&
		applyRelationActionsEqual(left.action, right.action) &&
		left.preflight.equal(right.preflight)
}

func applyRelationActionsEqual(left, right reconcile.RelationAction) bool {
	return reflect.DeepEqual(
		relationFingerprintRows([]reconcile.RelationAction{left}),
		relationFingerprintRows([]reconcile.RelationAction{right}),
	)
}

func applyRelationActionFingerprint(
	action reconcile.RelationAction,
) (mutation.OperationFingerprint, error) {
	canonical, err := json.Marshal(relationFingerprintRows([]reconcile.RelationAction{action})[0])
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf("marshal relation action identity: %w", err)
	}
	return mutation.NewOperationFingerprint(canonical), nil
}

type applyFinalRoutePlanFingerprintInput struct {
	PhaseEstablished        bool                                `json:"phase_established"`
	StatefileInitiallyBound bool                                `json:"statefile_initially_bound"`
	Routes                  []applyFinalRoutePlanFingerprintRow `json:"routes"`
}

type applyFinalRoutePlanFingerprintRow struct {
	Reference string                         `json:"reference"`
	Action    relationFingerprintFacts       `json:"action"`
	Work      operationplan.RouteWork        `json:"work"`
	Preflight applyRoutePreflightFingerprint `json:"preflight"`
}

type applyRoutePreflightFingerprint struct {
	Kind                   applyRoutePreflightKind     `json:"kind"`
	Command                *applyHostRouteCommandFacts `json:"command,omitempty"`
	RejectionCode          hostroute.ReasonCode        `json:"rejection_code,omitempty"`
	RejectionSubject       topology.SubjectID          `json:"rejection_subject,omitempty"`
	RejectionDetail        string                      `json:"rejection_detail,omitempty"`
	OperationalType        string                      `json:"operational_type,omitempty"`
	OperationalFingerprint [sha256.Size]byte           `json:"operational_fingerprint,omitempty"`
}

type applyHostRouteCommandFacts struct {
	Subject      topology.SubjectID               `json:"subject"`
	RouteRequest delegate.Request                 `json:"route_request"`
	Attempt      subprocess.CommandAttemptRequest `json:"attempt"`
	Disclosure   *applyHostRouteDisclosureFacts   `json:"disclosure,omitempty"`
}

type applyHostRouteDisclosureFacts struct {
	ExecutionSubject      string   `json:"execution_subject"`
	InvocationKind        string   `json:"invocation_kind"`
	CWDPolicy             string   `json:"cwd_policy"`
	EffectClasses         []string `json:"effect_classes"`
	RetainedEffectClasses []string `json:"retained_effect_classes"`
	NonClaims             []string `json:"non_claims"`
}

func applyFinalRoutePlanFingerprint(
	routes []applyRouteScheduleFact,
	phaseEstablished bool,
	statefileInitiallyBound bool,
) (mutation.OperationFingerprint, error) {
	rows := make([]applyFinalRoutePlanFingerprintRow, 0, len(routes))
	for index := range routes {
		preflight, err := applyRoutePreflightFingerprintFor(routes[index].preflight)
		if err != nil {
			return mutation.OperationFingerprint{}, fmt.Errorf(
				"final host route preflight[%d]: %w",
				index,
				err,
			)
		}
		rows = append(rows, applyFinalRoutePlanFingerprintRow{
			Reference: routes[index].ref,
			Action:    relationFingerprintRows([]reconcile.RelationAction{routes[index].action})[0],
			Work:      routes[index].work,
			Preflight: preflight,
		})
	}
	canonical, err := json.Marshal(applyFinalRoutePlanFingerprintInput{
		PhaseEstablished:        phaseEstablished,
		StatefileInitiallyBound: statefileInitiallyBound,
		Routes:                  rows,
	})
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf("marshal final host route plan: %w", err)
	}
	return mutation.NewOperationFingerprint(canonical), nil
}

func applyRoutePreflightFingerprintFor(
	preflight applyRoutePreflight,
) (applyRoutePreflightFingerprint, error) {
	result := applyRoutePreflightFingerprint{Kind: preflight.kind}
	switch preflight.kind {
	case applyRoutePreflightNotApplicable:
		return result, nil
	case applyRoutePreflightAccepted:
		command := applyHostRouteCommandFingerprint(preflight.command)
		result.Command = &command
		return result, nil
	case applyRoutePreflightRejected:
		if preflight.rejection == nil {
			return applyRoutePreflightFingerprint{}, fmt.Errorf("typed rejection is unavailable")
		}
		result.RejectionCode = preflight.rejection.Code()
		result.RejectionSubject = preflight.rejection.Subject()
		result.RejectionDetail = preflight.rejection.Error()
		return result, nil
	case applyRoutePreflightOperationalFailure:
		if preflight.operationalType == "" {
			return applyRoutePreflightFingerprint{}, fmt.Errorf("operational failure is unavailable")
		}
		result.OperationalType = preflight.operationalType
		result.OperationalFingerprint = preflight.operationalFingerprint
		return result, nil
	default:
		return applyRoutePreflightFingerprint{}, fmt.Errorf(
			"preflight kind %d is unsupported",
			preflight.kind,
		)
	}
}

func applyHostRouteCommandFingerprint(command hostroute.Command) applyHostRouteCommandFacts {
	result := applyHostRouteCommandFacts{
		Subject:      command.Subject(),
		RouteRequest: command.RouteRequest(),
		Attempt:      command.AttemptRequest(),
	}
	if disclosure, present := command.Disclosure(); present {
		facts := applyHostRouteDisclosureFingerprint(disclosure)
		result.Disclosure = &facts
	}
	return result
}

func applyHostRouteDisclosureFingerprint(
	disclosure hostroute.Disclosure,
) applyHostRouteDisclosureFacts {
	return applyHostRouteDisclosureFacts{
		ExecutionSubject:      disclosure.ExecutionSubject(),
		InvocationKind:        disclosure.InvocationKind(),
		CWDPolicy:             disclosure.CWDPolicy(),
		EffectClasses:         disclosure.EffectClasses(),
		RetainedEffectClasses: disclosure.RetainedEffectClasses(),
		NonClaims:             disclosure.NonClaims(),
	}
}

func applyHostRouteCommandsEqual(left, right hostroute.Command) bool {
	if left.Subject() != right.Subject() || !left.RouteRequest().Equal(right.RouteRequest()) {
		return false
	}
	leftAttempt := left.AttemptRequest()
	rightAttempt := right.AttemptRequest()
	if leftAttempt.Command != rightAttempt.Command ||
		leftAttempt.WorkDir != rightAttempt.WorkDir ||
		leftAttempt.Stdin != rightAttempt.Stdin ||
		leftAttempt.OutputLimit != rightAttempt.OutputLimit ||
		!slices.Equal(leftAttempt.Args, rightAttempt.Args) ||
		!slices.Equal(leftAttempt.EnvRefs, rightAttempt.EnvRefs) {
		return false
	}
	leftDisclosure, leftPresent := left.Disclosure()
	rightDisclosure, rightPresent := right.Disclosure()
	return leftPresent == rightPresent &&
		(!leftPresent || applyHostRouteDisclosuresEqual(leftDisclosure, rightDisclosure))
}

func applyHostRouteDisclosuresEqual(left, right hostroute.Disclosure) bool {
	return left.ExecutionSubject() == right.ExecutionSubject() &&
		left.InvocationKind() == right.InvocationKind() &&
		left.CWDPolicy() == right.CWDPolicy() &&
		slices.Equal(left.EffectClasses(), right.EffectClasses()) &&
		slices.Equal(left.RetainedEffectClasses(), right.RetainedEffectClasses()) &&
		slices.Equal(left.NonClaims(), right.NonClaims())
}

func applyHostRouteValidationErrorsEqual(left, right *hostroute.ValidationError) bool {
	return left != nil && right != nil &&
		left.Code() == right.Code() &&
		left.Subject() == right.Subject() &&
		left.Error() == right.Error()
}
