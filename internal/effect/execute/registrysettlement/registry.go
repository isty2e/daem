package registrysettlement

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal"
	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output/ownership"
)

func Read(ctx context.Context, operation *journal.NativeRecoveryOperation, assessment journalqualification.NativeRecoveryAssessment, store ownershipmutation.RegistryStore) (ownership.Registry, error) {
	if len(assessment.MissingDocuments()) == 0 {
		return store.LoadForClaimRemovals(ctx, assessment.ExpectedClaimRemovals(), physicalwork.MaximumPhysicalPathDepth, operation.Budget())
	}
	selected, admitted := store.(ownershipmutation.DocumentRegistryReader)
	if !admitted {
		return ownership.Registry{}, fmt.Errorf("joint recovery requires combined document and removal Registry qualification")
	}
	return selected.LoadForDocuments(ctx, operation.Owner(), assessment.MissingDocuments(), assessment.ExpectedClaimRemovals(), physicalwork.MaximumPhysicalPathDepth, operation.Budget())
}

func Converge(ctx context.Context, operation *journal.NativeRecoveryOperation, assessment journalqualification.NativeRecoveryAssessment, store ownershipmutation.RegistryStore, claims ownership.ClaimConvergence, bindings ownership.DocumentBindingConvergence) error {
	if len(assessment.MissingDocuments()) == 0 {
		_, err := store.ConvergeRegistry(ctx, claims, bindings)
		return err
	}
	selected, admitted := store.(ownershipmutation.DocumentRegistryStore)
	if !admitted {
		return fmt.Errorf("joint recovery requires combined Registry convergence")
	}
	_, err := selected.ConvergeForDocuments(ctx, claims, bindings, operation.Owner(), assessment.MissingDocuments(), physicalwork.MaximumPhysicalPathDepth, operation.Budget())
	return err
}
