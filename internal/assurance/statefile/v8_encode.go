package statefile

import (
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	assurancepostcondition "github.com/isty2e/daem/internal/assurance/postcondition"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/realization"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	"github.com/isty2e/daem/internal/topology"
)

func persistedSnapshot(snapshot durable.Snapshot) snapshotDTO {
	persisted := snapshotDTO{
		Version:                   snapshotVersion,
		ManagedPaths:              make([]managedPathDTO, 0, len(snapshot.ManagedPaths())),
		ManagedAggregateBaselines: make([]managedAggregateDTO, 0, len(snapshot.ManagedAggregates())),
		PendingCarrierInstalls:    make([]pendingCarrierInstallDTO, 0, len(snapshot.PendingCarrierInstalls())),
		PendingCarrierRemovals:    make([]pendingCarrierRemovalDTO, 0, len(snapshot.PendingCarrierRemovals())),
		ManagedCarrierClaims:      make([]managedCarrierClaimDTO, 0, len(snapshot.ManagedCarrierClaims())),
		DelegateAttempts:          make([]delegateAttemptDTO, 0, len(snapshot.DelegateAttempts())),
		HostRouteAttempts:         make([]hostRouteAttemptDTO, 0, len(snapshot.HostRouteAttempts())),
	}
	for _, state := range snapshot.ManagedPaths() {
		var fileMode *uint32
		if state.PermissionPolicy() == realization.PathPermissionsExact {
			mode := uint32(state.FileMode())
			fileMode = &mode
		}
		consumerTargets := make([]string, 0, len(state.ConsumerTargets()))
		for _, consumer := range state.ConsumerTargets() {
			consumerTargets = append(consumerTargets, string(consumer))
		}
		persisted.ManagedPaths = append(persisted.ManagedPaths, managedPathDTO{
			Subject:          persistedSubject(state.Subject()),
			ConsumerTargets:  consumerTargets,
			Scope:            string(state.Scope()),
			Destination:      state.Destination().String(),
			ContentHash:      string(state.ContentHash()),
			ContentKind:      string(state.ContentKind()),
			PermissionPolicy: string(state.PermissionPolicy()),
			FileMode:         fileMode,
		})
	}
	for _, state := range snapshot.ManagedAggregates() {
		contribution := state.Contribution()
		persisted.ManagedAggregateBaselines = append(
			persisted.ManagedAggregateBaselines,
			managedAggregateDTO{
				Subject:               persistedSubject(state.Subject()),
				PlacementID:           contribution.PlacementID(),
				Target:                string(contribution.Target()),
				Scope:                 string(contribution.Scope()),
				AggregateRoot:         contribution.AggregateRoot().String(),
				ContentPath:           contribution.ContentPath(),
				MergeUnit:             string(contribution.MergeUnit()),
				Cardinality:           string(contribution.Cardinality()),
				SiblingRetention:      string(contribution.SiblingRetention()),
				SiblingPreservation:   string(contribution.SiblingPreservation()),
				Equivalence:           string(contribution.Equivalence()),
				CanonicalContribution: contribution.CanonicalContribution(),
				CodecContractID:       string(contribution.CodecContractID()),
				ComparedFields:        contribution.ComparedFields(),
			},
		)
	}
	for _, pending := range snapshot.PendingCarrierInstalls() {
		persisted.PendingCarrierInstalls = append(
			persisted.PendingCarrierInstalls,
			pendingCarrierInstallDTO{
				Owner:          persistedStateAuthority(pending.Owner()),
				Identity:       persistedManagedCarrierIdentity(pending.Identity()),
				InstallRequest: persistedDelegatedRequest(pending.InstallRequest()),
			},
		)
	}
	for _, pending := range snapshot.PendingCarrierRemovals() {
		persisted.PendingCarrierRemovals = append(
			persisted.PendingCarrierRemovals,
			pendingCarrierRemovalDTO{
				Claim:         persistedManagedCarrierClaim(pending.Claim()),
				RemoveRequest: persistedDelegatedRequest(pending.RemoveRequest()),
				EffectPostconditions: persistedEffectPostconditionRequirements(
					pending.EffectPostconditions().Requirements(),
				),
				EffectBaselines: persistedEffectBaselines(
					pending.EffectBaselines().Baselines(),
				),
			},
		)
	}
	for _, claim := range snapshot.ManagedCarrierClaims() {
		persisted.ManagedCarrierClaims = append(
			persisted.ManagedCarrierClaims,
			persistedManagedCarrierClaim(claim),
		)
	}
	for _, attempt := range snapshot.DelegateAttempts() {
		exitCode, hasExitCode := attempt.ExitCode()
		persisted.DelegateAttempts = append(persisted.DelegateAttempts, delegateAttemptDTO{
			Subject:         persistedSubject(attempt.Subject()),
			Target:          string(attempt.Target()),
			Scope:           string(attempt.Scope()),
			PlanIdentityKey: attempt.PlanIdentityKey(),
			ObservedAt:      attempt.ObservedAt().Format(time.RFC3339Nano),
			Status:          string(attempt.Status()),
			Reason:          string(attempt.Reason()),
			Observation:     string(attempt.ObservationSummary()),
			Postcondition:   string(attempt.PostconditionSummary()),
			ExitCode:        optionalInt(exitCode, hasExitCode),
			TimedOut:        attempt.TimedOut(),
			StdoutTruncated: attempt.StdoutTruncated(),
			StderrTruncated: attempt.StderrTruncated(),
			Redacted:        attempt.Redacted(),
		})
	}
	for _, attempt := range snapshot.HostRouteAttempts() {
		exitCode, hasExitCode := attempt.ExitCode()
		persisted.HostRouteAttempts = append(persisted.HostRouteAttempts, hostRouteAttemptDTO{
			Subject:          persistedSubject(attempt.Subject()),
			Target:           string(attempt.Target()),
			Scope:            string(attempt.Scope()),
			Operation:        string(attempt.Operation()),
			RouteID:          attempt.RouteID(),
			RouteRequestHash: attempt.RouteRequestHash(),
			ObservedAt:       attempt.ObservedAt().Format(time.RFC3339Nano),
			ResultClass:      string(attempt.ResultClass()),
			Reason:           string(attempt.Reason()),
			AttemptObserved:  attempt.AttemptObserved(),
			AttemptReason:    string(attempt.AttemptReason()),
			Observation:      string(attempt.ObservationSummary()),
			Postcondition:    string(attempt.PostconditionSummary()),
			EffectPostconditions: persistedEffectPostconditionSummaries(
				attempt.EffectPostconditions().Summaries(),
			),
			ExitCode: optionalInt(exitCode, hasExitCode),
			TimedOut: attempt.TimedOut(),
			Redacted: attempt.Redacted(),
		})
	}
	return persisted
}

