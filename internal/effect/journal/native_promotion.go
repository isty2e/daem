package journal

import (
	"context"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
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
	return reloadNativePublication(ctx, directory, operationID, fingerprint, nativePublicationContext{owner: batch.owner, before: batch.before, roots: batch.roots, limits: batch.limits, budget: batch.budget, resolver: options.Resolver, ordinaryCapabilities: options.RootedCapability}, after, options)
}
