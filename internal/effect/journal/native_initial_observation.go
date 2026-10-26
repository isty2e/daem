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

type nativeDocumentObservationInput struct {
	owner      stateauthority.Authority
	before     durable.Snapshot
	aliases    []aggregate.DocumentAddress
	roots      NativeDocumentRoots
	filesystem mutationfs.RootedReader
	registry   ownershipmutation.RegistryReader
	limits     configdocument.Limits
	budget     *recovery.PhysicalWorkBudget
}

type nativeDocumentOriginal struct {
	document aggregate.Document
	mode     *recovery.PermissionMode
	identity mutationfs.EntryIdentity
}

// The batch retains actual pre-decision evidence, not a publication or execution
// grant. Root lifetime and the shared allocation remain with the containing owner.
type nativeDocumentObservationBatch struct {
	owner       stateauthority.Authority
	before      durable.Snapshot
	namespaces  []NativeDocumentNamespace
	registry    ownership.Registry
	originals   []nativeDocumentOriginal
	limits      configdocument.Limits
	budget      *recovery.PhysicalWorkBudget
	initialized bool
}

func observeNativeDocumentBatch(ctx context.Context, input nativeDocumentObservationInput) (nativeDocumentObservationBatch, error) {
	if ctx == nil || input.registry == nil {
		return nativeDocumentObservationBatch{}, fmt.Errorf("native observation requires context and the selected registry reader")
	}
	if err := errors.Join(ctx.Err(), input.owner.Validate(), input.limits.Validate(), input.budget.RequireCapturePending()); err != nil {
		return nativeDocumentObservationBatch{}, err
	}
	if _, err := documentAliasSet(ctx, input.aliases); err != nil {
		return nativeDocumentObservationBatch{}, err
	}
	before, err := indexDocumentSnapshot(ctx, input.before)
	if err != nil {
		return nativeDocumentObservationBatch{}, err
	}
	nativeBefore, err := before.nativeDestinations(ctx)
	if err != nil {
		return nativeDocumentObservationBatch{}, err
	}
	namespaces, err := observeNativeDocumentGroups(ctx, input)
	if err != nil {
		return nativeDocumentObservationBatch{}, err
	}
	var missing []pathauthority.Provisional
	for _, namespace := range namespaces {
		provisional, absent := namespace.observation.Provisional()
		if !absent {
			continue
		}
		footprints, err := before.documentFootprints(ctx, namespace.Addresses())
		if err != nil {
			return nativeDocumentObservationBatch{}, err
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
	current, err := readNativeCaptureRegistry(ctx, input.registry, input.owner, missing, input.budget)
	if err != nil {
		return nativeDocumentObservationBatch{}, err
	}
	originals := make([]nativeDocumentOriginal, 0, len(namespaces))
	for _, namespace := range namespaces {
		document, mode, identity, err := readNativeRecoveryDocument(ctx, namespace, min(input.limits.MaximumBytes, recovery.MaximumRecoveryBackupFileBytes), input.budget)
		if err != nil {
			return nativeDocumentObservationBatch{}, err
		}
		if document.Exists() && (identity == nil || identity.Kind() != mutationfs.EntryKindFile) {
			return nativeDocumentObservationBatch{}, fmt.Errorf("native original requires regular-file identity")
		}
		originals = append(originals, nativeDocumentOriginal{document: document, mode: mode, identity: identity})
	}
	if err := errors.Join(ctx.Err(), input.budget.RequireCapturePending()); err != nil {
		return nativeDocumentObservationBatch{}, err
	}
	return nativeDocumentObservationBatch{
		owner: input.owner, before: input.before, namespaces: namespaces, registry: current,
		originals: originals, limits: input.limits, budget: input.budget, initialized: true,
	}, nil
}

func observeNativeDocumentGroups(ctx context.Context, input nativeDocumentObservationInput) ([]NativeDocumentNamespace, error) {
	aliases := slices.Clone(input.aliases)
	slices.SortFunc(aliases, func(left, right aggregate.DocumentAddress) int {
		return compareDocumentAliasRefs(persistedDocumentAlias(left), persistedDocumentAlias(right))
	})
	indices := make(map[string]int)
	var representatives []NativeDocumentNamespace
	var groups [][]aggregate.DocumentAddress
	for _, alias := range aliases {
		namespace, err := ObserveNativeDocumentNamespace(ctx, []aggregate.DocumentAddress{alias}, input.roots, input.filesystem, input.budget)
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
		namespace, err := ObserveNativeDocumentNamespace(ctx, aliases, input.roots, input.filesystem, input.budget)
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

func (batch nativeDocumentObservationBatch) document(alias aggregate.DocumentAddress) (aggregate.Document, *recovery.PermissionMode, bool) {
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

func (batch nativeDocumentObservationBatch) prepareOwnership(ctx context.Context, after durable.Snapshot, operationID string) ([]nativeDocumentCaptureInput, error) {
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