func persistedEffectBaselines(
	baselines []durablecarrier.EffectBaseline,
) []effectBaselineDTO {
	persisted := make([]effectBaselineDTO, 0, len(baselines))
	for _, baseline := range baselines {
		contentHash, hasContent := baseline.ContentHash()
		row := effectBaselineDTO{
			Requirement: string(baseline.Requirement()),
			State:       string(baseline.State()),
		}
		if hasContent {
			row.ContentHash = string(contentHash)
		}
		persisted = append(persisted, row)
	}
	return persisted
}

func persistedEffectPostconditionRequirements(
	requirements []effectpostcondition.Requirement,
) []string {
	persisted := make([]string, 0, len(requirements))
	for _, requirement := range requirements {
		persisted = append(persisted, string(requirement))
	}
	return persisted
}

func persistedEffectPostconditionSummaries(
	summaries []assurancepostcondition.Summary,
) []effectPostconditionSummaryDTO {
	persisted := make([]effectPostconditionSummaryDTO, 0, len(summaries))
	for _, summary := range summaries {
		persisted = append(persisted, effectPostconditionSummaryDTO{
			Requirement: string(summary.Requirement()),
			State:       string(summary.State()),
		})
	}
	return persisted
}

func persistedManagedCarrierClaim(claim durablecarrier.ManagedCarrierClaim) managedCarrierClaimDTO {
	return managedCarrierClaimDTO{
		Owner:          persistedStateAuthority(claim.Owner()),
		Identity:       persistedManagedCarrierIdentity(claim.Identity()),
		InstallRequest: persistedDelegatedRequest(claim.InstallRequest()),
		Provenance:     string(claim.Provenance()),
	}
}

func persistedStateAuthority(authority stateauthority.Authority) stateAuthorityDTO {
	statefile := authority.StatefileAuthority()
	return stateAuthorityDTO{
		StatefileAuthority: pathAuthorityDTO{
			Key:     statefile.Key(),
			Witness: statefile.Witness(),
		},
		ManifestPath: authority.ManifestPath(),
	}
}

func persistedManagedCarrierIdentity(identity durablecarrier.ManagedCarrierIdentity) managedCarrierIdentityDTO {
	key := identity.Carrier().Key()
	relation := identity.ExpectedRelation()
	return managedCarrierIdentityDTO{
		CarrierSubject:     persistedSubject(identity.CarrierSubject()),
		CarrierFamily:      string(identity.Carrier().Family()),
		Target:             string(identity.Target()),
		Scope:              string(identity.Scope()),
		SourceKind:         string(key.Source().Kind()),
		SourceRef:          key.Source().Ref(),
		RelationSubject:    persistedSubject(identity.RelationSubject()),
		RelationSubjectKey: string(relation.SubjectKey()),
		ManagedInstanceKey: string(relation.ManagedInstanceKey()),
	}
}

func persistedDelegatedRequest(request realizationdelegate.Request) delegatedRequestDTO {
	return delegatedRequestDTO{
		RouteID:                request.RouteID(),
		AdapterContractVersion: request.ContractVersion(),
		CanonicalRequestHash:   request.CanonicalRequestHash(),
	}
}

func persistedSubject(subject topology.SubjectID) subjectDTO {
	return subjectDTO{
		Kind:      string(subject.Kind()),
		Namespace: subject.Namespace(),
		Name:      subject.Key(),
	}
}

func optionalInt(value int, present bool) *int {
	if !present {
		return nil
	}
	return &value
}
