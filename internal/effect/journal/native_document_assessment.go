package journal

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

type nativeDocumentRecoveryReadOptions struct {
	documents nativeDocumentReadOptions
	roots     NativeDocumentRoots
	state     durable.SnapshotReader
	registry  ownershipmutation.RegistryReader
}

type nativeDocumentDisposition string

const (
	nativeDocumentsBefore   nativeDocumentDisposition = "documents_before"
	nativeDocumentsRollback nativeDocumentDisposition = "documents_rollback"
	nativeDocumentsAfter    nativeDocumentDisposition = "documents_after"
	nativeDocumentsFinalize nativeDocumentDisposition = "documents_finalize"
)

type nativeDocumentRecoveryStage struct {
	current     aggregate.Document
	mode        *recovery.PermissionMode
	identity    mutationfs.EntryIdentity
	restoration DocumentRestoration
	expected    bool
}

// This assessment covers all document groups, not ordinary entries, selection,
// removal, leases or whole-envelope retirement. It grants no execution authority.
type nativeDocumentAssessment struct {
	qualified   nativeQualifiedDocumentRecords
	state       durable.Snapshot
	registry    ownership.Registry
	stages      []nativeDocumentRecoveryStage
	disposition nativeDocumentDisposition
	rollback    ownership.ClaimConvergence
	finalize    ownership.ClaimConvergence
	bindings    ownership.DocumentBindingConvergence
}

