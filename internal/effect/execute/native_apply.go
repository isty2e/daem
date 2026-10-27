package execute

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
)

// NativeApplyInput supplies the containing operation's live observations and
// derived state. Ordinary Apply entrypoints remain closed to native effects.
type NativeApplyInput struct {
	ApplyInput
	Preparation NativeApplyStatePreparation
	Documents   journal.NativeDocumentObservationBatch
	Registry    ownership.Registry
	Budget      *recovery.PhysicalWorkBudget
	// RecoveryCodecs reconstructs the same profile without inheriting forward cancellation.
	RecoveryCodecs func(context.Context) (aggregate.CodecCatalog, durable.SnapshotCodec, error)
}

type nativeApplyWork struct {
	edits     []compose.ProjectionEdit
	mutating  []bool
	promoting []bool
	changed   bool
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
	if !input.Preparation.BeforeState().Equal(input.CurrentState) || !input.Documents.BeforeState().Equal(input.CurrentState) || input.Budget == nil {
		return nativeApplyWork{}, fmt.Errorf("native execution inputs differ from their complete observed Before")
	}
	if len(input.Preparation.ManagedPathEffects())+len(input.Preparation.Narrowings())+len(input.Preparation.WholeAdoptions()) != 0 {
		return nativeApplyWork{}, fmt.Errorf("native whole-document and ordinary effects require their execution integration")
	}
	namespaces := input.Documents.Namespaces()
	work := nativeApplyWork{mutating: make([]bool, len(namespaces)), promoting: make([]bool, len(namespaces)), changed: !input.CurrentState.Equal(input.Preparation.AfterState())}
	for index, namespace := range namespaces {
		_, work.promoting[index] = namespace.Observation().Provisional()
	}
	for _, effect := range input.Preparation.AggregateEffects() {
		selection, err := effect.BeforeSnapshot().Selection()
		if err != nil {
			return nativeApplyWork{}, err
		}
		participant, err := journal.NativeJournalParticipant(selection, input.Codecs)
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
		found := false
		for index, namespace := range namespaces {
			if slices.Contains(namespace.Addresses(), effect.DocumentAddress()) {
				found = true
				work.mutating[index] = work.mutating[index] || effect.Kind() != AggregateEffectRecord
			}
		}
		if !found {
			return nativeApplyWork{}, fmt.Errorf("native effect has no retained namespace")
		}
		if !effect.Rendered().Document().Exists() {
			return nativeApplyWork{}, fmt.Errorf("native root removal requires its bounded document removal integration")
		}
		work.changed = true
	}
	return work, nil
}

func MaximumNativeForwardEffectValidationCount(input NativeApplyInput) (int, error) {
	work, err := nativeApplyWorkFor(input)
	if err != nil || !work.changed {
		return 0, err
	}
	count := 5 // journal, ownership preparation/finalization, state and retirement
	for index, mutating := range work.mutating {
		if mutating {
			count++
		}
		if work.promoting[index] {
			count += 2
		}
	}
	return count, nil
}

func PrepareNativeApplyEffectPlan(input NativeApplyInput) (ApplyEffectPlan, error) {
	work, err := nativeApplyWorkFor(input)
	if err != nil {
		return ApplyEffectPlan{}, err
	}
	var builder operationplan.EffectStructureBuilder
	if !work.changed {
		return compileApplyEffectPlan(applyStateTransition{}, managedPathExecutionSchedule{}, ownershipMutationState{}, nil)
	}
	nodes := []operationplan.EffectNode{
		applyForwardObligation(&builder, "apply/native-journal-publication", operationplan.EffectStepPersistence),
		applyCheckedStep(&builder, "apply/native-preparation", operationplan.EffectStepObservation),
		applyForwardObligation(&builder, "apply/native-ownership-preparation", operationplan.EffectStepPersistence),
	}
	for index, mutating := range work.mutating {
		prefix := fmt.Sprintf("apply/native-document/%d", index)
		if mutating {
			nodes = append(nodes, applyForwardObligation(&builder, prefix, operationplan.EffectStepPersistence))
		} else {
			nodes = append(nodes, applyCheckedStep(&builder, prefix+"/record", operationplan.EffectStepObservation))
		}
		if work.promoting[index] {
			nodes = append(nodes, applyForwardObligation(&builder, prefix+"/promotion", operationplan.EffectStepPersistence), applyForwardObligation(&builder, prefix+"/claims", operationplan.EffectStepPersistence))
		}
	}
	if slices.Contains(work.promoting, true) {
		nodes = append(nodes, applyCheckedStep(&builder, "apply/native-promoted-preparation", operationplan.EffectStepObservation))
	}
	nodes = append(
		nodes,
		applyCheckedStep(&builder, "apply/native-host-postconditions", operationplan.EffectStepObservation),
		applyForwardObligation(&builder, "apply/statefile-publication", operationplan.EffectStepPersistence),
		applyCheckedStep(&builder, "apply/native-finalization-observation", operationplan.EffectStepObservation),
		applyForwardObligation(&builder, "apply/native-ownership-finalization", operationplan.EffectStepPersistence),
	)
	nodes = append(nodes, applyJournalRetirementSchedule(&builder)...)
	segment := operationplan.EffectSequence(nodes...)
	structure, err := builder.Compile(builder.ForwardPhase("apply-effect/core", segment))
	if err != nil {
		return ApplyEffectPlan{}, err
	}
	failure, err := compileApplyFailureSettlementStructure(false)
	if err != nil {
		return ApplyEffectPlan{}, err
	}
	return ApplyEffectPlan{segment: segment, structure: structure, failureStructure: failure, changed: true, valid: true}, nil
}

