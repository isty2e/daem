package journal

import (
	"context"
	"fmt"
	"slices"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/topology"
)

type documentReconstructionMode uint8

const (
	documentReconstructionPrepared documentReconstructionMode = iota + 1
	documentReconstructionLoaded
)

type documentReconstructionPhase uint8

const (
	documentReconstructionReady documentReconstructionPhase = iota + 1
	documentReconstructionAwaitingImages
	documentReconstructionComplete
	documentReconstructionFinished
	documentReconstructionFailed
)

type documentImageDemand struct {
	session            *documentReconstructionSession
	index              int
	original, expected recoveryDocumentImage
}

type documentReconstructionGroup struct {
	record      recoveryDocumentGroup
	aliases     []aggregate.DocumentAddress
	subjects    []topology.SubjectID
	selections  []aggregate.Selection
	claims      ownershipmutation.DocumentClaimTransition
	provisional []journaldocument.ProvisionalAcquire
}

// Artifact reads stay outside the session; prepared image evidence is derived
// here. Each policy check precedes the next demand, without exposing partial records.
type documentReconstructionSession struct {
	mode      documentReconstructionMode
	phase     documentReconstructionPhase
	journal   nativeJournalSnapshots
	limits    configdocument.Limits
	codecs    aggregate.CodecCatalog
	indexes   journaldocument.SnapshotIndexes
	bindings  [][]ownership.DocumentBindingIntent
	prepared  []journaldocument.Comparison
	canonical []journaldocument.Comparison
	inputs    []journaldocument.StateGroupInput
	result    nativeDocumentRecords
	cursor    int
	pending   documentReconstructionGroup
}

