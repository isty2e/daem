package journal

import (
	"context"
	"fmt"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// nativeDocumentCaptureCandidate retains actual comparison inputs before the
// operation's complete removal set can be admitted. It is not a valid record.
type nativeDocumentCaptureCandidate struct {
	group      recoveryDocumentGroup
	comparison journaldocument.Comparison
	namespace  journalnamespace.DocumentNamespace
	manifest   journalnamespace.RootGrant
	relations  []aggregate.DocumentAddress
	budget     *physicalwork.Budget
}

func (candidate nativeDocumentCaptureCandidate) captureRemoval(ctx context.Context, budget *physicalwork.Budget) (recoveryDocumentGroup, journaldocument.Comparison, error) {
	if ctx == nil || budget == nil || budget != candidate.budget {
		return recoveryDocumentGroup{}, journaldocument.Comparison{}, fmt.Errorf("document capture requires context and its original physical budget")
	}
	group := candidate.group
	var err error
	group.Removal, err = captureDocumentRemoval(ctx, candidate.namespace, candidate.comparison, candidate.relations, budget)
	if err != nil {
		return recoveryDocumentGroup{}, journaldocument.Comparison{}, err
	}
	if err := group.validate(persistedNativeRoot(candidate.manifest.Provenance())); err != nil {
		return recoveryDocumentGroup{}, journaldocument.Comparison{}, err
	}
	if err := ctx.Err(); err != nil {
		return recoveryDocumentGroup{}, journaldocument.Comparison{}, err
	}
	return group, candidate.comparison, nil
}

func captureNativeDocumentCandidates(ctx context.Context, candidates []nativeDocumentCaptureCandidate, ordinary recovery.RemovalDemandSet, budget *physicalwork.Budget) ([]recoveryDocumentGroup, []journaldocument.Comparison, recovery.RemovalDemandSet, error) {
	if ctx == nil || budget == nil {
		return nil, nil, recovery.RemovalDemandSet{}, fmt.Errorf("native capture requires context and the shared physical budget")
	}
	demands := ordinary.Demands()
	for _, candidate := range candidates {
		if candidate.budget != budget {
			return nil, nil, recovery.RemovalDemandSet{}, fmt.Errorf("document candidate was prepared under a different physical budget")
		}
		groupDemands, err := candidate.comparison.RemovalDemands(ctx, candidate.relations)
		if err != nil {
			return nil, nil, recovery.RemovalDemandSet{}, err
		}
		demands = append(demands, groupDemands.Demands()...)
	}
	complete, err := recovery.NewRemovalDemandSet(demands)
	if err != nil {
		return nil, nil, recovery.RemovalDemandSet{}, err
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, recovery.RemovalDemandSet{}, err
	}
	if err := budget.AdmitRemovalDemands(complete); err != nil {
		return nil, nil, recovery.RemovalDemandSet{}, err
	}

	groups := make([]recoveryDocumentGroup, 0, len(candidates))
	comparisons := make([]journaldocument.Comparison, 0, len(candidates))
	for _, candidate := range candidates {
		group, comparison, err := candidate.captureRemoval(ctx, budget)
		if err != nil {
			return nil, nil, recovery.RemovalDemandSet{}, err
		}
		groups = append(groups, group)
		comparisons = append(comparisons, comparison)
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, recovery.RemovalDemandSet{}, err
	}
	return groups, comparisons, complete, nil
}
