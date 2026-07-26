package statefile

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	assurancepostcondition "github.com/isty2e/daem/internal/assurance/postcondition"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/codec"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func (persisted snapshotDTO) canonical() (durable.Snapshot, error) {
	if persisted.ManagedPaths == nil ||
		persisted.ManagedAggregateBaselines == nil ||
		persisted.PendingCarrierInstalls == nil ||
		persisted.PendingCarrierRemovals == nil ||
		persisted.ManagedCarrierClaims == nil ||
		persisted.DelegateAttempts == nil ||
		persisted.HostRouteAttempts == nil {
		return durable.Snapshot{}, fmt.Errorf("statefile v7 requires every durable fact-family array")
	}
	input := durable.SnapshotInput{
		ManagedPaths:           make([]durable.ManagedPathState, 0, len(persisted.ManagedPaths)),
		ManagedAggregates:      make([]durable.ManagedAggregateState, 0, len(persisted.ManagedAggregateBaselines)),
		PendingCarrierInstalls: make([]durablecarrier.PendingCarrierInstall, 0, len(persisted.PendingCarrierInstalls)),
		PendingCarrierRemovals: make([]durablecarrier.PendingCarrierRemoval, 0, len(persisted.PendingCarrierRemovals)),
		ManagedCarrierClaims:   make([]durablecarrier.ManagedCarrierClaim, 0, len(persisted.ManagedCarrierClaims)),
		DelegateAttempts:       make([]durableattempt.DelegateAttempt, 0, len(persisted.DelegateAttempts)),
		HostRouteAttempts:      make([]durableattempt.HostRouteAttempt, 0, len(persisted.HostRouteAttempts)),
	}
	for index, row := range persisted.ManagedPaths {
		state, err := row.canonical()
		if err != nil {
			return durable.Snapshot{}, fmt.Errorf("managed_paths[%d]: %w", index, err)
		}
		input.ManagedPaths = append(input.ManagedPaths, state)
	}
	for index, row := range persisted.ManagedAggregateBaselines {
		state, err := row.canonical()
		if err != nil {
			return durable.Snapshot{}, fmt.Errorf("managed_aggregate_contributions[%d]: %w", index, err)
		}
		input.ManagedAggregates = append(input.ManagedAggregates, state)
	}
	for index, row := range persisted.PendingCarrierInstalls {
		pending, err := row.canonical()
		if err != nil {
			return durable.Snapshot{}, fmt.Errorf("pending_carrier_installs[%d]: %w", index, err)
		}
		input.PendingCarrierInstalls = append(input.PendingCarrierInstalls, pending)
	}
	for index, row := range persisted.PendingCarrierRemovals {
		pending, err := row.canonical()
		if err != nil {
			return durable.Snapshot{}, fmt.Errorf("pending_carrier_removals[%d]: %w", index, err)
		}
		input.PendingCarrierRemovals = append(input.PendingCarrierRemovals, pending)
	}
	for index, row := range persisted.ManagedCarrierClaims {
		claim, err := row.canonical()
		if err != nil {
			return durable.Snapshot{}, fmt.Errorf("managed_carrier_claims[%d]: %w", index, err)
		}
		input.ManagedCarrierClaims = append(input.ManagedCarrierClaims, claim)
	}
	for index, row := range persisted.DelegateAttempts {
		attempt, err := row.canonical()
		if err != nil {
			return durable.Snapshot{}, fmt.Errorf("delegate_attempts[%d]: %w", index, err)
		}
		input.DelegateAttempts = append(input.DelegateAttempts, attempt)
	}
	for index, row := range persisted.HostRouteAttempts {
		attempt, err := row.canonical()
		if err != nil {
			return durable.Snapshot{}, fmt.Errorf("host_route_attempts[%d]: %w", index, err)
		}
		input.HostRouteAttempts = append(input.HostRouteAttempts, attempt)
	}
	snapshot, err := durable.NewSnapshot(input)
	if err != nil {
		return durable.Snapshot{}, err
	}
	if err := validateSnapshotForPersistence(snapshot); err != nil {
		return durable.Snapshot{}, err
	}
	return snapshot, nil
}

