package record

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

type ImageDemand struct {
	session            *LoadedReconstruction
	index              int
	original, expected DocumentImage
}

func (demand ImageDemand) Original() DocumentImage { return demand.original }

func (demand ImageDemand) Expected() DocumentImage { return demand.expected }

type documentReconstructionGroup struct {
	record             recoveryDocumentGroup
	original, expected DocumentImage
	aliases            []aggregate.DocumentAddress
	subjects           []topology.SubjectID
	selections         []aggregate.Selection
	claims             ownershipmutation.DocumentClaimTransition
	provisional        []journaldocument.ProvisionalAcquire
}

// Artifact reads stay outside the session; prepared image evidence is derived
// here. Each policy check precedes the next demand, without exposing partial records.
type LoadedReconstruction struct {
	mode      documentReconstructionMode
	phase     documentReconstructionPhase
	journal   Native
	limits    configdocument.Limits
	codecs    aggregate.CodecCatalog
	indexes   journaldocument.SnapshotIndexes
	bindings  [][]ownership.DocumentBindingIntent
	prepared  []journaldocument.Comparison
	canonical []journaldocument.Comparison
	inputs    []journaldocument.StateGroupInput
	result    LoadedDocuments
	cursor    int
	pending   documentReconstructionGroup
}

func (journal Native) BeginLoadedDocuments(ctx context.Context, limits configdocument.Limits, codecs aggregate.CodecCatalog) (*LoadedReconstruction, error) {
	return newDocumentReconstruction(ctx, journal, limits, codecs, documentReconstructionLoaded, nil)
}

func newDocumentReconstruction(ctx context.Context, journal Native, limits configdocument.Limits, codecs aggregate.CodecCatalog, mode documentReconstructionMode, prepared []journaldocument.Comparison) (*LoadedReconstruction, error) {
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
	return &LoadedReconstruction{
		mode: mode, phase: phase, journal: journal, limits: limits, codecs: codecs,
		indexes: indexes, bindings: bindings, prepared: slices.Clone(prepared),
	}, nil
}

func (session *LoadedReconstruction) reject(err error) error {
	if session != nil {
		session.phase = documentReconstructionFailed
	}
	return err
}

func (session *LoadedReconstruction) Next(ctx context.Context) (ImageDemand, bool, error) {
	if session != nil && session.phase == documentReconstructionComplete {
		return ImageDemand{}, false, nil
	}
	if session == nil || session.phase != documentReconstructionReady {
		return ImageDemand{}, false, session.reject(fmt.Errorf("document reconstruction cannot request another group in its current phase"))
	}
	if ctx == nil {
		return ImageDemand{}, false, session.reject(fmt.Errorf("document reconstruction context is required"))
	}
	if err := ctx.Err(); err != nil {
		return ImageDemand{}, false, session.reject(err)
	}

	record := session.journal.record.DocumentGroups[session.cursor]
	if session.mode == documentReconstructionPrepared {
		if err := record.validate(session.journal.record.ManifestRootProvenance); err != nil {
			return ImageDemand{}, false, session.reject(err)
		}
	}
	aliases, subjects, err := record.canonicalReferences(ctx)
	if err != nil {
		return ImageDemand{}, false, session.reject(err)
	}
	selections, err := session.indexes.SelectedContracts(ctx, subjects, aliases)
	if err != nil {
		return ImageDemand{}, false, session.reject(err)
	}
	claims, provisional, err := session.journal.documentClaims(ctx, session.indexes, record, aliases)
	if err != nil {
		return ImageDemand{}, false, session.reject(err)
	}
	before, err := documentImageReference(record.Original)
	if err != nil {
		return ImageDemand{}, false, session.reject(err)
	}
	after, err := documentImageReference(record.Expected)
	if err != nil {
		return ImageDemand{}, false, session.reject(err)
	}

	session.pending = documentReconstructionGroup{record: record, original: before, expected: after, aliases: aliases, subjects: subjects, selections: selections, claims: claims, provisional: provisional}
	session.phase = documentReconstructionAwaitingImages
	return ImageDemand{session: session, index: session.cursor, original: before, expected: after}, true, nil
}

func (session *LoadedReconstruction) supplyPreparedImages(ctx context.Context, demand ImageDemand) error {
	if session == nil || session.phase != documentReconstructionAwaitingImages || session.mode != documentReconstructionPrepared || demand.session != session || demand.index != session.cursor {
		return session.reject(fmt.Errorf("document reconstruction has no matching prepared-image demand"))
	}

	images, err := preparedNativeDocumentImages(ctx, session.pending.record, session.prepared[session.cursor], session.limits)
	if err != nil {
		return session.reject(err)
	}
	return session.completeImages(ctx, demand, images)
}

func (session *LoadedReconstruction) SupplyLoaded(ctx context.Context, demand ImageDemand, original, expected LoadedImage) error {
	if session == nil || session.mode != documentReconstructionLoaded {
		return session.reject(fmt.Errorf("document reconstruction cannot receive loaded images for its selected source"))
	}
	return session.completeImages(ctx, demand, LoadedImages{original: original, expected: expected})
}

func (session *LoadedReconstruction) completeImages(ctx context.Context, demand ImageDemand, images LoadedImages) error {
	if session == nil || session.phase != documentReconstructionAwaitingImages || demand.session != session || demand.index != session.cursor {
		return session.reject(fmt.Errorf("document reconstruction has no matching image demand"))
	}

	group := session.pending
	if session.mode == documentReconstructionLoaded {
		paired, err := loadedDocumentImagePair(images.original, images.expected)
		if err != nil {
			return session.reject(err)
		}
		if paired.original.source != group.original || paired.expected.source != group.expected {
			return session.reject(fmt.Errorf("loaded document images differ from the current recorded witnesses"))
		}
		images = paired
	}
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

func (session *LoadedReconstruction) complete(ctx context.Context) (LoadedDocuments, error) {
	if session == nil || session.phase != documentReconstructionComplete {
		return LoadedDocuments{}, session.reject(fmt.Errorf("document reconstruction is incomplete"))
	}

	completed, err := session.journal.completeDocuments(ctx, session.indexes, session.inputs, session.result, session.codecs)
	if err != nil {
		return LoadedDocuments{}, session.reject(err)
	}
	if session.mode == documentReconstructionPrepared {
		for index, comparison := range session.canonical {
			if err := ctx.Err(); err != nil {
				return LoadedDocuments{}, session.reject(err)
			}
			group, err := completed.groups[index].CanonicalRecovery(ctx, comparison)
			if err != nil {
				return LoadedDocuments{}, session.reject(err)
			}
			completed.groups[index] = group
		}
	}

	session.phase = documentReconstructionFinished
	return completed, nil
}