func newDocumentReconstruction(ctx context.Context, journal nativeJournalSnapshots, limits configdocument.Limits, codecs aggregate.CodecCatalog, mode documentReconstructionMode, prepared []journaldocument.Comparison) (*documentReconstructionSession, error) {
	if ctx == nil {
		return nil, fmt.Errorf("prepared native document context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := journal.record.validateRemovalCoverage(); err != nil {
		return nil, err
	}
	if err := journal.record.validateWriterSpellingPartition(ctx); err != nil {
		return nil, err
	}
	if err := journal.owner.Validate(); err != nil {
		return nil, err
	}
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	if mode == documentReconstructionPrepared && len(prepared) != len(journal.record.DocumentGroups) {
		return nil, fmt.Errorf("prepared comparisons must cover every recorded document group exactly once")
	}
	if mode != documentReconstructionPrepared && mode != documentReconstructionLoaded {
		return nil, fmt.Errorf("document reconstruction requires its selected image source")
	}

	indexes, err := journaldocument.IndexSnapshots(ctx, journal.before, journal.after)
	if err != nil {
		return nil, err
	}
	bindings, err := journal.documentBindings(ctx)
	if err != nil {
		return nil, err
	}

	phase := documentReconstructionReady
	if len(journal.record.DocumentGroups) == 0 {
		phase = documentReconstructionComplete
	}
	return &documentReconstructionSession{
		mode: mode, phase: phase, journal: journal, limits: limits, codecs: codecs,
		indexes: indexes, bindings: bindings, prepared: slices.Clone(prepared),
	}, nil
}

func (session *documentReconstructionSession) reject(err error) error {
	if session != nil {
		session.phase = documentReconstructionFailed
	}
	return err
}

func (session *documentReconstructionSession) next(ctx context.Context) (documentImageDemand, bool, error) {
	if session != nil && session.phase == documentReconstructionComplete {
		return documentImageDemand{}, false, nil
	}
	if session == nil || session.phase != documentReconstructionReady {
		return documentImageDemand{}, false, session.reject(fmt.Errorf("document reconstruction cannot request another group in its current phase"))
	}
	if ctx == nil {
		return documentImageDemand{}, false, session.reject(fmt.Errorf("document reconstruction context is required"))
	}
	if err := ctx.Err(); err != nil {
		return documentImageDemand{}, false, session.reject(err)
	}

	record := session.journal.record.DocumentGroups[session.cursor]
	if session.mode == documentReconstructionPrepared {
		if err := record.validate(session.journal.record.ManifestRootProvenance); err != nil {
			return documentImageDemand{}, false, session.reject(err)
		}
	}
	aliases, subjects, err := record.canonicalReferences(ctx)
	if err != nil {
		return documentImageDemand{}, false, session.reject(err)
	}
	selections, err := session.indexes.SelectedContracts(ctx, subjects, aliases)
	if err != nil {
		return documentImageDemand{}, false, session.reject(err)
	}
	claims, provisional, err := session.journal.documentClaims(ctx, session.indexes, record, aliases)
	if err != nil {
		return documentImageDemand{}, false, session.reject(err)
	}

	session.pending = documentReconstructionGroup{record: record, aliases: aliases, subjects: subjects, selections: selections, claims: claims, provisional: provisional}
	session.phase = documentReconstructionAwaitingImages
	return documentImageDemand{session: session, index: session.cursor, original: record.Original, expected: record.Expected}, true, nil
}

func (session *documentReconstructionSession) supplyPreparedImages(ctx context.Context, demand documentImageDemand) error {
	if session == nil || session.phase != documentReconstructionAwaitingImages || session.mode != documentReconstructionPrepared || demand.session != session || demand.index != session.cursor {
		return session.reject(fmt.Errorf("document reconstruction has no matching prepared-image demand"))
	}

	images, err := preparedNativeDocumentImages(ctx, session.pending.record, session.prepared[session.cursor], session.limits)
	if err != nil {
		return session.reject(err)
	}
	return session.completeImages(ctx, demand, images)
}

func (session *documentReconstructionSession) supplyLoadedImages(ctx context.Context, demand documentImageDemand, images loadedDocumentImages) error {
	if session == nil || session.mode != documentReconstructionLoaded {
		return session.reject(fmt.Errorf("document reconstruction cannot receive loaded images for its selected source"))
	}
	return session.completeImages(ctx, demand, images)
}

func (session *documentReconstructionSession) completeImages(ctx context.Context, demand documentImageDemand, images loadedDocumentImages) error {
	if session == nil || session.phase != documentReconstructionAwaitingImages || demand.session != session || demand.index != session.cursor {
		return session.reject(fmt.Errorf("document reconstruction has no matching image demand"))
	}

	group := session.pending
	if err := group.record.validateRemovalImages(images); err != nil {
		return session.reject(err)
	}
	reconstructed, err := reconstructNativeDocumentComparison(ctx, group.record, images, group.selections, session.limits, session.codecs)
	if err != nil {
		return session.reject(err)
	}

	completion := reconstructed
	if session.mode == documentReconstructionPrepared {
		completion = session.prepared[session.cursor]
		if err := completion.RequireCanonicalRecoveryPolicy(ctx, reconstructed); err != nil {
			return session.reject(err)
		}
		session.canonical = append(session.canonical, reconstructed)
	}

	session.inputs = append(session.inputs, journaldocument.NewStateGroupInput(group.aliases, group.subjects, completion, session.bindings[session.cursor]))
	session.result.images = append(session.result.images, images)
	session.result.claims = append(session.result.claims, group.claims)
	session.result.provisional = append(session.result.provisional, group.provisional)

	session.pending = documentReconstructionGroup{}
	session.cursor++
	session.phase = documentReconstructionReady
	if session.cursor == len(session.journal.record.DocumentGroups) {
		session.phase = documentReconstructionComplete
	}
	return nil
}

func (session *documentReconstructionSession) complete(ctx context.Context) (nativeDocumentRecords, error) {
	if session == nil || session.phase != documentReconstructionComplete {
		return nativeDocumentRecords{}, session.reject(fmt.Errorf("document reconstruction is incomplete"))
	}

	completed, err := session.journal.completeDocuments(ctx, session.indexes, session.inputs, session.result, session.codecs)
	if err != nil {
		return nativeDocumentRecords{}, session.reject(err)
	}
	if session.mode == documentReconstructionPrepared {
		for index, comparison := range session.canonical {
			if err := ctx.Err(); err != nil {
				return nativeDocumentRecords{}, session.reject(err)
			}
			group, err := completed.groups[index].CanonicalRecovery(ctx, comparison)
			if err != nil {
				return nativeDocumentRecords{}, session.reject(err)
			}
			completed.groups[index] = group
		}
	}

	session.phase = documentReconstructionFinished
	return completed, nil
}
