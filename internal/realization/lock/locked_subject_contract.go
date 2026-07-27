package lock

import (
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/supply/artifact"
	skillrepair "github.com/isty2e/daem/internal/supply/compat/skill/repair"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

// OwnershipBasis records why daem owns the locked subject.
type OwnershipBasis string

const (
	OwnershipManifest OwnershipBasis = "manifest"
	OwnershipAdopted  OwnershipBasis = "adopted"
)

// OnAbsentPolicy records the locked policy when the subject is absent.
type OnAbsentPolicy string

const (
	OnAbsentApply         OnAbsentPolicy = "apply"
	OnAbsentBlock         OnAbsentPolicy = "block"
	OnAbsentObserveOnly   OnAbsentPolicy = "observe_only"
	OnAbsentRemoveBinding OnAbsentPolicy = "remove_binding"
)

// SkillSetMemberCorrelation identifies the selector-backed declaration that
// produced one exact-Supply Skill contract. Child identity remains on the
// owning locked subject contract rather than being duplicated here.
type SkillSetMemberCorrelation struct {
	declarationIdentity skill.SkillSetDeclarationIdentity
}

// ExactFileUse records durable use intent for an exact file Supply without
// claiming a target path or current file mode.
type ExactFileUse struct {
	scope      target.Scope
	executable bool
}

// NewExactFileUse constructs exact file use intent.
func NewExactFileUse(scope target.Scope, executable bool) (ExactFileUse, error) {
	parsedScope, err := target.ParseScope(string(scope))
	if err != nil {
		return ExactFileUse{}, err
	}
	return ExactFileUse{scope: parsedScope, executable: executable}, nil
}

// Scope returns the desired locality of the exact file use.
func (use ExactFileUse) Scope() target.Scope { return use.scope }

// Executable reports whether the projected file must be executable.
func (use ExactFileUse) Executable() bool { return use.executable }

// Equal reports whether two exact file uses contain the same durable intent.
func (use ExactFileUse) Equal(other ExactFileUse) bool {
	return use.scope == other.scope && use.executable == other.executable
}

// NewSkillSetMemberCorrelation constructs contextual selector-member correlation.
func NewSkillSetMemberCorrelation(
	declarationIdentity skill.SkillSetDeclarationIdentity,
) (SkillSetMemberCorrelation, error) {
	if err := declarationIdentity.Validate(); err != nil {
		return SkillSetMemberCorrelation{}, err
	}
	return SkillSetMemberCorrelation{declarationIdentity: declarationIdentity}, nil
}

// DeclarationIdentity returns the canonical selector-backed declaration identity.
func (correlation SkillSetMemberCorrelation) DeclarationIdentity() skill.SkillSetDeclarationIdentity {
	return correlation.declarationIdentity
}

// Equal reports whether two correlations name the same declaration.
func (correlation SkillSetMemberCorrelation) Equal(other SkillSetMemberCorrelation) bool {
	return correlation.declarationIdentity.Equal(other.declarationIdentity)
}

// LockedSubjectContractInput carries orthogonal durable facts for one lowered subject.
type LockedSubjectContractInput struct {
	EntityID                  entity.ID
	SubjectID                 topology.SubjectID
	ExactSupply               *artifact.ExactIdentity
	ExactFileUse              *ExactFileUse
	Realization               *realization.RealizationSpec
	Derivation                *DerivationContract
	RepairRecipe              *skillrepair.Recipe
	DelegatePlan              *realizationdelegate.DelegatePlan
	SkillSetMemberCorrelation *SkillSetMemberCorrelation
	Ownership                 OwnershipBasis
	OnAbsent                  OnAbsentPolicy
	Replay                    ReplayCoverage
	OperationContracts        []OperationContract
}

// LockedSubjectContract is canonical locked desired state for one lowered subject.
// Exact Supply and structural realization are independent optional facets; at
// least one must be present.
type LockedSubjectContract struct {
	entityID                  entity.ID
	subjectID                 topology.SubjectID
	exactSupply               *artifact.ExactIdentity
	exactFileUse              *ExactFileUse
	realization               *realization.RealizationSpec
	derivation                *DerivationContract
	repairRecipe              *skillrepair.Recipe
	delegatePlan              *realizationdelegate.DelegatePlan
	skillSetMemberCorrelation *SkillSetMemberCorrelation
	ownership                 OwnershipBasis
	onAbsent                  OnAbsentPolicy
	replay                    ReplayCoverage
	operationContracts        map[OperationKind]OperationContract
}

// NewLockedSubjectContract validates and defensively copies one locked subject contract.
func NewLockedSubjectContract(input LockedSubjectContractInput) (LockedSubjectContract, error) {
	contract := LockedSubjectContract{
		entityID:           input.EntityID,
		subjectID:          input.SubjectID,
		ownership:          input.Ownership,
		onAbsent:           input.OnAbsent,
		replay:             cloneReplayCoverage(input.Replay),
		operationContracts: make(map[OperationKind]OperationContract, len(input.OperationContracts)),
	}
	if input.ExactSupply != nil {
		identity := *cloneExactArtifactIdentity(*input.ExactSupply)
		contract.exactSupply = &identity
	}
	if input.ExactFileUse != nil {
		use, err := NewExactFileUse(input.ExactFileUse.scope, input.ExactFileUse.executable)
		if err != nil {
			return LockedSubjectContract{}, err
		}
		contract.exactFileUse = &use
	}
	if input.Realization != nil {
		realization := *input.Realization
		contract.realization = &realization
	}
	if input.Derivation != nil {
		derivation := cloneDerivationContract(*input.Derivation)
		contract.derivation = &derivation
	}
	if input.RepairRecipe != nil {
		recipe, err := cloneSkillRepairRecipe(*input.RepairRecipe)
		if err != nil {
			return LockedSubjectContract{}, err
		}
		contract.repairRecipe = &recipe
	}
	if input.DelegatePlan != nil {
		if err := input.DelegatePlan.Validate(); err != nil {
			return LockedSubjectContract{}, err
		}
		plan := *input.DelegatePlan
		contract.delegatePlan = &plan
	}
	if input.SkillSetMemberCorrelation != nil {
		correlation, err := NewSkillSetMemberCorrelation(input.SkillSetMemberCorrelation.declarationIdentity)
		if err != nil {
			return LockedSubjectContract{}, err
		}
		contract.skillSetMemberCorrelation = &correlation
	}
	for _, operationContract := range input.OperationContracts {
		cloned := cloneOperationContract(operationContract)
		if _, exists := contract.operationContracts[cloned.operation]; exists {
			return LockedSubjectContract{}, fmt.Errorf("duplicate operation contract %q", cloned.operation)
		}
		contract.operationContracts[cloned.operation] = cloned
	}
	if err := contract.validate(); err != nil {
		return LockedSubjectContract{}, err
	}
	return contract, nil
}

// EntityID returns the canonical Desired entity associated by lowering.
func (contract LockedSubjectContract) EntityID() entity.ID { return contract.entityID }

// SubjectID returns the canonical Topology subject identity.
func (contract LockedSubjectContract) SubjectID() topology.SubjectID { return contract.subjectID }

// ExactSupply returns the exact Supply identity when present.
func (contract LockedSubjectContract) ExactSupply() (artifact.ExactIdentity, bool) {
	if contract.exactSupply == nil {
		return artifact.ExactIdentity{}, false
	}
	return *cloneExactArtifactIdentity(*contract.exactSupply), true
}

// ExactFileUse returns durable exact-file use intent when present.
func (contract LockedSubjectContract) ExactFileUse() (ExactFileUse, bool) {
	if contract.exactFileUse == nil {
		return ExactFileUse{}, false
	}
	return *contract.exactFileUse, true
}

// MaterializedFileIdentity returns the exact output identity selected by the
// contract's file-use intent and deterministic derivation.
func (contract LockedSubjectContract) MaterializedFileIdentity() (artifact.ExactIdentity, bool) {
	if contract.exactSupply == nil || contract.exactFileUse == nil || contract.derivation == nil {
		return artifact.ExactIdentity{}, false
	}
	if identity, direct := contract.derivation.DirectResolution(); direct {
		return identity, true
	}
	if transform, deterministic := contract.derivation.DeterministicTransform(); deterministic {
		return transform.ExpectedOutputIdentity, true
	}
	return artifact.ExactIdentity{}, false
}

// Realization returns the closed structural realization when present.
func (contract LockedSubjectContract) Realization() (realization.RealizationSpec, bool) {
	if contract.realization == nil {
		return realization.RealizationSpec{}, false
	}
	return *contract.realization, true
}

// ManagedAggregateContribution returns the subject-correlated aggregate
// realization when this contract owns one.
func (contract LockedSubjectContract) ManagedAggregateContribution() (
	aggregate.SubjectContribution,
	bool,
	error,
) {
	if contract.realization == nil ||
		contract.realization.Kind() != realization.RealizationManagedAggregateContribution {
		return aggregate.SubjectContribution{}, false, nil
	}
	contribution, ok := contract.realization.ManagedAggregateContribution()
	if !ok {
		return aggregate.SubjectContribution{}, true, fmt.Errorf("managed aggregate realization is malformed")
	}
	item, err := aggregate.NewSubjectContribution(contract.subjectID, contribution)
	if err != nil {
		return aggregate.SubjectContribution{}, true, err
	}
	return item, true, nil
}

// Derivation returns the exact Supply derivation when present.
func (contract LockedSubjectContract) Derivation() (DerivationContract, bool) {
	if contract.derivation == nil {
		return DerivationContract{}, false
	}
	return cloneDerivationContract(*contract.derivation), true
}

// RepairRecipe returns the canonical mechanical repair recipe when present.
func (contract LockedSubjectContract) RepairRecipe() (skillrepair.Recipe, bool) {
	if contract.repairRecipe == nil {
		return skillrepair.Recipe{}, false
	}
	return *contract.repairRecipe, true
}

// DelegatePlan returns the locked launcher plan when present.
func (contract LockedSubjectContract) DelegatePlan() (realizationdelegate.DelegatePlan, bool) {
	if contract.delegatePlan == nil {
		return realizationdelegate.DelegatePlan{}, false
	}
	return *contract.delegatePlan, true
}

// SkillSetMemberCorrelation returns selector-member correlation when present.
func (contract LockedSubjectContract) SkillSetMemberCorrelation() (SkillSetMemberCorrelation, bool) {
	if contract.skillSetMemberCorrelation == nil {
		return SkillSetMemberCorrelation{}, false
	}
	return *contract.skillSetMemberCorrelation, true
}

// Ownership returns why daem owns this locked subject.
func (contract LockedSubjectContract) Ownership() OwnershipBasis { return contract.ownership }

// OnAbsent returns the locked absence policy.
func (contract LockedSubjectContract) OnAbsent() OnAbsentPolicy { return contract.onAbsent }

// ReplayCoverage returns the durable replay classification.
func (contract LockedSubjectContract) ReplayCoverage() ReplayCoverage {
	return cloneReplayCoverage(contract.replay)
}

// OperationContract returns one exact operation contract.
func (contract LockedSubjectContract) OperationContract(kind OperationKind) (OperationContract, bool) {
	operationContract, ok := contract.operationContracts[kind]
	if !ok {
		return OperationContract{}, false
	}
	return cloneOperationContract(operationContract), true
}

// OperationKinds returns stable operation keys for this subject.
func (contract LockedSubjectContract) OperationKinds() []OperationKind {
	kinds := make([]OperationKind, 0, len(contract.operationContracts))
	for kind := range contract.operationContracts {
		kinds = append(kinds, kind)
	}
	slices.Sort(kinds)
	return kinds
}

// CompareIdentity orders contracts by Desired entity and then Topology subject.
func (contract LockedSubjectContract) CompareIdentity(other LockedSubjectContract) int {
	if order := entity.Compare(contract.entityID, other.entityID); order != 0 {
		return order
	}
	return topology.CompareSubjectID(contract.subjectID, other.subjectID)
}

// Equal reports whether every durable locked fact is equal.
func (contract LockedSubjectContract) Equal(other LockedSubjectContract) bool {
	if contract.validate() != nil || other.validate() != nil || contract.CompareIdentity(other) != 0 {
		return false
	}
	return optionalExactIdentityEqual(contract.exactSupply, other.exactSupply) &&
		optionalExactFileUseEqual(contract.exactFileUse, other.exactFileUse) &&
		optionalRealizationEqual(contract.realization, other.realization) &&
		optionalDerivationEqual(contract.derivation, other.derivation) &&
		optionalSkillRepairRecipeEqual(contract.repairRecipe, other.repairRecipe) &&
		optionalDelegatePlanEqual(contract.delegatePlan, other.delegatePlan) &&
		optionalSkillSetCorrelationEqual(contract.skillSetMemberCorrelation, other.skillSetMemberCorrelation) &&
		contract.ownership == other.ownership &&
		contract.onAbsent == other.onAbsent &&
		replayCoverageEqual(contract.replay, other.replay) &&
		operationContractMapsEqual(contract.operationContracts, other.operationContracts)
}

func optionalExactFileUseEqual(left *ExactFileUse, right *ExactFileUse) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func optionalExactIdentityEqual(left *artifact.ExactIdentity, right *artifact.ExactIdentity) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func optionalRealizationEqual(left *realization.RealizationSpec, right *realization.RealizationSpec) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func optionalDerivationEqual(left *DerivationContract, right *DerivationContract) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	if left.kind != right.kind {
		return false
	}
	switch left.kind {
	case DerivationDirectResolution:
		return optionalExactIdentityEqual(left.directResolution, right.directResolution)
	case DerivationDeterministicTransform:
		if left.deterministicTransform == nil || right.deterministicTransform == nil {
			return left.deterministicTransform == nil && right.deterministicTransform == nil
		}
		return deterministicTransformsEqual(*left.deterministicTransform, *right.deterministicTransform)
	default:
		return false
	}
}

