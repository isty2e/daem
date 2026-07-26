package live

import (
	"context"
	"fmt"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	assurancehostroute "github.com/isty2e/daem/internal/assurance/hostroute"
	observeantigravity "github.com/isty2e/daem/internal/assurance/observe/antigravityplugin"
	observecodexplugin "github.com/isty2e/daem/internal/assurance/observe/codexplugin"
	observepipackage "github.com/isty2e/daem/internal/assurance/observe/pipackage"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

// CarrierRemovalBaselineInput selects one concrete host boundary before a
// delegated carrier removal.
type CarrierRemovalBaselineInput struct {
	CommandRoot          string
	Carrier              desiredextension.CarrierKey
	EffectPostconditions effectpostcondition.Set
}

// CaptureCarrierRemovalBaselines dispatches one canonical carrier identity to
// its concrete passive pre-effect observer.
func CaptureCarrierRemovalBaselines(
	ctx context.Context,
	input CarrierRemovalBaselineInput,
) (durablecarrier.EffectBaselineSet, error) {
	if ctx == nil {
		return durablecarrier.EffectBaselineSet{}, fmt.Errorf("carrier removal baseline context is required")
	}
	if err := ctx.Err(); err != nil {
		return durablecarrier.EffectBaselineSet{}, err
	}
	if err := input.Carrier.Validate(); err != nil {
		return durablecarrier.EffectBaselineSet{}, fmt.Errorf("carrier removal baseline identity: %w", err)
	}
	if err := input.EffectPostconditions.Validate(); err != nil {
		return durablecarrier.EffectBaselineSet{}, fmt.Errorf("carrier removal baseline requirements: %w", err)
	}
	if len(input.EffectPostconditions.Requirements()) == 0 {
		return durablecarrier.NewEffectBaselineSet(nil)
	}
	switch input.Carrier.Carrier() {
	case desiredextension.CarrierAntigravityCLIPlugin:
		return observeantigravity.CaptureRemovalBaselines(
			ctx,
			input.Carrier,
			input.EffectPostconditions,
		)
	case desiredextension.CarrierCodexPlugin:
		return observecodexplugin.CaptureRemovalBaselines(
			ctx,
			input.Carrier,
			input.EffectPostconditions,
		)
	case desiredextension.CarrierPiPackage:
		return observepipackage.CaptureRemovalBaselines(
			ctx,
			observepipackage.RemovalBaselineInput{
				CommandRoot:          input.CommandRoot,
				Carrier:              input.Carrier,
				EffectPostconditions: input.EffectPostconditions,
			},
		)
	default:
		return durablecarrier.EffectBaselineSet{}, fmt.Errorf(
			"carrier %q has no removal baseline observer",
			input.Carrier.Carrier(),
		)
	}
}

// CarrierRemovalInput selects one concrete current host boundary after a
// delegated removal or during pending-removal settlement.
type CarrierRemovalInput struct {
	Paths   daempaths.Paths
	Pending durablecarrier.PendingCarrierRemoval
}

// ObserveCarrierRemoval dispatches one durable pending removal to its concrete
// passive relation and effect observer.
func ObserveCarrierRemoval(
	ctx context.Context,
	input CarrierRemovalInput,
) assurancehostroute.ObservationFact {
	if err := input.Pending.Validate(); err != nil {
		return assurancehostroute.ObservationUnavailable(
			assurancehostroute.ResultReasonObservationParseFailed,
		)
	}
	carrier := input.Pending.Identity().Carrier().Key()
	switch carrier.Carrier() {
	case desiredextension.CarrierClaudeCodePlugin,
		desiredextension.CarrierOpenCodePlugin:
		if len(input.Pending.EffectPostconditions().Requirements()) != 0 {
			return assurancehostroute.ObservationUnavailable(
				assurancehostroute.ResultReasonObservationParseFailed,
			)
		}
		correlation, err := observeCarrierRemovalRelation(ctx, input)
		if err != nil {
			return assurancehostroute.ObservationUnavailable(
				assurancehostroute.ResultReasonObservationUnavailable,
			)
		}
		return assurancehostroute.CurrentObservation(correlation)
	case desiredextension.CarrierCodexPlugin:
		correlation, err := observeCarrierRemovalRelation(ctx, input)
		if err != nil {
			return assurancehostroute.ObservationUnavailable(
				assurancehostroute.ResultReasonObservationUnavailable,
			)
		}
		paths, err := observecodexplugin.ResolveHostPaths()
		if err != nil {
			return assurancehostroute.ObservationUnavailable(
				assurancehostroute.ResultReasonObservationUnavailable,
			)
		}
		evidence, err := observecodexplugin.ObserveRemovalEffects(
			ctx,
			paths,
			input.Pending,
		)
		if err != nil {
			return assurancehostroute.ObservationUnavailable(
				assurancehostroute.ResultReasonObservationUnavailable,
			)
		}
		return assurancehostroute.CurrentObservationWithEffectEvidence(
			correlation,
			evidence,
		)
	case desiredextension.CarrierAntigravityCLIPlugin:
		correlation, err := observeCarrierRemovalRelation(ctx, input)
		if err != nil {
			return assurancehostroute.ObservationUnavailable(
				assurancehostroute.ResultReasonObservationUnavailable,
			)
		}
		paths, err := observeantigravity.ResolveHostPaths()
		if err != nil {
			return assurancehostroute.ObservationUnavailable(
				assurancehostroute.ResultReasonObservationUnavailable,
			)
		}
		evidence, err := observeantigravity.ObserveRemovalEffects(
			ctx,
			paths,
			input.Pending,
		)
		if err != nil {
			return assurancehostroute.ObservationUnavailable(
				assurancehostroute.ResultReasonObservationUnavailable,
			)
		}
		return assurancehostroute.CurrentObservationWithEffectEvidence(
			correlation,
			evidence,
		)
	case desiredextension.CarrierPiPackage:
		observation, err := observepipackage.ObserveRemoval(
			ctx,
			observepipackage.RemovalObservationInput{
				Settings: observepipackage.SettingsInput{
					WorkDir:     input.Paths.ManifestRoot,
					ProjectRoot: input.Paths.ManifestRoot,
					Scope:       carrier.Scope(),
				},
				CommandRoot: input.Paths.ManifestRoot,
				Pending:     input.Pending,
			},
		)
		if err != nil {
			return assurancehostroute.ObservationUnavailable(
				assurancehostroute.ResultReasonObservationUnavailable,
			)
		}
		return observation
	default:
		return assurancehostroute.ObservationUnavailable(
			assurancehostroute.ResultReasonObservationUnsupported,
		)
	}
}

func observeCarrierRemovalRelation(
	ctx context.Context,
	input CarrierRemovalInput,
) (observerelation.CorrelationResult, error) {
	if ctx == nil {
		return observerelation.CorrelationResult{}, fmt.Errorf(
			"carrier removal relation observation context is required",
		)
	}
	if err := ctx.Err(); err != nil {
		return observerelation.CorrelationResult{}, err
	}
	pending := input.Pending
	identity := pending.Identity()
	correlation, err := observerelation.NewCorrelationKey(
		identity.RelationSubject(),
		identity.ExpectedRelation(),
	)
	if err != nil {
		return observerelation.CorrelationResult{}, err
	}
	selection, err := targetselection.ForAvailableTargets(
		[]target.Target{identity.Target()},
		nil,
	)
	if err != nil {
		return observerelation.CorrelationResult{}, err
	}
	batch, err := relationhost.Observe(ctx, relationhost.Input{
		Paths:                input.Paths,
		ManagedCarrierClaims: []durablecarrier.ManagedCarrierClaim{pending.Claim()},
		Selection:            selection,
		OnlyCorrelation:      &correlation,
	})
	if err != nil {
		return observerelation.CorrelationResult{}, err
	}
	result, ok := batch.Correlation(correlation)
	if !ok {
		return observerelation.CorrelationResult{}, fmt.Errorf(
			"carrier removal relation observer produced no exact correlation",
		)
	}
	return result, nil
}
