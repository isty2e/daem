package publication

import (
	"context"
	"fmt"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
)

type nativePublicationContext struct {
	protection           journalnamespace.ReservationSelectors
	expected             []journalnamespace.DocumentNamespace
	owner                stateauthority.Authority
	before               durable.Snapshot
	roots                journalnamespace.Roots
	limits               configdocument.Limits
	budget               *physicalwork.Budget
	resolver             func(output.Destination) (string, error)
	ordinaryCapabilities journalnamespace.CapabilityResolver
	references           []pathauthority.Reference
}

func reloadNativeCapture(ctx context.Context, directory, operationID, fingerprint string, batch NativeDocumentObservationBatch, after durable.Snapshot, options CaptureOptions) (CapturedNativeJournal, error) {
	return reloadNativePublication(ctx, directory, operationID, fingerprint, nativePublicationContext{owner: batch.owner, before: batch.Evidence().BeforeState(), roots: batch.roots, protection: batch.protection, expected: batch.Evidence().Namespaces(), limits: batch.Evidence().DocumentLimits(), budget: batch.budget, resolver: options.Resolver, ordinaryCapabilities: options.RootedCapability}, after, options)
}

type NativePromotionOptions struct {
	journalqualification.NativeRecoveryOptions
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
	observed, err := captured.AssessForward(ctx, options.NativeRecoveryOptions)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	candidate, changed, err := observed.PromoteDocument(ctx, index)
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
