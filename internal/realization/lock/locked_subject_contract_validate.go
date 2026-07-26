package lock

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/supply/artifact"
	skillrepair "github.com/isty2e/daem/internal/supply/compat/skill/repair"
	"github.com/isty2e/daem/internal/topology"
	resourcetopology "github.com/isty2e/daem/internal/topology/resource"
)

func (contract LockedSubjectContract) validate() error {
	if err := contract.entityID.Validate(); err != nil {
		return fmt.Errorf("locked subject entity id: %w", err)
	}
	if err := contract.subjectID.Validate(); err != nil {
		return fmt.Errorf("locked subject topology id: %w", err)
	}
	if contract.exactSupply == nil && contract.realization == nil {
		return fmt.Errorf("locked subject contract requires exact Supply, realization, or both")
	}
	if contract.exactSupply != nil {
		if err := contract.exactSupply.Validate(); err != nil {
			return fmt.Errorf("locked subject exact Supply: %w", err)
		}
	}
	if contract.realization != nil {
		if err := contract.realization.Validate(); err != nil {
			return fmt.Errorf("locked subject realization: %w", err)
		}
	}
	if err := contract.validateExactFileUse(); err != nil {
		return err
	}
	if err := contract.validateSubjectFacets(); err != nil {
		return err
	}
	if contract.derivation != nil {
		if err := contract.derivation.validate(); err != nil {
			return err
		}
		if contract.exactSupply == nil {
			return fmt.Errorf("locked subject derivation requires exact Supply identity")
		}
		if err := validateExactArtifactDerivationMatchesSupply(
			*contract.derivation,
			*contract.exactSupply,
			contract.exactFileUse != nil,
		); err != nil {
			return err
		}
	}
	if err := contract.validateRepairRecipe(); err != nil {
		return err
	}
	if contract.delegatePlanIdentity != nil {
		if _, err := NewDelegatePlanIdentity(*contract.delegatePlanIdentity); err != nil {
			return err
		}
		if contract.realization == nil {
			return fmt.Errorf("delegate plan identity requires managed aggregate realization")
		}
		if _, ok := contract.realization.ManagedAggregateContribution(); !ok {
			return fmt.Errorf("delegate plan identity requires managed aggregate realization")
		}
	}
	if contract.skillSetMemberCorrelation != nil {
		if err := contract.validateSkillSetMemberCorrelation(); err != nil {
			return err
		}
	}
	if err := validateOwnershipBasis(contract.ownership); err != nil {
		return err
	}
	if err := validateOnAbsentPolicy(contract.onAbsent); err != nil {
		return err
	}
	if err := contract.replay.validate(); err != nil {
		return err
	}
	if contract.realization != nil && contract.realization.Kind() == realization.RealizationDelegatedRelation && contract.replay.OutcomeReplayable() {
		return fmt.Errorf("delegated relation lock must not be outcome replayable")
	}
	if len(contract.operationContracts) == 0 {
		return fmt.Errorf("locked subject %s requires at least one operation contract", contract.subjectID)
	}
	for _, operationKind := range contract.OperationKinds() {
		operationContract := contract.operationContracts[operationKind]
		if err := operationContract.validate(); err != nil {
			return err
		}
		if err := contract.validateOperationCompatibility(operationContract); err != nil {
			return err
		}
	}
	return nil
}

func (contract LockedSubjectContract) validateExactFileUse() error {
	if contract.exactFileUse == nil {
		return nil
	}
	if _, err := NewExactFileUse(contract.exactFileUse.scope, contract.exactFileUse.executable); err != nil {
		return fmt.Errorf("locked subject exact file use: %w", err)
	}
	if contract.exactSupply == nil || contract.exactSupply.Kind() != artifact.ArtifactKindFile {
		return fmt.Errorf("exact file use requires exact file Supply identity")
	}
	if contract.realization == nil {
		if contract.subjectID.Kind() != topology.SubjectResource {
			return fmt.Errorf("exact file use without realization requires resource subject")
		}
		return nil
	}
	projection, ok := contract.realization.ManagedPathProjection()
	if !ok || projection.ContentKind() != realization.PathProjectionFile {
		return fmt.Errorf("exact file use requires managed file path realization when realized")
	}
	if projection.Scope() != contract.exactFileUse.scope {
		return fmt.Errorf(
			"exact file use scope %q does not match managed path scope %q",
			contract.exactFileUse.scope,
			projection.Scope(),
		)
	}
	return nil
}

