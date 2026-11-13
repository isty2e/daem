package nativeoperation

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"time"

	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/execute/applyprogram"
	"github.com/isty2e/daem/internal/effect/execute/executionevent"
	"github.com/isty2e/daem/internal/effect/execute/hostmutation"
	"github.com/isty2e/daem/internal/effect/execute/registrysettlement"

	"github.com/isty2e/daem/internal/effect/execute/documentmutation"

	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
)

// NativeApplyInput supplies the containing operation's live observations and
// derived state. Ordinary Apply entrypoints remain closed to native effects.
type NativeApplyInput struct {
	execute.ApplyInput
	Preparation effectpreparation.NativeApplyStatePreparation
	Documents   journalpublication.NativeDocumentObservationBatch
	Registry    ownership.Registry
	Budget      *physicalwork.Budget
	// RecoveryCodecs reconstructs the same profile without inheriting forward cancellation.
	RecoveryCodecs func(context.Context) (catalog aggregate.CodecCatalog, journalCodec durable.SnapshotCodec, statefileCodec statefile.PhysicalCodec, err error)
}

type nativeApplyWork struct {
	documents          journalpublication.NativeDocumentObservationBatch
	managed            []effectpreparation.ManagedPathEffect
	whole              []effectpreparation.ManagedPathEffect
	ordinaryAggregates []effectpreparation.AggregateEffect
	edits              []compose.ProjectionEdit
	mutating           []bool
	promoting          []bool
	changed            bool
	registryScope      ownership.DocumentScope
}