func optionalSkillRepairRecipeEqual(left *skillrepair.Recipe, right *skillrepair.Recipe) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func cloneSkillRepairRecipe(recipe skillrepair.Recipe) (skillrepair.Recipe, error) {
	return skillrepair.NewRecipe(recipe.Input(), recipe.Output(), recipe.Operations())
}

func deterministicTransformsEqual(left DeterministicTransform, right DeterministicTransform) bool {
	return left.InputIdentity.Equal(right.InputIdentity) &&
		left.RecipeHash == right.RecipeHash &&
		left.AlgorithmID == right.AlgorithmID &&
		left.AlgorithmVersion == right.AlgorithmVersion &&
		left.ExecutionDomain == right.ExecutionDomain &&
		left.ExpectedOutputIdentity.Equal(right.ExpectedOutputIdentity)
}

func optionalDelegatePlanEqual(
	left *realizationdelegate.DelegatePlan,
	right *realizationdelegate.DelegatePlan,
) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func optionalSkillSetCorrelationEqual(left *SkillSetMemberCorrelation, right *SkillSetMemberCorrelation) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func replayCoverageEqual(left ReplayCoverage, right ReplayCoverage) bool {
	if left.invocation != right.invocation || left.outcome != right.outcome || left.derivation != right.derivation ||
		len(left.exclusions) != len(right.exclusions) {
		return false
	}
	for index := range left.exclusions {
		if left.exclusions[index] != right.exclusions[index] {
			return false
		}
	}
	return true
}

