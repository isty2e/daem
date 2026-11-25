package apply

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
	"github.com/isty2e/daem/internal/assurance/observe/relation/selecteddoc"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/desired/agentconfig"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/execute/applyprogram"
	"github.com/isty2e/daem/internal/effect/execute/nativeoperation"
	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/effect/mutation"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/operationplan"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	projectionbuild "github.com/isty2e/daem/internal/reconcile/build/projection"
	"github.com/isty2e/daem/internal/recoverygate"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/topology"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

type nativePlanInputs struct {
	state          effectpreparation.NativeApplyStatePreparation
	admission      nativeoperation.ForwardDocumentAdmission
	codecs         aggregate.CodecCatalog
	stateCodec     durable.SnapshotCodec
	statefileCodec statefile.PhysicalCodec
	stateDocuments []aggregate.DocumentAddress
	opaque         bool
	prefix         effectpreparation.NativeApplyStatePreparation
	suffix         []effectpreparation.AggregateEffect
	orders         []selecteddoc.SelectedDocumentEdit
}

func rejectUnsupportedCommandPlan(planned commandPlan) error {
	if planned.nativePlan != nil {
		return nil
	}
	if len(planned.assessment.NativeSources) != 0 || planned.assessment.CurrentState.ContainsAgentConfig() {
		return effectpreparation.ErrNativeOperationRequired
	}
	return execute.RejectUnsupportedActions(planned.assessment.Reconciliation)
}

func maximumCommandForwardValidationCount(current commandPlan, ordinary execute.ApplyInput) (int, error) {
	if current.nativePlan == nil {
		return execute.MaximumForwardEffectValidationCount(ordinary)
	}
	input, err := nativeApplyInputFor(current)
	if err != nil {
		return 0, err
	}
	return nativeoperation.MaximumNativeForwardEffectValidationCount(input)
}

func prepareCommandApplyEffectPlan(current commandPlan, ordinary execute.ApplyInput) (applyprogram.Plan, error) {
	if current.nativePlan == nil {
		return execute.PrepareApplyEffectPlan(ordinary)
	}
	input, err := nativePrefixApplyInputFor(current)
	if err != nil {
		return applyprogram.Plan{}, err
	}
	return nativeoperation.PrepareNativeApplyEffectPlan(input)
}