func (persisted managedPathDTO) canonical() (durable.ManagedPathState, error) {
	subject, err := persisted.Subject.canonical()
	if err != nil {
		return durable.ManagedPathState{}, err
	}
	if err := requireCanonicalStrings(persisted.ConsumerTargets, "consumer_targets"); err != nil {
		return durable.ManagedPathState{}, err
	}
	consumerTargets := make([]target.Target, 0, len(persisted.ConsumerTargets))
	for _, value := range persisted.ConsumerTargets {
		parsed, err := target.ParseTarget(value)
		if err != nil {
			return durable.ManagedPathState{}, err
		}
		consumerTargets = append(consumerTargets, parsed)
	}
	scope, err := target.ParseScope(persisted.Scope)
	if err != nil {
		return durable.ManagedPathState{}, err
	}
	var fileMode os.FileMode
	if persisted.FileMode != nil {
		fileMode = os.FileMode(*persisted.FileMode)
	}
	return durable.NewManagedPathState(
		subject,
		consumerTargets,
		scope,
		output.Destination(persisted.Destination),
		artifact.ContentHash(persisted.ContentHash),
		realization.PathProjectionContentKind(persisted.ContentKind),
		realization.PathPermissionPolicy(persisted.PermissionPolicy),
		fileMode,
	)
}

func (persisted managedAggregateDTO) canonical() (durable.ManagedAggregateState, error) {
	subject, err := persisted.Subject.canonical()
	if err != nil {
		return durable.ManagedAggregateState{}, err
	}
	if err := requireCanonicalStrings(persisted.ComparedFields, "compared_fields"); err != nil {
		return durable.ManagedAggregateState{}, err
	}
	contribution, err := aggregate.NewManagedContribution(aggregate.ManagedContributionInput{
		PlacementID:           persisted.PlacementID,
		Target:                target.Target(persisted.Target),
		Scope:                 target.Scope(persisted.Scope),
		AggregateRoot:         persisted.AggregateRoot,
		ContentPath:           persisted.ContentPath,
		MergeUnit:             aggregate.MergeUnit(persisted.MergeUnit),
		Cardinality:           aggregate.ContributionCardinality(persisted.Cardinality),
		SiblingRetention:      aggregate.SiblingRetention(persisted.SiblingRetention),
		SiblingPreservation:   aggregate.SiblingPreservation(persisted.SiblingPreservation),
		Equivalence:           aggregate.Equivalence(persisted.Equivalence),
		CanonicalContribution: persisted.CanonicalContribution,
		CodecContractID:       aggregate.CodecContractID(persisted.CodecContractID),
		ComparedFields:        persisted.ComparedFields,
	})
	if err != nil {
		return durable.ManagedAggregateState{}, err
	}
	return durable.NewManagedAggregateState(subject, contribution)
}

func (persisted stateAuthorityDTO) canonical() (durablecarrier.StateAuthority, error) {
	return durablecarrier.NewStateAuthority(persisted.StatefileKey, persisted.ManifestPath)
}

