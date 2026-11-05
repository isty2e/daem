package execute

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/effect/execute/registrysettlement"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/output/ownership"
)

const (
	nativeRecoveryMetadataReference        = "native-recovery/metadata"
	nativeRecoveryCleanupReference         = "native-recovery/removal-cleanup"
	nativeRecoveryRetirementReference      = "native-recovery/journal-retirement"
	nativeRecoveryOrdinaryReference        = "native-recovery/ordinary"
	nativeRecoveryOrdinaryCleanupReference = "native-recovery/ordinary-cleanup"
)

// ExecuteNativeRecovery reopens the exact disclosed native16 operation. The
// containing workflow retains leases and last-safe-point visibility ownership.
func ExecuteNativeRecovery(ctx context.Context, plan journal.NativeRecoveryPlan, paths Paths, load journal.PlanLoadOptions, options RecoveryOptions) (resultErr error) {
	if ctx == nil || options.Filesystem == nil || options.ValidateBeforeEffects == nil || options.AcceptVisibilityChanges == nil || options.OwnershipRegistryBinder == nil || load.NativeDocuments == nil {
		return fmt.Errorf("native recovery requires its selected dependencies and containing authority")
	}
	operation, err := journal.OpenNativeRecovery(ctx, paths.journalPaths(), plan, load)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, operation.Close()) }()
	budget := operation.Budget()
	authority, err := captureMutationAuthorityWithPhysicalWorkBudget(paths, true, operation.ManifestRoot(), options.Resolver, options.Filesystem, budget)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, authority.close()) }()
	authority.ownershipRegistryBinder = options.OwnershipRegistryBinder
	if err := authority.bindProjectStatefile(paths.ManifestRoot, paths.StatefilePath); err != nil {
		return err
	}
	if err := authority.bindRecoveryJournal(paths.ManifestRoot, plan.OperationDir()); err != nil {
		return err
	}
	if err := authority.bindOwnershipRegistry(paths.OwnershipRegistryPath); err != nil {
		return err
	}
	store, err := authority.rootedOwnershipRegistry()
	if err != nil {
		return err
	}
	prepared, err := compileNativeRecoverySchedule(operation)
	if err != nil {
		return err
	}
	return executeNativeRecoveryOperation(ctx, operation, authority, store, options, nil, prepared)
}

