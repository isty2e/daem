package journal

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
)

type nativePublicationContext struct {
	owner                stateauthority.Authority
	before               durable.Snapshot
	roots                NativeDocumentRoots
	limits               configdocument.Limits
	budget               *recovery.PhysicalWorkBudget
	resolver             func(output.Destination) (string, error)
	ordinaryCapabilities RootedCapabilityResolver
}

func reloadNativeCapture(ctx context.Context, directory, operationID, fingerprint string, batch NativeDocumentObservationBatch, after durable.Snapshot, options CaptureOptions) (CapturedNativeJournal, error) {
	return reloadNativePublication(ctx, directory, operationID, fingerprint, nativePublicationContext{owner: batch.owner, before: batch.before, roots: batch.roots, limits: batch.limits, budget: batch.budget, resolver: options.Resolver, ordinaryCapabilities: options.RootedCapability}, after, options)
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
	return captured.replaceRecord(ctx, candidate, options)
}

func (captured CapturedNativeJournal) NeedsPromotion() bool {
	if len(captured.journal.record.ProvisionalAcquires) != 0 {
		return true
	}
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
	if !observed.HostMatchesExpected() || !observed.assessment.claimsReady {
		return false
	}
	assessment := observed.assessment
	for _, group := range assessment.qualified.records.groups {
		for _, intent := range group.bindingIntents {
			if _, ready := intent.KnownAfter(); !ready {
				return false
			}
		}
	}
	_, changed, err := assessment.preparation.Apply(assessment.registry)
	return err == nil && !changed
}