func (persisted managedCarrierIdentityDTO) canonical() (durablecarrier.ManagedCarrierIdentity, error) {
	persistedCarrierSubject, err := persisted.CarrierSubject.canonical()
	if err != nil {
		return durablecarrier.ManagedCarrierIdentity{}, fmt.Errorf("carrier_subject: %w", err)
	}
	carrierFamily, err := desiredextension.ParseCarrier(persisted.CarrierFamily)
	if err != nil {
		return durablecarrier.ManagedCarrierIdentity{}, fmt.Errorf("carrier_family: %w", err)
	}
	selectedTarget, err := target.ParseTarget(persisted.Target)
	if err != nil {
		return durablecarrier.ManagedCarrierIdentity{}, fmt.Errorf("target: %w", err)
	}
	scope, err := target.ParseScope(persisted.Scope)
	if err != nil {
		return durablecarrier.ManagedCarrierIdentity{}, fmt.Errorf("scope: %w", err)
	}
	source, err := desiredextension.NewSourceRef(
		desiredextension.SourceKind(persisted.SourceKind),
		persisted.SourceRef,
	)
	if err != nil {
		return durablecarrier.ManagedCarrierIdentity{}, fmt.Errorf("source: %w", err)
	}
	key, err := desiredextension.NewCarrierKey(
		carrierFamily,
		selectedTarget,
		scope,
		source,
	)
	if err != nil {
		return durablecarrier.ManagedCarrierIdentity{}, err
	}
	carrier, err := extensiontopology.NewCarrier(key)
	if err != nil {
		return durablecarrier.ManagedCarrierIdentity{}, err
	}
	if carrier.SubjectID() != persistedCarrierSubject {
		return durablecarrier.ManagedCarrierIdentity{}, fmt.Errorf(
			"carrier_subject does not match canonical carrier identity",
		)
	}
	relationSubject, err := persisted.RelationSubject.canonical()
	if err != nil {
		return durablecarrier.ManagedCarrierIdentity{}, fmt.Errorf("relation_subject: %w", err)
	}
	subjectKey, err := hostrelation.NewSubjectKey(persisted.RelationSubjectKey)
	if err != nil {
		return durablecarrier.ManagedCarrierIdentity{}, fmt.Errorf("relation_subject_key: %w", err)
	}
	managedKey, err := hostrelation.NewManagedInstanceKey(persisted.ManagedInstanceKey)
	if err != nil {
		return durablecarrier.ManagedCarrierIdentity{}, fmt.Errorf("managed_instance_key: %w", err)
	}
	expected, err := hostrelation.NewExpectedRelation(subjectKey, managedKey)
	if err != nil {
		return durablecarrier.ManagedCarrierIdentity{}, err
	}
	return durablecarrier.NewManagedCarrierIdentity(carrier, relationSubject, expected)
}

func (persisted delegatedRequestDTO) canonical() (realizationdelegate.Request, error) {
	return realizationdelegate.NewRequest(
		persisted.RouteID,
		persisted.AdapterContractVersion,
		persisted.CanonicalRequestHash,
	)
}

func (persisted pendingCarrierInstallDTO) canonical() (durablecarrier.PendingCarrierInstall, error) {
	owner, err := persisted.Owner.canonical()
	if err != nil {
		return durablecarrier.PendingCarrierInstall{}, fmt.Errorf("owner: %w", err)
	}
	identity, err := persisted.Identity.canonical()
	if err != nil {
		return durablecarrier.PendingCarrierInstall{}, fmt.Errorf("identity: %w", err)
	}
	request, err := persisted.InstallRequest.canonical()
	if err != nil {
		return durablecarrier.PendingCarrierInstall{}, fmt.Errorf("install_request: %w", err)
	}
	return durablecarrier.NewPendingCarrierInstall(owner, identity, request)
}

func (persisted pendingCarrierRemovalDTO) canonical() (durablecarrier.PendingCarrierRemoval, error) {
	if persisted.EffectPostconditions == nil {
		return durablecarrier.PendingCarrierRemoval{}, fmt.Errorf("effect_postconditions is required")
	}
	if persisted.EffectBaselines == nil {
		return durablecarrier.PendingCarrierRemoval{}, fmt.Errorf("effect_baselines is required")
	}
	claim, err := persisted.Claim.canonical()
	if err != nil {
		return durablecarrier.PendingCarrierRemoval{}, fmt.Errorf("claim: %w", err)
	}
	request, err := persisted.RemoveRequest.canonical()
	if err != nil {
		return durablecarrier.PendingCarrierRemoval{}, fmt.Errorf("remove_request: %w", err)
	}
	effectPostconditions, err := canonicalEffectPostconditionRequirements(
		persisted.EffectPostconditions,
	)
	if err != nil {
		return durablecarrier.PendingCarrierRemoval{}, fmt.Errorf("effect_postconditions: %w", err)
	}
	effectBaselines, err := canonicalEffectBaselines(persisted.EffectBaselines)
	if err != nil {
		return durablecarrier.PendingCarrierRemoval{}, fmt.Errorf("effect_baselines: %w", err)
	}
	return durablecarrier.NewPendingCarrierRemoval(
		claim,
		request,
		effectPostconditions,
		effectBaselines,
	)
}

