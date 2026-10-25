package journal

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/topology"
)

// prepareDocuments checks prepared images against the complete record without
// artifact I/O. Source/action and live namespace authority remain separate joins.
func (journal nativeJournalSnapshots) prepareDocuments(ctx context.Context, comparisons []DocumentComparison, limits configdocument.Limits, codecs aggregate.CodecCatalog) (nativeDocumentRecords, error) {
	if ctx == nil {
		return nativeDocumentRecords{}, fmt.Errorf("prepared native document context is required")
	}
	if err := ctx.Err(); err != nil {
		return nativeDocumentRecords{}, err
	}
	if err := journal.record.validateRemovalCoverage(); err != nil {
		return nativeDocumentRecords{}, err
	}
	if err := journal.owner.Validate(); err != nil {
		return nativeDocumentRecords{}, err
	}
	if err := limits.Validate(); err != nil {
		return nativeDocumentRecords{}, err
	}
	if len(comparisons) != len(journal.record.DocumentGroups) {
		return nativeDocumentRecords{}, fmt.Errorf("prepared comparisons must cover every recorded document group exactly once")
	}
	indexes, err := indexDocumentSnapshots(ctx, journal.before, journal.after)
	if err != nil {
		return nativeDocumentRecords{}, err
	}
	bindings, err := journal.documentBindings(ctx)
	if err != nil {
		return nativeDocumentRecords{}, err
	}
	var inputs []documentStateGroupInput
	var canonicalComparisons []DocumentComparison
	result := nativeDocumentRecords{}
	for index, record := range journal.record.DocumentGroups {
		if err := ctx.Err(); err != nil {
			return nativeDocumentRecords{}, err
		}
		if err := record.validate(journal.record.ManifestRootProvenance); err != nil {
			return nativeDocumentRecords{}, err
		}
		aliases, subjects, err := record.canonicalReferences(ctx)
		if err != nil {
			return nativeDocumentRecords{}, err
		}
		selections, err := indexes.selectedContracts(ctx, subjects, aliases)
		if err != nil {
			return nativeDocumentRecords{}, err
		}
		claims, provisional, err := journal.documentClaims(ctx, indexes, record, aliases)
		if err != nil {
			return nativeDocumentRecords{}, err
		}
		comparison := comparisons[index]
		images, err := preparedNativeDocumentImages(ctx, record, comparison, limits)
		if err != nil {
			return nativeDocumentRecords{}, err
		}
		if err := record.validateRemovalImages(images); err != nil {
			return nativeDocumentRecords{}, err
		}
		reconstructed, err := reconstructNativeDocumentComparison(ctx, record, images, selections, limits, codecs)
		if err != nil {
			return nativeDocumentRecords{}, err
		}
		if err := comparison.requireCanonicalRecoveryPolicy(ctx, reconstructed); err != nil {
			return nativeDocumentRecords{}, err
		}
		canonicalComparisons = append(canonicalComparisons, reconstructed)
		inputs = append(inputs, documentStateGroupInput{aliases: aliases, selectedSubjects: subjects, comparison: comparison, bindingIntents: bindings[index]})
		result.images = append(result.images, images)
		result.claims = append(result.claims, claims)
		result.provisional = append(result.provisional, provisional)
	}
	completed, err := journal.completeDocuments(ctx, indexes, inputs, result, codecs)
	if err != nil {
		return nativeDocumentRecords{}, err
	}
	for index, comparison := range canonicalComparisons {
		if err := ctx.Err(); err != nil {
			return nativeDocumentRecords{}, err
		}
		completed.groups[index].comparison = comparison
	}
	return completed, nil
}

