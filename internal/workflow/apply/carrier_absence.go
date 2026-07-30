package apply

import (
	"context"
	"fmt"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	carrierclaimstore "github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	daempaths "github.com/isty2e/daem/internal/paths"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/topology"
)

type carrierAbsenceFingerprintFacts struct {
	StatefileKey           string
	ManifestPath           string
	CarrierSubject         topology.SubjectID
	RelationSubject        topology.SubjectID
	Target                 string
	Scope                  string
	SourceNamespace        string
	RelationSubjectKey     string
	ManagedInstanceKey     string
	InstallRequest         realizationdelegate.Request
	Provenance             durablecarrier.ClaimProvenance
	Desired                carrierabsence.DesiredRelationState
	Decision               carrierabsence.Decision
	ObservationPresent     bool
	CorrelationState       observerelation.CorrelationState
	CorrelationReason      observerelation.ReasonCode
	EvidenceAvailability   observerelation.InventoryAvailability
	EvidenceFreshness      observerelation.EvidenceFreshness
	Watchpoints            []observerelation.Watchpoint
	DaemKnownConsumers     []carrierConsumerFingerprintFacts
	RemovalOperation       *carrierRemovalOperationFingerprintFacts
	RemovalRequest         realizationdelegate.Request
	PendingRemoval         *carrierPendingRemovalFingerprintFacts
	PreservesSharedCarrier bool
	RemovedEffects         []string
	RetainedEffects        []string
	NonClaims              []string
	InvokesHostRoute       bool
	RetiresClaim           bool
	StateOnly              bool
	VerifiesPendingRemoval bool
	BlocksOrdinaryApply    bool
}

type carrierConsumerFingerprintFacts struct {
	StatefileKey       string
	ManifestPath       string
	RelationSubject    topology.SubjectID
	ManagedInstanceKey string
}

type carrierRemovalOperationFingerprintFacts struct {
	Operation            lock.OperationKind
	Actuation            lock.ActuationKind
	Authority            lock.AuthorityKind
	Route                lock.RouteContractRef
	HostCompatibility    lock.HostCompatibilityConstraint
	Preconditions        []string
	EffectEnvelope       lock.EffectEnvelopeClass
	EffectPostconditions []effectpostcondition.Requirement
	Idempotency          lock.IdempotencyContract
	Verification         lock.VerificationContract
	TrustActivation      lock.TrustActivationRequirement
	Recovery             lock.OperationRecoveryClass
}

type carrierPendingRemovalFingerprintFacts struct {
	RemovalRequest       realizationdelegate.Request
	EffectPostconditions []effectpostcondition.Requirement
	EffectBaselines      []carrierEffectBaselineFingerprintFacts
}

type carrierEffectBaselineFingerprintFacts struct {
	Requirement effectpostcondition.Requirement
	State       durablecarrier.EffectBaselineState
	ContentHash string
}

