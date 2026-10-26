package journal

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// NativeDocumentObservationInput supplies independently selected observation
// dependencies. Root and allocation ownership stay with the containing operation.
type NativeDocumentObservationInput struct {
	Owner      stateauthority.Authority
	Before     durable.Snapshot
	Aliases    []aggregate.DocumentAddress
	Roots      NativeDocumentRoots
	Filesystem mutationfs.RootedReader
	Registry   ownershipmutation.RegistryReader
	Limits     configdocument.Limits
	Budget     *recovery.PhysicalWorkBudget
}

type nativeDocumentOriginal struct {
	document aggregate.Document
	mode     *recovery.PermissionMode
	identity mutationfs.EntryIdentity
}

// NativeDocumentObservationBatch retains evidence, not a publication or execution
// grant. Root lifetime and the shared allocation remain with the containing owner.
type NativeDocumentObservationBatch struct {
	owner       stateauthority.Authority
	before      durable.Snapshot
	namespaces  []NativeDocumentNamespace
	registry    ownership.Registry
	originals   []nativeDocumentOriginal
	limits      configdocument.Limits
	budget      *recovery.PhysicalWorkBudget
	initialized bool
}

// ObserveNativeDocuments borrows the selected roots and pending budget. The
// resulting evidence requires separate ownership and operation qualification.
func ObserveNativeDocuments(ctx context.Context, input NativeDocumentObservationInput) (NativeDocumentObservationBatch, error) {
	if ctx == nil || input.Registry == nil {
		return NativeDocumentObservationBatch{}, fmt.Errorf("native observation requires context and the selected registry reader")
	}
	if err := errors.Join(ctx.Err(), input.Owner.Validate(), input.Limits.Validate(), input.Budget.RequireCapturePending()); err != nil {
		return NativeDocumentObservationBatch{}, err
	}
	if _, err := documentAliasSet(ctx, input.Aliases); err != nil {
		return NativeDocumentObservationBatch{}, err
	}
	before, err := indexDocumentSnapshot(ctx, input.Before)
	if err != nil {
		return NativeDocumentObservationBatch{}, err
	}
	nativeBefore, err := before.nativeDestinations(ctx)
	if err != nil {
		return NativeDocumentObservationBatch{}, err
	}
	namespaces, err := observeNativeDocumentGroups(ctx, input)
	if err != nil {
		return NativeDocumentObservationBatch{}, err
	}
	var missing []pathauthority.Provisional
	for _, namespace := range namespaces {
		provisional, absent := namespace.observation.Provisional()
		if !absent {
			continue
		}
		footprints, err := before.documentFootprints(ctx, namespace.Addresses())
		if err != nil {
			return NativeDocumentObservationBatch{}, err
		}
		hasHistory := len(footprints) != 0
		for _, alias := range namespace.Addresses() {
			_, native := nativeBefore[alias.AggregateRoot()]
			hasHistory = hasHistory || native
		}
		if hasHistory {
			missing = append(missing, provisional)
		}
	}
	current, err := readNativeCaptureRegistry(ctx, input.Registry, input.Owner, missing, input.Budget)
	if err != nil {
		return NativeDocumentObservationBatch{}, err
	}
	originals := make([]nativeDocumentOriginal, 0, len(namespaces))
	for _, namespace := range namespaces {
		document, mode, identity, err := readNativeRecoveryDocument(ctx, namespace, min(input.Limits.MaximumBytes, recovery.MaximumRecoveryBackupFileBytes), input.Budget)
		if err != nil {
			return NativeDocumentObservationBatch{}, err
		}
		if document.Exists() && (identity == nil || identity.Kind() != mutationfs.EntryKindFile) {
			return NativeDocumentObservationBatch{}, fmt.Errorf("native original requires regular-file identity")
		}
		originals = append(originals, nativeDocumentOriginal{document: document, mode: mode, identity: identity})
	}
	if err := errors.Join(ctx.Err(), input.Budget.RequireCapturePending()); err != nil {
		return NativeDocumentObservationBatch{}, err
	}
	return NativeDocumentObservationBatch{
		owner: input.Owner, before: input.Before, namespaces: namespaces, registry: current,
		originals: originals, limits: input.Limits, budget: input.Budget, initialized: true,
	}, nil
}

