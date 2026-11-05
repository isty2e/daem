package journal

import (
	"context"
	"fmt"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
)

type nativePublicationContext struct {
	owner                stateauthority.Authority
	before               durable.Snapshot
	roots                journalnamespace.Roots
	limits               configdocument.Limits
	budget               *physicalwork.Budget
	resolver             func(output.Destination) (string, error)
	ordinaryCapabilities RootedCapabilityResolver
}

func reloadNativeCapture(ctx context.Context, directory, operationID, fingerprint string, batch NativeDocumentObservationBatch, after durable.Snapshot, options CaptureOptions) (CapturedNativeJournal, error) {
	return reloadNativePublication(ctx, directory, operationID, fingerprint, nativePublicationContext{owner: batch.owner, before: batch.Evidence().BeforeState(), roots: batch.roots, limits: batch.Evidence().DocumentLimits(), budget: batch.budget, resolver: options.Resolver, ordinaryCapabilities: options.RootedCapability}, after, options)
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
	if options.RecordAuthority == nil || options.StateCodec == nil || index < 0 {
		return CapturedNativeJournal{}, fmt.Errorf("native promotion requires selected record authority, codec and group")
	}
	if _, err := captured.journal.DocumentAt(index); err != nil {
		return CapturedNativeJournal{}, fmt.Errorf("native promotion requires selected record authority, codec and group")
	}
	observed, err := captured.AssessRecovery(ctx, options.NativeRecoveryOptions)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	assessment := observed.assessment
	if !assessment.state.Equal(captured.journal.Before()) || !assessment.stages[index].expected {
		return CapturedNativeJournal{}, fmt.Errorf("native promotion requires observed Before state and a published expected document")
	}
	namespace, present := assessment.qualified.NamespaceAt(index)
	if !present {
		return CapturedNativeJournal{}, fmt.Errorf("native recovery document facts are incomplete")
	}
	exact, known := namespace.Observation().Exact()
	if !known {
		return CapturedNativeJournal{}, fmt.Errorf("published native document still has no exact namespace")
	}
	candidate, changed, err := captured.journal.PromoteDocument(ctx, index, namespace.Addresses(), exact)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	if !changed {
		return captured, nil
	}
	return captured.replaceRecord(ctx, candidate, options)
}

func (captured CapturedNativeJournal) NeedsPromotion() bool {
	intents, err := captured.journal.ProvisionalAcquires()
	if err != nil {
		return true
	}
	if len(intents) != 0 {
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
	for index := range assessment.documents.Len() {
		loaded, present := assessment.documents.At(index)
		if !present {
			return false
		}
		group := loaded.Group()
		for _, intent := range group.BindingIntents() {
			if _, ready := intent.KnownAfter(); !ready {
				return false
			}
		}
	}
	_, changed, err := assessment.preparation.Apply(assessment.registry)
	return err == nil && !changed
}