// ValidateFileMaterialization checks one observed exact file derivation against
// the contract's exact Supply, file-use intent, and derivation reference.
func (contract LockedSubjectContract) ValidateFileMaterialization(
	materialization artifact.FileMaterialization,
) error {
	if contract.exactSupply == nil || contract.exactFileUse == nil || contract.derivation == nil {
		return fmt.Errorf("file materialization requires exact Supply, exact file use, and derivation")
	}
	if contract.exactFileUse.executable != materialization.Executable() {
		return fmt.Errorf("file materialization executable intent does not match exact file use")
	}
	if !contract.exactSupply.Equal(materialization.InputIdentity()) {
		return fmt.Errorf("file materialization input does not match exact Supply identity")
	}
	if !materialization.ChangesIdentity() {
		identity, direct := contract.derivation.DirectResolution()
		if !direct || !identity.Equal(materialization.InputIdentity()) {
			return fmt.Errorf("unchanged file materialization requires matching direct resolution")
		}
		return nil
	}

	transform, deterministic := contract.derivation.DeterministicTransform()
	if !deterministic ||
		!transform.InputIdentity.Equal(materialization.InputIdentity()) ||
		!transform.ExpectedOutputIdentity.Equal(materialization.OutputIdentity()) ||
		transform.RecipeHash != materialization.RecipeHash() ||
		transform.AlgorithmID != artifact.FileMaterializationAlgorithmID ||
		transform.AlgorithmVersion != artifact.FileMaterializationAlgorithmVersion ||
		transform.ExecutionDomain != artifact.FileMaterializationExecutionDomain {
		return fmt.Errorf("file materialization does not match deterministic derivation")
	}
	return nil
}

func (contract LockedSubjectContract) validateRepairRecipe() error {
	if contract.repairRecipe == nil {
		return nil
	}
	if err := contract.repairRecipe.Validate(); err != nil {
		return fmt.Errorf("locked subject repair recipe: %w", err)
	}
	if contract.exactSupply == nil || contract.derivation == nil {
		return fmt.Errorf("repair recipe requires exact Supply and deterministic derivation")
	}
	if contract.entityID.Kind() != entity.KindSkill {
		return fmt.Errorf("repair recipe requires Skill entity")
	}
	transform, ok := contract.derivation.DeterministicTransform()
	if !ok {
		return fmt.Errorf("repair recipe requires deterministic derivation")
	}
	if !contract.repairRecipe.Input().Equal(transform.InputIdentity) ||
		!contract.repairRecipe.Output().Equal(transform.ExpectedOutputIdentity) ||
		contract.repairRecipe.Hash() != transform.RecipeHash {
		return fmt.Errorf("repair recipe does not match deterministic derivation")
	}
	if transform.AlgorithmID != skillrepair.DerivationAlgorithmID ||
		transform.AlgorithmVersion != fmt.Sprintf("v%d", contract.repairRecipe.Version()) ||
		transform.ExecutionDomain != skillrepair.DerivationExecutionDomain {
		return fmt.Errorf("repair recipe does not match skill repair execution contract")
	}
	if contract.replay.Derivation() != ReplayExact {
		return fmt.Errorf("repair recipe requires exact derivation replay coverage")
	}
	return nil
}

func (contract LockedSubjectContract) validateSubjectFacets() error {
	switch contract.subjectID.Kind() {
	case topology.SubjectResource:
		if contract.exactSupply == nil || contract.realization != nil {
			return fmt.Errorf("resource subject requires exact Supply and no realization")
		}
		expected, err := resourcetopology.Subject(contract.entityID)
		if err != nil {
			return err
		}
		if contract.subjectID != expected {
			return fmt.Errorf(
				"resource subject %q does not match entity %q canonical subject %q",
				contract.subjectID,
				contract.entityID,
				expected,
			)
		}
	case topology.SubjectProjection:
		if contract.realization == nil {
			return fmt.Errorf("projection subject requires a realization")
		}
		switch contract.realization.Kind() {
		case realization.RealizationManagedPathProjection:
			if contract.exactSupply != nil {
				return fmt.Errorf("managed path projection must correlate exact Supply by entity instead of duplicating it")
			}
		case realization.RealizationManagedAggregateContribution:
		default:
			return fmt.Errorf("projection subject requires managed path or aggregate realization")
		}
	case topology.SubjectHostRelation:
		if contract.exactSupply != nil || contract.realization == nil {
			return fmt.Errorf("host relation subject requires delegated realization and no exact Supply")
		}
		if _, ok := contract.realization.DelegatedRelation(); !ok {
			return fmt.Errorf("host relation subject requires delegated realization")
		}
	default:
		return fmt.Errorf("locked subject kind %q is unsupported", contract.subjectID.Kind())
	}
	return nil
}