func carrierAbsenceFingerprintRows(
	actions []carrierabsence.Action,
) []carrierAbsenceFingerprintFacts {
	rows := make([]carrierAbsenceFingerprintFacts, 0, len(actions))
	for _, action := range actions {
		claim := action.Claim()
		identity := claim.Identity()
		expected := identity.ExpectedRelation()
		route := action.RouteAdmission()
		fact := carrierAbsenceFingerprintFacts{
			StatefileKey:           claim.Owner().StatefileKey(),
			ManifestPath:           claim.Owner().ManifestPath(),
			CarrierSubject:         identity.CarrierSubject(),
			RelationSubject:        identity.RelationSubject(),
			Target:                 string(action.Target()),
			Scope:                  string(action.Scope()),
			SourceNamespace:        identity.SourceNamespace(),
			RelationSubjectKey:     string(expected.SubjectKey()),
			ManagedInstanceKey:     string(expected.ManagedInstanceKey()),
			InstallRequest:         claim.InstallRequest(),
			Provenance:             claim.Provenance(),
			Desired:                action.Desired(),
			Decision:               action.Decision(),
			DaemKnownConsumers:     carrierConsumerFingerprintRows(action.Occupancy().DaemKnownConsumers()),
			RemovalRequest:         route.Request(),
			PreservesSharedCarrier: route.PreservesSharedCarrier(),
			RemovedEffects:         route.RemovedEffects(),
			RetainedEffects:        route.RetainedEffects(),
			NonClaims:              action.NonClaims(),
			InvokesHostRoute:       action.InvokesHostRoute(),
			RetiresClaim:           action.RetiresClaim(),
			StateOnly:              action.StateOnly(),
			VerifiesPendingRemoval: action.VerifiesPendingRemoval(),
			BlocksOrdinaryApply:    action.BlocksOrdinaryApply(),
		}
		if observation, present := action.Observation(); present {
			fact.ObservationPresent = true
			fact.CorrelationState = observation.Result.State()
			fact.CorrelationReason = observation.Result.Reason()
			fact.EvidenceAvailability = observation.Result.EvidenceAvailability()
			fact.EvidenceFreshness = observation.Result.EvidenceFreshness()
			fact.Watchpoints = observation.Result.Watchpoints()
		}
		if route.Status() == carrierabsence.RouteAdmitted {
			operation := route.Operation()
			fact.RemovalOperation = &carrierRemovalOperationFingerprintFacts{
				Operation:            operation.Operation(),
				Actuation:            operation.Actuation(),
				Authority:            operation.Authority(),
				Route:                operation.Route(),
				HostCompatibility:    operation.HostCompatibility(),
				Preconditions:        operation.Preconditions(),
				EffectEnvelope:       operation.EffectEnvelope(),
				EffectPostconditions: operation.EffectPostconditions().Requirements(),
				Idempotency:          operation.Idempotency(),
				Verification:         operation.Verification(),
				TrustActivation:      operation.TrustActivation(),
				Recovery:             operation.Recovery(),
			}
		}
		if pending, present := action.PendingRemoval(); present {
			baselines := pending.EffectBaselines().Baselines()
			baselineFacts := make([]carrierEffectBaselineFingerprintFacts, 0, len(baselines))
			for _, baseline := range baselines {
				contentHash, _ := baseline.ContentHash()
				baselineFacts = append(baselineFacts, carrierEffectBaselineFingerprintFacts{
					Requirement: baseline.Requirement(),
					State:       baseline.State(),
					ContentHash: string(contentHash),
				})
			}
			fact.PendingRemoval = &carrierPendingRemovalFingerprintFacts{
				RemovalRequest:       pending.RemoveRequest(),
				EffectPostconditions: pending.EffectPostconditions().Requirements(),
				EffectBaselines:      baselineFacts,
			}
		}
		rows = append(rows, fact)
	}
	return rows
}

func carrierConsumerFingerprintRows(
	consumers []durablecarrier.CarrierConsumer,
) []carrierConsumerFingerprintFacts {
	rows := make([]carrierConsumerFingerprintFacts, 0, len(consumers))
	for _, consumer := range consumers {
		rows = append(rows, carrierConsumerFingerprintFacts{
			StatefileKey:       consumer.Owner().StatefileKey(),
			ManifestPath:       consumer.Owner().ManifestPath(),
			RelationSubject:    consumer.RelationSubject(),
			ManagedInstanceKey: string(consumer.ManagedInstanceKey()),
		})
	}
	return rows
}

func stateOnlyCarrierClaimRetirements(
	actions []carrierabsence.Action,
) (
	project []durablecarrier.ManagedCarrierClaim,
	global []durablecarrier.ManagedCarrierClaim,
	err error,
) {
	for index, action := range actions {
		if err := action.Validate(); err != nil {
			return nil, nil, fmt.Errorf("state-only carrier absence[%d]: %w", index, err)
		}
		if !action.StateOnly() {
			continue
		}
		if !action.RetiresClaim() {
			return nil, nil, fmt.Errorf(
				"state-only carrier absence[%d] does not retire its exact claim",
				index,
			)
		}
		switch action.Scope() {
		case target.ScopeProject:
			project = append(project, action.Claim())
		case target.ScopeGlobal:
			global = append(global, action.Claim())
		default:
			return nil, nil, fmt.Errorf(
				"state-only carrier absence[%d] has unsupported scope %q",
				index,
				action.Scope(),
			)
		}
	}
	return project, global, nil
}

func retireClaim(
	ctx context.Context,
	input carrierRemovalInput,
	action carrierabsence.Action,
	pending durablecarrier.PendingCarrierRemoval,
	authority *rootedpath.EntryAuthority,
	result *carrierRemovalResult,
) error {
	switch action.Scope() {
	case target.ScopeProject:
		next, err := execute.CommitRetiredProjectCarrierRemoval(
			ctx,
			filesystem(input),
			authority,
			result.State,
			pending,
			statefile.Codec{},
		)
		if err != nil {
			return err
		}
		result.State = next
		return nil
	case target.ScopeGlobal:
		if input.RemoveGlobalClaim == nil {
			return fmt.Errorf("global carrier removal registry capability is required")
		}
		expected, changed, err := result.GlobalClaims.WithoutClaim(action.Claim())
		if err != nil {
			return fmt.Errorf("derive retired global carrier registry: %w", err)
		}
		if !changed {
			return fmt.Errorf("derive retired global carrier registry: exact claim is absent")
		}
		next, err := execute.CommitClearedGlobalCarrierRemovalPending(
			ctx,
			filesystem(input),
			authority,
			result.State,
			pending,
			statefile.Codec{},
		)
		if err != nil {
			return err
		}
		result.State = next
		registry, err := input.RemoveGlobalClaim(ctx, action.Claim())
		if err != nil {
			return fmt.Errorf("retire global carrier claim: %w", err)
		}
		if !registry.Equal(expected) {
			return fmt.Errorf("retire global carrier claim: registry returned an inexact successor")
		}
		result.GlobalClaims = registry
		return nil
	default:
		return fmt.Errorf("carrier removal scope %q is unsupported", action.Scope())
	}
}