func executeNativeRecoveryOperation(ctx context.Context, operation *journal.NativeRecoveryOperation, authority *mutationAuthority, store ownershipmutation.RegistryStore, options RecoveryOptions, retiring *journal.NativeJournalRetirement, prepared operationplan.EffectStructure) (resultErr error) {
	budget, plan := operation.Budget(), operation.Plan()
	if plan.HasErrors() {
		return fmt.Errorf("joint recovery has blocked or unavailable cleanup evidence")
	}
	aliases, err := plan.DocumentAliases()
	if err != nil {
		return err
	}
	for _, group := range aliases {
		for _, alias := range group {
			path, err := options.Resolver(alias.AggregateRoot())
			if err != nil {
				return err
			}
			request := mutation.PhysicalAuthorityRequest{Path: path, Target: string(alias.Target()), Scope: string(alias.Scope())}
			if !slices.Contains(authority.physicalAuthorityRequests, request) {
				authority.physicalAuthorityRequests = append(authority.physicalAuthorityRequests, request)
			}
		}
	}
	requests, err := operation.OrdinaryPhysicalAuthorityRequests()
	if err != nil {
		return err
	}
	for _, request := range requests {
		if !slices.Contains(authority.physicalAuthorityRequests, request) {
			authority.physicalAuthorityRequests = append(authority.physicalAuthorityRequests, request)
		}
	}
	initial := operation.InitialAssessment()
	registryScope, err := operation.RegistryScope()
	if err != nil {
		return err
	}
	if !initial.CleanBefore() && !initial.CleanAfter() && !initial.RequiresRollback() && !initial.RequiresFinalization() {
		return fmt.Errorf("native recovery has no admitted execution phase")
	}
	stateReader := func(ctx context.Context) (durable.Snapshot, error) {
		return authority.readNativeState(ctx, operation.StateCodec(), budget)
	}
	semanticGuard := func(ctx context.Context) error {
		state, err := stateReader(ctx)
		if err != nil || !state.Equal(operation.ObservedState()) {
			return errors.Join(fmt.Errorf("joint recovery state changed before restoration"), err)
		}
		registry, err := registrysettlement.Read(ctx, operation, initial, store)
		if err != nil {
			return errors.Join(fmt.Errorf("joint recovery Registry changed before restoration"), err)
		}
		if err := registryScope.RequireWritable(registry); err != nil {
			return err
		}
		if same, err := registryScope.Matches(operation.ObservedRegistry(), registry); err != nil || !same {
			return errors.Join(fmt.Errorf("joint recovery Registry scope changed before restoration"), err)
		}
		return nil
	}
	observe := func() (journal.NativeRecoveryAssessment, error) {
		state, err := stateReader(ctx)
		if err != nil || !state.Equal(operation.ObservedState()) {
			return journal.NativeRecoveryAssessment{}, errors.Join(fmt.Errorf("native recovery state changed during settlement"), err)
		}
		return operation.Assess(ctx, journal.NativeRecoveryOptions{Filesystem: options.Filesystem, OperationAuthority: authority.recoveryJournal, State: stateReader, Registry: store, Codecs: operation.Codecs()})
	}
	beforeEffect := func() error {
		set, err := authority.physicalAuthority()
		if err != nil {
			return err
		}
		return options.ValidateBeforeEffects(ctx, set)
	}
	afterEffect := func() error { return options.AcceptVisibilityChanges(ctx) }
	if err := beforeEffect(); err != nil {
		return err
	}
	hostActions, err := recoveryHostActionsFor(plan.OrdinaryActions())
	if err != nil {
		return err
	}
	if err := prepareNativeRecoveryRemovals(ctx, authority, operation, hostActions); err != nil {
		return err
	}
	ordinary, err := prepareNativeOrdinaryRecovery(ctx, authority, operation, hostActions)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, ordinary.cleanup(context.WithoutCancel(ctx), authority)) }()
	if retiring == nil {
		retiring, err = operation.PrepareRetirement(ctx, options.Filesystem)
	}
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, retiring.Close()) }()
	emptyBindings, err := ownership.NewDocumentBindingConvergence(nil)
	if err != nil {
		return err
	}
	var discharged []recovery.RemovalCleanupObligation
	steps := []nativeRecoveryStep{{name: nativeRecoveryCleanupReference, kind: operationplan.EffectStepCleanup, run: func() error {
		if err := beforeEffect(); err != nil {
			return err
		}
		intents, err := plan.RemovalIntents()
		if err != nil {
			return err
		}
		discharged, err = authority.cleanupRemovalIntentList(ctx, intents, operation.UsesCapturedRemovalReservations(), plan.CleanupReady)
		if err != nil {
			return err
		}
		if len(ordinary.actions) != 0 {
			if err := budget.ConcludeCleanupLifecycle(); err != nil {
				return err
			}
		}
		return afterEffect()
	}}}
	if initial.RequiresRollback() {
		for index, document := range operation.Documents() {
			if !document.Restoration().Changed() {
				continue
			}
			steps = append(steps, nativeRecoveryStep{name: fmt.Sprintf("native-recovery/document/%d", index), kind: operationplan.EffectStepPersistence, run: func() error {
				if err := beforeEffect(); err != nil {
					return err
				}
				if options.ValidateVisibilityAuthority != nil {
					if err := options.ValidateVisibilityAuthority(ctx); err != nil {
						return err
					}
				}
				if err := semanticGuard(ctx); err != nil {
					return err
				}
				destination, err := nativeRecoveryDestination(ctx, document, budget)
				if err != nil {
					return err
				}
				current, mode := document.Current()
				outcome := publishDocumentRestoration(ctx, authority, destination, document.Comparison().MaximumDocumentBytes(), current, mode, document.Restoration())
				if outcome.err != nil {
					return outcome.err
				}
				return afterEffect()
			}})
		}
	}
	if len(ordinary.actions) != 0 {
		steps = append(steps, nativeRecoveryStep{name: nativeRecoveryOrdinaryReference, kind: operationplan.EffectStepPersistence, run: func() error {
			if err := authority.beginJointGeneralRecoveryExecution(); err != nil {
				return err
			}
			gate := visibilityEffectGate{before: func(ctx context.Context) error {
				if err := beforeEffect(); err != nil {
					return err
				}
				if options.ValidateVisibilityAuthority != nil {
					if err := options.ValidateVisibilityAuthority(ctx); err != nil {
						return err
					}
				}
				return semanticGuard(ctx)
			}, after: options.AcceptVisibilityChanges}
			return ordinary.execute(ctx, authority, operation, store, semanticGuard, gate)
		}})
		steps = append(steps, nativeRecoveryStep{name: nativeRecoveryOrdinaryCleanupReference, kind: operationplan.EffectStepCleanup, run: func() error {
			return ordinary.cleanup(context.WithoutCancel(ctx), authority)
		}})
	}
	steps = append(steps, nativeRecoveryStep{name: nativeRecoveryMetadataReference, kind: operationplan.EffectStepPersistence, run: func() error {
		if err := beforeEffect(); err != nil {
			return err
		}
		assessment, err := observe()
		if err != nil {
			return err
		}
		switch {
		case assessment.RequiresRollback(), assessment.CleanBefore():
			err = registrysettlement.Converge(ctx, operation, assessment, store, assessment.RollbackClaims(), emptyBindings)
		case assessment.RequiresFinalization(), assessment.CleanAfter():
			err = registrysettlement.Converge(ctx, operation, assessment, store, assessment.FinalizeClaims(), assessment.FinalizeBindings())
		default:
			return fmt.Errorf("native recovery metadata phase changed")
		}
		if err != nil {
			return err
		}
		return afterEffect()
	}})
	steps = append(steps, nativeRecoveryStep{name: nativeRecoveryRetirementReference, kind: operationplan.EffectStepRetirement, run: func() error {
		if !plan.CleanupReady(discharged) {
			return fmt.Errorf("joint removal cleanup obligations are not discharged")
		}
		assessment, err := observe()
		if err != nil {
			return err
		}
		if !assessment.CleanBefore() && !assessment.CleanAfter() {
			return fmt.Errorf("joint recovery is not clean enough to retire")
		}
		structure, err := compileActiveJournalRetirementStructure()
		if err != nil {
			return err
		}
		retirementExecution := newActiveRetirementExecution(structure)
		gate := nativeRetirementGate{ctx: ctx, before: beforeEffect, after: afterEffect, execution: retirementExecution}
		err = retiring.ExecuteWithGate(ctx, assessment, &gate)
		return errors.Join(err, retirementExecution.finish(err))
	}})
	return runNativeRecoverySteps(steps, prepared)
}