func nativeApplyWorkFor(input NativeApplyInput) (nativeApplyWork, error) {
	if len(input.ManagedPathEffects)+len(input.AggregateEffects)+len(input.NativeNarrowings)+len(input.RetiredProjectCarrierClaims)+len(input.AdoptedProjectCarrierClaims)+len(input.ConfirmedRelationActions) != 0 {
		return nativeApplyWork{}, fmt.Errorf("native execution requires its derived effects rather than independent ordinary decisions")
	}
	if input.RecoveryCodecs == nil {
		return nativeApplyWork{}, fmt.Errorf("native execution requires its cancellation-independent recovery codec factory")
	}
	if err := input.Preparation.Validate(); err != nil {
		return nativeApplyWork{}, err
	}
	if !input.Preparation.BeforeState().Equal(input.CurrentState) || !input.Documents.Evidence().BeforeState().Equal(input.CurrentState) || input.Budget == nil {
		return nativeApplyWork{}, fmt.Errorf("native execution inputs differ from their complete observed Before")
	}
	documents, members, err := input.Documents.CaptureMembers(input.Preparation.AfterState(), journalpublication.PlanningOperationID(), input.Preparation.SequenceEdits()...)
	if err != nil {
		return nativeApplyWork{}, err
	}
	namespaces := documents.Evidence().Namespaces()
	promoting := make([]bool, len(members))
	for index, member := range members {
		promoting[index] = member.RequiresPromotion()
	}
	work := nativeApplyWork{
		documents: documents, promoting: promoting, mutating: make([]bool, len(namespaces)),
		managed: input.Preparation.ManagedPathEffects(), changed: input.Preparation.StateChanged(),
	}
	var registryAliases []aggregate.DocumentAddress
	wholeGroups := make(map[int]struct{})
	ordinaryManaged := work.managed[:0:0]
	for _, effect := range work.managed {
		if !aggregate.IsHostConfigDocumentSubject(effect.Subject()) {
			ordinaryManaged = append(ordinaryManaged, effect)
			continue
		}
		work.whole = append(work.whole, effect)
	}
	work.managed = ordinaryManaged
	wholeSchedule, err := hostmutation.PlanManagedPaths(work.whole)
	if err != nil {
		return nativeApplyWork{}, err
	}
	for _, phase := range []hostmutation.ManagedPathPhase{hostmutation.ManagedPathPublishPhase, hostmutation.ManagedPathRetirePhase} {
		for operation := range wholeSchedule.Operations(phase) {
			effect := operation.Effect()
			consumers, scope, destination := effect.ConsumerTargets(), effect.Scope(), effect.Destination()
			if previous, present := effect.PreviousState(); present && (len(consumers) == 0 || operation.Relocates() && phase == hostmutation.ManagedPathRetirePhase) {
				consumers = previous.ConsumerTargets()
				if operation.Relocates() && phase == hostmutation.ManagedPathRetirePhase {
					scope, destination = previous.Scope(), previous.Destination()
				}
			}
			if len(consumers) != 1 {
				return nativeApplyWork{}, fmt.Errorf("native whole effect requires one consumer")
			}
			address, err := aggregate.NewDocumentAddress(consumers[0], scope, destination)
			if err != nil {
				return nativeApplyWork{}, err
			}
			found := false
			for index, namespace := range namespaces {
				if !slices.Contains(namespace.Addresses(), address) {
					continue
				}
				if _, duplicate := wholeGroups[index]; duplicate {
					return nativeApplyWork{}, fmt.Errorf("native document has multiple whole writers")
				}
				wholeGroups[index] = struct{}{}
				work.mutating[index], found = effect.Kind() != effectpreparation.ManagedPathEffectRecord, true
			}
			if !found {
				return nativeApplyWork{}, fmt.Errorf("native whole effect has no retained namespace")
			}
			registryAliases = append(registryAliases, address)
		}
	}
	for _, action := range input.Preparation.Narrowings() {
		placement, _ := action.SourceContract().HostConfigPlacement()
		address, err := aggregate.NewDocumentAddress(placement.Target(), placement.Scope(), placement.Destination())
		if err != nil {
			return nativeApplyWork{}, err
		}
		registryAliases = append(registryAliases, address)
		plan, present := action.KeyPlan()
		if !present {
			continue
		}
		selection, err := plan.Before().Selection()
		if err != nil {
			return nativeApplyWork{}, err
		}
		participant, err := journaldocument.Participant(selection, input.Codecs)
		if err != nil {
			return nativeApplyWork{}, err
		}
		codec, ok := input.Codecs.Lookup(selection.CodecContractID())
		if !ok {
			return nativeApplyWork{}, fmt.Errorf("native narrowing codec is not admitted")
		}
		edit, err := compose.NewProjectionEdit(codec, plan, participant.Syntax(), participant.RewriteAreas())
		if err != nil {
			return nativeApplyWork{}, err
		}
		work.edits = append(work.edits, edit)
		rendered, failure := codec.Render(action.CurrentDocument(), plan)
		if failure != nil {
			return nativeApplyWork{}, failure
		}
		for index, namespace := range namespaces {
			if slices.Contains(namespace.Addresses(), address) {
				if _, whole := wholeGroups[index]; whole {
					return nativeApplyWork{}, fmt.Errorf("whole document overlaps ownership narrowing")
				}
				work.mutating[index] = !rendered.Document().Equal(action.CurrentDocument())
			}
		}
	}
	for _, effect := range input.Preparation.AggregateEffects() {
		group := -1
		for index, namespace := range namespaces {
			if slices.Contains(namespace.Addresses(), effect.DocumentAddress()) {
				group = index
				break
			}
		}
		if group < 0 {
			if _, native := aggregate.HostConfigSyntaxForCodec(effect.CodecContractID()); native {
				return nativeApplyWork{}, fmt.Errorf("native effect has no retained namespace")
			}
			work.ordinaryAggregates = append(work.ordinaryAggregates, effect)
			work.changed = true
			continue
		}
		if _, whole := wholeGroups[group]; whole {
			return nativeApplyWork{}, fmt.Errorf("whole document overlaps selected effects")
		}
		registryAliases = append(registryAliases, effect.DocumentAddress())
		selection, err := effect.BeforeSnapshot().Selection()
		if err != nil {
			return nativeApplyWork{}, err
		}
		participant, err := journaldocument.Participant(selection, input.Codecs)
		if err != nil {
			return nativeApplyWork{}, err
		}
		codec, admitted := input.Codecs.Lookup(selection.CodecContractID())
		if !admitted {
			return nativeApplyWork{}, fmt.Errorf("document effect codec is not admitted")
		}
		edit, err := compose.NewProjectionEdit(codec, effect.CodecPlan(), participant.Syntax(), participant.RewriteAreas())
		if err != nil {
			return nativeApplyWork{}, err
		}
		work.edits = append(work.edits, edit)
		work.mutating[group] = work.mutating[group] || effect.Kind() != effectpreparation.AggregateEffectRecord
		if effect.BeforeDocument().Exists() && !effect.Rendered().Document().Exists() {
			return nativeApplyWork{}, fmt.Errorf("native root removal requires its bounded document removal integration")
		}
		work.changed = true
	}
	work.changed = work.changed || len(work.managed) != 0
	for _, order := range input.Preparation.SequenceEdits() {
		alias := order.DocumentAddress()
		found := false
		for index, namespace := range namespaces {
			if slices.Contains(namespace.Addresses(), alias) {
				if _, whole := wholeGroups[index]; whole {
					return nativeApplyWork{}, fmt.Errorf("whole document overlaps package ordering")
				}
				work.mutating[index], work.changed, found = true, true, true
				registryAliases = append(registryAliases, alias)
			}
		}
		if !found {
			return nativeApplyWork{}, fmt.Errorf("Pi order has no retained native settings member")
		}
	}
	work.registryScope, err = documents.Evidence().RegistryScopeFor(registryAliases)
	if err != nil {
		return nativeApplyWork{}, err
	}
	return work, nil
}

