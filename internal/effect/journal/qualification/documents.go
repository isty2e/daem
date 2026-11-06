package qualification

import (
	"context"
	"fmt"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

type ReadOptions struct {
	OrdinaryCapabilities journalnamespace.CapabilityResolver
	Artifacts            *rootedpath.CapturedRoot
	Filesystem           mutationfs.RootedReader
	Budget               *physicalwork.Budget
	Limits               configdocument.Limits
	Codecs               aggregate.CodecCatalog
}

func LoadDocuments(journal journalrecord.Native, ctx context.Context, options ReadOptions) (journalrecord.LoadedDocuments, error) {
	if ctx == nil || options.Artifacts == nil || options.Filesystem == nil || options.Budget == nil {
		return journalrecord.LoadedDocuments{}, fmt.Errorf("native document read context, artifact root, filesystem and shared budget are required")
	}
	session, err := journal.BeginLoadedDocuments(ctx, options.Limits, options.Codecs)
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
		original, expected, err := LoadImages(ctx, demand.Original(), demand.Expected(), options.Artifacts, options.Filesystem, options.Budget, recovery.MaximumRecoveryBackupFileBytes)
		if err != nil {
			return journalrecord.LoadedDocuments{}, err
		}
		if err := session.SupplyLoaded(ctx, demand, original, expected); err != nil {
			return journalrecord.LoadedDocuments{}, err
		}
	}
	return session.FinishLoaded(ctx)
}