type nativeRecoveryStep struct {
	name string
	kind operationplan.EffectStepKind
	run  func() error
}

func runNativeRecoverySteps(steps []nativeRecoveryStep, prepared operationplan.EffectStructure) error {
	structure, err := compileNativeRecoverySteps(steps)
	if err != nil {
		return err
	}
	if !structure.Equal(prepared) {
		return fmt.Errorf("native recovery effects differ from their compiled handoff")
	}
	cursor := structure.Begin()
	for _, step := range steps {
		if err := cursor.Consume(step.name, step.kind); err != nil {
			return err
		}
		operationErr := step.run()
		alternative := 1
		if operationErr != nil {
			alternative = 0
		}
		if err := cursor.SelectAlternative(step.name+"/outcome", alternative); err != nil {
			return errors.Join(operationErr, err)
		}
		if operationErr != nil {
			return errors.Join(operationErr, cursor.Consume(step.name+"/failed", operationplan.EffectStepTerminal), cursor.FinishSuccess())
		}
	}
	return errors.Join(cursor.Consume("native-recovery/complete", operationplan.EffectStepTerminal), cursor.FinishSuccess())
}

type nativeRetirementGate struct {
	ctx           context.Context
	before, after func() error
	execution     *activeRetirementExecution
}

func (gate *nativeRetirementGate) AdmitRetirementStep(step journal.RetirementExecutionStep) error {
	if err := gate.before(); err != nil {
		return err
	}
	return gate.execution.AdmitRetirementStep(step)
}

func (gate *nativeRetirementGate) SettleRetirementStep(step journal.RetirementExecutionStep, succeeded bool) error {
	if err := gate.execution.SettleRetirementStep(step, succeeded); err != nil {
		return err
	}
	if succeeded {
		switch step {
		case journal.RetirementStepPublishControl, journal.RetirementStepRetireActiveJournal, journal.RetirementStepAdvanceRecord, journal.RetirementStepCleanupResidue, journal.RetirementStepRetireControl, journal.RetirementStepCleanupGarbage:
			return gate.after()
		}
	}
	return nil
}

func compileNativeRecoverySchedule(operation *journal.NativeRecoveryOperation) (operationplan.EffectStructure, error) {
	steps := []nativeRecoveryStep{{name: nativeRecoveryCleanupReference, kind: operationplan.EffectStepCleanup}}
	if operation.InitialAssessment().RequiresRollback() {
		for index, document := range operation.Documents() {
			if document.Restoration().Changed() {
				steps = append(steps, nativeRecoveryStep{name: fmt.Sprintf("native-recovery/document/%d", index), kind: operationplan.EffectStepPersistence})
			}
		}
	}
	hostActions, err := recoveryHostActionsFor(operation.Plan().OrdinaryActions())
	if err != nil {
		return operationplan.EffectStructure{}, err
	}
	if len(hostActions) != 0 {
		steps = append(
			steps,
			nativeRecoveryStep{name: nativeRecoveryOrdinaryReference, kind: operationplan.EffectStepPersistence},
			nativeRecoveryStep{name: nativeRecoveryOrdinaryCleanupReference, kind: operationplan.EffectStepCleanup},
		)
	}
	steps = append(steps, nativeRecoveryStep{name: nativeRecoveryMetadataReference, kind: operationplan.EffectStepPersistence}, nativeRecoveryStep{name: nativeRecoveryRetirementReference, kind: operationplan.EffectStepRetirement})
	return compileNativeRecoverySteps(steps)
}

func compileNativeRecoverySteps(steps []nativeRecoveryStep) (operationplan.EffectStructure, error) {
	var builder operationplan.EffectStructureBuilder
	node := builder.Step("native-recovery/complete", operationplan.EffectStepTerminal)
	for index := len(steps) - 1; index >= 0; index-- {
		step := steps[index]
		node = operationplan.EffectSequence(builder.Step(step.name, step.kind), builder.Choice(step.name+"/outcome", builder.Step(step.name+"/failed", operationplan.EffectStepTerminal), node))
	}
	return builder.Compile(node)
}
