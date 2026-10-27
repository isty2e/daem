package journal

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output/ownership"
)

type nativePublicationContext struct {
	owner  stateauthority.Authority
	before durable.Snapshot
	roots  NativeDocumentRoots
	limits configdocument.Limits
	budget *recovery.PhysicalWorkBudget
}

func reloadNativeCapture(ctx context.Context, directory, operationID, fingerprint string, batch NativeDocumentObservationBatch, after durable.Snapshot, options CaptureOptions) (CapturedNativeJournal, error) {
	return reloadNativePublication(ctx, directory, operationID, fingerprint, nativePublicationContext{owner: batch.owner, before: batch.before, roots: batch.namespaces[0].roots, limits: batch.limits, budget: batch.budget}, after, options)
}

type NativePromotionOptions struct {
	NativeRecoveryOptions
	RecordAuthority *rootedpath.EntryAuthority
	StateCodec      durable.SnapshotCodec
}

// PromoteDocument derives the complete pending group from fresh exact namespace
// evidence, publishes it by record CAS, then reloads the native operation.
// Original namespace/images remain intact; no Registry or state write occurs here.
func (captured CapturedNativeJournal) PromoteDocument(ctx context.Context, index int, options NativePromotionOptions) (CapturedNativeJournal, error) {
	if err := captured.Validate(); err != nil {
		return CapturedNativeJournal{}, err
	}
	filesystem, admitted := options.Filesystem.(mutationfs.Store)
	if !admitted {
		return CapturedNativeJournal{}, fmt.Errorf("native promotion requires selected inventory observation ports")
	}
	if options.RecordAuthority == nil || options.StateCodec == nil || index < 0 || index >= len(captured.journal.record.DocumentGroups) {
		return CapturedNativeJournal{}, fmt.Errorf("native promotion requires selected record authority, codec and group")
	}
	observed, err := captured.AssessRecovery(ctx, options.NativeRecoveryOptions)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	assessment := observed.assessment
	if !assessment.state.Equal(captured.journal.before) || !assessment.stages[index].expected {
		return CapturedNativeJournal{}, fmt.Errorf("native promotion requires observed Before state and a published expected document")
	}
	namespace := assessment.qualified.namespaces[index]
	exact, known := namespace.observation.Exact()
	if !known {
		return CapturedNativeJournal{}, fmt.Errorf("published native document still has no exact namespace")
	}
	group := captured.journal.record.DocumentGroups[index]
	pending, err := group.CapturedTarget.provisional()
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	if err := pending.AdmitsExact(exact); err != nil {
		return CapturedNativeJournal{}, err
	}

	changed := false
	if group.ExclusiveClaims.Kind == "provisional" {
		group.ExclusiveClaims, err = group.ExclusiveClaims.promoted(namespace.Addresses(), captured.journal.owner, captured.capture.OperationID, pending, exact)
		if err != nil {
			return CapturedNativeJournal{}, err
		}
		changed = true
	}
	bindings, err := group.canonicalBindingIntents(ctx, captured.journal.owner)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	group.BindingIntents = slices.Clone(group.BindingIntents)
	for position, intent := range bindings {
		if _, ready := intent.KnownAfter(); ready {
			continue
		}
		promoted, err := intent.Promote(exact)
		if err != nil {
			return CapturedNativeJournal{}, err
		}
		group.BindingIntents[position], err = persistedDocumentBindingIntent(promoted)
		if err != nil {
			return CapturedNativeJournal{}, err
		}
		changed = true
	}
	if !changed {
		return captured, nil
	}
	candidate := captured.journal.record
	candidate.DocumentGroups = slices.Clone(candidate.DocumentGroups)
	candidate.DocumentGroups[index] = group
	content, err := marshalNativeJournalRecord(ctx, candidate, captured.journal.before, captured.journal.after, options.StateCodec)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	work, err := recovery.NewArtifactWork(0, int64(len(content)))
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	if err := captured.budget.AdmitTree(work); err != nil {
		return CapturedNativeJournal{}, err
	}

	capability, err := options.RecordAuthority.Acquire()
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	path, err := capability.Destination().LexicalPath()
	if err != nil || path != captured.capture.JournalPath {
		return CapturedNativeJournal{}, errors.Join(fmt.Errorf("native promotion record authority identifies another entry"), err, capability.Close())
	}
	limit := min(maximumRecoveryJournalBytes, captured.budget.RemainingBytes())
	if limit <= 0 {
		return CapturedNativeJournal{}, errors.Join(fmt.Errorf("native promotion exhausted its record byte allocation"), capability.Close())
	}
	bytes, mode, identity, err := options.Filesystem.ReadRootedRegularFileUpTo(ctx, capability, limit)
	charged := int64(len(bytes))
	if err != nil {
		charged = limit
	}
	work, workErr := recovery.NewArtifactWork(0, charged)
	if workErr == nil {
		workErr = captured.budget.AdmitTree(work)
	}
	err = errors.Join(err, workErr)
	if err != nil || mode.Perm() != recoveryJournalMode || recoveryJournalRecordFingerprint(bytes) != captured.capture.RecordFingerprint {
		return CapturedNativeJournal{}, errors.Join(fmt.Errorf("native promotion journal revision changed"), err, capability.Close())
	}
	outcome, parent, err := options.Filesystem.ReplaceRootedFileAndRefreshParent(ctx, capability, content, recoveryJournalMode, identity, captured.active.identity)
	if err != nil {
		return CapturedNativeJournal{}, &NativeJournalPublicationError{directory: captured.capture.Directory, outcome: outcome, cause: err}
	}
	if outcome.State() != mutationfs.CommitOutcomeComplete || parent == nil {
		return CapturedNativeJournal{}, fmt.Errorf("native promotion has no confirmed refreshed record authority")
	}
	active, err := newActiveJournalAuthority(parent)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	if err := ValidateActiveJournalAuthority(ctx, options.Filesystem, options.OperationAuthority, active); err != nil {
		return CapturedNativeJournal{}, err
	}
	return reloadNativePublication(ctx, captured.capture.Directory, captured.capture.OperationID, recoveryJournalRecordFingerprint(content), nativePublicationContext{
		owner: captured.journal.owner, before: captured.journal.before, roots: namespace.roots, limits: captured.qualified.records.groups[index].comparison.limits, budget: captured.budget,
	}, captured.journal.after, CaptureOptions{Filesystem: filesystem, StateCodec: options.StateCodec, Codecs: options.Codecs, OperationAuthority: options.OperationAuthority})
}

