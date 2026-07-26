package lockfile

import (
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/desired/entity"
	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func subjectsFromDTO(subjects []lockedSubjectDTO) ([]lock.LockedSubjectContract, error) {
	converted := make([]lock.LockedSubjectContract, 0, len(subjects))
	for index, subject := range subjects {
		contract, err := subjectFromDTO(subject)
		if err != nil {
			return nil, fmt.Errorf("locked.subject[%d]: %w", index, err)
		}
		converted = append(converted, contract)
	}
	return converted, nil
}

func subjectsToDTO(subjects []lock.LockedSubjectContract) ([]lockedSubjectDTO, error) {
	if len(subjects) == 0 {
		return nil, nil
	}
	converted := make([]lockedSubjectDTO, 0, len(subjects))
	for index, subject := range subjects {
		dto, err := subjectToDTO(subject)
		if err != nil {
			return nil, fmt.Errorf("locked subject[%d]: %w", index, err)
		}
		converted = append(converted, dto)
	}
	return converted, nil
}

func subjectFromDTO(dto lockedSubjectDTO) (lock.LockedSubjectContract, error) {
	entityID, err := entity.Parse(dto.EntityID)
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("entity_id: %w", err)
	}
	subjectID, err := topology.ParseSubjectID(dto.SubjectID)
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("subject_id: %w", err)
	}
	exactSupply, err := optionalExactIdentityFromDTO(dto.ExactSupply)
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("exact_supply: %w", err)
	}
	exactFileUse, err := exactFileUseFromDTO(dto.ExactFileUse)
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("exact_file_use: %w", err)
	}
	realization, err := realizationFromDTO(dto.Realization)
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("realization: %w", err)
	}
	derivation, err := derivationFromDTO(dto.Derivation)
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("derivation: %w", err)
	}
	recipe, err := repairRecipeFromDTO(dto.RepairRecipe)
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("repair_recipe: %w", err)
	}
	delegatePlan, err := delegatePlanFromDTO(dto.DelegatePlan)
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("delegate_plan: %w", err)
	}
	correlation, err := skillSetMemberFromDTO(dto.SkillSetMember)
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("skill_set_member: %w", err)
	}
	replay, err := replayCoverageFromDTO(dto.Replay)
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("replay: %w", err)
	}
	operations, err := operationContractsFromDTO(dto.Operations)
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("operation: %w", err)
	}
	return lock.NewLockedSubjectContract(lock.LockedSubjectContractInput{
		EntityID:                  entityID,
		SubjectID:                 subjectID,
		ExactSupply:               exactSupply,
		ExactFileUse:              exactFileUse,
		Realization:               realization,
		Derivation:                derivation,
		RepairRecipe:              recipe,
		DelegatePlanIdentity:      delegatePlan,
		SkillSetMemberCorrelation: correlation,
		Ownership:                 lock.OwnershipBasis(dto.Ownership),
		OnAbsent:                  lock.OnAbsentPolicy(dto.OnAbsent),
		Replay:                    replay,
		OperationContracts:        operations,
	})
}

func subjectToDTO(contract lock.LockedSubjectContract) (lockedSubjectDTO, error) {
	if contract.EntityID().Validate() != nil || contract.SubjectID().Validate() != nil {
		return lockedSubjectDTO{}, fmt.Errorf("invalid locked subject identity")
	}
	return lockedSubjectDTO{
		EntityID:       contract.EntityID().String(),
		SubjectID:      contract.SubjectID().String(),
		ExactSupply:    optionalExactIdentityToDTO(contract.ExactSupply()),
		ExactFileUse:   exactFileUseToDTO(contract.ExactFileUse()),
		Realization:    realizationToDTO(contract.Realization()),
		Derivation:     derivationToDTO(contract.Derivation()),
		RepairRecipe:   repairRecipeToDTO(contract.RepairRecipe()),
		DelegatePlan:   delegatePlanToDTO(contract),
		SkillSetMember: skillSetMemberToDTO(contract.SkillSetMemberCorrelation()),
		Ownership:      string(contract.Ownership()),
		OnAbsent:       string(contract.OnAbsent()),
		Replay:         replayCoverageToDTO(contract.ReplayCoverage()),
		Operations:     operationContractsToDTO(contract),
	}, nil
}