func (journal nativeJournalSnapshots) assessDocumentRecovery(ctx context.Context, options nativeDocumentRecoveryReadOptions) (nativeDocumentAssessment, error) {
	if ctx == nil || options.state == nil || options.registry == nil {
		return nativeDocumentAssessment{}, fmt.Errorf("native recovery context, selected state reader and registry reader are required")
	}
	qualified, err := journal.loadQualifiedDocuments(ctx, options.documents, options.roots)
	if err != nil {
		return nativeDocumentAssessment{}, err
	}
	state, err := options.state(ctx)
	if err != nil {
		return nativeDocumentAssessment{}, fmt.Errorf("observe native recovery state: %w", err)
	}
	before, after := state.Equal(journal.before), state.Equal(journal.after)
	if !before && !after {
		return nativeDocumentAssessment{}, fmt.Errorf("native recovery state differs from both recorded Snapshots")
	}

	rollback, finalize, err := nativeDocumentClaimConvergences(ctx, qualified.records.claims)
	if err != nil {
		return nativeDocumentAssessment{}, err
	}
	removals := append(rollback.ExpectedRemovals(), finalize.ExpectedRemovals()...)
	var missingHistory []pathauthority.Provisional
	for index, namespace := range qualified.namespaces {
		claims := qualified.records.claims[index]
		if missing, provisional := namespace.observation.Provisional(); provisional && len(claims.Before())+len(claims.After()) != 0 {
			missingHistory = append(missingHistory, missing)
		}
	}
	var registry ownership.Registry
	if len(missingHistory) == 0 {
		registry, err = options.registry.LoadForClaimRemovals(ctx, removals, recovery.MaximumPhysicalPathDepth, options.documents.budget)
	} else {
		reader, supported := options.registry.(ownershipmutation.DocumentRegistryReader)
		if !supported {
			return nativeDocumentAssessment{}, fmt.Errorf("missing native history requires a document-qualified registry reader")
		}
		registry, err = reader.LoadForDocuments(ctx, journal.owner, missingHistory, recovery.MaximumPhysicalPathDepth, options.documents.budget)
	}
	if err != nil {
		return nativeDocumentAssessment{}, fmt.Errorf("observe native recovery registry: %w", err)
	}
	if err := validateNativeProvisionalRegistry(ctx, qualified, registry); err != nil {
		return nativeDocumentAssessment{}, err
	}
	claimsReady := true
	for _, pending := range qualified.records.provisional {
		if err := ctx.Err(); err != nil {
			return nativeDocumentAssessment{}, err
		}
		claimsReady = claimsReady && len(pending) == 0
	}
	_, rollbackChanged, rollbackErr := rollback.Apply(registry)
	_, finalizeChanged, finalizeErr := finalize.Apply(registry)
	metadata, err := nativeDocumentBindingPhases(ctx, qualified.records.groups, registry)
	if err != nil {
		return nativeDocumentAssessment{}, err
	}
	if before && after && (!metadata.unchanged || !nativeDocumentClaimsUnchanged(qualified.records.claims) || !metadata.ready || !claimsReady) {
		return nativeDocumentAssessment{}, fmt.Errorf("equal native Snapshots retain nontrivial document metadata work")
	}

	narrowed, err := journal.narrowedDocumentGroups(ctx, qualified.records.groups)
	if err != nil {
		return nativeDocumentAssessment{}, err
	}
	stages := make([]nativeDocumentRecoveryStage, 0, len(qualified.records.groups))
	allBefore, allExpected := true, true
	for index, group := range qualified.records.groups {
		if err := ctx.Err(); err != nil {
			return nativeDocumentAssessment{}, err
		}
		current, mode, identity, err := readNativeRecoveryDocument(ctx, qualified.namespaces[index], group.comparison.MaximumDocumentBytes(), options.documents.budget)
		if err != nil {
			return nativeDocumentAssessment{}, fmt.Errorf("observe native document[%d]: %w", index, err)
		}
		match, err := group.comparison.Match(ctx, current, mode)
		if err != nil {
			return nativeDocumentAssessment{}, err
		}
		var restoration DocumentRestoration
		if _, narrowing := narrowed[index]; before && narrowing && current.Equal(group.comparison.expected) {
			root, err := recovery.NewPartialDocumentState(current.Exists(), mode, nil)
			if err != nil {
				return nativeDocumentAssessment{}, err
			}
			if !root.SameRootMetadata(group.comparison.beforeState) && !root.SameRootMetadata(group.comparison.expectedState) {
				return nativeDocumentAssessment{}, fmt.Errorf("narrowed document root is outside the recorded metadata envelope")
			}
			restoration, err = newDocumentRestoration(current, mode, group.comparison.before, group.comparison.beforeMode())
		} else {
			restoration, err = group.comparison.RestoreBefore(ctx, current, mode)
		}
		if err != nil {
			if !match.Recoverable() && ctx.Err() == nil && !errors.Is(err, configdocument.ErrLimitExceeded) {
				return nativeDocumentAssessment{}, &nativeSelectedPhaseError{cause: err}
			}
			return nativeDocumentAssessment{}, fmt.Errorf("stage native document[%d]: %w", index, err)
		}
		allBefore = allBefore && !restoration.Changed()
		allExpected = allExpected && match.MatchesExpected()
		stages = append(stages, nativeDocumentRecoveryStage{current: current, mode: mode, identity: identity, restoration: restoration, expected: match.MatchesExpected()})
	}

	result := nativeDocumentAssessment{qualified: qualified, state: state, registry: registry, stages: stages, rollback: rollback, finalize: finalize, bindings: metadata.convergence}
	switch {
	case after && allExpected && claimsReady && metadata.ready && finalizeErr == nil:
		result.disposition = nativeDocumentsAfter
		if finalizeChanged || !metadata.after {
			result.disposition = nativeDocumentsFinalize
		}
	case before && metadata.before && rollbackErr == nil:
		result.disposition = nativeDocumentsRollback
		if !after && allBefore && !rollbackChanged {
			result.disposition = nativeDocumentsBefore
		}
	default:
		return nativeDocumentAssessment{}, errors.Join(fmt.Errorf("native document state, host and registry phases do not admit recovery"), rollbackErr, finalizeErr)
	}
	if err := ctx.Err(); err != nil {
		return nativeDocumentAssessment{}, err
	}
	return result, nil
}

