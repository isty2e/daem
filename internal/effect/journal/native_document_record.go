package journal

import (
	"context"
	"fmt"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

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

func loadDocuments(journal journalrecord.Native, ctx context.Context, options nativeDocumentReadOptions) (journalrecord.LoadedDocuments, error) {
	if ctx == nil || options.artifacts == nil || options.filesystem == nil || options.budget == nil {
		return journalrecord.LoadedDocuments{}, fmt.Errorf("native document read context, artifact root, filesystem and shared budget are required")
	}
	session, err := journal.BeginLoadedDocuments(ctx, options.limits, options.codecs)
	if err != nil {
		return journalrecord.LoadedDocuments{}, err
	}
	for {
		demand, present, err := session.Next(ctx)
		if err != nil {
			return journalrecord.LoadedDocuments{}, err
		}
		if !present {
			break
		}
		original, expected, err := loadDocumentImages(ctx, demand.Original(), demand.Expected(), options.artifacts, options.filesystem, options.budget, recovery.MaximumRecoveryBackupFileBytes)
		if err != nil {
			return journalrecord.LoadedDocuments{}, err
		}
		if err := session.SupplyLoaded(ctx, demand, original, expected); err != nil {
			return journalrecord.LoadedDocuments{}, err
		}
	}
	return session.FinishLoaded(ctx)
}
