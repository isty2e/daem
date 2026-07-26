package reconcile

import (
	"cmp"
	"fmt"
	"slices"
	"sort"

	"github.com/isty2e/daem/internal/realization/delegate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

// DelegateDisposition is the closed planner state for one delegated route.
type DelegateDisposition string

const (
	DelegateSkipped   DelegateDisposition = "skipped"
	DelegateScheduled DelegateDisposition = "scheduled"
	DelegateBlocked   DelegateDisposition = "blocked"
)

// DelegatePolicyOutcome is the normalized policy classification behind a disposition.
type DelegatePolicyOutcome string

const (
	DelegatePolicySkip  DelegatePolicyOutcome = "skip"
	DelegatePolicyAllow DelegatePolicyOutcome = "allow"
	DelegatePolicyWarn  DelegatePolicyOutcome = "warn"
	DelegatePolicyBlock DelegatePolicyOutcome = "block"
)

// DelegateDependencyKind identifies a decision family that must precede a delegate action.
type DelegateDependencyKind string

const (
	DelegateDependencyProjection DelegateDependencyKind = "projection"
)

// DelegateDependency records ordering without merging action identities.
type DelegateDependency struct {
	Kind    DelegateDependencyKind
	Subject topology.SubjectID
}

// DelegateRiskCode is a stable policy reason attached to a delegate action.
type DelegateRiskCode string

const (
	DelegateRiskDryRunDisclosure    DelegateRiskCode = "dry_run_disclosure"
	DelegateRiskMissingRunner       DelegateRiskCode = "missing_runner"
	DelegateRiskMissingEnvRef       DelegateRiskCode = "missing_env_ref"
	DelegateRiskExternalStore       DelegateRiskCode = "external_store"
	DelegateRiskFloatingPackage     DelegateRiskCode = "floating_package"
	DelegateRiskHostSelected        DelegateRiskCode = "host_selected"
	DelegateRiskPreconditionBlocked DelegateRiskCode = "precondition_blocked"
)

// DelegateRiskSeverity says how a risk affects scheduling.
type DelegateRiskSeverity string

const (
	DelegateRiskInfo  DelegateRiskSeverity = "info"
	DelegateRiskWarn  DelegateRiskSeverity = "warn"
	DelegateRiskBlock DelegateRiskSeverity = "block"
)

// DelegateRisk records one policy fact without presentation wording.
type DelegateRisk struct {
	Code     DelegateRiskCode
	Severity DelegateRiskSeverity
	Subject  string
}

// DelegateDisclosure is the exact request that dry-run and apply may present.
type DelegateDisclosure struct {
	IdentityKey string
	RunnerKind  delegate.RunnerKind
	Command     string
	Args        []string
	Env         []lock.DelegateEnvBinding
	Package     *lock.DelegatePackageIdentity
	PinPolicy   delegate.PinPolicy
}

// MatchesPlan reports whether the disclosure losslessly represents one valid
// locked delegate plan identity.
func (disclosure DelegateDisclosure) MatchesPlan(input lock.DelegatePlanIdentity) bool {
	planIdentity, err := lock.NewDelegatePlanIdentity(input)
	return err == nil && delegateDisclosureMatchesPlan(disclosure, planIdentity)
}

// DelegateActionInput contains already-evaluated facts for one delegate action.
type DelegateActionInput struct {
	Subject      topology.SubjectID
	Target       target.Target
	Scope        target.Scope
	Plan         lock.DelegatePlanIdentity
	Disposition  DelegateDisposition
	Disclosure   DelegateDisclosure
	Risks        []DelegateRisk
	Dependencies []DelegateDependency
}

// DelegateAction is a planned delegated-route decision. It never carries an attempt outcome.
type DelegateAction struct {
	subject      topology.SubjectID
	target       target.Target
	scope        target.Scope
	plan         lock.DelegatePlanIdentity
	disposition  DelegateDisposition
	disclosure   DelegateDisclosure
	risks        []DelegateRisk
	dependencies []DelegateDependency
}

// Compare returns the canonical ordering of two delegated-route decisions.
// Target rank is product-defined rather than lexical, preserving planner and
// execution order when the aggregate Result canonicalizes its input.
func (action DelegateAction) Compare(other DelegateAction) int {
	if order := cmp.Compare(targetRank(action.target), targetRank(other.target)); order != 0 {
		return order
	}
	if order := cmp.Compare(action.scope, other.scope); order != 0 {
		return order
	}
	if order := topology.CompareSubjectID(action.subject, other.subject); order != 0 {
		return order
	}
	return cmp.Compare(action.plan.IdentityKey, other.plan.IdentityKey)
}

// NewDelegateAction validates and constructs one delegated-route decision.
func NewDelegateAction(input DelegateActionInput) (DelegateAction, error) {
	if err := input.Subject.Validate(); err != nil {
		return DelegateAction{}, err
	}
	if _, err := target.ParseTarget(string(input.Target)); err != nil {
		return DelegateAction{}, err
	}
	if _, err := target.ParseScope(string(input.Scope)); err != nil {
		return DelegateAction{}, err
	}
	planIdentity, err := lock.NewDelegatePlanIdentity(input.Plan)
	if err != nil {
		return DelegateAction{}, err
	}
	if err := validateDelegateDisposition(input.Disposition); err != nil {
		return DelegateAction{}, err
	}
	if !delegateDisclosureMatchesPlan(input.Disclosure, planIdentity) {
		return DelegateAction{}, fmt.Errorf("delegate action disclosure does not match locked plan identity")
	}
	risks, err := normalizeDelegateRisks(input.Risks)
	if err != nil {
		return DelegateAction{}, err
	}
	if err := validateDelegateDispositionRisks(input.Disposition, risks); err != nil {
		return DelegateAction{}, err
	}
	dependencies, err := normalizeDelegateDependencies(input.Dependencies)
	if err != nil {
		return DelegateAction{}, err
	}

	return DelegateAction{
		subject:      input.Subject,
		target:       input.Target,
		scope:        input.Scope,
		plan:         cloneDelegatePlanIdentity(planIdentity),
		disposition:  input.Disposition,
		disclosure:   cloneDelegateDisclosure(input.Disclosure),
		risks:        risks,
		dependencies: dependencies,
	}, nil
}

func (action DelegateAction) Subject() topology.SubjectID { return action.subject }
func (action DelegateAction) Target() target.Target       { return action.target }
func (action DelegateAction) Scope() target.Scope         { return action.scope }

// PlanIdentity returns the exact locked delegated-route identity.
func (action DelegateAction) PlanIdentity() lock.DelegatePlanIdentity {
	return cloneDelegatePlanIdentity(action.plan)
}

func (action DelegateAction) Disposition() DelegateDisposition { return action.disposition }

// PolicyOutcome reconstructs the lossless policy classification from disposition and risks.
func (action DelegateAction) PolicyOutcome() DelegatePolicyOutcome {
	switch action.disposition {
	case DelegateSkipped:
		return DelegatePolicySkip
	case DelegateBlocked:
		return DelegatePolicyBlock
	}
	for _, risk := range action.risks {
		if risk.Severity == DelegateRiskWarn {
			return DelegatePolicyWarn
		}
	}
	return DelegatePolicyAllow
}

func (action DelegateAction) Disclosure() DelegateDisclosure {
	return cloneDelegateDisclosure(action.disclosure)
}

func (action DelegateAction) Risks() []DelegateRisk {
	return append([]DelegateRisk(nil), action.risks...)
}

func (action DelegateAction) Dependencies() []DelegateDependency {
	return append([]DelegateDependency(nil), action.dependencies...)
}

func (action DelegateAction) SchedulesAttempt() bool {
	return action.disposition == DelegateScheduled
}

func validateDelegateDisposition(disposition DelegateDisposition) error {
	switch disposition {
	case DelegateSkipped, DelegateScheduled, DelegateBlocked:
		return nil
	default:
		return fmt.Errorf("delegate action disposition %q is unsupported", disposition)
	}
}

func validateDelegateDispositionRisks(disposition DelegateDisposition, risks []DelegateRisk) error {
	hasBlock := false
	hasDryRun := false
	for _, risk := range risks {
		hasBlock = hasBlock || risk.Severity == DelegateRiskBlock
		hasDryRun = hasDryRun || risk.Code == DelegateRiskDryRunDisclosure
	}
	switch disposition {
	case DelegateBlocked:
		if !hasBlock {
			return fmt.Errorf("blocked delegate action requires a blocking risk")
		}
	case DelegateScheduled:
		if hasBlock || hasDryRun {
			return fmt.Errorf("scheduled delegate action has incompatible policy risks")
		}
	}
	return nil
}

func normalizeDelegateRisks(values []DelegateRisk) ([]DelegateRisk, error) {
	risks := append([]DelegateRisk(nil), values...)
	for index, risk := range risks {
		if err := validateDelegateRisk(risk); err != nil {
			return nil, fmt.Errorf("delegate risk[%d]: %w", index, err)
		}
	}
	sort.SliceStable(risks, func(left int, right int) bool {
		if risks[left].Severity != risks[right].Severity {
			return risks[left].Severity < risks[right].Severity
		}
		if risks[left].Code != risks[right].Code {
			return risks[left].Code < risks[right].Code
		}
		return risks[left].Subject < risks[right].Subject
	})
	for index := 1; index < len(risks); index++ {
		if risks[index] == risks[index-1] {
			return nil, fmt.Errorf("duplicate delegate risk %q for %q", risks[index].Code, risks[index].Subject)
		}
	}
	return risks, nil
}

func validateDelegateRisk(risk DelegateRisk) error {
	switch risk.Code {
	case DelegateRiskDryRunDisclosure,
		DelegateRiskMissingRunner,
		DelegateRiskMissingEnvRef,
		DelegateRiskExternalStore,
		DelegateRiskFloatingPackage,
		DelegateRiskHostSelected,
		DelegateRiskPreconditionBlocked:
	default:
		return fmt.Errorf("unsupported code %q", risk.Code)
	}
	switch risk.Severity {
	case DelegateRiskInfo, DelegateRiskWarn, DelegateRiskBlock:
		return nil
	default:
		return fmt.Errorf("unsupported severity %q", risk.Severity)
	}
}

func normalizeDelegateDependencies(values []DelegateDependency) ([]DelegateDependency, error) {
	dependencies := append([]DelegateDependency(nil), values...)
	seen := make(map[DelegateDependency]struct{}, len(dependencies))
	canonical := make([]DelegateDependency, 0, len(dependencies))
	for index, dependency := range dependencies {
		if dependency.Kind != DelegateDependencyProjection {
			return nil, fmt.Errorf("delegate dependency[%d] kind %q is unsupported", index, dependency.Kind)
		}
		if err := dependency.Subject.Validate(); err != nil {
			return nil, fmt.Errorf("delegate dependency[%d] subject: %w", index, err)
		}
		if _, duplicate := seen[dependency]; duplicate {
			continue
		}
		seen[dependency] = struct{}{}
		canonical = append(canonical, dependency)
	}
	sort.SliceStable(canonical, func(left int, right int) bool {
		if canonical[left].Kind != canonical[right].Kind {
			return canonical[left].Kind < canonical[right].Kind
		}
		return topology.CompareSubjectID(canonical[left].Subject, canonical[right].Subject) < 0
	})
	return canonical, nil
}

func delegateDisclosureMatchesPlan(disclosure DelegateDisclosure, plan lock.DelegatePlanIdentity) bool {
	if disclosure.IdentityKey != plan.IdentityKey ||
		disclosure.RunnerKind != plan.RunnerKind ||
		disclosure.Command != plan.Command ||
		disclosure.PinPolicy != plan.PinPolicy ||
		!slices.Equal(disclosure.Args, plan.Args) ||
		!slices.Equal(disclosure.Env, plan.Env) {
		return false
	}
	if disclosure.Package == nil || plan.Package == nil {
		return disclosure.Package == nil && plan.Package == nil
	}
	return *disclosure.Package == *plan.Package
}

func cloneDelegatePlanIdentity(identity lock.DelegatePlanIdentity) lock.DelegatePlanIdentity {
	cloned := lock.DelegatePlanIdentity{
		IdentityKey: identity.IdentityKey,
		RunnerKind:  identity.RunnerKind,
		Command:     identity.Command,
		Args:        append([]string(nil), identity.Args...),
		Env:         append([]lock.DelegateEnvBinding(nil), identity.Env...),
		PinPolicy:   identity.PinPolicy,
	}
	if identity.Package != nil {
		value := *identity.Package
		cloned.Package = &value
	}
	return cloned
}

func cloneDelegateDisclosure(disclosure DelegateDisclosure) DelegateDisclosure {
	cloned := DelegateDisclosure{
		IdentityKey: disclosure.IdentityKey,
		RunnerKind:  disclosure.RunnerKind,
		Command:     disclosure.Command,
		Args:        append([]string(nil), disclosure.Args...),
		Env:         append([]lock.DelegateEnvBinding(nil), disclosure.Env...),
		PinPolicy:   disclosure.PinPolicy,
	}
	if disclosure.Package != nil {
		value := *disclosure.Package
		cloned.Package = &value
	}
	return cloned
}