func MaximumNativeForwardEffectValidationCount(input NativeApplyInput) (int, error) {
	work, err := nativeApplyWorkFor(input)
	if err != nil {
		return 0, err
	}
	return maximumNativeWorkValidationCount(input, work)
}

func maximumNativeWorkValidationCount(input NativeApplyInput, work nativeApplyWork) (int, error) {
	if !work.changed {
		return 0, nil
	}
	ordinary, err := nativeOrdinaryApplyFor(input, work, journalpublication.PlanningOperationID())
	if err != nil {
		return 0, err
	}
	count := 5
	for index, mutating := range work.mutating {
		if mutating {
			count++
		}
		if work.promoting[index] {
			count += 2
		}
	}
	for _, effect := range work.ordinaryAggregates {
		if effect.Kind() != effectpreparation.AggregateEffectRecord {
			count++
		}
	}
	count, err = applyprogram.AddValidationCount(count, ordinary.managedSchedule.WriteCount())
	if err != nil {
		return 0, err
	}
	promoting, err := applyprogram.MultiplyValidationCount(ordinary.ownership.IntentCount(), 2)
	if err != nil {
		return 0, err
	}
	return applyprogram.AddValidationCount(count, promoting)
}

func PrepareNativeApplyEffectPlan(input NativeApplyInput) (applyprogram.Plan, error) {
	work, err := nativeApplyWorkFor(input)
	if err != nil {
		return applyprogram.Plan{}, err
	}
	return prepareNativeApplyEffectPlan(input, work)
}

