package codexplugin

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observepostcondition "github.com/isty2e/daem/internal/assurance/observe/postcondition"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
)

// CaptureRemovalBaselines validates the selected Codex removal contract.
// Cache absence is a current-state predicate and needs no content baseline.
func CaptureRemovalBaselines(
	ctx context.Context,
	carrier desiredextension.CarrierKey,
	requirements effectpostcondition.Set,
) (durablecarrier.EffectBaselineSet, error) {
	if err := validateRemovalContext(ctx); err != nil {
		return durablecarrier.EffectBaselineSet{}, err
	}
	if err := validateRemovalCarrier(carrier, requirements); err != nil {
		return durablecarrier.EffectBaselineSet{}, err
	}
	return durablecarrier.NewEffectBaselineSet(nil)
}

// ObserveRemovalEffects reads the selected Codex plugin cache postcondition.
func ObserveRemovalEffects(
	ctx context.Context,
	paths HostPaths,
	pending durablecarrier.PendingCarrierRemoval,
) (observepostcondition.Set, error) {
	if err := validateRemovalContext(ctx); err != nil {
		return observepostcondition.Set{}, err
	}
	if err := pending.Validate(); err != nil {
		return observepostcondition.Set{}, fmt.Errorf("Codex pending removal: %w", err)
	}
	if string(pending.Identity().ExpectedRelation().SubjectKey()) !=
		pending.Identity().Carrier().Key().Source().Ref() {
		return observepostcondition.Set{}, fmt.Errorf(
			"Codex pending removal relation key does not match the carrier selector",
		)
	}
	if err := validateRemovalCarrier(
		pending.Identity().Carrier().Key(),
		pending.EffectPostconditions(),
	); err != nil {
		return observepostcondition.Set{}, err
	}
	cachePath, err := paths.PluginCachePath(pending.Identity().Carrier().Key())
	if err != nil {
		return observepostcondition.Set{}, err
	}
	state := observeCacheAbsence(cachePath, os.Lstat)
	evidence, err := observepostcondition.NewEvidence(
		effectpostcondition.CarrierArtifactsAbsent,
		state,
	)
	if err != nil {
		return observepostcondition.Set{}, err
	}
	return observepostcondition.NewSet(observepostcondition.SetInput{
		Subject:      pending.Identity().RelationSubject(),
		RouteRequest: pending.RemoveRequest(),
		Evidence:     []observepostcondition.Evidence{evidence},
	})
}

func validateRemovalCarrier(
	carrier desiredextension.CarrierKey,
	requirements effectpostcondition.Set,
) error {
	if err := carrier.Validate(); err != nil {
		return fmt.Errorf("Codex removal carrier: %w", err)
	}
	if carrier.Carrier() != desiredextension.CarrierCodexPlugin {
		return fmt.Errorf("Codex removal observer does not support carrier %q", carrier.Carrier())
	}
	if err := requirements.Validate(); err != nil {
		return fmt.Errorf("Codex removal postconditions: %w", err)
	}
	expected, err := effectpostcondition.NewSet(
		[]effectpostcondition.Requirement{effectpostcondition.CarrierArtifactsAbsent},
	)
	if err != nil {
		return err
	}
	if !requirements.Equal(expected) {
		return fmt.Errorf(
			"Codex removal requires exact postcondition %q",
			effectpostcondition.CarrierArtifactsAbsent,
		)
	}
	return nil
}

func observeCacheAbsence(
	cachePath string,
	lstat func(string) (fs.FileInfo, error),
) observepostcondition.EvidenceState {
	_, err := lstat(cachePath)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return observepostcondition.EvidenceSatisfied
	case err != nil:
		return observepostcondition.EvidenceUnavailable
	default:
		return observepostcondition.EvidenceUnsatisfied
	}
}

func validateRemovalContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("Codex removal observation context is required")
	}
	return ctx.Err()
}