func exactIdentityFromDTO(dto exactIdentityDTO) (artifact.ExactIdentity, error) {
	return artifact.NewExactIdentity(
		artifact.SourceID(dto.SourceID),
		artifact.ResolvedRef(dto.ResolvedRef),
		artifact.ArtifactKind(dto.Kind),
		artifact.ContentHash(dto.ContentHash),
	)
}

func optionalExactIdentityFromDTO(dto *exactIdentityDTO) (*artifact.ExactIdentity, error) {
	if dto == nil {
		return nil, nil
	}
	identity, err := exactIdentityFromDTO(*dto)
	if err != nil {
		return nil, err
	}
	return &identity, nil
}

func exactIdentityToDTO(identity artifact.ExactIdentity) exactIdentityDTO {
	return exactIdentityDTO{
		SourceID:    string(identity.SourceID()),
		ResolvedRef: string(identity.ResolvedRef()),
		Kind:        string(identity.Kind()),
		ContentHash: string(identity.ContentHash()),
	}
}

func optionalExactIdentityToDTO(identity artifact.ExactIdentity, present bool) *exactIdentityDTO {
	if !present {
		return nil
	}
	dto := exactIdentityToDTO(identity)
	return &dto
}

func exactFileUseFromDTO(dto *exactFileUseDTO) (*lock.ExactFileUse, error) {
	if dto == nil {
		return nil, nil
	}
	if dto.Executable == nil {
		return nil, fmt.Errorf("executable is required")
	}
	use, err := lock.NewExactFileUse(target.Scope(dto.Scope), *dto.Executable)
	if err != nil {
		return nil, err
	}
	return &use, nil
}

func exactFileUseToDTO(use lock.ExactFileUse, present bool) *exactFileUseDTO {
	if !present {
		return nil
	}
	executable := use.Executable()
	return &exactFileUseDTO{Scope: string(use.Scope()), Executable: &executable}
}

func realizationFromDTO(dto *realizationDTO) (*realization.RealizationSpec, error) {
	if dto == nil {
		return nil, nil
	}
	count := 0
	if dto.ManagedPath != nil {
		count++
	}
	if dto.ManagedAggregate != nil {
		count++
	}
	if dto.DelegatedRelation != nil {
		count++
	}
	if count != 1 {
		return nil, fmt.Errorf("exactly one realization body is required")
	}
	var (
		spec realization.RealizationSpec
		err  error
	)
	if body := dto.ManagedPath; body != nil {
		exactPermissionMode, modeErr := exactPathPermissionModeFromDTO(body.ExactPermissionMode)
		if modeErr != nil {
			return nil, modeErr
		}
		spec, err = realization.NewManagedPathProjection(realization.ManagedPathProjectionInput{
			PlacementID: body.PlacementID, ConsumerTargets: targetsFromDTO(body.ConsumerTargets), Scope: target.Scope(body.Scope),
			Destination: body.Destination, ContentKind: realization.PathProjectionContentKind(body.ContentKind),
			PlacementMode:          realization.PathProjectionMode(body.PlacementMode),
			PermissionPolicy:       realization.PathPermissionPolicy(body.PermissionPolicy),
			ExactPermissionMode:    exactPermissionMode,
			AdapterContractVersion: body.AdapterContractVersion,
		})
	} else if body := dto.ManagedAggregate; body != nil {
		spec, err = realization.NewManagedAggregateContribution(aggregate.ManagedContributionInput{
			PlacementID: body.PlacementID, Target: target.Target(body.Target), Scope: target.Scope(body.Scope),
			AggregateRoot: body.AggregateRoot, ContentPath: body.ContentPath,
			MergeUnit: aggregate.MergeUnit(body.MergeUnit), SiblingRetention: aggregate.SiblingRetention(body.SiblingRetention),
			Cardinality:         aggregate.ContributionCardinality(body.ContributionCardinality),
			SiblingPreservation: aggregate.SiblingPreservation(body.SiblingPreservation),
			Equivalence:         aggregate.Equivalence(body.Equivalence), CanonicalContribution: body.CanonicalContribution,
			CodecContractID: aggregate.CodecContractID(body.CodecContract), ComparedFields: body.ComparedFields,
		})
	} else {
		body := dto.DelegatedRelation
		subjectKey, keyErr := hostrelation.NewSubjectKey(body.RelationSubjectKey)
		if keyErr != nil {
			return nil, keyErr
		}
		managedKey, keyErr := hostrelation.NewManagedInstanceKey(body.ManagedInstanceKey)
		if keyErr != nil {
			return nil, keyErr
		}
		expected, keyErr := hostrelation.NewExpectedRelation(subjectKey, managedKey)
		if keyErr != nil {
			return nil, keyErr
		}
		spec, err = realization.NewDelegatedRelation(realization.DelegatedRelationInput{
			PlacementID: body.PlacementID, Target: target.Target(body.Target), Scope: target.Scope(body.Scope),
			SourceNamespace: body.SourceNamespace, ExpectedRelation: expected, RouteID: body.RouteID,
			RouteContractVersion: body.RouteContractVersion, CanonicalRequestHash: body.CanonicalRequestHash,
			VerifiedRelationFields: body.VerifiedRelationFields,
		})
	}
	if err != nil {
		return nil, err
	}
	return &spec, nil
}

