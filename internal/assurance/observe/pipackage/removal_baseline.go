package pipackage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observepostcondition "github.com/isty2e/daem/internal/assurance/observe/postcondition"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	artifactaccess "github.com/isty2e/daem/internal/supply/artifact/access"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

const (
	maximumLocalSourceEntries = 100_000
	maximumLocalSourceBytes   = 1 << 30
)

// CaptureRemovalBaselines observes immutable pre-effect facts before a Pi
// removal command. Npm and Git removals require no content baseline.
func CaptureRemovalBaselines(
	ctx context.Context,
	input RemovalBaselineInput,
) (durablecarrier.EffectBaselineSet, error) {
	if err := validateObservationContext(ctx); err != nil {
		return durablecarrier.EffectBaselineSet{}, err
	}
	source, err := validateRemovalCarrier(input.Carrier, input.EffectPostconditions)
	if err != nil {
		return durablecarrier.EffectBaselineSet{}, err
	}
	if source.Class() != extensiontopology.CarrierSourceLocal {
		return durablecarrier.NewEffectBaselineSet(nil)
	}
	identity, err := sourceIdentityForInput(
		input.Carrier.Source().Ref(),
		input.CommandRoot,
		input.Carrier.Scope(),
	)
	if err != nil {
		return durablecarrier.EffectBaselineSet{}, fmt.Errorf("derive Pi local removal source: %w", err)
	}
	baseline, err := captureLocalSourceBaseline(ctx, identity.key)
	if err != nil {
		return durablecarrier.EffectBaselineSet{}, err
	}
	return durablecarrier.NewEffectBaselineSet([]durablecarrier.EffectBaseline{baseline})
}

func captureLocalSourceBaseline(
	ctx context.Context,
	sourcePath string,
) (durablecarrier.EffectBaseline, error) {
	view, err := artifactaccess.OpenView(sourcePath)
	if errors.Is(err, fs.ErrNotExist) {
		return durablecarrier.NewAbsentEffectBaseline(effectpostcondition.LocalSourceUnchanged)
	}
	if err != nil {
		return durablecarrier.EffectBaseline{}, fmt.Errorf("open Pi local removal source: %w", err)
	}
	limit, err := localSourceTraversalLimit()
	if err != nil {
		return durablecarrier.EffectBaseline{}, err
	}
	contentHash, _, err := view.HashWithLimit(ctx, limit)
	if err != nil {
		return durablecarrier.EffectBaseline{}, fmt.Errorf("hash Pi local removal source: %w", err)
	}
	return durablecarrier.NewContentEffectBaseline(
		effectpostcondition.LocalSourceUnchanged,
		contentHash,
	)
}

func observeLocalSourceUnchanged(
	ctx context.Context,
	commandRoot string,
	carrier desiredextension.CarrierKey,
	pending durablecarrier.PendingCarrierRemoval,
) (observepostcondition.EvidenceState, error) {
	baseline, present := pending.EffectBaselines().For(effectpostcondition.LocalSourceUnchanged)
	if !present {
		return observepostcondition.EvidenceMalformed, nil
	}
	identity, err := sourceIdentityForInput(
		carrier.Source().Ref(),
		commandRoot,
		carrier.Scope(),
	)
	if err != nil {
		return observepostcondition.EvidenceMalformed, nil
	}
	view, err := artifactaccess.OpenView(identity.key)
	if errors.Is(err, fs.ErrNotExist) {
		if baseline.State() == durablecarrier.EffectBaselineAbsent {
			return observepostcondition.EvidenceSatisfied, nil
		}
		return observepostcondition.EvidenceUnsatisfied, nil
	}
	if err != nil {
		return observepostcondition.EvidenceUnavailable, nil
	}
	if baseline.State() == durablecarrier.EffectBaselineAbsent {
		return observepostcondition.EvidenceUnsatisfied, nil
	}
	limit, err := localSourceTraversalLimit()
	if err != nil {
		return "", err
	}
	currentHash, _, err := view.HashWithLimit(ctx, limit)
	if err != nil {
		return observepostcondition.EvidenceUnavailable, nil
	}
	expectedHash, present := baseline.ContentHash()
	if !present {
		return observepostcondition.EvidenceMalformed, nil
	}
	if currentHash != expectedHash {
		return observepostcondition.EvidenceUnsatisfied, nil
	}
	return observepostcondition.EvidenceSatisfied, nil
}

func localSourceTraversalLimit() (artifactaccess.TraversalLimit, error) {
	return artifactaccess.NewTraversalLimit(
		maximumLocalSourceEntries,
		maximumLocalSourceBytes,
	)
}