func admitNativeCommandPlan(ctx context.Context, planned *commandPlan) error {
	assessment := planned.assessment
	if len(assessment.NativeSources) == 0 && !assessment.CurrentState.ContainsAgentConfig() && !assessment.Reconciliation.ContainsAgentConfig() {
		return nil
	}
	if err := checkNativeCommandSupport(assessment); err != nil {
		return err
	}
	var native, whole []mutation.DirectoryEntryAuthorityObservation
	for _, evidence := range assessment.NativeApproval {
		native = append(native, evidence.Authority)
		if nativeconfig.IsWholeInvocationDocument(evidence.Aliases, assessment.NativeSources, assessment.CurrentState) {
			whole = append(whole, evidence.Authority)
		}
	}
	checkPaths := func(paths []relationobserve.AuthorityPath, selected []mutation.DirectoryEntryAuthorityObservation, reason nativeSupportReason) error {
		if len(selected) == 0 || len(paths) == 0 {
			return nil
		}
		budget, err := planned.context.PersistenceEpoch.PhysicalBudget()
		if err != nil {
			return err
		}
		err = nativeconfig.RequireDisjointOpaqueNamespaces(ctx, selected, paths, budget)
		if errors.Is(err, nativeconfig.ErrOpaqueNamespaceOverlap) {
			return nativeSupportError{reason}
		}
		return err
	}
	checkNamespaces := func(key desiredextension.CarrierKey, operation lock.OperationKind) error {
		profile, err := relationhost.NativeSettingsProfileFor(key, operation)
		if err != nil {
			return err
		}
		paths, err := profile.ExclusionPaths(planned.context.Paths)
		if err != nil {
			return err
		}
		if err := checkPaths(paths, native, nativeSupportNamespace); err != nil {
			return err
		}
		rewrite, err := profile.RewritePaths(planned.context.Paths, planned.context.physicalBudget)
		if err != nil {
			return err
		}
		return checkPaths(rewrite, whole, nativeSupportWholeRewrite)
	}
	for _, action := range assessment.ReservationReconciliation().Relations() {
		if action.InvokesHostRoute() {
			if err := checkNamespaces(action.CarrierIdentity().Carrier().Key(), lock.OperationInstall); err != nil {
				return err
			}
		}
	}
	for _, action := range assessment.Reconciliation.CarrierAbsences() {
		if action.InvokesHostRoute() || action.MutatesDirectProjection() {
			if err := checkNamespaces(action.Claim().Identity().Carrier().Key(), lock.OperationRemove); err != nil {
				return err
			}
		}
	}
	codecs, codec, physicalCodec, err := nativePersistenceCodecs(ctx)
	if err != nil {
		return err
	}
	physicalCodec, err = planned.context.PersistenceEpoch.PhysicalStateCodec(physicalCodec)
	if err != nil {
		return err
	}
	stateInput := effectpreparation.NativeApplyStateInput{
		ClaudeHookSettings: planned.context.Paths.ClaudeHookSettings,
		Before:             assessment.CurrentState, ManagedPaths: assessment.Reconciliation.ManagedPaths(),
		Aggregates: assessment.Reconciliation.Aggregates(), StateCodec: codec,
		ConfirmedRelationActions: assessment.Reconciliation.Relations(),
		Narrowings:               assessment.Reconciliation.NativeNarrowings(),
	}
	for _, source := range assessment.NativeSources {
		placement, _ := source.SourceContract().AgentConfigPlacement()
		if placement.Ownership() == agentconfig.OwnershipDocument {
			stateInput.WholeSources = append(stateInput.WholeSources, source.SourceContract())
		}
	}
	state, err := effectpreparation.PrepareNativeApplyState(ctx, stateInput)
	if err != nil {
		return err
	}
	var nativeOrders []selecteddoc.SelectedDocumentEdit
	for _, order := range assessment.SequenceEdits {
		if _, ordered := order.SelectedSequence().Constraint(); !ordered {
			continue
		}
		alias := order.DocumentAddress()
		for _, evidence := range assessment.NativeApproval {
			if !slices.Contains(evidence.Aliases, alias) {
				continue
			}
			member, err := evidence.HasMembers(ctx, assessment.CurrentState, state.AfterState())
			if err != nil {
				return err
			}
			if member {
				nativeOrders = append(nativeOrders, order)
				break
			}
		}
	}

	for _, decision := range assessment.Reconciliation.RelationOrders() {
		if !decision.RequiresMutation() {
			continue
		}
		found := false
		for _, order := range nativeOrders {
			if order.Sequence().SequenceID() != decision.SequenceID() || !order.Changed() {
				continue
			}
			stateInput.SequenceEdits = append(stateInput.SequenceEdits, order)
			found = true
		}
		if !found {
			for _, constraint := range planned.context.Lockfile.Locked.OrderConstraints() {
				if constraint.ClassID() != decision.ClassID() {
					continue
				}
				authority, err := relationhost.OrderAuthorityPaths(relationhost.OrderInput{Paths: planned.context.Paths, Lockfile: planned.context.Lockfile, Constraint: constraint})
				if err != nil {
					return err
				}
				var paths []relationobserve.AuthorityPath
				for _, selected := range authority {
					path, err := relationobserve.NewAuthorityPath(selected.Path(), selected.Target(), selected.Scope())
					if err != nil {
						return err
					}
					paths = append(paths, path)
				}
				if err := checkPaths(paths, native, nativeSupportNamespace); err != nil {
					return err
				}
				found = true
				break
			}
			if !found {
				return nativeSupportError{nativeSupportOrder}
			}
		}
	}
	if len(stateInput.SequenceEdits) != 0 {
		state, err = effectpreparation.PrepareNativeApplyState(ctx, stateInput)
		if err != nil {
			return err
		}
	}
	_, handoff := assessment.Reconciliation.CarrierHandoff()
	opaque := handoff || nativeDelegatesScheduled(assessment.Reconciliation.Delegates())
	for _, action := range assessment.Reconciliation.Relations() {
		opaque = opaque || action.InvokesHostRoute()
	}
	for _, action := range assessment.Reconciliation.CarrierAbsences() {
		opaque = opaque || action.InvokesHostRoute()
	}
	for _, prerequisite := range assessment.ReservationMCPProviders() {
		_, present, err := prerequisite.InstallAction()
		if err != nil {
			return err
		}
		opaque = opaque || present
	}
	documents, err := nativeStateDocuments(ctx, assessment, state.AfterState())
	if err != nil {
		return err
	}
	planned.nativePlan = &nativePlanInputs{state: state, codecs: codecs, stateCodec: codec, statefileCodec: physicalCodec, stateDocuments: documents, orders: nativeOrders}
	if opaque {
		if err := prepareNativeInvocationRemainder(ctx, planned); err != nil {
			return err
		}
	}
	return observeNativeAdmissionForPlan(ctx, planned)
}

