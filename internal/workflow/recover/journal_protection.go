package recover

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/recoverygate"
)

// requireProtectionClear checks only the selected journal's effects. It does
// not qualify the protected invocation or prepare a permission recovery.
func (prepared recoveryPreparation) requireProtectionClear(ctx context.Context) error {
	if prepared.plan.AuthorityKind() != journal.RecoveryAuthorityActiveJournal || prepared.plan.HasErrors() {
		return nil
	}
	namespaces, err := prepared.authorityEvidence.initialDocumentWrites.Namespaces()
	if err != nil {
		return err
	}
	for _, slot := range prepared.authorityEvidence.removalSlots {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Cleanup changes the private directory entry, not a link's referent.
		entry, err := mutation.ObserveDirectoryEntryAuthorityBounded(slot, physicalwork.MaximumPhysicalPathDepth, prepared.physicalPathBudget)
		if err != nil {
			return err
		}
		namespaces = append(namespaces, entry)
	}
	scope, err := recoverygate.DocumentScopeForObservations(namespaces)
	if err != nil {
		return err
	}
	if native, ok := journal.NativeActiveRecoveryPlan(prepared.plan); ok {
		return native.RequireProtectionClear(scope)
	}

	// Native release already checks the exact released claims and binding in
	// its loader and executor. Its own file-set rollback can also write a file.
	ordinary, isOrdinary := journal.ActiveRecoveryPlan(prepared.plan)
	claims := ordinary.ClaimTransitions()
	checkClaims := isOrdinary && len(claims) != 0 &&
		(ordinary.Classification() == recovery.ClassificationNeedsRollback || ordinary.Classification() == recovery.ClassificationNeedsFinalize)
	if len(namespaces) == 0 && !checkClaims {
		return nil
	}
	registry, err := ownershipstore.ReadProtectionRegistryBounded(ctx, prepared.paths.OwnershipRegistryPath, physicalwork.MaximumPhysicalPathDepth, prepared.physicalPathBudget)
	if err != nil {
		return err
	}
	if err := scope.RequireWritable(registry); err != nil {
		return err
	}
	if !checkClaims {
		return nil
	}
	set, err := ownershipmutation.NewClaimTransitionSet(claims)
	if err != nil {
		return err
	}
	var convergence ownership.ClaimConvergence
	if ordinary.Classification() == recovery.ClassificationNeedsRollback {
		convergence, err = set.Rollback()
	} else {
		convergence, err = set.Finalization()
	}
	if err != nil {
		return err
	}
	// The same convergence used at publication distinguishes changes from
	// already-settled claims and checks their recorded physical authority.
	if _, _, err := convergence.Apply(registry); err != nil {
		return fmt.Errorf("qualify recovery claim changes: %w", err)
	}
	return nil
}
