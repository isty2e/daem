package delegatepolicy

import (
	"fmt"
	"sort"
	"strings"

	"github.com/isty2e/daem/internal/realization/delegate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	reconciliation "github.com/isty2e/daem/internal/reconcile"
)

// Mode identifies the operation context asking whether a delegate may run.
type Mode string

const (
	ModeApply  Mode = "apply"
	ModeDryRun Mode = "dry_run"
)

// RunnerReadiness is caller-provided passive runner availability.
type RunnerReadiness string

const (
	RunnerUnknown   RunnerReadiness = "unknown"
	RunnerAvailable RunnerReadiness = "available"
	RunnerMissing   RunnerReadiness = "missing"
)

// Outcome is the scheduling decision for a delegate attempt.
type Outcome string

const (
	OutcomeSkip  Outcome = "skip"
	OutcomeAllow Outcome = "allow"
	OutcomeWarn  Outcome = "warn"
	OutcomeBlock Outcome = "block"
)

// Input contains already-observed facts for one delegate policy decision.
type Input struct {
	Plan               lock.DelegatePlanIdentity
	Mode               Mode
	Runner             RunnerReadiness
	MissingEnvRefs     []string
	PreconditionBlocks []PreconditionBlock
}

// PreconditionBlock is a caller-provided fact that a required lifecycle dependency failed.
type PreconditionBlock struct {
	Subject string
}

// Decision is a pure scheduling result. It does not execute, probe, or present.
type Decision struct {
	outcome    Outcome
	disclosure reconciliation.DelegateDisclosure
	risks      []reconciliation.DelegateRisk
}

// Evaluate classifies whether a locked delegate plan may be scheduled.
func Evaluate(input Input) (Decision, error) {
	plan, err := lock.NewDelegatePlanIdentity(input.Plan)
	if err != nil {
		return Decision{}, err
	}
	if err := validateMode(input.Mode); err != nil {
		return Decision{}, err
	}
	if err := validateRunnerReadiness(input.Runner); err != nil {
		return Decision{}, err
	}
	missingEnvRefs, err := normalizeMissingEnvRefs(plan, input.MissingEnvRefs)
	if err != nil {
		return Decision{}, err
	}
	preconditionBlocks, err := normalizePreconditionBlocks(input.PreconditionBlocks)
	if err != nil {
		return Decision{}, err
	}

	risks := classifyRisks(plan, input.Mode, input.Runner, missingEnvRefs, preconditionBlocks)
	outcome := outcomeFor(input.Mode, risks)
	return Decision{
		outcome:    outcome,
		disclosure: disclosureFromPlan(plan),
		risks:      risks,
	}, nil
}

// Outcome returns the canonical scheduling decision.
func (decision Decision) Outcome() Outcome {
	return decision.outcome
}

// Disclosure returns a defensive copy of the exact locked invocation facts.
func (decision Decision) Disclosure() reconciliation.DelegateDisclosure {
	return cloneDisclosure(decision.disclosure)
}

// Risks returns a stable copy of policy reasons.
func (decision Decision) Risks() []reconciliation.DelegateRisk {
	return append([]reconciliation.DelegateRisk(nil), decision.risks...)
}

func classifyRisks(
	plan lock.DelegatePlanIdentity,
	mode Mode,
	runner RunnerReadiness,
	missingEnvRefs []string,
	preconditionBlocks []PreconditionBlock,
) []reconciliation.DelegateRisk {
	risks := make([]reconciliation.DelegateRisk, 0)
	if mode == ModeDryRun {
		risks = append(risks, reconciliation.DelegateRisk{
			Code:     reconciliation.DelegateRiskDryRunDisclosure,
			Severity: reconciliation.DelegateRiskInfo,
			Subject:  plan.Command,
		})
	}
	if runner == RunnerMissing {
		risks = append(risks, reconciliation.DelegateRisk{
			Code:     reconciliation.DelegateRiskMissingRunner,
			Severity: reconciliation.DelegateRiskBlock,
			Subject:  plan.Command,
		})
	}
	for _, envRef := range missingEnvRefs {
		risks = append(risks, reconciliation.DelegateRisk{
			Code:     reconciliation.DelegateRiskMissingEnvRef,
			Severity: reconciliation.DelegateRiskBlock,
			Subject:  envRef,
		})
	}
	for _, block := range preconditionBlocks {
		risks = append(risks, reconciliation.DelegateRisk{
			Code:     reconciliation.DelegateRiskPreconditionBlocked,
			Severity: reconciliation.DelegateRiskBlock,
			Subject:  block.Subject,
		})
	}
	if runnerMayUseExternalStore(plan.RunnerKind) {
		risks = append(risks, reconciliation.DelegateRisk{
			Code:     reconciliation.DelegateRiskExternalStore,
			Severity: reconciliation.DelegateRiskWarn,
			Subject:  string(plan.RunnerKind),
		})
	}
	switch plan.PinPolicy {
	case delegate.PinFloating:
		risks = append(risks, reconciliation.DelegateRisk{
			Code:     reconciliation.DelegateRiskFloatingPackage,
			Severity: reconciliation.DelegateRiskWarn,
			Subject:  packageSubject(plan),
		})
	case delegate.PinHostSelected:
		risks = append(risks, reconciliation.DelegateRisk{
			Code:     reconciliation.DelegateRiskHostSelected,
			Severity: reconciliation.DelegateRiskWarn,
			Subject:  string(plan.RunnerKind),
		})
	}
	return risks
}