func (persisted managedCarrierClaimDTO) canonical() (durablecarrier.ManagedCarrierClaim, error) {
	owner, err := persisted.Owner.canonical()
	if err != nil {
		return durablecarrier.ManagedCarrierClaim{}, fmt.Errorf("owner: %w", err)
	}
	identity, err := persisted.Identity.canonical()
	if err != nil {
		return durablecarrier.ManagedCarrierClaim{}, fmt.Errorf("identity: %w", err)
	}
	request, err := persisted.InstallRequest.canonical()
	if err != nil {
		return durablecarrier.ManagedCarrierClaim{}, fmt.Errorf("install_request: %w", err)
	}
	return durablecarrier.NewManagedCarrierClaim(
		owner,
		identity,
		request,
		durablecarrier.ClaimProvenance(persisted.Provenance),
	)
}

func (persisted delegateAttemptDTO) canonical() (durableattempt.DelegateAttempt, error) {
	subject, err := persisted.Subject.canonical()
	if err != nil {
		return durableattempt.DelegateAttempt{}, err
	}
	selectedTarget, err := target.ParseTarget(persisted.Target)
	if err != nil {
		return durableattempt.DelegateAttempt{}, err
	}
	scope, err := target.ParseScope(persisted.Scope)
	if err != nil {
		return durableattempt.DelegateAttempt{}, err
	}
	observedAt, err := time.Parse(time.RFC3339Nano, persisted.ObservedAt)
	if err != nil {
		return durableattempt.DelegateAttempt{}, fmt.Errorf("observed_at: %w", err)
	}
	return durableattempt.NewDelegateAttempt(durableattempt.DelegateAttemptInput{
		Subject:         subject,
		Target:          selectedTarget,
		Scope:           scope,
		PlanIdentityKey: persisted.PlanIdentityKey,
		ObservedAt:      observedAt,
		Status:          durableattempt.DelegateAttemptStatus(persisted.Status),
		Reason:          durableattempt.DelegateAttemptReason(persisted.Reason),
		Observation:     observerelation.ObservationSummary(persisted.Observation),
		Postcondition:   observerelation.PostconditionSummary(persisted.Postcondition),
		ExitCode:        cloneOptionalInt(persisted.ExitCode),
		TimedOut:        persisted.TimedOut,
		StdoutTruncated: persisted.StdoutTruncated,
		StderrTruncated: persisted.StderrTruncated,
		Redacted:        persisted.Redacted,
	})
}

