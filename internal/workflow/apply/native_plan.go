package apply

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/mutation"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/operationplan"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/recoverygate"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/topology"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

type nativePlanInputs struct {
	state      execute.NativeApplyStatePreparation
	codecs     aggregate.CodecCatalog
	stateCodec durable.SnapshotCodec
}

func rejectUnsupportedCommandPlan(planned commandPlan) error {
	if planned.nativePlan != nil {
		return nil
	}
	if len(planned.assessment.NativeSources) != 0 || planned.assessment.CurrentState.ContainsHostConfig() {
		return execute.ErrNativeOperationRequired
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
	return execute.MaximumNativeForwardEffectValidationCount(input)
}

func prepareCommandApplyEffectPlan(current commandPlan, ordinary execute.ApplyInput) (execute.ApplyEffectPlan, error) {
	if current.nativePlan == nil {
		return execute.PrepareApplyEffectPlan(ordinary)
	}
	input, err := nativeApplyInputFor(current)
	if err != nil {
		return execute.ApplyEffectPlan{}, err
	}
	return execute.PrepareNativeApplyEffectPlan(input)
}

func admitNativeCommandPlan(ctx context.Context, planned *commandPlan) error {
	assessment := planned.assessment
	if len(assessment.NativeSources) == 0 && !assessment.CurrentState.ContainsHostConfig() && !assessment.Reconciliation.ContainsHostConfig() {
		return nil
	}
	if len(assessment.Reconciliation.MutatingManagedPaths())+len(assessment.Reconciliation.Relations())+len(assessment.Reconciliation.CarrierAbsences())+len(assessment.Reconciliation.CarrierAdoptions())+len(assessment.Reconciliation.Delegates()) != 0 {
		return fmt.Errorf("mixed native command effects require their containing execution integration")
	}
	for _, order := range assessment.Reconciliation.RelationOrders() {
		if order.Kind() != reconcile.OrderExact {
			return fmt.Errorf("changed native package order requires its containing execution integration")
		}
	}
	if _, changed, err := assessment.CurrentState.WithConvergedGlobalCarrierClaims(assessment.GlobalCarrierClaims); err != nil {
		return err
	} else if changed {
		return fmt.Errorf("native execution requires its separate carrier-state convergence integration")
	}
	codecs, err := nativeconfig.Codecs(ctx)
	if err != nil {
		return err
	}
	codec, err := statefile.NewNativeCodec(codecs.Contributions())
	if err != nil {
		return err
	}
	state, err := execute.PrepareNativeApplyState(ctx, execute.NativeApplyStateInput{
		Before: assessment.CurrentState, ManagedPaths: assessment.Reconciliation.ManagedPaths(),
		Aggregates: assessment.Reconciliation.Aggregates(), StateCodec: codec,
	})
	if err != nil {
		return err
	}
	planned.nativePlan = &nativePlanInputs{state: state, codecs: codecs, stateCodec: codec}
	return nil
}

func nativeApplyInputFor(current commandPlan) (execute.NativeApplyInput, error) {
	if current.nativePlan == nil || current.nativeOperation == nil {
		return execute.NativeApplyInput{}, execute.ErrNativeOperationRequired
	}
	input, live := current.nativeOperation.NativeInput()
	if !live || input.Documents == nil {
		return execute.NativeApplyInput{}, fmt.Errorf("native execution has no live retained document operation")
	}
	return execute.NativeApplyInput{
		ApplyInput: execute.ApplyInput{
			Paths: executePaths(current.context.Paths, current.assessment.StatePath), Resolver: destinationResolver(current.context.Paths).Resolve,
			CurrentState: current.assessment.CurrentState, GlobalCarrierClaims: current.assessment.GlobalCarrierClaims,
			Owner: current.assessment.Owner, Ownership: current.assessment.Ownership, ProjectRoot: current.projectRoot,
			Codecs: current.nativePlan.codecs, StateCodec: current.nativePlan.stateCodec, Filesystem: storagecommit.Adapter{}, OwnershipRegistryBinder: ownershipstore.BindRooted,
		},
		Preparation: current.nativePlan.state, Documents: *input.Documents, Registry: input.Registry, Budget: input.Budget, RecoveryCodecs: nativePersistenceCodecs,
	}, nil
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
	if planned.nativePlan == nil && (planned.assessment.Reconciliation.ContainsHostConfig() || planned.assessment.CurrentState.ContainsHostConfig()) {
		return mutation.OperationFingerprint{}, execute.ErrNativeOperationRequired
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
		placement, _ := contract.HostConfigPlacement()
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

func nativePersistenceCodecs(ctx context.Context) (aggregate.CodecCatalog, durable.SnapshotCodec, error) {
	codecs, err := nativeconfig.Codecs(ctx)
	if err != nil {
		return aggregate.CodecCatalog{}, nil, err
	}
	codec, err := statefile.NewNativeCodec(codecs.Contributions())
	return codecs, codec, err
}