func runAfterCarrierClaimRetirements(
	ctx context.Context,
	paths daempaths.Paths,
	locked lock.File,
	selection targetselection.Selection,
	stateResult execute.ApplyResult,
	carrierOwner stateauthority.Authority,
	globalClaims durablecarrier.GlobalCarrierClaims,
	globalRetirements []durablecarrier.ManagedCarrierClaim,
	globalAdoptions []durablecarrier.ManagedCarrierClaim,
	reconciliation reconcile.Result,
	relationObservations observerelation.Batch,
	options runOptions,
) (runResult, error) {
	nextGlobalClaims, globalRetirementCount, err := retireGlobalCarrierClaims(
		ctx,
		paths.CarrierClaimRegistryPath,
		globalClaims,
		globalRetirements,
	)
	if err != nil {
		return runResult{
			ActionCount: stateResult.ActionCount + globalRetirementCount,
			StatePath:   stateResult.StatePath,
			State:       stateResult.State,
		}, err
	}
	removalResult, err := runCarrierRemovals(ctx, carrierRemovalInput{
		StatePath:              stateResult.StatePath,
		SelectedRoot:           paths.ManifestRoot,
		Current:                stateResult.State,
		GlobalClaims:           nextGlobalClaims,
		Actions:                reconciliation.CarrierAbsences(),
		RelationAuthorityPaths: relationObservations.AuthorityPaths(),
		ProjectRoot:            options.projectRoot,
		Adapter:                options.CarrierRemovalAdapter,
		Executor:               options.HostRouteExecutor,
		Observer:               options.CarrierRemovalObserver,
		BaselineObserver:       options.CarrierRemovalBaselineObserver,
		RemoveGlobalClaim: func(
			ctx context.Context,
			claim durablecarrier.ManagedCarrierClaim,
		) (durablecarrier.GlobalCarrierClaims, error) {
			store, storeErr := carrierclaimstore.New(paths.CarrierClaimRegistryPath)
			if storeErr != nil {
				return durablecarrier.GlobalCarrierClaims{}, storeErr
			}
			return store.Remove(ctx, claim)
		},
		ValidateBeforeEffects: options.validateBeforeEffects,
	})
	if err != nil {
		return runResult{
			ActionCount:       stateResult.ActionCount + globalRetirementCount + removalResult.ActionCount,
			StatePath:         stateResult.StatePath,
			State:             removalResult.State,
			HostRouteAttempts: removalResult.Attempts,
		}, err
	}
	next, err := runHostRoutesOrderDelegatesAndPersistAttemptRecords(
		ctx,
		paths,
		locked,
		selection,
		stateResult.StatePath,
		removalResult.State,
		carrierOwner,
		removalResult.GlobalClaims,
		stateResult.ActionCount+globalRetirementCount+removalResult.ActionCount,
		reconciliation,
		options,
	)
	next.HostRouteAttempts = append(removalResult.Attempts, next.HostRouteAttempts...)
	if err != nil {
		return next, err
	}
	nextGlobalClaims, adoptionCount, err := commitGlobalCarrierAdoptions(
		ctx,
		paths.CarrierClaimRegistryPath,
		next.GlobalCarrierClaims,
		globalAdoptions,
	)
	next.GlobalCarrierClaims = nextGlobalClaims
	next.ActionCount += adoptionCount
	return next, err
}

func retireGlobalCarrierClaims(
	ctx context.Context,
	registryPath string,
	current durablecarrier.GlobalCarrierClaims,
	claims []durablecarrier.ManagedCarrierClaim,
) (durablecarrier.GlobalCarrierClaims, int, error) {
	if len(claims) == 0 {
		return current, 0, nil
	}
	store, err := carrierclaimstore.New(registryPath)
	if err != nil {
		return current, 0, err
	}
	next := current
	for index, claim := range claims {
		updated, err := store.Remove(ctx, claim)
		if err != nil {
			return next, index, fmt.Errorf(
				"retire global carrier claim[%d]: %w",
				index,
				err,
			)
		}
		if updated.Equal(next) {
			return next, index, fmt.Errorf(
				"retire global carrier claim[%d]: exact claim is absent",
				index,
			)
		}
		next = updated
	}
	return next, len(claims), nil
}