func readNativeRecoveryDocument(ctx context.Context, namespace NativeDocumentNamespace, maximumBytes int64, budget *recovery.PhysicalWorkBudget) (aggregate.Document, *recovery.PermissionMode, mutationfs.EntryIdentity, error) {
	if maximumBytes <= 0 || maximumBytes > recovery.MaximumRecoveryBackupFileBytes || budget == nil {
		return aggregate.Document{}, nil, nil, fmt.Errorf("native document read limit or shared budget is invalid")
	}
	limit := min(maximumBytes, budget.RemainingBytes())
	capability, err := namespace.AcquireReferent(ctx, budget)
	if err != nil {
		return aggregate.Document{}, nil, nil, err
	}
	content, mode, identity, readErr := namespace.filesystem.ReadRootedRegularFileUpTo(ctx, capability, limit)
	closeErr := capability.Close()
	charged := int64(len(content))
	if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
		charged = limit
	}
	work, workErr := recovery.NewArtifactWork(0, charged)
	if workErr == nil {
		workErr = budget.AdmitTree(work)
	}
	if closeErr == nil && workErr == nil && errors.Is(readErr, fs.ErrNotExist) {
		return aggregate.AbsentDocument(), nil, nil, ctx.Err()
	}
	if err := errors.Join(readErr, closeErr, workErr, ctx.Err()); err != nil {
		return aggregate.Document{}, nil, nil, err
	}
	return aggregate.ExistingDocument(content), recovery.NewPermissionMode(mode), identity, nil
}

func nativeDocumentClaimConvergences(ctx context.Context, transitions []ownershipmutation.DocumentClaimTransition) (ownership.ClaimConvergence, ownership.ClaimConvergence, error) {
	var rollback, finalize []ownership.DocumentClaimChange
	for _, transition := range transitions {
		if err := ctx.Err(); err != nil {
			return ownership.ClaimConvergence{}, ownership.ClaimConvergence{}, err
		}
		if len(transition.Before()) == 0 && len(transition.After()) == 0 {
			continue
		}
		before, err := transition.Rollback()
		if err != nil {
			return ownership.ClaimConvergence{}, ownership.ClaimConvergence{}, err
		}
		after, err := transition.Finalization()
		if err != nil {
			return ownership.ClaimConvergence{}, ownership.ClaimConvergence{}, err
		}
		rollback, finalize = append(rollback, before), append(finalize, after)
	}
	empty, err := ownership.NewClaimConvergence(nil)
	if err != nil {
		return ownership.ClaimConvergence{}, ownership.ClaimConvergence{}, err
	}
	before, err := empty.WithDocumentChanges(rollback)
	if err != nil {
		return ownership.ClaimConvergence{}, ownership.ClaimConvergence{}, err
	}
	after, err := empty.WithDocumentChanges(finalize)
	return before, after, err
}

type nativeDocumentBindingPhase struct {
	before, after, ready, unchanged bool
	convergence                     ownership.DocumentBindingConvergence
}

func nativeDocumentBindingPhases(ctx context.Context, groups []documentStateGroup, registry ownership.Registry) (nativeDocumentBindingPhase, error) {
	phase := nativeDocumentBindingPhase{before: true, after: true, ready: true, unchanged: true}
	var exact []ownership.DocumentBindingIntent
	for _, group := range groups {
		for _, intent := range group.bindingIntents {
			if err := ctx.Err(); err != nil {
				return nativeDocumentBindingPhase{}, err
			}
			current := ownership.NoDocumentBinding()
			if binding, found := registry.DocumentBinding(intent.Owner().Key(), intent.Destination()); found {
				var err error
				current, err = ownership.PresentDocumentBinding(binding)
				if err != nil {
					return nativeDocumentBindingPhase{}, err
				}
			}
			phase.before = phase.before && current.Equal(intent.Before())
			after, ready := intent.KnownAfter()
			phase.ready = phase.ready && ready
			phase.after = phase.after && ready && current.Equal(after)
			changed, err := intent.ChangesBinding()
			if err != nil {
				return nativeDocumentBindingPhase{}, err
			}
			phase.unchanged = phase.unchanged && !changed
			if ready {
				exact = append(exact, intent)
			}
		}
	}
	convergence, err := ownership.NewDocumentBindingConvergence(exact)
	if err != nil {
		return nativeDocumentBindingPhase{}, err
	}
	_, _, err = convergence.Apply(registry)
	if err != nil {
		return nativeDocumentBindingPhase{}, err
	}
	phase.convergence = convergence
	return phase, nil
}