func operationContractMapsEqual(left map[OperationKind]OperationContract, right map[OperationKind]OperationContract) bool {
	if len(left) != len(right) {
		return false
	}
	for kind, leftContract := range left {
		rightContract, ok := right[kind]
		if !ok || !operationContractsEqual(leftContract, rightContract) {
			return false
		}
	}
	return true
}

func operationContractsEqual(left OperationContract, right OperationContract) bool {
	return left.operation == right.operation &&
		left.actuation == right.actuation &&
		left.authority == right.authority &&
		left.route == right.route &&
		left.hostCompatibility == right.hostCompatibility &&
		slices.Equal(left.preconditions, right.preconditions) &&
		left.effectEnvelope == right.effectEnvelope &&
		left.effectPostconditions.Equal(right.effectPostconditions) &&
		left.idempotency == right.idempotency &&
		left.verification == right.verification &&
		left.trustActivation == right.trustActivation &&
		left.recovery == right.recovery
}

func validateOwnershipBasis(value OwnershipBasis) error {
	switch value {
	case OwnershipManifest, OwnershipAdopted:
		return nil
	default:
		return fmt.Errorf("ownership basis %q is unsupported", value)
	}
}

func validateOnAbsentPolicy(value OnAbsentPolicy) error {
	switch value {
	case OnAbsentApply, OnAbsentBlock, OnAbsentObserveOnly, OnAbsentRemoveBinding:
		return nil
	default:
		return fmt.Errorf("on-absent policy %q is unsupported", value)
	}
}