func observeNativeDocumentGroups(ctx context.Context, input NativeDocumentObservationInput) ([]NativeDocumentNamespace, error) {
	aliases := slices.Clone(input.Aliases)
	slices.SortFunc(aliases, func(left, right aggregate.DocumentAddress) int {
		return compareDocumentAliasRefs(persistedDocumentAlias(left), persistedDocumentAlias(right))
	})
	indices := make(map[string]int)
	var representatives []NativeDocumentNamespace
	var groups [][]aggregate.DocumentAddress
	for _, alias := range aliases {
		namespace, err := ObserveNativeDocumentNamespace(ctx, []aggregate.DocumentAddress{alias}, input.Roots, input.Filesystem, input.Budget)
		if err != nil {
			return nil, err
		}
		key := ""
		if exact, known := namespace.observation.Exact(); known {
			key = "exact:" + exact.Key()
		} else {
			provisional, _ := namespace.observation.Provisional()
			key = "provisional:" + provisional.CandidateKey()
		}
		index, present := indices[key]
		if !present {
			indices[key] = len(groups)
			representatives = append(representatives, namespace)
			groups = append(groups, []aggregate.DocumentAddress{alias})
			continue
		}
		if !sameNativeDocumentObservation(representatives[index].observation, namespace.observation) {
			return nil, fmt.Errorf("native aliases disagree on the same comparison identity")
		}
		groups[index] = append(groups[index], alias)
	}
	result := make([]NativeDocumentNamespace, 0, len(groups))
	for index, aliases := range groups {
		namespace, err := ObserveNativeDocumentNamespace(ctx, aliases, input.Roots, input.Filesystem, input.Budget)
		if err != nil {
			return nil, err
		}
		if !sameNativeDocumentObservation(representatives[index].observation, namespace.observation) {
			return nil, fmt.Errorf("native namespace changed while coalescing aliases")
		}
		result = append(result, namespace)
	}
	return result, nil
}

func readNativeCaptureRegistry(ctx context.Context, reader ownershipmutation.RegistryReader, owner stateauthority.Authority, missing []pathauthority.Provisional, budget *recovery.PhysicalWorkBudget) (ownership.Registry, error) {
	if reader == nil || budget == nil {
		return ownership.Registry{}, fmt.Errorf("native capture requires its selected registry reader and shared budget")
	}
	if len(missing) == 0 {
		return reader.LoadForClaimRemovals(ctx, nil, recovery.MaximumPhysicalPathDepth, budget)
	}
	qualified, supported := reader.(ownershipmutation.DocumentRegistryReader)
	if !supported {
		return ownership.Registry{}, fmt.Errorf("missing native history requires a document-qualified registry reader")
	}
	return qualified.LoadForDocuments(ctx, owner, missing, recovery.MaximumPhysicalPathDepth, budget)
}

// Document returns the retained original; false means unobserved, not absent.
func (batch NativeDocumentObservationBatch) Document(alias aggregate.DocumentAddress) (aggregate.Document, *recovery.PermissionMode, bool) {
	if !batch.initialized {
		return aggregate.Document{}, nil, false
	}
	for index, namespace := range batch.namespaces {
		if slices.Contains(namespace.Addresses(), alias) {
			original := batch.originals[index]
			if original.mode == nil {
				return original.document, nil, true
			}
			mode := *original.mode
			return original.document, &mode, true
		}
	}
	return aggregate.Document{}, nil, false
}

func (batch NativeDocumentObservationBatch) prepareOwnership(ctx context.Context, after durable.Snapshot, operationID string) ([]nativeDocumentCaptureInput, error) {
	if !batch.initialized || ctx == nil {
		return nil, fmt.Errorf("native observation batch and context are required")
	}
	if err := errors.Join(ctx.Err(), batch.budget.RequireCapturePending()); err != nil {
		return nil, err
	}
	for index, namespace := range batch.namespaces {
		document, mode, identity, err := readNativeRecoveryDocument(ctx, namespace, min(batch.limits.MaximumBytes, recovery.MaximumRecoveryBackupFileBytes), batch.budget)
		if err != nil {
			return nil, err
		}
		original := batch.originals[index]
		sameMode := mode == nil && original.mode == nil || mode != nil && original.mode != nil && *mode == *original.mode
		sameIdentity := identity == nil && original.identity == nil || identity != nil && original.identity != nil && identity.Equal(original.identity)
		if !document.Equal(original.document) || !sameMode || !sameIdentity {
			return nil, fmt.Errorf("native original changed after its decision observation")
		}
	}
	if err := errors.Join(ctx.Err(), batch.budget.RequireCapturePending()); err != nil {
		return nil, err
	}
	input := nativeCaptureOwnershipInput{
		owner: batch.owner, operationID: operationID, before: batch.before, after: after,
		namespaces: batch.namespaces, limits: batch.limits, budget: batch.budget,
	}
	return input.prepareFromRegistry(ctx, batch.registry)
}