func nativeDocumentClaimsUnchanged(transitions []ownershipmutation.DocumentClaimTransition) bool {
	var before, after []ownership.Claim
	for _, transition := range transitions {
		before, after = append(before, transition.Before()...), append(after, transition.After()...)
	}
	left, leftErr := ownership.NewRegistry(before)
	right, rightErr := ownership.NewRegistry(after)
	return leftErr == nil && rightErr == nil && left.Equal(right)
}

func validateNativeProvisionalRegistry(ctx context.Context, qualified nativeQualifiedDocumentRecords, registry ownership.Registry) error {
	for index, acquires := range qualified.records.provisional {
		for _, acquire := range acquires {
			if err := ctx.Err(); err != nil {
				return err
			}
			observation := qualified.namespaces[index].observation
			var conflict ownership.Claim
			var found bool
			if exact, known := observation.Exact(); known {
				address, err := ownership.NewManagedAddress(exact, acquire.intent.Footprint())
				if err != nil {
					return err
				}
				if err := acquire.intent.AdmitAddress(address); err != nil {
					return err
				}
				conflict, found = registry.Conflict(address)
			} else {
				candidate, known := observation.Provisional()
				if !known || !candidate.Equal(acquire.intent.Path()) {
					return fmt.Errorf("native provisional acquisition namespace changed")
				}
				conflict, found = registry.ProvisionalAncestorConflict(candidate)
			}
			if found {
				return fmt.Errorf("native provisional acquisition conflicts with a durable claim owned by %q", conflict.Owner().ManifestPath())
			}
		}
	}
	return ctx.Err()
}

func (journal nativeJournalSnapshots) narrowedDocumentGroups(ctx context.Context, groups []documentStateGroup) (map[int]struct{}, error) {
	before, after := make(map[aggregate.DocumentAddress]struct{}), make(map[aggregate.DocumentAddress]struct{})
	keyedAfter := make(map[aggregate.DocumentAddress]struct{})
	for _, row := range journal.after.ManagedAggregates() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, native := aggregate.HostConfigSyntaxForCodec(row.Contribution().CodecContractID()); native {
			keyedAfter[row.Contribution().Contract().Address().Document()] = struct{}{}
		}
	}
	for side, snapshot := range []durable.Snapshot{journal.before, journal.after} {
		for _, row := range snapshot.ManagedPaths() {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			id, found := topologyprojection.EntityID(row.Subject())
			if !found || id.Kind() != entity.KindHostConfig {
				continue
			}
			for _, target := range row.ConsumerTargets() {
				address, err := aggregate.NewDocumentAddress(target, row.Scope(), row.Destination())
				if err != nil {
					return nil, err
				}
				if side == 0 {
					before[address] = struct{}{}
				} else {
					after[address] = struct{}{}
				}
			}
		}
	}
	result := make(map[int]struct{})
	for index, group := range groups {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if group.comparison.whole || !group.comparison.before.Exists() || !group.comparison.expected.Exists() {
			continue
		}
		for _, alias := range group.aliases {
			_, wasWhole := before[alias]
			_, staysWhole := after[alias]
			_, becomesKeyed := keyedAfter[alias]
			if wasWhole && !staysWhole && becomesKeyed {
				result[index] = struct{}{}
			}
		}
	}
	return result, nil
}