func (persisted hostRouteAttemptDTO) canonical() (durableattempt.HostRouteAttempt, error) {
	if persisted.EffectPostconditions == nil {
		return durableattempt.HostRouteAttempt{}, fmt.Errorf("effect_postconditions is required")
	}
	subject, err := persisted.Subject.canonical()
	if err != nil {
		return durableattempt.HostRouteAttempt{}, err
	}
	selectedTarget, err := target.ParseTarget(persisted.Target)
	if err != nil {
		return durableattempt.HostRouteAttempt{}, err
	}
	scope, err := target.ParseScope(persisted.Scope)
	if err != nil {
		return durableattempt.HostRouteAttempt{}, err
	}
	operation, err := lock.ParseOperationKind(persisted.Operation)
	if err != nil {
		return durableattempt.HostRouteAttempt{}, fmt.Errorf("operation: %w", err)
	}
	observedAt, err := time.Parse(time.RFC3339Nano, persisted.ObservedAt)
	if err != nil {
		return durableattempt.HostRouteAttempt{}, fmt.Errorf("observed_at: %w", err)
	}
	effectPostconditions, err := canonicalEffectPostconditionSummaries(
		persisted.EffectPostconditions,
	)
	if err != nil {
		return durableattempt.HostRouteAttempt{}, fmt.Errorf("effect_postconditions: %w", err)
	}
	return durableattempt.NewHostRouteAttempt(durableattempt.HostRouteAttemptInput{
		Subject:              subject,
		Target:               selectedTarget,
		Scope:                scope,
		Operation:            operation,
		RouteID:              persisted.RouteID,
		RouteRequestHash:     persisted.RouteRequestHash,
		ObservedAt:           observedAt,
		ResultClass:          durableattempt.HostRouteResultClass(persisted.ResultClass),
		Reason:               durableattempt.HostRouteResultReason(persisted.Reason),
		AttemptObserved:      persisted.AttemptObserved,
		AttemptReason:        durableattempt.HostRouteAttemptReason(persisted.AttemptReason),
		Observation:          observerelation.ObservationSummary(persisted.Observation),
		Postcondition:        observerelation.PostconditionSummary(persisted.Postcondition),
		EffectPostconditions: effectPostconditions,
		ExitCode:             cloneOptionalInt(persisted.ExitCode),
		TimedOut:             persisted.TimedOut,
		Redacted:             persisted.Redacted,
	})
}

func canonicalEffectPostconditionRequirements(
	values []string,
) (effectpostcondition.Set, error) {
	requirements := make([]effectpostcondition.Requirement, 0, len(values))
	for _, value := range values {
		requirements = append(
			requirements,
			effectpostcondition.Requirement(value),
		)
	}
	return effectpostcondition.NewSet(requirements)
}

func canonicalEffectPostconditionSummaries(
	values []effectPostconditionSummaryDTO,
) (assurancepostcondition.SummarySet, error) {
	summaries := make([]assurancepostcondition.Summary, 0, len(values))
	for index, value := range values {
		summary, err := assurancepostcondition.NewSummary(
			effectpostcondition.Requirement(value.Requirement),
			assurancepostcondition.SummaryState(value.State),
		)
		if err != nil {
			return assurancepostcondition.SummarySet{}, fmt.Errorf(
				"summary[%d]: %w",
				index,
				err,
			)
		}
		summaries = append(summaries, summary)
	}
	return assurancepostcondition.NewSummarySet(summaries)
}

func (persisted subjectDTO) canonical() (topology.SubjectID, error) {
	return topology.NewSubjectID(
		topology.SubjectKind(persisted.Kind),
		persisted.Namespace,
		persisted.Name,
	)
}

func cloneOptionalInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func requireCanonicalStrings(values []string, field string) error {
	if !sort.StringsAreSorted(values) {
		return fmt.Errorf("%s must be sorted and duplicate-free", field)
	}
	for index := 1; index < len(values); index++ {
		if values[index-1] == values[index] {
			return fmt.Errorf("%s must be sorted and duplicate-free", field)
		}
	}
	return nil
}

func validateSnapshotForPersistence(snapshot durable.Snapshot) error {
	codecs := aggregatecodec.Catalog()
	for index, state := range snapshot.ManagedPaths() {
		entityID, entityBacked := topologyprojection.EntityID(state.Subject())
		if !entityBacked {
			return fmt.Errorf("managed_paths[%d]: managed path state requires entity-backed projection subject identity", index)
		}
		if err := profile.ValidateManagedPathOccupancy(
			entityID,
			state.Subject().Namespace(),
			state.ConsumerTargets(),
			state.Scope(),
			string(state.Destination()),
			state.ContentKind(),
		); err != nil {
			return fmt.Errorf("managed_paths[%d]: managed path occupancy: %w", index, err)
		}
	}
	for index, state := range snapshot.ManagedAggregates() {
		if err := codecs.ValidateSubjectContribution(state.Subject(), state.Contribution()); err != nil {
			return fmt.Errorf("managed_aggregate_contributions[%d]: %w", index, err)
		}
	}
	return nil
}
