package pipackage

import (
	"context"
	"fmt"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	assurancehostroute "github.com/isty2e/daem/internal/assurance/hostroute"
	observepostcondition "github.com/isty2e/daem/internal/assurance/observe/postcondition"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

// RemovalObservationInput selects one exact current Pi removal postcondition.
type RemovalObservationInput struct {
	Settings    SettingsInput
	CommandRoot string
	Pending     durablecarrier.PendingCarrierRemoval
}

// RemovalBaselineInput selects the exact pre-effect facts required by one
// current Pi removal dossier.
type RemovalBaselineInput struct {
	CommandRoot          string
	Carrier              desiredextension.CarrierKey
	EffectPostconditions effectpostcondition.Set
}

// ObserveRemoval reads the exact settings relation and source-kind-specific
// current effect facts for one durable pending Pi removal.
func ObserveRemoval(
	ctx context.Context,
	input RemovalObservationInput,
) (assurancehostroute.ObservationFact, error) {
	if err := validateObservationContext(ctx); err != nil {
		return assurancehostroute.ObservationFact{}, err
	}
	if err := input.Pending.Validate(); err != nil {
		return assurancehostroute.ObservationFact{}, fmt.Errorf("Pi pending removal: %w", err)
	}
	claim := input.Pending.Claim()
	carrier := claim.Identity().Carrier().Key()
	source, err := validateRemovalCarrier(carrier, input.Pending.EffectPostconditions())
	if err != nil {
		return assurancehostroute.ObservationFact{}, err
	}
	if input.Settings.Scope != carrier.Scope() {
		return assurancehostroute.ObservationFact{}, fmt.Errorf(
			"Pi removal settings scope %q does not match carrier scope %q",
			input.Settings.Scope,
			carrier.Scope(),
		)
	}

	inventory, err := ReadSettings(input.Settings)
	if err != nil {
		return assurancehostroute.ObservationFact{}, err
	}
	key, err := observerelation.NewCorrelationKey(
		claim.Identity().RelationSubject(),
		claim.Identity().ExpectedRelation(),
	)
	if err != nil {
		return assurancehostroute.ObservationFact{}, err
	}
	expected, err := NewScopedRelation(key, carrier.Scope(), input.CommandRoot)
	if err != nil {
		return assurancehostroute.ObservationFact{}, err
	}
	correlation, err := Correlate(expected, inventory)
	if err != nil {
		return assurancehostroute.ObservationFact{}, err
	}

	evidence, err := removalEffectEvidence(
		ctx,
		inventory,
		input.CommandRoot,
		carrier,
		source,
		input.Pending,
	)
	if err != nil {
		return assurancehostroute.ObservationFact{}, err
	}
	return assurancehostroute.CurrentObservationWithEffectEvidence(correlation, evidence), nil
}

func validateRemovalCarrier(
	carrier desiredextension.CarrierKey,
	requirements effectpostcondition.Set,
) (extensiontopology.CarrierSource, error) {
	if err := carrier.Validate(); err != nil {
		return extensiontopology.CarrierSource{}, fmt.Errorf("Pi removal carrier: %w", err)
	}
	if carrier.Carrier() != desiredextension.CarrierPiPackage {
		return extensiontopology.CarrierSource{}, fmt.Errorf(
			"Pi removal observer does not support carrier %q",
			carrier.Carrier(),
		)
	}
	if err := requirements.Validate(); err != nil {
		return extensiontopology.CarrierSource{}, fmt.Errorf("Pi removal postconditions: %w", err)
	}
	source, err := extensiontopology.InterpretCarrierSource(carrier)
	if err != nil {
		return extensiontopology.CarrierSource{}, err
	}
	expectedRequirement := effectpostcondition.CarrierArtifactsAbsent
	if source.Class() == extensiontopology.CarrierSourceLocal {
		expectedRequirement = effectpostcondition.LocalSourceUnchanged
	}
	expected, err := effectpostcondition.NewSet(
		[]effectpostcondition.Requirement{expectedRequirement},
	)
	if err != nil {
		return extensiontopology.CarrierSource{}, err
	}
	if !requirements.Equal(expected) {
		return extensiontopology.CarrierSource{}, fmt.Errorf(
			"Pi %s removal requires exact postcondition %q",
			source.Class(),
			expectedRequirement,
		)
	}
	return source, nil
}

func removalEffectEvidence(
	ctx context.Context,
	inventory Inventory,
	commandRoot string,
	carrier desiredextension.CarrierKey,
	source extensiontopology.CarrierSource,
	pending durablecarrier.PendingCarrierRemoval,
) (observepostcondition.Set, error) {
	facts := make([]observepostcondition.Evidence, 0, 1)
	for _, requirement := range pending.EffectPostconditions().Requirements() {
		state, err := observeRemovalEffect(
			ctx,
			inventory,
			commandRoot,
			carrier,
			source,
			pending,
			requirement,
		)
		if err != nil {
			return observepostcondition.Set{}, err
		}
		fact, err := observepostcondition.NewEvidence(requirement, state)
		if err != nil {
			return observepostcondition.Set{}, err
		}
		facts = append(facts, fact)
	}
	return observepostcondition.NewSet(observepostcondition.SetInput{
		Subject:      pending.Identity().RelationSubject(),
		RouteRequest: pending.RemoveRequest(),
		Evidence:     facts,
	})
}

func observeRemovalEffect(
	ctx context.Context,
	inventory Inventory,
	commandRoot string,
	carrier desiredextension.CarrierKey,
	source extensiontopology.CarrierSource,
	pending durablecarrier.PendingCarrierRemoval,
	requirement effectpostcondition.Requirement,
) (observepostcondition.EvidenceState, error) {
	switch requirement {
	case effectpostcondition.CarrierArtifactsAbsent:
		return observeManagedArtifactAbsence(inventory, source), nil
	case effectpostcondition.LocalSourceUnchanged:
		return observeLocalSourceUnchanged(ctx, commandRoot, carrier, pending)
	default:
		return "", fmt.Errorf("Pi removal effect requirement %q is unsupported", requirement)
	}
}

func validateObservationContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("Pi removal observation context is required")
	}
	return ctx.Err()
}
