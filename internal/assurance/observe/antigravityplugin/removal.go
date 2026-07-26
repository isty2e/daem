package antigravityplugin

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
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

// CaptureRemovalBaselines validates the selected Antigravity removal contract.
// Bundle absence is a current-state predicate and needs no content baseline.
func CaptureRemovalBaselines(
	ctx context.Context,
	carrier desiredextension.CarrierKey,
	requirements effectpostcondition.Set,
) (durablecarrier.EffectBaselineSet, error) {
	if err := validateRemovalContext(ctx); err != nil {
		return durablecarrier.EffectBaselineSet{}, err
	}
	if _, err := validateRemovalCarrier(carrier, requirements); err != nil {
		return durablecarrier.EffectBaselineSet{}, err
	}
	return durablecarrier.NewEffectBaselineSet(nil)
}

// ObserveRemovalEffects reads the selected Antigravity plugin bundle
// postcondition independently from the import-manifest relation observation.
func ObserveRemovalEffects(
	ctx context.Context,
	paths HostPaths,
	pending durablecarrier.PendingCarrierRemoval,
) (observepostcondition.Set, error) {
	if err := validateRemovalContext(ctx); err != nil {
		return observepostcondition.Set{}, err
	}
	if err := pending.Validate(); err != nil {
		return observepostcondition.Set{}, fmt.Errorf(
			"Antigravity CLI pending removal: %w",
			err,
		)
	}
	source, err := validateRemovalCarrier(
		pending.Identity().Carrier().Key(),
		pending.EffectPostconditions(),
	)
	if err != nil {
		return observepostcondition.Set{}, err
	}
	if string(pending.Identity().ExpectedRelation().SubjectKey()) !=
		source.RelationIdentity() {
		return observepostcondition.Set{}, fmt.Errorf(
			"Antigravity CLI pending removal relation key does not match the host plugin name",
		)
	}
	bundlePath, err := paths.PluginDirectoryPath(source.RelationIdentity())
	if err != nil {
		return observepostcondition.Set{}, err
	}
	evidence, err := observepostcondition.NewEvidence(
		effectpostcondition.CarrierArtifactsAbsent,
		observeArtifactAbsence(bundlePath),
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
) (extensiontopology.CarrierSource, error) {
	interpreted, err := validateObservedCarrier(carrier)
	if err != nil {
		return extensiontopology.CarrierSource{}, err
	}
	if err := requirements.Validate(); err != nil {
		return extensiontopology.CarrierSource{}, fmt.Errorf(
			"Antigravity CLI removal postconditions: %w",
			err,
		)
	}
	expected, err := effectpostcondition.NewSet(
		[]effectpostcondition.Requirement{effectpostcondition.CarrierArtifactsAbsent},
	)
	if err != nil {
		return extensiontopology.CarrierSource{}, err
	}
	if !requirements.Equal(expected) {
		return extensiontopology.CarrierSource{}, fmt.Errorf(
			"Antigravity CLI removal requires exact postcondition %q",
			effectpostcondition.CarrierArtifactsAbsent,
		)
	}
	return interpreted, nil
}

func observeArtifactAbsence(path string) observepostcondition.EvidenceState {
	_, err := os.Lstat(path)
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
		return fmt.Errorf("Antigravity CLI removal observation context is required")
	}
	return ctx.Err()
}