func realizationToDTO(spec realization.RealizationSpec, present bool) *realizationDTO {
	if !present {
		return nil
	}
	dto := &realizationDTO{}
	if body, ok := spec.ManagedPathProjection(); ok {
		dto.ManagedPath = &managedPathProjectionDTO{
			PlacementID: body.PlacementID(), ConsumerTargets: targetsToDTO(body.ConsumerTargets()), Scope: string(body.Scope()),
			Destination: body.Destination(), ContentKind: string(body.ContentKind()),
			PlacementMode: string(body.PlacementMode()), PermissionPolicy: string(body.PermissionPolicy()),
			ExactPermissionMode:    exactPathPermissionModeToDTO(body),
			AdapterContractVersion: body.AdapterContractVersion(),
		}
	} else if body, ok := spec.ManagedAggregateContribution(); ok {
		dto.ManagedAggregate = &managedAggregateContributionDTO{
			PlacementID: body.PlacementID(), Target: string(body.Target()), Scope: string(body.Scope()),
			AggregateRoot: body.AggregateRoot(), ContentPath: body.ContentPath(), MergeUnit: string(body.MergeUnit()),
			ContributionCardinality: string(body.Cardinality()),
			SiblingRetention:        string(body.SiblingRetention()), SiblingPreservation: string(body.SiblingPreservation()),
			Equivalence: string(body.Equivalence()), CanonicalContribution: body.CanonicalContribution(),
			CodecContract:  string(body.CodecContractID()),
			ComparedFields: body.ComparedFields(),
		}
	} else if body, ok := spec.DelegatedRelation(); ok {
		expected := body.ExpectedRelation()
		dto.DelegatedRelation = &delegatedRelationDTO{
			PlacementID: body.PlacementID(), Target: string(body.Target()), Scope: string(body.Scope()),
			SourceNamespace: body.SourceNamespace(), RelationSubjectKey: string(expected.SubjectKey()),
			ManagedInstanceKey: string(expected.ManagedInstanceKey()), RouteID: body.RouteID(),
			RouteContractVersion: body.RouteContractVersion(), CanonicalRequestHash: body.CanonicalRequestHash(),
			VerifiedRelationFields: body.VerifiedRelationFields(),
		}
	}
	return dto
}

func exactPathPermissionModeFromDTO(value *uint32) (realization.ExactPathPermissionMode, error) {
	if value == nil {
		return realization.ExactPathPermissionMode{}, nil
	}
	mode, err := realization.NewExactPathPermissionMode(os.FileMode(*value))
	if err != nil {
		return realization.ExactPathPermissionMode{}, err
	}
	return mode, nil
}

func exactPathPermissionModeToDTO(projection realization.ManagedPathProjection) *uint32 {
	mode, present := projection.ExactPermissionMode()
	if !present {
		return nil
	}
	value := uint32(mode.FileMode())
	return &value
}

func targetsFromDTO(values []string) []target.Target {
	result := make([]target.Target, 0, len(values))
	for _, value := range values {
		result = append(result, target.Target(value))
	}
	return result
}

func targetsToDTO(values []target.Target) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, string(value))
	}
	return result
}