func (contract LockedSubjectContract) validateSkillSetMemberCorrelation() error {
	if err := contract.skillSetMemberCorrelation.declarationIdentity.Validate(); err != nil {
		return err
	}
	if contract.entityID.Kind() != entity.KindSkill {
		return fmt.Errorf("SkillSet member correlation requires Skill entity")
	}
	if contract.subjectID.Kind() != topology.SubjectResource || contract.exactSupply == nil || contract.realization != nil {
		return fmt.Errorf("SkillSet member correlation requires exact-Supply artifact subject")
	}
	return nil
}

func (contract LockedSubjectContract) validateOperationCompatibility(operationContract OperationContract) error {
	if contract.realization == nil {
		if operationContract.actuation == ActuationDelegatedHostRoute {
			return fmt.Errorf("Supply-only subject operation %q must not use delegated host route", operationContract.operation)
		}
		return nil
	}

	if contract.realization.Kind() == realization.RealizationDelegatedRelation {
		relation, ok := contract.realization.DelegatedRelation()
		if !ok {
			return fmt.Errorf("delegated relation realization is malformed")
		}
		if operationContract.actuation == ActuationDirectProjection &&
			(operationContract.operation != OperationRemove ||
				operationContract.authority != AuthorityRemove) {
			return fmt.Errorf(
				"delegated relation operation %q may use direct projection only for removal",
				operationContract.operation,
			)
		}
		if operationContract.actuation == ActuationNoMutation || operationContract.operation != OperationInstall {
			return nil
		}
		if operationContract.route.AdapterContractVersion != relation.RouteContractVersion() {
			return fmt.Errorf(
				"operation %q adapter contract %q does not match delegated route contract %q",
				operationContract.operation,
				operationContract.route.AdapterContractVersion,
				relation.RouteContractVersion(),
			)
		}
		if operationContract.route.RouteID != relation.RouteID() {
			return fmt.Errorf(
				"delegated operation %q route %q does not match realization route %q",
				operationContract.operation,
				operationContract.route.RouteID,
				relation.RouteID(),
			)
		}
		return nil
	}

	contractVersion, contractLabel, err := realizationOperationContractVersion(*contract.realization)
	if err != nil {
		return err
	}
	if operationContract.actuation != ActuationNoMutation && operationContract.route.AdapterContractVersion != contractVersion {
		return fmt.Errorf(
			"operation %q adapter contract %q does not match %s %q",
			operationContract.operation,
			operationContract.route.AdapterContractVersion,
			contractLabel,
			contractVersion,
		)
	}
	if operationContract.actuation == ActuationDelegatedHostRoute {
		return fmt.Errorf("managed projection operation %q must not use delegated host route", operationContract.operation)
	}
	if contract.subjectID.Kind() == topology.SubjectProjection {
		switch operationContract.operation {
		case OperationObserve, OperationWriteProjection, OperationRemoveProjection:
		default:
			if operationContract.OrdinaryMutationEligible() {
				return fmt.Errorf("projection subject operation %q must not be ordinary mutation eligible", operationContract.operation)
			}
		}
	}
	return nil
}

func realizationOperationContractVersion(spec realization.RealizationSpec) (string, string, error) {
	switch spec.Kind() {
	case realization.RealizationManagedPathProjection:
		projection, ok := spec.ManagedPathProjection()
		if !ok {
			return "", "", fmt.Errorf("managed path realization is malformed")
		}
		return projection.AdapterContractVersion(), "managed path adapter contract", nil
	case realization.RealizationManagedAggregateContribution:
		contribution, ok := spec.ManagedAggregateContribution()
		if !ok {
			return "", "", fmt.Errorf("managed aggregate realization is malformed")
		}
		return string(contribution.CodecContractID()), "managed aggregate codec contract", nil
	case realization.RealizationDelegatedRelation:
		relation, ok := spec.DelegatedRelation()
		if !ok {
			return "", "", fmt.Errorf("delegated relation realization is malformed")
		}
		return relation.RouteContractVersion(), "delegated route contract", nil
	default:
		return "", "", fmt.Errorf("realization kind %q has no operation contract", spec.Kind())
	}
}
