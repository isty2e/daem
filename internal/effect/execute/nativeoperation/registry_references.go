package nativeoperation

import (
	"context"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/execute/registrysettlement"
	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output/ownership"
)

func (operation *operation) registryReferences(captured journalpublication.CapturedNativeJournal) []pathauthority.Reference {
	references := operation.destinations.References()
	if !operation.ownerReference.IsZero() {
		references = append(references, operation.ownerReference)
	}
	return append(references, captured.References()...)
}

func (operation *operation) convergeCapturedRegistry(ctx context.Context, captured journalpublication.CapturedNativeJournal, store ownershipmutation.RegistryStore, budget *physicalwork.Budget, claims ownership.ClaimConvergence, bindings ownership.DocumentBindingConvergence) error {
	missing, err := captured.MissingClaimNamespaces()
	if err != nil {
		return err
	}
	return registrysettlement.ConvergeDocuments(ctx, store, captured.Owner(), missing, budget, claims, bindings, operation.registryReferences(captured)...)
}