func (captured CapturedNativeJournal) NeedsPromotion() bool {
	for _, document := range captured.Documents() {
		if len(document.ProvisionalAcquires()) != 0 {
			return true
		}
		for _, intent := range document.BindingIntents() {
			if _, ready := intent.KnownAfter(); !ready {
				return true
			}
		}
	}
	return false
}

// HostReadyForStateCommit requires both expected host data and already prepared
// exact metadata. A pending binding or unreserved new claim is not commit-ready.
func (observed NativeRecoveryAssessment) HostReadyForStateCommit() bool {
	if !observed.HostMatchesExpected() {
		return false
	}
	assessment := observed.assessment
	for _, pending := range assessment.qualified.records.provisional {
		if len(pending) != 0 {
			return false
		}
	}
	for _, group := range assessment.qualified.records.groups {
		for _, intent := range group.bindingIntents {
			if _, ready := intent.KnownAfter(); !ready {
				return false
			}
		}
	}
	changes := make([]ownership.DocumentClaimChange, 0, len(assessment.qualified.records.claims))
	for _, claims := range assessment.qualified.records.claims {
		if len(claims.Before())+len(claims.After()) == 0 {
			continue
		}
		change, err := claims.Preparation()
		if err != nil {
			return false
		}
		changes = append(changes, change)
	}
	prepared, err := ownership.NewClaimConvergence(nil)
	if err != nil {
		return false
	}
	prepared, err = prepared.WithDocumentChanges(changes)
	if err != nil {
		return false
	}
	_, changed, err := prepared.Apply(assessment.registry)
	return err == nil && !changed
}
