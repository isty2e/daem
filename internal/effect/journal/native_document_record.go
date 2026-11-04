package journal

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

type nativeDocumentReadOptions struct {
	ordinaryCapabilities RootedCapabilityResolver
	artifacts            *rootedpath.CapturedRoot
	filesystem           mutationfs.RootedReader
	budget               *physicalwork.Budget
	limits               configdocument.Limits
	codecs               aggregate.CodecCatalog
}

func (journal nativeJournalSnapshots) loadDocuments(ctx context.Context, options nativeDocumentReadOptions) (nativeDocumentRecords, error) {
	if ctx == nil || options.artifacts == nil || options.filesystem == nil || options.budget == nil {
		return nativeDocumentRecords{}, fmt.Errorf("native document read context, artifact root, filesystem and shared budget are required")
	}
	session, err := newDocumentReconstruction(ctx, journal, options.limits, options.codecs, documentReconstructionLoaded, nil)
	if err != nil {
		return nativeDocumentRecords{}, err
	}
	for {
		demand, present, err := session.next(ctx)
		if err != nil {
			return nativeDocumentRecords{}, err
		}
		if !present {
			break
		}
		images, err := loadDocumentImages(ctx, demand.original, demand.expected, options.artifacts, options.filesystem, options.budget, recovery.MaximumRecoveryBackupFileBytes)
		if err != nil {
			return nativeDocumentRecords{}, err
		}
		if err := session.supplyLoadedImages(ctx, demand, images); err != nil {
			return nativeDocumentRecords{}, err
		}
	}
	return session.finishLoaded(ctx)
}