// ApplyNativeWithEffectPlan settles failed published effects from the observed
// durable phase. Unqualified or changed recovery evidence remains retained.
func ApplyNativeWithEffectPlan(ctx context.Context, input NativeApplyInput, plan ApplyEffectPlan, options ApplyOptions) (result ApplyResult, resultErr error) {
	if ctx == nil || input.Filesystem == nil || input.StateCodec == nil || input.OwnershipRegistryBinder == nil || options.AcceptVisibilityChanges == nil || options.ValidateBeforeEffects == nil && options.ValidatePhysicalAuthorityRequests == nil {
		return ApplyResult{}, fmt.Errorf("native execution requires its containing authority, visibility callbacks and selected persistence ports")
	}
	if err := input.Documents.ValidatePlanningContext(ctx, input.Owner, input.Registry, input.Budget); err != nil {
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
		execution, err := beginApplyEffectExecution(&plan, expectedPlan)
		if err != nil {
			return ApplyResult{}, err
		}
		defer func() { resultErr = errors.Join(resultErr, execution.finish(resultErr)) }()
		return ApplyResult{StatePath: input.Paths.StatefilePath, State: input.CurrentState}, nil
	}
	stateContent, err := input.StateCodec.Encode(input.Preparation.AfterState())
	if err != nil {
		return ApplyResult{}, err
	}
	authority, err := captureMutationAuthorityWithPhysicalWorkBudget(input.Paths, true, input.ProjectRoot, input.Resolver, input.Filesystem, input.Budget)
	if err != nil {
		return ApplyResult{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, options.closeAuthority(authority)) }()
	authority.ownershipRegistryBinder = input.OwnershipRegistryBinder
	if err := authority.bindProjectStatefile(input.Paths.ManifestRoot, input.Paths.StatefilePath); err != nil {
		return ApplyResult{}, err
	}
	if err := authority.bindOwnershipRegistry(input.Paths.OwnershipRegistryPath); err != nil {
		return ApplyResult{}, err
	}
	store, err := authority.rootedOwnershipRegistry()
	if err != nil {
		return ApplyResult{}, err
	}
	actualRegistry, err := store.LoadForClaimRemovals(ctx, nil, recovery.MaximumPhysicalPathDepth, input.Budget)
	if err != nil || !actualRegistry.Equal(input.Registry) {
		return ApplyResult{}, errors.Join(fmt.Errorf("native Registry changed before execution"), err)
	}
	for _, namespace := range input.Documents.Namespaces() {
		for _, alias := range namespace.Addresses() {
			path, err := input.Resolver(alias.AggregateRoot())
			if err != nil {
				return ApplyResult{}, err
			}
			authority.physicalAuthorityRequests = append(authority.physicalAuthorityRequests, mutation.PhysicalAuthorityRequest{Path: path, Target: string(alias.Target()), Scope: string(alias.Scope())})
		}
	}
	execution, err := beginApplyEffectExecution(&plan, expectedPlan)
	if err != nil {
		return ApplyResult{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, execution.finish(resultErr)) }()
	gate := visibilityEffectGate{before: func(ctx context.Context) error { return options.validatePhysicalAuthority(ctx, authority) }, after: options.acceptVisibilityChanges}
	run := func(prefix string, action func() error) error {
		failure := applyForwardFailureGuardedRecovery
		if prefix == "apply/native-journal-publication" {
			failure = applyForwardFailureNoCompensation
		}
		step := execution.visibilityGate(gate, prefix, operationplan.EffectStepPersistence, failure)
		if err := step.validateBefore(ctx); err != nil {
			return err
		}
		if err := step.applyEffect(action); err != nil {
			return err
		}
		return step.acceptAfter(ctx)
	}
	created := time.Now().UTC()
	id := journal.OperationID(created)
	var captured journal.CapturedNativeJournal
	if err := run("apply/native-journal-publication", func() error {
		result.ExecutionAttempted = true
		if err := authority.bindRecoveryJournal(input.Paths.ManifestRoot, filepath.Join(input.Paths.RecoveryDir, id)); err != nil {
			return err
		}
		var err error
		captured, err = journal.CaptureNativeJournalWithOptions(ctx, input.Paths.journalPaths(), id, created, input.Documents, input.Preparation.AfterState(), work.edits, journal.NativeCaptureOptions{CaptureOptions: journal.CaptureOptions{
			Filesystem: input.Filesystem, StateCodec: input.StateCodec, Codecs: input.Codecs,
			OperationAuthority: authority.recoveryJournal, ManifestRoot: authority.capturedRoot,
		}})
		return err
	}); err != nil {
		return result, execution.settleFailureWithoutCompensation(err)
	}
	var retiring *journal.NativeJournalRetirement
	var preparation ownership.ClaimConvergence
	var emptyBindings ownership.DocumentBindingConvergence
	settle := func(primary error) error {
		return settleNativeApplyFailure(ctx, primary, input, options, authority, store, captured, retiring, execution, &result)
	}
	if err := execution.runObservation("apply/native-preparation", applyForwardFailureGuardedRecovery, func() error {
		var err error
		retiring, preparation, emptyBindings, err = prepareNativeCapturedEffects(ctx, captured, input)
		return err
	}); err != nil {
		return result, settle(err)
	}
	defer func() { resultErr = errors.Join(resultErr, retiring.Close()) }()
	if err := run("apply/native-ownership-preparation", func() error { _, err := store.ConvergeRegistry(ctx, preparation, emptyBindings); return err }); err != nil {
		return result, settle(err)
	}
	for index := range work.mutating {
		document := captured.Documents()[index]
		prefix := fmt.Sprintf("apply/native-document/%d", index)
		var err error
		if work.mutating[index] {
			err = run(prefix, func() error { return publishNativeDocument(ctx, input, document) })
		} else {
			err = execution.runObservation(prefix+"/record", applyForwardFailureGuardedRecovery, func() error { return ctx.Err() })
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
			if err := run(prefix+"/claims", func() error { return reserveNativePromotedDocument(ctx, store, captured.Documents()[index]) }); err != nil {
				return result, settle(err)
			}
		}
	}
	if slices.Contains(work.promoting, true) {
		if err := execution.runObservation("apply/native-promoted-preparation", applyForwardFailureGuardedRecovery, func() error {
			if captured.NeedsPromotion() {
				return fmt.Errorf("native operation retains unresolved namespace promotion")
			}
			if retiring == nil {
				var err error
				retiring, err = journal.PrepareNativeJournalRetirement(ctx, captured, input.Filesystem, input.StateCodec)
				return err
			}
			return nil
		}); err != nil {
			return result, settle(err)
		}
	}
	if err := execution.runObservation("apply/native-host-postconditions", applyForwardFailureGuardedRecovery, func() error {
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
	stateGate := execution.visibilityGate(gate, "apply/statefile-publication", operationplan.EffectStepPersistence, applyForwardFailureGuardedRecovery)
	if err := stateGate.validateBefore(ctx); err != nil {
		return result, settle(err)
	}
	if err := stateGate.applyEffect(func() error {
		before, err := readNativeCommittedState(ctx, authority, input)
		if err != nil || !before.Equal(input.CurrentState) {
			return errors.Join(fmt.Errorf("native state changed before commit"), err)
		}
		commit := options.statefileCommit(ctx, authority, input.Paths.StatefilePath, stateContent, 0o600)
		if !commit.committed() {
			return fmt.Errorf("native state commit: %w", commit.failure())
		}
		result.State, result.StatePath = input.Preparation.AfterState(), input.Paths.StatefilePath
		result.ActionCount = aggregateSubjectCount(input.Preparation.AggregateEffects())
		return nil
	}); err != nil {
		return result, settle(err)
	}
	if err := stateGate.acceptAfter(ctx); err != nil {
		return result, settle(err)
	}
	observe := func() (journal.NativeRecoveryAssessment, error) {
		return observeNativeRuntime(ctx, captured, authority, input, store)
	}
	var assessment journal.NativeRecoveryAssessment
	if err := execution.runObservation("apply/native-finalization-observation", applyForwardFailureGuardedRecovery, func() error { var err error; assessment, err = observe(); return err }); err != nil {
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
		{"forward", operationplan.EffectStepForwardEffect, func() error { return gate.validateBefore(ctx) }},
		{"reload", operationplan.EffectStepObservation, func() error { var err error; assessment, err = observe(); return err }},
		{"settlement", operationplan.EffectStepRetirement, func() error { return retiring.ExecuteWithGate(ctx, assessment, nil) }},
		{"acceptance", operationplan.EffectStepObservation, func() error { return gate.acceptAfter(ctx) }},
		{"final-project-validation", operationplan.EffectStepObservation, func() error { return authority.validateProjectSelection(input.Paths.ManifestRoot) }},
	}
	for _, step := range steps {
		if err := execution.runCheckedStep("apply/journal-retirement/"+step.name, step.kind, applyForwardFailureNoCompensation, step.run); err != nil {
			return result, execution.settleFailureWithoutCompensation(err)
		}
	}
	return result, nil
}
