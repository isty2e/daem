package journal

import (
	"context"
	"fmt"
	"slices"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

func (plan NativeRecoveryPlan) assessCleanup(ctx context.Context, options PlanLoadOptions, budget *physicalwork.Budget) (NativeRecoveryPlan, error) {
	intents, err := plan.RemovalIntents()
	if err != nil {
		return NativeRecoveryPlan{}, err
	}
	plan.cleanup = make([]recovery.RemovalCleanupObligation, 0, len(intents))
	for index, intent := range intents {
		if err := ctx.Err(); err != nil {
			return NativeRecoveryPlan{}, err
		}
		pending, err := recovery.NewPendingRemovalCleanupObligation(intent)
		if err != nil {
			return NativeRecoveryPlan{}, err
		}
		var cached *recovery.RemovalCleanupObligation
		for _, checkpoint := range plan.assessment.ordinary.checkpoints {
			if pending.SameBasis(checkpoint) {
				cached = &checkpoint
				break
			}
		}
		if cached != nil {
			plan.cleanup = append(plan.cleanup, *cached)
			continue
		}
		if err := budget.AdmitObservation(); err != nil {
			return NativeRecoveryPlan{}, err
		}
		obligation, err := journalqualification.ObserveRemovalCleanupIntent(ctx, intent, journalqualification.RemovalReadOptions{Filesystem: options.Filesystem, Resolver: options.Resolver}, budget)
		if err != nil {
			return NativeRecoveryPlan{}, fmt.Errorf("assess joint removal[%d]: %w", index, err)
		}
		plan.cleanup = append(plan.cleanup, obligation)
	}
	return plan, nil
}

// RemovalCleanupObligations includes both ordinary and document continuations.
// It is fresh disclosure evidence, not permission to remove a path.
func (plan NativeRecoveryPlan) RemovalCleanupObligations() []recovery.RemovalCleanupObligation {
	return slices.Clone(plan.cleanup)
}

func (plan NativeRecoveryPlan) sameCleanup(other NativeRecoveryPlan) bool {
	return slices.EqualFunc(plan.cleanup, other.cleanup, recovery.RemovalCleanupObligation.Equal)
}