func derivationFromDTO(dto *derivationDTO) (*lock.DerivationContract, error) {
	if dto == nil {
		return nil, nil
	}
	if (dto.DirectResolution == nil) == (dto.DeterministicTransform == nil) {
		return nil, fmt.Errorf("exactly one derivation body is required")
	}
	var (
		derivation lock.DerivationContract
		err        error
	)
	if dto.DirectResolution != nil {
		identity, identityErr := exactIdentityFromDTO(*dto.DirectResolution)
		if identityErr != nil {
			return nil, identityErr
		}
		derivation, err = lock.NewDirectResolutionDerivation(identity)
	} else {
		body := dto.DeterministicTransform
		input, inputErr := exactIdentityFromDTO(body.InputIdentity)
		if inputErr != nil {
			return nil, inputErr
		}
		output, outputErr := exactIdentityFromDTO(body.ExpectedOutputIdentity)
		if outputErr != nil {
			return nil, outputErr
		}
		derivation, err = lock.NewDeterministicTransformDerivation(lock.DeterministicTransform{
			InputIdentity: input, RecipeHash: body.RecipeHash, AlgorithmID: body.AlgorithmID,
			AlgorithmVersion: body.AlgorithmVersion, ExecutionDomain: body.ExecutionDomain,
			ExpectedOutputIdentity: output,
		})
	}
	if err != nil {
		return nil, err
	}
	return &derivation, nil
}

func derivationToDTO(derivation lock.DerivationContract, present bool) *derivationDTO {
	if !present {
		return nil
	}
	dto := &derivationDTO{}
	if identity, ok := derivation.DirectResolution(); ok {
		encoded := exactIdentityToDTO(identity)
		dto.DirectResolution = &encoded
	} else if body, ok := derivation.DeterministicTransform(); ok {
		dto.DeterministicTransform = &deterministicTransformDTO{
			InputIdentity: exactIdentityToDTO(body.InputIdentity), RecipeHash: body.RecipeHash,
			AlgorithmID: body.AlgorithmID, AlgorithmVersion: body.AlgorithmVersion,
			ExecutionDomain: body.ExecutionDomain, ExpectedOutputIdentity: exactIdentityToDTO(body.ExpectedOutputIdentity),
		}
	}
	return dto
}

func skillSetMemberFromDTO(dto *skillSetMemberCorrelationDTO) (*lock.SkillSetMemberCorrelation, error) {
	if dto == nil {
		return nil, nil
	}
	identity, err := desiredskill.ParseSkillSetDeclarationIdentity(dto.DeclarationIdentity)
	if err != nil {
		return nil, err
	}
	correlation, err := lock.NewSkillSetMemberCorrelation(identity)
	if err != nil {
		return nil, err
	}
	return &correlation, nil
}

func skillSetMemberToDTO(correlation lock.SkillSetMemberCorrelation, present bool) *skillSetMemberCorrelationDTO {
	if !present {
		return nil
	}
	return &skillSetMemberCorrelationDTO{DeclarationIdentity: correlation.DeclarationIdentity().String()}
}

func replayCoverageFromDTO(dto replayCoverageDTO) (lock.ReplayCoverage, error) {
	return lock.NewReplayCoverage(
		lock.ReplayClass(dto.Invocation), lock.ReplayClass(dto.Outcome), lock.ReplayClass(dto.Derivation),
		replayExclusionsFromDTO(dto.Exclusions),
	)
}

func replayCoverageToDTO(coverage lock.ReplayCoverage) replayCoverageDTO {
	return replayCoverageDTO{
		Invocation: string(coverage.Invocation()), Outcome: string(coverage.Outcome()), Derivation: string(coverage.Derivation()),
		Exclusions: replayExclusionsToDTO(coverage.Exclusions()),
	}
}

func replayExclusionsFromDTO(exclusions []replayExclusionDTO) []lock.ReplayExclusion {
	if len(exclusions) == 0 {
		return nil
	}
	converted := make([]lock.ReplayExclusion, 0, len(exclusions))
	for _, exclusion := range exclusions {
		converted = append(converted, lock.ReplayExclusion{Component: exclusion.Component, Reason: lock.ReplayExclusionReason(exclusion.Reason)})
	}
	return converted
}

func replayExclusionsToDTO(exclusions []lock.ReplayExclusion) []replayExclusionDTO {
	if len(exclusions) == 0 {
		return nil
	}
	converted := make([]replayExclusionDTO, 0, len(exclusions))
	for _, exclusion := range exclusions {
		converted = append(converted, replayExclusionDTO{Component: exclusion.Component, Reason: string(exclusion.Reason)})
	}
	return converted
}