func (comparison DocumentComparison) requireCanonicalRecoveryPolicy(ctx context.Context, canonical DocumentComparison) error {
	if (comparison.order == nil) != (canonical.order == nil) {
		return fmt.Errorf("prepared package order differs from the recorded comparison")
	}
	participants := make(map[documentSelectionKey]compose.Participant, len(canonical.participants))
	for _, participant := range canonical.participants {
		selection := participant.Selection()
		participants[documentSelectionKey{document: selection.DocumentAddress(), codec: selection.CodecContractID()}] = participant
	}
	areas := make(map[documentSelectionKey][]output.KeyPath, len(participants))
	for _, participant := range comparison.participants {
		if err := ctx.Err(); err != nil {
			return err
		}
		selection := participant.Selection()
		key := documentSelectionKey{document: selection.DocumentAddress(), codec: selection.CodecContractID()}
		expected, found := participants[key]
		if !found || participant.Syntax() != expected.Syntax() {
			return fmt.Errorf("prepared participant grammar or codec contract differs from the operation")
		}
		areas[key] = append(areas[key], participant.RewriteAreas()...)
	}
	for _, participant := range canonical.participants {
		if err := ctx.Err(); err != nil {
			return err
		}
		selection := participant.Selection()
		key := documentSelectionKey{document: selection.DocumentAddress(), codec: selection.CodecContractID()}
		if !slices.Equal(output.TopmostKeyPaths(areas[key]), participant.RewriteAreas()) {
			return fmt.Errorf("prepared native rewrite envelope differs from the operation")
		}
	}
	return ctx.Err()
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

func reconstructNativeDocumentComparison(ctx context.Context, record recoveryDocumentGroup, images loadedDocumentImages, selections []aggregate.Selection, limits configdocument.Limits, codecs aggregate.CodecCatalog) (DocumentComparison, error) {
	input := DocumentComparisonInput{Before: images.original.document, Expected: images.expected.document, BeforeMode: images.original.mode, ExpectedMode: images.expected.mode, Limits: limits}
	if record.Extent == "whole" {
		return NewWholeDocumentComparison(ctx, input)
	}
	participants := make([]compose.Participant, 0, len(selections))
	for _, selection := range selections {
		participant, err := nativeJournalParticipant(selection, codecs)
		if err != nil {
			return DocumentComparison{}, err
		}
		participants = append(participants, participant)
	}
	var order *pipackage.OrderObservation
	if record.PiOrder != nil {
		observed, err := record.PiOrder.Evidence.canonical(ctx, images.original.document)
		if err != nil {
			return DocumentComparison{}, err
		}
		order = &observed
	}
	candidate, err := ReconstructSelectedDocument(ctx, input, participants, order)
	if err != nil {
		return DocumentComparison{}, err
	}
	comparison := candidate.Comparison()
	relations := make([]aggregate.DocumentAddress, 0, len(selections)+1)
	for _, selection := range selections {
		relations = append(relations, selection.DocumentAddress())
	}
	if record.PiOrder != nil {
		alias, err := record.PiOrder.Alias.canonical()
		if err != nil {
			return DocumentComparison{}, err
		}
		relations = append(relations, alias)
	}
	expected, err := comparison.removalDemands(ctx, relations)
	if err != nil {
		return DocumentComparison{}, err
	}
	actual := recovery.RemovalDemandSet{}
	if record.Removal != nil {
		removal, err := record.Removal.canonical()
		if err != nil {
			return DocumentComparison{}, err
		}
		actual = removal.Demands()
	}
	if !actual.Equal(expected) {
		return DocumentComparison{}, fmt.Errorf("selected removal demands do not exactly cover the actual frontier")
	}
	return comparison, nil
}

func preparedNativeDocumentImages(ctx context.Context, record recoveryDocumentGroup, comparison DocumentComparison, limits configdocument.Limits) (loadedDocumentImages, error) {
	if err := comparison.Validate(); err != nil {
		return loadedDocumentImages{}, err
	}
	if comparison.limits != limits || comparison.whole != (record.Extent == "whole") {
		return loadedDocumentImages{}, fmt.Errorf("prepared document extent or limits differ from the operation")
	}
	images := loadedDocumentImages{
		original: loadedDocumentImage{document: comparison.before, mode: comparison.beforeMode(), initialized: true},
		expected: loadedDocumentImage{document: comparison.expected, initialized: true},
	}
	if mode, present := comparison.expectedState.RegularFileMode(); present {
		images.expected.mode = &mode
	}
	for index, image := range []loadedDocumentImage{images.original, images.expected} {
		if err := validateComparisonDocument(ctx, image.document, limits); err != nil {
			return loadedDocumentImages{}, err
		}
		if err := image.validate(); err != nil {
			return loadedDocumentImages{}, err
		}
		evidence := []recoveryDocumentImage{record.Original, record.Expected}[index]
		if err := evidence.validate(); err != nil {
			return loadedDocumentImages{}, err
		}
		if image.document.Exists() != (evidence.Kind == "file") {
			return loadedDocumentImages{}, fmt.Errorf("prepared document presence differs from its recorded witness")
		}
		if !image.document.Exists() {
			continue
		}
		if *image.mode != *evidence.Mode || image.document.ContentSize() != *evidence.Artifact.Size || artifact.HashFileContent(image.document.Content()) != evidence.Artifact.Digest {
			return loadedDocumentImages{}, fmt.Errorf("prepared document mode, size or digest differs from its recorded witness")
		}
	}
	if err := ctx.Err(); err != nil {
		return loadedDocumentImages{}, err
	}
	return images, nil
}
