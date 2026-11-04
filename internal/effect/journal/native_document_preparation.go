package journal

import (
	"context"
	"fmt"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/topology"
)

// prepareDocuments checks prepared images against the complete record without
// artifact I/O. Source/action and live namespace authority remain separate joins.
func (journal nativeJournalSnapshots) prepareDocuments(ctx context.Context, comparisons []journaldocument.Comparison, limits configdocument.Limits, codecs aggregate.CodecCatalog) (preparedDocumentRecord, error) {
	session, err := newDocumentReconstruction(ctx, journal, limits, codecs, documentReconstructionPrepared, comparisons)
	if err != nil {
		return preparedDocumentRecord{}, err
	}
	for {
		demand, present, err := session.next(ctx)
		if err != nil {
			return preparedDocumentRecord{}, err
		}
		if !present {
			break
		}
		if err := session.supplyPreparedImages(ctx, demand); err != nil {
			return preparedDocumentRecord{}, err
		}
	}
	return session.finishPrepared(ctx)
}

func (record recoveryDocumentGroup) canonicalReferences(ctx context.Context) ([]aggregate.DocumentAddress, []topology.SubjectID, error) {
	aliases := make([]aggregate.DocumentAddress, 0, len(record.Aliases))
	for _, alias := range record.Aliases {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		address, err := alias.Address.canonical()
		if err != nil {
			return nil, nil, err
		}
		aliases = append(aliases, address)
	}
	subjects := make([]topology.SubjectID, 0, len(record.SelectedSubjects))
	for _, ref := range record.SelectedSubjects {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		subject, err := ref.canonical()
		if err != nil {
			return nil, nil, err
		}
		subjects = append(subjects, subject)
	}
	return aliases, subjects, nil
}

func reconstructNativeDocumentComparison(ctx context.Context, record recoveryDocumentGroup, images loadedDocumentImages, selections []aggregate.Selection, limits configdocument.Limits, codecs aggregate.CodecCatalog) (journaldocument.Comparison, error) {
	input := journaldocument.ComparisonInput{Before: images.original.document, Expected: images.expected.document, BeforeMode: images.original.mode, ExpectedMode: images.expected.mode, Limits: limits}
	if record.Extent == "whole" {
		return journaldocument.NewWholeComparison(ctx, input)
	}
	participants := make([]compose.Participant, 0, len(selections))
	for _, selection := range selections {
		participant, err := journaldocument.Participant(selection, codecs)
		if err != nil {
			return journaldocument.Comparison{}, err
		}
		participants = append(participants, participant)
	}
	var order *pipackage.OrderObservation
	if record.PiOrder != nil {
		observed, err := record.PiOrder.Evidence.canonical(ctx, images.original.document)
		if err != nil {
			return journaldocument.Comparison{}, err
		}
		order = &observed
	}
	candidate, err := journaldocument.ReconstructSelected(ctx, input, participants, order)
	if err != nil {
		return journaldocument.Comparison{}, err
	}
	comparison := candidate.Comparison()
	relations := make([]aggregate.DocumentAddress, 0, len(selections)+1)
	for _, selection := range selections {
		relations = append(relations, selection.DocumentAddress())
	}
	if record.PiOrder != nil {
		alias, err := record.PiOrder.Alias.canonical()
		if err != nil {
			return journaldocument.Comparison{}, err
		}
		relations = append(relations, alias)
	}
	expected, err := comparison.RemovalDemands(ctx, relations)
	if err != nil {
		return journaldocument.Comparison{}, err
	}
	actual := recovery.RemovalDemandSet{}
	if record.Removal != nil {
		removal, err := record.Removal.canonical()
		if err != nil {
			return journaldocument.Comparison{}, err
		}
		actual = removal.Demands()
	}
	if !actual.Equal(expected) {
		return journaldocument.Comparison{}, fmt.Errorf("selected removal demands do not exactly cover the actual frontier")
	}
	return comparison, nil
}

func preparedNativeDocumentImages(ctx context.Context, record recoveryDocumentGroup, comparison journaldocument.Comparison, limits configdocument.Limits) (loadedDocumentImages, error) {
	if err := comparison.Validate(); err != nil {
		return loadedDocumentImages{}, err
	}
	if comparison.Limits() != limits || comparison.WholeDocument() != (record.Extent == "whole") {
		return loadedDocumentImages{}, fmt.Errorf("prepared document extent or limits differ from the operation")
	}
	var expectedMode *recovery.PermissionMode
	if mode, present := comparison.ExpectedState().RegularFileMode(); present {
		expectedMode = &mode
	}
	original, err := record.Original.preparedImage(ctx, comparison.BeforeDocument(), comparison.BeforeMode(), limits)
	if err != nil {
		return loadedDocumentImages{}, err
	}
	expected, err := record.Expected.preparedImage(ctx, comparison.ExpectedDocument(), expectedMode, limits)
	if err != nil {
		return loadedDocumentImages{}, err
	}
	if err := ctx.Err(); err != nil {
		return loadedDocumentImages{}, err
	}
	return loadedDocumentImagePair(original, expected)
}