func prepareNativeApplyEffectPlan(input NativeApplyInput, work nativeApplyWork) (applyprogram.Plan, error) {
	var builder operationplan.EffectStructureBuilder
	if !work.changed {
		return applyprogram.NoOpPlan()
	}
	ordinary, err := nativeOrdinaryApplyFor(input, work, journalpublication.PlanningOperationID())
	if err != nil {
		return applyprogram.Plan{}, err
	}
	promotions, err := applyprogram.NewPromotionSchedule(ordinary.ownership.Intents(), false)
	if err != nil {
		return applyprogram.Plan{}, err
	}
	// Native16 finalizes the complete metadata set unconditionally.
	nodes := []operationplan.EffectNode{
		applyprogram.ForwardObligation(&builder, "apply/native-journal-publication", operationplan.EffectStepPersistence),
		applyprogram.CheckedStep(&builder, "apply/native-preparation", operationplan.EffectStepObservation),
		applyprogram.ForwardObligation(&builder, "apply/native-ownership-preparation", operationplan.EffectStepPersistence),
	}
	for index, mutating := range work.mutating {
		prefix := fmt.Sprintf("apply/native-document/%d", index)
		if mutating {
			nodes = append(nodes, applyprogram.ForwardObligation(&builder, prefix, operationplan.EffectStepPersistence))
		} else {
			nodes = append(nodes, applyprogram.CheckedStep(&builder, prefix+"/record", operationplan.EffectStepObservation))
		}
		if work.promoting[index] {
			nodes = append(nodes, applyprogram.ForwardObligation(&builder, prefix+"/promotion", operationplan.EffectStepPersistence), applyprogram.ForwardObligation(&builder, prefix+"/claims", operationplan.EffectStepPersistence))
		}
	}
	nodes = append(nodes, hostmutation.ForwardEffectNodes(&builder, ordinary.managedSchedule, work.ordinaryAggregates, promotions)...)
	if err := promotions.RequireComplete(); err != nil {
		return applyprogram.Plan{}, err
	}
	if slices.Contains(work.promoting, true) || ordinary.ownership.IntentCount() != 0 {
		nodes = append(nodes, applyprogram.CheckedStep(&builder, "apply/native-promoted-preparation", operationplan.EffectStepObservation))
	}
	nodes = append(
		nodes,
		applyprogram.CheckedStep(&builder, "apply/native-host-postconditions", operationplan.EffectStepObservation),
		applyprogram.ForwardObligation(&builder, "apply/statefile-publication", operationplan.EffectStepPersistence),
		applyprogram.CheckedStep(&builder, "apply/native-finalization-observation", operationplan.EffectStepObservation),
		applyprogram.ForwardObligation(&builder, "apply/native-ownership-finalization", operationplan.EffectStepPersistence),
	)
	nodes = append(nodes, applyprogram.JournalRetirementSchedule(&builder)...)
	segment := operationplan.EffectSequence(nodes...)
	structure, err := builder.Compile(builder.ForwardPhase("apply-effect/core", segment))
	if err != nil {
		return applyprogram.Plan{}, err
	}
	failure, err := applyprogram.CompileFailureSettlement(false)
	if err != nil {
		return applyprogram.Plan{}, err
	}
	return promotions.BindPlan(segment, structure, failure)
}

