package journal

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/target"
)

type ordinaryRecordPhase uint8

const (
	ordinaryRecordEntryReady ordinaryRecordPhase = iota + 1
	ordinaryRecordBefore
	ordinaryRecordBinding
	ordinaryRecordRemoval
	ordinaryRecordClaimsReady
	ordinaryRecordClaims
	ordinaryRecordComplete
	ordinaryRecordFinished
	ordinaryRecordFailed
)

type ordinaryRecordCaptureInput struct {
	operationID    string
	createdAt      time.Time
	before, after  durable.Snapshot
	manifest       rootedpath.AuthorityProvenance
	mutations      []captureMutation
	removalDemands recovery.RemovalDemandSet
}

type capturedGlobalBinding struct {
	path       string
	provenance rootedpath.AuthorityProvenance
}

func (binding capturedGlobalBinding) persisted() *recoveryGlobalPathBinding {
	return &recoveryGlobalPathBinding{ResolvedPath: binding.path, RootProvenance: persistedRecoveryRootProvenance(binding.provenance)}
}

// The draft sequences record facts around caller-owned effects. Completion
// supplies neither family-specific wire admission nor execution authority.
type ordinaryRecordDraft struct {
	phase           ordinaryRecordPhase
	input           ordinaryRecordCaptureInput
	state           map[recoveryStateKey]recoveryManagedState
	demands         map[removalRelationKey]recovery.RemovalDemand
	entries         []recoveryEntry
	cursor          int
	pending         recoveryEntry
	pendingBefore   recovery.BeforePathState
	pendingExpected recovery.ExpectedPathState
	coverage        *claimCoverageSession
	removalIntents  []recoveryRemovalIntent
}

func newOrdinaryRecordDraft(input ordinaryRecordCaptureInput) (*ordinaryRecordDraft, error) {
	state, err := recoveryStateByAction(input.before)
	if err != nil {
		return nil, err
	}
	if err := input.removalDemands.Validate(); err != nil {
		return nil, fmt.Errorf("validate executable removal demands: %w", err)
	}
	if err := validateCaptureMutations(input.mutations); err != nil {
		return nil, err
	}
	input.mutations = slices.Clone(input.mutations)
	for index := range input.mutations {
		input.mutations[index].facts = cloneCaptureMutation(input.mutations[index].facts)
	}
	demands := make(map[removalRelationKey]recovery.RemovalDemand, input.removalDemands.Len())
	for _, demand := range input.removalDemands.Demands() {
		demands[removalRelationKey{scope: demand.Scope(), destination: demand.Destination()}] = demand
	}
	return &ordinaryRecordDraft{
		phase: ordinaryRecordEntryReady, input: input, state: state, demands: demands,
		entries: make([]recoveryEntry, 0, len(input.mutations)),
	}, nil
}

func cloneCaptureMutation(value pathMutation) pathMutation {
	value.ConsumerTargets = slices.Clone(value.ConsumerTargets)
	if value.PreviousState != nil {
		previous := *value.PreviousState
		previous.ConsumerTargets = slices.Clone(previous.ConsumerTargets)
		value.PreviousState = &previous
	}
	if value.AggregateContract != nil {
		contract := value.AggregateContract.Clone()
		value.AggregateContract = &contract
	}
	return value
}

func (draft *ordinaryRecordDraft) reject(err error) error {
	if draft != nil {
		draft.phase = ordinaryRecordFailed
	}
	return err
}

func (draft *ordinaryRecordDraft) entryCount() int { return len(draft.input.mutations) }

func (draft *ordinaryRecordDraft) beginEntry(ctx context.Context) (captureMutation, error) {
	if draft == nil || draft.phase != ordinaryRecordEntryReady || draft.cursor >= len(draft.input.mutations) {
		return captureMutation{}, draft.reject(fmt.Errorf("record capture has no next entry in its current phase"))
	}
	if ctx == nil {
		return captureMutation{}, draft.reject(fmt.Errorf("record capture context is required"))
	}
	if err := ctx.Err(); err != nil {
		return captureMutation{}, draft.reject(err)
	}
	mutation := draft.input.mutations[draft.cursor]
	if err := mutation.Validate(); err != nil {
		return captureMutation{}, draft.reject(err)
	}
	mutation.facts = cloneCaptureMutation(mutation.facts)
	draft.phase = ordinaryRecordBefore
	return mutation, nil
}

func (draft *ordinaryRecordDraft) supplyBefore(before recovery.BeforePathState) error {
	if draft == nil || draft.phase != ordinaryRecordBefore {
		return draft.reject(fmt.Errorf("record capture has no outstanding before-state demand"))
	}
	action := draft.input.mutations[draft.cursor].facts
	stateBefore, err := captureRecoveryStateBefore(action, draft.state)
	if err != nil {
		return draft.reject(err)
	}
	expected, stateExpected, err := captureRecoveryExpectedAfter(action)
	if err != nil {
		return draft.reject(err)
	}
	before.PathMode = clonePermissionMode(before.PathMode)
	draft.pendingBefore, draft.pendingExpected = before, expected.Clone()
	draft.pending = recoveryEntry{
		Subject: subjectRefFromAction(action), Target: string(action.Target), Targets: targetStrings(action.ConsumerTargets),
		Scope: string(action.Scope), Path: action.Destination.String(), ContentPath: string(action.ContentPath), ContentKind: string(action.ContentKind),
		Before: persistedBeforePathState(before), ExpectedAfter: persistedExpectedPathState(expected),
		StateBeforeIdentity: stateBeforeIdentityFromAction(action), StateBefore: stateBefore, StateExpectedAfter: stateExpected,
		StateIndependent: action.StateIndependent,
	}
	draft.phase = ordinaryRecordBinding
	return nil
}

