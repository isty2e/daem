package delegatepolicy

import (
	"fmt"
	"sort"
	"strings"

	"github.com/isty2e/daem/internal/realization/delegate"
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
	Plan               delegate.DelegatePlan
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
	outcome Outcome
	risks   []reconciliation.DelegateRisk
}

// Evaluate classifies whether a locked delegate plan may be scheduled.
func Evaluate(input Input) (Decision, error) {
	if err := input.Plan.Validate(); err != nil {
		return Decision{}, err
	}
	if err := validateMode(input.Mode); err != nil {
		return Decision{}, err
	}
	if err := validateRunnerReadiness(input.Runner); err != nil {
		return Decision{}, err
	}
	missingEnvRefs, err := normalizeMissingEnvRefs(input.Plan, input.MissingEnvRefs)
	if err != nil {
		return Decision{}, err
	}
	preconditionBlocks, err := normalizePreconditionBlocks(input.PreconditionBlocks)
	if err != nil {
		return Decision{}, err
	}

	risks := classifyRisks(input.Plan, input.Mode, input.Runner, missingEnvRefs, preconditionBlocks)
	outcome := outcomeFor(input.Mode, risks)
	return Decision{
		outcome: outcome,
		risks:   risks,
	}, nil
}

// Outcome returns the canonical scheduling decision.
func (decision Decision) Outcome() Outcome {
	return decision.outcome
}

// Risks returns a stable copy of policy reasons.
func (decision Decision) Risks() []reconciliation.DelegateRisk {
	return append([]reconciliation.DelegateRisk(nil), decision.risks...)
}

func classifyRisks(
	plan delegate.DelegatePlan,
	mode Mode,
	runner RunnerReadiness,
	missingEnvRefs []string,
	preconditionBlocks []PreconditionBlock,
) []reconciliation.DelegateRisk {
	risks := make([]reconciliation.DelegateRisk, 0)
	command := plan.Command()
	runnerKind := plan.Runner().Kind()
	if mode == ModeDryRun {
		risks = append(risks, reconciliation.DelegateRisk{
			Code:     reconciliation.DelegateRiskDryRunDisclosure,
			Severity: reconciliation.DelegateRiskInfo,
			Subject:  command.Name(),
		})
	}
	if runner == RunnerMissing {
		risks = append(risks, reconciliation.DelegateRisk{
			Code:     reconciliation.DelegateRiskMissingRunner,
			Severity: reconciliation.DelegateRiskBlock,
			Subject:  command.Name(),
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
	if runnerMayUseExternalStore(runnerKind) {
		risks = append(risks, reconciliation.DelegateRisk{
			Code:     reconciliation.DelegateRiskExternalStore,
			Severity: reconciliation.DelegateRiskWarn,
			Subject:  string(runnerKind),
		})
	}
	switch plan.PinPolicy() {
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
			Subject:  string(runnerKind),
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

func normalizeMissingEnvRefs(
	plan delegate.DelegatePlan,
	missing []string,
) ([]string, error) {
	if len(missing) == 0 {
		return nil, nil
	}
	requiredNames := plan.Env().SourceNames()
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

func packageSubject(plan delegate.DelegatePlan) string {
	packageRef, present := plan.PackageRef()
	if !present {
		return string(plan.Runner().Kind())
	}
	if strings.TrimSpace(packageRef.Selector()) == "" {
		return packageRef.Name()
	}
	return packageRef.Name() + "@" + packageRef.Selector()
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