func outcomeFor(mode Mode, risks []reconciliation.DelegateRisk) Outcome {
	if mode != ModeApply {
		return OutcomeSkip
	}
	for _, risk := range risks {
		if risk.Severity == reconciliation.DelegateRiskBlock {
			return OutcomeBlock
		}
	}
	for _, risk := range risks {
		if risk.Severity == reconciliation.DelegateRiskWarn {
			return OutcomeWarn
		}
	}
	return OutcomeAllow
}

func disclosureFromPlan(plan lock.DelegatePlanIdentity) reconciliation.DelegateDisclosure {
	disclosure := reconciliation.DelegateDisclosure{
		IdentityKey: plan.IdentityKey,
		RunnerKind:  plan.RunnerKind,
		Command:     plan.Command,
		Args:        append([]string(nil), plan.Args...),
		Env:         append([]lock.DelegateEnvBinding(nil), plan.Env...),
		PinPolicy:   plan.PinPolicy,
	}
	if plan.Package != nil {
		disclosure.Package = &lock.DelegatePackageIdentity{
			Ecosystem: plan.Package.Ecosystem,
			Name:      plan.Package.Name,
			Selector:  plan.Package.Selector,
		}
	}
	return disclosure
}

func cloneDisclosure(disclosure reconciliation.DelegateDisclosure) reconciliation.DelegateDisclosure {
	cloned := reconciliation.DelegateDisclosure{
		IdentityKey: disclosure.IdentityKey,
		RunnerKind:  disclosure.RunnerKind,
		Command:     disclosure.Command,
		Args:        append([]string(nil), disclosure.Args...),
		Env:         append([]lock.DelegateEnvBinding(nil), disclosure.Env...),
		PinPolicy:   disclosure.PinPolicy,
	}
	if disclosure.Package != nil {
		cloned.Package = &lock.DelegatePackageIdentity{
			Ecosystem: disclosure.Package.Ecosystem,
			Name:      disclosure.Package.Name,
			Selector:  disclosure.Package.Selector,
		}
	}
	return cloned
}

func normalizeMissingEnvRefs(
	plan lock.DelegatePlanIdentity,
	missing []string,
) ([]string, error) {
	if len(missing) == 0 {
		return nil, nil
	}
	requiredNames := plan.EnvSourceNames()
	required := make(map[string]struct{}, len(requiredNames))
	for _, envRef := range requiredNames {
		required[envRef] = struct{}{}
	}
	bindings := make([]delegate.EnvBinding, 0, len(missing))
	for _, name := range missing {
		binding, err := delegate.NewEnvBinding(name, name)
		if err != nil {
			return nil, err
		}
		bindings = append(bindings, binding)
	}
	set, err := delegate.NewEnvBindingSet(bindings)
	if err != nil {
		return nil, err
	}
	normalized := set.SourceNames()
	for _, envRef := range normalized {
		if _, ok := required[envRef]; !ok {
			return nil, fmt.Errorf("missing env ref %q is not required by delegate plan", envRef)
		}
	}
	return normalized, nil
}

func normalizePreconditionBlocks(values []PreconditionBlock) ([]PreconditionBlock, error) {
	if len(values) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(values))
	subjects := make([]string, 0, len(values))
	for _, value := range values {
		subject := strings.TrimSpace(value.Subject)
		if subject == "" {
			return nil, fmt.Errorf("delegate precondition block subject is required")
		}
		if subject != value.Subject {
			return nil, fmt.Errorf("delegate precondition block subject %q must be normalized", value.Subject)
		}
		if _, exists := seen[subject]; exists {
			continue
		}
		seen[subject] = struct{}{}
		subjects = append(subjects, subject)
	}
	sort.Strings(subjects)
	normalized := make([]PreconditionBlock, 0, len(subjects))
	for _, subject := range subjects {
		normalized = append(normalized, PreconditionBlock{Subject: subject})
	}
	return normalized, nil
}

func runnerMayUseExternalStore(kind delegate.RunnerKind) bool {
	switch kind {
	case delegate.RunnerNPX, delegate.RunnerUVX, delegate.RunnerDocker:
		return true
	default:
		return false
	}
}

func packageSubject(plan lock.DelegatePlanIdentity) string {
	if plan.Package == nil {
		return string(plan.RunnerKind)
	}
	if strings.TrimSpace(plan.Package.Selector) == "" {
		return plan.Package.Name
	}
	return plan.Package.Name + "@" + plan.Package.Selector
}

func validateMode(mode Mode) error {
	switch mode {
	case ModeApply, ModeDryRun:
		return nil
	default:
		return fmt.Errorf("delegate policy mode %q is unsupported", mode)
	}
}

func validateRunnerReadiness(readiness RunnerReadiness) error {
	switch readiness {
	case RunnerUnknown, RunnerAvailable, RunnerMissing:
		return nil
	default:
		return fmt.Errorf("delegate runner readiness %q is unsupported", readiness)
	}
}