func (draft *ordinaryRecordDraft) supplyGlobalBinding(binding *capturedGlobalBinding) error {
	if draft == nil || draft.phase != ordinaryRecordBinding {
		return draft.reject(fmt.Errorf("record capture has no outstanding global-binding demand"))
	}
	action := draft.input.mutations[draft.cursor].facts
	if (action.Scope == target.ScopeGlobal) != (binding != nil) {
		return draft.reject(fmt.Errorf("record capture global binding differs from its entry scope"))
	}
	rootedBefore, rootedExpected := completeRemovalTransitionStates(action, draft.pendingBefore, draft.pendingExpected)
	transition, err := removalTransitionForCapture(action.Scope, action.Destination, rootedBefore, rootedExpected, draft.demands)
	if err != nil {
		return draft.reject(fmt.Errorf("capture removal transition for %q: %w", action.Destination, err))
	}
	contract, err := persistedAggregateContractFromMutation(action)
	if err != nil {
		return draft.reject(err)
	}
	if binding != nil {
		draft.pending.GlobalPathBinding = binding.persisted()
	}
	draft.pending.RemovalTransition, draft.pending.Aggregate = transition, contract
	draft.entries = append(draft.entries, draft.pending)
	draft.pending = recoveryEntry{}
	draft.pendingBefore, draft.pendingExpected = recovery.BeforePathState{}, recovery.ExpectedPathState{}
	draft.cursor++
	draft.phase = ordinaryRecordEntryReady
	return nil
}

func (draft *ordinaryRecordDraft) closeEntries() error {
	if draft == nil || draft.phase != ordinaryRecordEntryReady || draft.cursor != len(draft.input.mutations) {
		return draft.reject(fmt.Errorf("record capture entries are incomplete"))
	}
	captured, err := removalDemandSetFromEntries(draft.entries)
	if err != nil {
		return draft.reject(err)
	}
	if !draft.input.removalDemands.Equal(captured) {
		return draft.reject(fmt.Errorf("executable removal demands do not exactly match captured removal transitions"))
	}
	draft.phase = ordinaryRecordRemoval
	return nil
}

func (draft *ordinaryRecordDraft) supplyRemovalIntents(intents []recovery.RemovalIntent) error {
	if draft == nil || draft.phase != ordinaryRecordRemoval {
		return draft.reject(fmt.Errorf("record capture has no outstanding removal evidence demand"))
	}
	if err := validateRecoveryRemovalCoverage(draft.input.removalDemands, intents); err != nil {
		return draft.reject(err)
	}
	persisted, err := persistedRecoveryRemovalIntents(intents)
	if err != nil {
		return draft.reject(err)
	}
	draft.removalIntents = persisted
	draft.phase = ordinaryRecordClaimsReady
	return nil
}

func (draft *ordinaryRecordDraft) beginClaimCoverage(transitions []ownershipmutation.ClaimTransition, intents []ownership.ProvisionalAcquireIntent) error {
	if draft == nil || draft.phase != ordinaryRecordClaimsReady {
		return draft.reject(fmt.Errorf("record capture cannot begin claim coverage before removal evidence"))
	}
	sortRecoveryEntries(draft.entries)
	coverage, err := newClaimCoverageSession(draft.entries, transitions, intents)
	if err != nil {
		return draft.reject(err)
	}
	draft.coverage, draft.phase = coverage, ordinaryRecordClaims
	return nil
}

func (draft *ordinaryRecordDraft) nextClaimAuthority(ctx context.Context) (claimAuthorityDemand, bool, error) {
	if draft == nil || draft.phase != ordinaryRecordClaims {
		return claimAuthorityDemand{}, false, draft.reject(fmt.Errorf("record capture claim coverage is not active"))
	}
	demand, present, err := draft.coverage.next(ctx)
	if err != nil {
		return claimAuthorityDemand{}, false, draft.reject(err)
	}
	if !present {
		draft.phase = ordinaryRecordComplete
	}
	return demand, present, nil
}

func (draft *ordinaryRecordDraft) bindClaimAuthority(observation mutation.DirectoryEntryAuthorityObservation) error {
	if draft == nil || draft.phase != ordinaryRecordClaims {
		return draft.reject(fmt.Errorf("record capture has no outstanding claim observation"))
	}
	if err := draft.coverage.bind(observation); err != nil {
		return draft.reject(err)
	}
	return nil
}

func (draft *ordinaryRecordDraft) finish() (ordinaryCaptureMaterial, error) {
	if draft == nil || draft.phase != ordinaryRecordComplete {
		return ordinaryCaptureMaterial{}, draft.reject(fmt.Errorf("record capture is incomplete"))
	}
	entries, err := draft.coverage.finish()
	if err != nil {
		return ordinaryCaptureMaterial{}, draft.reject(err)
	}
	transitions, err := recoveryClaimTransitions(draft.coverage.transitions)
	if err != nil {
		return ordinaryCaptureMaterial{}, draft.reject(err)
	}
	intents, err := recoveryProvisionalAcquireIntents(draft.coverage.intents)
	if err != nil {
		return ordinaryCaptureMaterial{}, draft.reject(err)
	}
	draft.phase = ordinaryRecordFinished
	return ordinaryCaptureMaterial{
		operationID: draft.input.operationID, operation: recoveryOperationApply, createdAt: draft.input.createdAt.UTC().Format(time.RFC3339),
		manifest: persistedRecoveryRootProvenance(draft.input.manifest), entries: entries,
		before: draft.input.before, after: draft.input.after,
		claims: transitions, provisional: intents, removals: draft.removalIntents, initialized: true,
	}, nil
}