func observeNativeAdmissionForPlan(ctx context.Context, planned *commandPlan) error {
	plan := planned.nativePlan
	assessment := planned.assessment
	phases := []effectpreparation.NativeApplyStatePreparation{plan.state}
	if plan.opaque {
		var err error
		phases, err = plan.state.AdmissionPhases(plan.prefix, plan.suffix, nativeDelegatesScheduled(assessment.Reconciliation.Delegates()))
		if err != nil {
			return err
		}
	}
	budget, err := planned.context.PersistenceEpoch.PhysicalBudget()
	if err != nil {
		return err
	}
	admission, err := nativeoperation.ObserveForwardDocumentPhases(ctx, planned.context.Paths.ManifestRoot, assessment.NativeApproval, destinationResolver(planned.context.Paths).Resolve, budget, phases...)
	if err != nil {
		var conflict *nativeoperation.WholeDocumentConflict
		if errors.As(err, &conflict) {
			return errors.Join(nativeSupportError{nativeDocumentConflictReason(conflict.Kind)}, err)
		}
		return err
	}
	plan.admission = admission
	return nil
}

// prepareNativeInvocationRemainder retains the containing invocation phases on
// an actual baseline; preparing the prefix does not execute or reserve it again.
func prepareNativeInvocationRemainder(ctx context.Context, planned *commandPlan) error {
	if planned.nativePlan == nil {
		return fmt.Errorf("native invocation continuation requires its native plan")
	}
	positive, err := projectionbuild.BuildPositiveNativePrefix(ctx, planned.assessment.Reconciliation.Aggregates())
	if err != nil {
		return err
	}
	prefix, err := effectpreparation.PrepareNativeApplyState(ctx, effectpreparation.NativeApplyStateInput{
		ClaudeHookSettings: planned.context.Paths.ClaudeHookSettings,
		Before:             planned.assessment.CurrentState, Aggregates: positive,
		Narrowings: planned.nativePlan.state.PositiveNarrowings(), StateCodec: planned.nativePlan.stateCodec,
	})
	if err != nil {
		return err
	}
	remaining, err := projectionbuild.BuildNativeApplySuffix(ctx, planned.assessment.Reconciliation.Aggregates())
	if err != nil {
		return err
	}
	suffix, err := effectpreparation.AggregateEffectsForNativeOperation(remaining)
	if err != nil {
		return err
	}
	if err := planned.nativePlan.state.ValidateAggregatePhaseSplit(prefix, suffix); err != nil {
		return nativeSupportError{nativeSupportRoutes}
	}
	planned.nativePlan.opaque, planned.nativePlan.prefix, planned.nativePlan.suffix = true, prefix, suffix
	return nil
}

// nativeStateDocuments includes unchanged namespace actors needed to publish a
// complete successor. Membership does not grant permission to write a document.
func nativeStateDocuments(ctx context.Context, assessment readiness.Assessment, after durable.Snapshot) ([]aggregate.DocumentAddress, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if after.Equal(assessment.CurrentState) {
		return nil, nil
	}
	indexes, err := journaldocument.IndexSnapshotsForClaims(ctx, assessment.CurrentState, after, journaldocument.NativeClaims)
	if err != nil {
		return nil, err
	}
	var documents []aggregate.DocumentAddress
	for _, evidence := range assessment.NativeApproval {
		member, err := indexes.HasMembers(ctx, evidence.Aliases)
		if err != nil {
			return nil, err
		}
		if member {
			documents = append(documents, evidence.Aliases...)
		}
	}
	return documents, nil
}

func nativeAuthorityRequestFor(current commandPlan, address aggregate.DocumentAddress) (mutation.PhysicalAuthorityRequest, error) {
	for _, evidence := range current.assessment.NativeApproval {
		if slices.Contains(evidence.Aliases, address) {
			return evidence.AuthorityRequest(address)
		}
	}
	return mutation.PhysicalAuthorityRequest{}, fmt.Errorf("native authority has no selected document alias")
}

func nativeApplyInputFor(current commandPlan) (nativeoperation.NativeApplyInput, error) {
	if current.nativePlan == nil || current.nativeOperation == nil {
		return nativeoperation.NativeApplyInput{}, effectpreparation.ErrNativeOperationRequired
	}
	input, live := current.nativeOperation.NativeInput()
	if !live || input.Documents == nil {
		return nativeoperation.NativeApplyInput{}, fmt.Errorf("native execution has no live retained document operation")
	}
	return nativeoperation.NativeApplyInput{
		ApplyInput: execute.ApplyInput{
			Paths: executePaths(current.context.Paths, current.assessment.StatePath), Resolver: destinationResolver(current.context.Paths).Resolve,
			CurrentState: current.assessment.CurrentState, GlobalCarrierClaims: current.assessment.GlobalCarrierClaims,
			ManagedPathEvidence: current.assessment.ManagedPathEvidence,
			Owner:               current.assessment.Owner, Ownership: current.assessment.Ownership, ProjectRoot: current.projectRoot,
			OwnerReference: current.assessment.OwnerReference,
			Codecs:         current.nativePlan.codecs, StateCodec: current.nativePlan.stateCodec, StatefileCodec: current.nativePlan.statefileCodec, Filesystem: storagecommit.Adapter{}, OwnershipRegistryBinder: ownershipstore.BindRooted,
		},
		Preparation: current.nativePlan.state, Documents: *input.Documents, Registry: input.Registry, Budget: input.Budget, RecoveryCodecs: nativePersistenceCodecs,
	}, nil
}