// ApplyNativeWithEffectPlan settles failed published effects from the observed
// durable phase. Unqualified or changed recovery evidence remains retained.
func ApplyNativeWithEffectPlan(ctx context.Context, input NativeApplyInput, plan applyprogram.Plan, options execute.ApplyOptions) (result ApplyResult, resultErr error) {
	defer func() {
		if resultErr != nil {
			result.nativeSettlement = nil
		}
	}()
	if ctx == nil || input.Filesystem == nil || input.StateCodec == nil || input.OwnershipRegistryBinder == nil || options.AcceptVisibilityChanges == nil || options.ValidateBeforeEffects == nil && options.ValidatePhysicalAuthorityRequests == nil {
		return ApplyResult{}, fmt.Errorf("native execution requires its containing authority, visibility callbacks and selected persistence ports")
	}
	if err := input.Documents.Evidence().ValidatePlanningContext(ctx, input.Owner, input.Registry, input.Budget); err != nil {
		return ApplyResult{}, err
	}
	work, err := nativeApplyWorkFor(input)
	if err != nil {
		return ApplyResult{}, err
	}
	expectedPlan, err := PrepareNativeApplyEffectPlan(input)
	if err != nil {
		return ApplyResult{}, err
	}
	if !work.changed {
		execution, err := applyprogram.Begin(&plan, expectedPlan)
		if err != nil {
			return ApplyResult{}, err
		}
		defer func() { resultErr = errors.Join(resultErr, execution.Finish(resultErr)) }()
		return ApplyResult{ApplyResult: execute.ApplyResult{StatePath: input.Paths.StatefilePath, State: input.CurrentState}}, nil
	}
	if input.StatefileCodec.IsZero() {
		return ApplyResult{}, fmt.Errorf("native statefile codec is required")
	}
	stateContent, err := input.StatefileCodec.EncodeState(input.Preparation.AfterState())
	if err != nil {
		return ApplyResult{}, err
	}
	authority, err := newOperation(input.Paths, input.ProjectRoot, input.Resolver, input.Filesystem, input.Budget)
	if err != nil {
		return ApplyResult{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, authority.close()) }()
	if !input.OwnerReference.IsZero() {
		input.OwnerReference, err = statefile.SelectStatefileReference(input.StatefileCodec.StatefileReference(), input.OwnerReference)
		if err != nil {
			return ApplyResult{}, err
		}
	}
	authority.ownerReference = input.OwnerReference
	if err := authority.bindProjectStatefile(input.Paths.ManifestRoot, input.Paths.StatefilePath); err != nil {
		return ApplyResult{}, err
	}
	store, err := authority.bindOwnershipRegistry(input.Paths.OwnershipRegistryPath, input.OwnershipRegistryBinder)
	if err != nil {
		return ApplyResult{}, err
	}
	actualRegistry, err := input.Documents.Evidence().ReadRegistry(ctx, store)
	if err != nil {
		return ApplyResult{}, errors.Join(fmt.Errorf("native Registry changed before execution"), err)
	}
	registryScope := work.registryScope
	if err := registryScope.RequireWritable(actualRegistry); err != nil {
		return ApplyResult{}, err
	}
	if same, err := registryScope.Matches(input.Registry, actualRegistry); err != nil || !same {
		return ApplyResult{}, errors.Join(fmt.Errorf("native Registry scope changed before execution"), err)
	}
	input.Registry = actualRegistry
	for _, namespace := range work.documents.Evidence().Namespaces() {
		for _, alias := range namespace.Aliases() {
			request, err := alias.PhysicalAuthorityRequest()
			if err != nil {
				return ApplyResult{}, err
			}
			authority.physicalAuthorityRequests = append(authority.physicalAuthorityRequests, request)
		}
	}
	if err := bindNativeOrdinaryApply(ctx, input, work, authority); err != nil {
		return ApplyResult{}, err
	}
	execution, err := applyprogram.Begin(&plan, expectedPlan)
	if err != nil {
		return ApplyResult{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, execution.Finish(resultErr)) }()
	gate := applyprogram.NewVisibilityGate(func(ctx context.Context) error {
		return options.ValidatePhysicalRequests(ctx, authority.physicalAuthorityRequests)
	}, options.AcceptVisibility)
	run := func(prefix string, action func() error) error {
		failure := applyprogram.FailureGuardedRecovery
		if prefix == "apply/native-journal-publication" {
			failure = applyprogram.FailureNoCompensation
		}
		step := execution.VisibilityGate(gate, prefix, operationplan.EffectStepPersistence, failure)
		if err := step.ValidateBefore(ctx); err != nil {
			return err
		}
		if err := step.ApplyEffect(action); err != nil {
			return err
		}
		return step.AcceptAfter(ctx)
	}
	created := time.Now().UTC()
	id := journalpublication.OperationID(created)
	ordinary, err := nativeOrdinaryApplyFor(input, work, id)
	if err != nil {
		return ApplyResult{}, err
	}
	ordinaryPromoting := ordinary.ownership.IntentCount() != 0
	wholeMutations, err := effectpreparation.ManagedPathJournalMutations(work.whole)
	if err != nil {
		return result, err
	}
	var captured journalpublication.CapturedNativeJournal
	if err := run("apply/native-journal-publication", func() error {
		result.ExecutionAttempted = true
		if err := authority.bindRecoveryJournal(input.Paths.ManifestRoot, filepath.Join(input.Paths.RecoveryDir, id)); err != nil {
			return err
		}
		var err error
		captured, err = journalpublication.CaptureNativeJournalWithOptions(ctx, input.Paths.JournalPaths(), id, created, work.documents, input.Preparation.AfterState(), work.edits, journalpublication.NativeCaptureOptions{CaptureOptions: journalpublication.CaptureOptions{
			Filesystem: input.Filesystem, StateCodec: input.StateCodec, Codecs: input.Codecs,
			OperationAuthority: authority.recoveryJournal, ManifestRoot: authority.capturedRoot,
			ManagedPathMutations: ordinary.managedMutations, ManagedAggregateMutations: ordinary.aggregateMutations, ManagedPathEvidence: input.ManagedPathEvidence,
			ClaimTransitions: ordinary.ownership.Transitions(), ProvisionalAcquires: ordinary.ownership.Intents(), RemovalDemands: ordinary.demands,
			Resolver: authority.destinations.JournalResolver(input.Resolver), RootedCapability: authority.destinations.JournalCapability,
		}, ProjectCarrierCompletions: input.Preparation.ProjectCarrierCompletions(), SequenceEdits: input.Preparation.SequenceEdits(), WholeDocuments: wholeMutations, Payloads: input.Payloads})
		if err != nil {
			return err
		}
		return authority.setJournalExecutionBasis(captured.CaptureResult().RecordFingerprint, captured.ActiveJournalAuthority())
	}); err != nil {
		return result, execution.SettleFailureWithoutCompensation(err)
	}
	var retiring *journal.NativeJournalRetirement
	var preparation ownership.ClaimConvergence
	var emptyBindings ownership.DocumentBindingConvergence
	settle := func(primary error) error {
		return settleNativeApplyFailure(ctx, primary, input, options, authority, store, captured, retiring, execution, &result)
	}
	if err := execution.Observe("apply/native-preparation", applyprogram.FailureGuardedRecovery, func() error {
		var err error
		if err = prepareNativeApplyRemovals(ctx, captured, ordinary, work, authority, input, store); err != nil {
			return err
		}
		retiring, preparation, emptyBindings, err = prepareNativeCapturedEffects(ctx, captured, input)
		return err
	}); err != nil {
		return result, settle(err)
	}
	defer func() { resultErr = errors.Join(resultErr, retiring.Close()) }()
	if err := run("apply/native-ownership-preparation", func() error {
		_, err := store.ConvergeRegistry(ctx, preparation, emptyBindings, authority.registryReferences(captured)...)
		return err
	}); err != nil {
		return result, settle(err)
	}
	for index := range work.mutating {
		document := captured.Documents()[index]
		prefix := fmt.Sprintf("apply/native-document/%d", index)
		var err error
		if work.mutating[index] {
			err = run(prefix, func() error {
				return documentmutation.New(authority.destinations, input.Filesystem, authority.removals, input.Budget).PublishCaptured(ctx, document)
			})
		} else {
			err = execution.Observe(prefix+"/record", applyprogram.FailureGuardedRecovery, func() error { return ctx.Err() })
		}
		if err != nil {
			return result, settle(err)
		}

		if work.promoting[index] {
			if err := run(prefix+"/promotion", func() error {
				promoted, err := promoteNativeCapturedDocument(ctx, captured, index, authority, input, store)
				if err == nil {
					captured = promoted
				}
				return err
			}); err != nil {
				return result, settle(err)
			}
			if err := run(prefix+"/claims", func() error {
				return reserveNativePromotedDocument(ctx, store, captured.Documents()[index], authority.registryReferences(captured)...)
			}); err != nil {
				return result, settle(err)
			}
		}
	}
	events := executionevent.NewEmitter(options.Events, len(work.managed)+effectpreparation.AggregateSubjectCount(input.Preparation.AggregateEffects()))
	managedProgress := hostmutation.NewActionProgress(ordinary.managedSchedule.MutationCount())
	promoteManaged := func(ctx context.Context, effect effectpreparation.ManagedPathEffect, phase hostmutation.ManagedPathPhase) error {
		return promoteNativeOrdinaryAcquires(ctx, &ordinary.ownership, hostmutation.ProvisionalAcquireKeysForManagedPath(effect, phase), &captured, authority, input, store, gate, execution)
	}
	if err := authority.hostMutation().ApplyManagedPathPhase(ctx, ordinary.managedSchedule, hostmutation.ManagedPathPublishPhase, input.Payloads, managedProgress, 0, events, gate, execution, promoteManaged); err != nil {
		return result, settle(err)
	}
	if _, err := authority.hostMutation().ApplyAggregateEffects(ctx, work.ordinaryAggregates, input.Codecs, len(work.managed), events, gate, execution, func(ctx context.Context, effect effectpreparation.AggregateEffect) error {
		return promoteNativeOrdinaryAcquires(ctx, &ordinary.ownership, hostmutation.ProvisionalAcquireKeysForAggregate(effect), &captured, authority, input, store, gate, execution)
	}); err != nil {
		return result, settle(err)
	}
	if err := authority.hostMutation().ApplyManagedPathPhase(ctx, ordinary.managedSchedule, hostmutation.ManagedPathRetirePhase, input.Payloads, managedProgress, 0, events, gate, execution, promoteManaged); err != nil {
		return result, settle(err)
	}
	if slices.Contains(work.promoting, true) || ordinaryPromoting {
		if err := execution.Observe("apply/native-promoted-preparation", applyprogram.FailureGuardedRecovery, func() error {
			if captured.NeedsPromotion() {
				return fmt.Errorf("native operation retains unresolved namespace promotion")
			}
			if retiring == nil {
				var err error
				retiring, err = journal.PrepareNativeJournalRetirement(ctx, captured, input.Budget, input.Filesystem, input.StateCodec)
				return err
			}
			return nil
		}); err != nil {
			return result, settle(err)
		}
	}
	if err := execution.Observe("apply/native-host-postconditions", applyprogram.FailureGuardedRecovery, func() error {
		observed, err := observeNativeRuntime(ctx, captured, authority, input, store)
		if err != nil {
			return err
		}
		if !observed.HostReadyForStateCommit() {
			return fmt.Errorf("native host postconditions are not satisfied")
		}
		return nil
	}); err != nil {
		return result, settle(err)
	}
	stateGate := execution.VisibilityGate(gate, "apply/statefile-publication", operationplan.EffectStepPersistence, applyprogram.FailureGuardedRecovery)
	if err := stateGate.ValidateBefore(ctx); err != nil {
		return result, settle(err)
	}
	if err := stateGate.ApplyEffect(func() error {
		before, err := authority.projectStatefile.Read(ctx, input.StatefileCodec, input.Budget)
		if err != nil || !before.Equal(input.CurrentState) {
			return errors.Join(fmt.Errorf("native state changed before commit"), err)
		}
		commit := authority.projectStatefile.Commit(ctx, stateContent, 0o600)
		if !commit.Committed() {
			return fmt.Errorf("native state commit: %w", commit.Failure())
		}
		result.State, result.StatePath = input.Preparation.AfterState(), input.Paths.StatefilePath
		result.ActionCount = input.Preparation.ActionCount()
		return nil
	}); err != nil {
		return result, settle(err)
	}
	if err := stateGate.AcceptAfter(ctx); err != nil {
		return result, settle(err)
	}
	observe := func() (journalqualification.NativeRecoveryAssessment, error) {
		return observeNativeRuntime(ctx, captured, authority, input, store)
	}
	var assessment journalqualification.NativeRecoveryAssessment
	if err := execution.Observe("apply/native-finalization-observation", applyprogram.FailureGuardedRecovery, func() error { var err error; assessment, err = observe(); return err }); err != nil {
		return result, settle(err)
	}
	if err := run("apply/native-ownership-finalization", func() error {
		_, err := store.ConvergeRegistry(ctx, assessment.FinalizeClaims(), assessment.FinalizeBindings())
		return err
	}); err != nil {
		return result, settle(err)
	}
	steps := []struct {
		name string
		kind operationplan.EffectStepKind
		run  func() error
	}{
		{"project-validation", operationplan.EffectStepObservation, func() error { return authority.validateProjectSelection(input.Paths.ManifestRoot) }},
		{"forward", operationplan.EffectStepForwardEffect, func() error { return gate.ValidateBefore(ctx) }},
		{"reload", operationplan.EffectStepObservation, func() error { var err error; assessment, err = observe(); return err }},
		{"settlement", operationplan.EffectStepRetirement, func() error { return retiring.ExecuteWithGate(ctx, assessment, nil) }},
		{"acceptance", operationplan.EffectStepObservation, func() error { return gate.AcceptAfter(ctx) }},
		{"final-project-validation", operationplan.EffectStepObservation, func() error { return authority.validateProjectSelection(input.Paths.ManifestRoot) }},
	}
	for _, step := range steps {
		if err := execution.RunStep("apply/journal-retirement/"+step.name, step.kind, applyprogram.FailureNoCompensation, step.run); err != nil {
			return result, execution.SettleFailureWithoutCompensation(err)
		}
	}
	state, registry, observed := assessment.ObservedPersistence()
	if !observed {
		return result, fmt.Errorf("native settlement has no final persistence observation")
	}
	observedScope, err := work.documents.Evidence().RegistryScope()
	if err != nil {
		return result, err
	}
	settlement, err := registrysettlement.NewPersistenceSnapshot(state, observedScope, registry)
	if err != nil {
		return result, err
	}
	result.nativeSettlement = &settlement
	return result, nil
}
