package hostroute

import (
	"fmt"
	"sort"

	lock "github.com/isty2e/daem/internal/realization/lock"
	reconciliation "github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/delegatepolicy"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

// DelegateReadinessFact is a passive prerequisite fact for one delegated route.
type DelegateReadinessFact struct {
	Subject        topology.SubjectID
	Runner         delegatepolicy.RunnerReadiness
	MissingEnvRefs []string
}

// DelegateBlockedDependency records a failed projection prerequisite.
type DelegateBlockedDependency struct {
	Kind    reconciliation.DelegateDependencyKind
	Subject topology.SubjectID
}

// DelegateInput contains normalized lock and policy facts for delegated routes.
type DelegateInput struct {
	Locked              lock.File
	SelectedTargets     reconciliation.SelectedTargets
	Context             reconciliation.OperationContext
	Readiness           []DelegateReadinessFact
	BlockedDependencies []DelegateBlockedDependency
}

// BuildDelegateActions creates selected delegated-route decisions from locked subjects.
func BuildDelegateActions(input DelegateInput) ([]reconciliation.DelegateAction, error) {
	if input.Context == reconciliation.ContextInspect {
		return nil, nil
	}
	mode, err := delegateMode(input.Context)
	if err != nil {
		return nil, err
	}
	readinessBySubject, err := delegateReadinessBySubject(input.Readiness)
	if err != nil {
		return nil, err
	}
	blockedDependencies, err := delegateBlockedDependenciesByKey(input.BlockedDependencies)
	if err != nil {
		return nil, err
	}

	actions := make([]reconciliation.DelegateAction, 0)
	for _, contract := range input.Locked.Locked.Subjects() {
		delegatePlan, ok := contract.DelegatePlan()
		if !ok {
			continue
		}
		context, err := delegateActionContextForContract(contract)
		if err != nil {
			return nil, err
		}
		if !input.SelectedTargets.Contains(context.target) {
			continue
		}

		readiness := readinessBySubject[contract.SubjectID()]
		decision, err := delegatepolicy.Evaluate(delegatepolicy.Input{
			Plan:           delegatePlan,
			Mode:           mode,
			Runner:         readiness.runner(),
			MissingEnvRefs: readiness.MissingEnvRefs,
			PreconditionBlocks: delegatePreconditionBlocks(
				context.dependencies,
				blockedDependencies,
			),
		})
		if err != nil {
			return nil, err
		}
		action, err := reconciliation.NewDelegateAction(reconciliation.DelegateActionInput{
			Subject:      contract.SubjectID(),
			Target:       context.target,
			Scope:        context.scope,
			Plan:         delegatePlan,
			Disposition:  delegateDisposition(decision.Outcome()),
			Risks:        decision.Risks(),
			Dependencies: context.dependencies,
		})
		if err != nil {
			return nil, err
		}
		actions = append(actions, action)
	}
	delegateSortActions(actions)
	return actions, nil
}

type delegateRecordContext struct {
	target       target.Target
	scope        target.Scope
	dependencies []reconciliation.DelegateDependency
}

type delegateReadinessFacts struct {
	Runner         delegatepolicy.RunnerReadiness
	MissingEnvRefs []string
}

func (facts delegateReadinessFacts) runner() delegatepolicy.RunnerReadiness {
	if facts.Runner == "" {
		return delegatepolicy.RunnerUnknown
	}
	return facts.Runner
}

func delegateMode(context reconciliation.OperationContext) (delegatepolicy.Mode, error) {
	switch context {
	case reconciliation.ContextApply:
		return delegatepolicy.ModeApply, nil
	case reconciliation.ContextDryRun:
		return delegatepolicy.ModeDryRun, nil
	default:
		return "", fmt.Errorf("operation context %q cannot plan delegated attempts", context)
	}
}

func delegateReadinessBySubject(
	facts []DelegateReadinessFact,
) (map[topology.SubjectID]delegateReadinessFacts, error) {
	result := make(map[topology.SubjectID]delegateReadinessFacts, len(facts))
	for _, fact := range facts {
		if err := fact.Subject.Validate(); err != nil {
			return nil, err
		}
		if _, exists := result[fact.Subject]; exists {
			return nil, fmt.Errorf(
				"duplicate delegate prerequisite fact for %s/%s %q",
				fact.Subject.Kind(),
				fact.Subject.Namespace(),
				fact.Subject.Key(),
			)
		}
		result[fact.Subject] = delegateReadinessFacts{
			Runner:         fact.Runner,
			MissingEnvRefs: append([]string(nil), fact.MissingEnvRefs...),
		}
	}
	return result, nil
}

func delegateBlockedDependenciesByKey(
	values []DelegateBlockedDependency,
) (map[reconciliation.DelegateDependency]struct{}, error) {
	result := make(map[reconciliation.DelegateDependency]struct{}, len(values))
	for index, value := range values {
		dependency := reconciliation.DelegateDependency{Kind: value.Kind, Subject: value.Subject}
		if dependency.Kind != reconciliation.DelegateDependencyProjection {
			return nil, fmt.Errorf("blocked delegate dependency[%d] kind %q is unsupported", index, dependency.Kind)
		}
		if err := dependency.Subject.Validate(); err != nil {
			return nil, fmt.Errorf("blocked delegate dependency[%d] subject: %w", index, err)
		}
		result[dependency] = struct{}{}
	}
	return result, nil
}

func delegatePreconditionBlocks(
	dependencies []reconciliation.DelegateDependency,
	blocked map[reconciliation.DelegateDependency]struct{},
) []delegatepolicy.PreconditionBlock {
	result := make([]delegatepolicy.PreconditionBlock, 0)
	for _, dependency := range dependencies {
		if _, isBlocked := blocked[dependency]; !isBlocked {
			continue
		}
		result = append(result, delegatepolicy.PreconditionBlock{
			Subject: string(dependency.Kind) + ":" + dependency.Subject.String(),
		})
	}
	return result
}

func delegateActionContextForContract(contract lock.LockedSubjectContract) (delegateRecordContext, error) {
	realization, ok := contract.Realization()
	if ok {
		if contribution, aggregate := realization.ManagedAggregateContribution(); aggregate {
			return delegateRecordContext{
				target: contribution.Target(),
				scope:  contribution.Scope(),
				dependencies: []reconciliation.DelegateDependency{
					{Kind: reconciliation.DelegateDependencyProjection, Subject: contract.SubjectID()},
				},
			}, nil
		}
	}
	subject := contract.SubjectID()
	return delegateRecordContext{}, fmt.Errorf(
		"locked subject %s/%s %q has delegate plan identity without a managed aggregate realization",
		subject.Kind(),
		subject.Namespace(),
		subject.Key(),
	)
}

func delegateDisposition(outcome delegatepolicy.Outcome) reconciliation.DelegateDisposition {
	switch outcome {
	case delegatepolicy.OutcomeAllow, delegatepolicy.OutcomeWarn:
		return reconciliation.DelegateScheduled
	case delegatepolicy.OutcomeBlock:
		return reconciliation.DelegateBlocked
	default:
		return reconciliation.DelegateSkipped
	}
}

func delegateSortActions(actions []reconciliation.DelegateAction) {
	sort.SliceStable(actions, func(left int, right int) bool {
		return actions[left].Compare(actions[right]) < 0
	})
}