func nativePrefixApplyInputFor(current commandPlan) (nativeoperation.NativeApplyInput, error) {
	input, err := nativeApplyInputFor(current)
	if err == nil && current.nativePlan.opaque {
		input.Preparation = current.nativePlan.prefix
	}
	return input, err
}

type readinessAssessor func(context.Context, readiness.Input) (readiness.Assessment, *nativeconfig.AssessmentOperation, error)

func previewReadiness(ctx context.Context, input readiness.Input) (readiness.Assessment, *nativeconfig.AssessmentOperation, error) {
	assessment, err := nativeconfig.Assess(ctx, input)
	return assessment, nil, err
}

func retainedReadiness(ctx context.Context, input readiness.Input) (readiness.Assessment, *nativeconfig.AssessmentOperation, error) {
	operation, err := nativeconfig.AssessRetained(ctx, input)
	if err != nil {
		return readiness.Assessment{}, nil, err
	}
	return operation.Assessment(), operation, nil
}

func planReadinessAtPathsWithBarrierValidation(ctx context.Context, input CommandInput, operationContext reconcile.OperationContext, paths daempaths.Paths, barrier *recoverygate.EffectAuthority, validateBarrier func(context.Context) error) (commandPlan, error) {
	return planReadinessAtPathsWithAssessor(ctx, input, operationContext, paths, barrier, validateBarrier, previewReadiness)
}

func applyOperationFingerprint(planned commandPlan, operationContext reconcile.OperationContext) (mutation.OperationFingerprint, error) {
	if planned.context.nativeReplacement != nil {
		payload, err := nativeReplacementApprovalPayload(planned)
		if err != nil {
			return mutation.OperationFingerprint{}, err
		}
		return mutation.NewOperationFingerprint(payload), nil
	}
	if planned.nativePlan == nil && (planned.assessment.Reconciliation.ContainsAgentConfig() || planned.assessment.CurrentState.ContainsAgentConfig()) {
		return mutation.OperationFingerprint{}, effectpreparation.ErrNativeOperationRequired
	}
	identity, err := applyOperationIdentity(planned, operationContext)
	if err != nil {
		return mutation.OperationFingerprint{}, err
	}
	if planned.nativePlan == nil {
		return operationplan.ApplyOperationFingerprint(identity)
	}
	type sourceFacts struct {
		Subject     topology.SubjectID
		SourceID    string
		ResolvedRef string
		ContentHash string
		Target      string
		Scope       string
		Path        string
		Ownership   string
		Grammar     string
	}
	var sources []sourceFacts
	for _, observed := range planned.assessment.NativeSources {
		if err := observed.Validate(); err != nil {
			return mutation.OperationFingerprint{}, err
		}
		contract := observed.SourceContract()
		supply, _ := contract.ExactSupply()
		placement, _ := contract.AgentConfigPlacement()
		sources = append(sources, sourceFacts{
			Subject: contract.SubjectID(), SourceID: string(supply.SourceID()), ResolvedRef: string(supply.ResolvedRef()), ContentHash: string(supply.ContentHash()),
			Target: string(placement.Target()), Scope: string(placement.Scope()), Path: placement.NativePath(), Ownership: string(placement.Ownership()), Grammar: string(placement.Grammar()),
		})
	}
	before, err := planned.nativePlan.stateCodec.Encode(planned.assessment.CurrentState)
	if err != nil {
		return mutation.OperationFingerprint{}, err
	}
	facts, err := json.Marshal(struct {
		Sources []sourceFacts
		Before  artifact.ContentHash
		Limits  configdocument.Limits
	}{sources, artifact.HashFileContent(before), nativeconfig.DocumentLimits()})
	if err != nil {
		return mutation.OperationFingerprint{}, err
	}
	return operationplan.NativeApplyOperationFingerprint(identity, facts)
}

func nativePersistenceCodecs(ctx context.Context) (aggregate.CodecCatalog, durable.SnapshotCodec, statefile.PhysicalCodec, error) {
	codecs, err := nativeconfig.Codecs(ctx)
	if err != nil {
		return aggregate.CodecCatalog{}, nil, statefile.PhysicalCodec{}, err
	}
	codec, err := statefile.NewNativeCodec(codecs.Contributions())
	if err != nil {
		return aggregate.CodecCatalog{}, nil, statefile.PhysicalCodec{}, err
	}
	physicalCodec, err := statefile.NewPhysicalCodec(codec)
	return codecs, codec, physicalCodec, err
}
