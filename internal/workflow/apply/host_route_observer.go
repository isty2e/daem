package apply

import (
	"context"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	assurancehostroute "github.com/isty2e/daem/internal/assurance/hostroute"
	observlive "github.com/isty2e/daem/internal/assurance/observe/live"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
	executehostroute "github.com/isty2e/daem/internal/effect/execute/hostroute"
	daempaths "github.com/isty2e/daem/internal/paths"
	lock "github.com/isty2e/daem/internal/realization/lock"
	reconciliation "github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func passiveHostRouteObserver(
	paths daempaths.Paths,
	locked lock.File,
	selection targetselection.Selection,
) HostRouteObserver {
	return func(
		ctx context.Context,
		command executehostroute.Command,
		_ []durablecarrier.PendingCarrierInstall,
		claims []durablecarrier.ManagedCarrierClaim,
	) assurancehostroute.ObservationFact {
		contract, ok := locked.Locked.Subject(command.Subject())
		if !ok {
			return assurancehostroute.ObservationUnavailable(assurancehostroute.ResultReasonObservationParseFailed)
		}
		identity, isCarrier, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(contract)
		if err != nil {
			return assurancehostroute.ObservationUnavailable(assurancehostroute.ResultReasonObservationParseFailed)
		}
		if !isCarrier {
			return assurancehostroute.ObservationUnavailable(assurancehostroute.ResultReasonObservationParseFailed)
		}
		onlyCorrelation, err := relationobserve.NewCorrelationKey(
			identity.RelationSubject(),
			identity.ExpectedRelation(),
		)
		if err != nil {
			return assurancehostroute.ObservationUnavailable(assurancehostroute.ResultReasonObservationParseFailed)
		}
		observations, err := relationhost.Observe(ctx, relationhost.Input{
			Paths:                paths,
			Lockfile:             locked,
			ManagedCarrierClaims: claims,
			Selection:            selection,
			OnlyCorrelation:      &onlyCorrelation,
		})
		if err != nil {
			return assurancehostroute.ObservationUnavailable(assurancehostroute.ResultReasonObservationParseFailed)
		}
		correlation, ok := observations.Correlation(onlyCorrelation)
		if !ok {
			return assurancehostroute.ObservationUnavailable(assurancehostroute.ResultReasonObservationUnsupported)
		}
		return assurancehostroute.CurrentObservation(correlation)
	}
}

func passiveCarrierRemovalBaselineObserver(
	paths daempaths.Paths,
) CarrierRemovalBaselineObserver {
	return func(
		ctx context.Context,
		action carrierabsence.Action,
	) (durablecarrier.EffectBaselineSet, error) {
		return observlive.CaptureCarrierRemovalBaselines(
			ctx,
			observlive.CarrierRemovalBaselineInput{
				CommandRoot:          paths.ManifestRoot,
				Carrier:              action.Claim().Identity().Carrier().Key(),
				EffectPostconditions: action.RouteAdmission().Operation().EffectPostconditions(),
			},
		)
	}
}

func passiveCarrierRemovalObserver(paths daempaths.Paths) CarrierRemovalObserver {
	return func(
		ctx context.Context,
		pending durablecarrier.PendingCarrierRemoval,
		_ []durablecarrier.ManagedCarrierClaim,
	) assurancehostroute.ObservationFact {
		return observlive.ObserveCarrierRemoval(
			ctx,
			observlive.CarrierRemovalInput{
				Paths:   paths,
				Pending: pending,
			},
		)
	}
}

func installRelationPostcondition(
	action reconciliation.RelationAction,
) assurancehostroute.PostconditionRequirement {
	postcondition := assurancehostroute.RelationPostconditionExact
	evidenceClass, err := action.CarrierIdentity().Carrier().RelationEvidence()
	if err == nil &&
		evidenceClass == extensiontopology.RelationEvidenceBoundedSameSubject {
		postcondition = assurancehostroute.RelationPostconditionPresent
	}
	return assurancehostroute.RequireRelationPostcondition(postcondition)
}
