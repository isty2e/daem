package lock

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/supply/artifact"
	skillrepair "github.com/isty2e/daem/internal/supply/compat/skill/repair"
	"github.com/isty2e/daem/internal/topology"
)

const exactSupplyAdapterContractVersion = "exact-supply.v1"

func defaultExactArtifactOperationContracts() ([]OperationContract, error) {
	write, err := NewOperationContract(OperationContractInput{
		Operation: OperationWriteProjection,
		Actuation: ActuationDirectProjection,
		Authority: AuthorityManage,
		Route: RouteContractRef{
			RouteID:                "exact-supply.write",
			AdapterContractVersion: exactSupplyAdapterContractVersion,
		},
		Preconditions:   []string{"content_hash_locked", "source_identity_locked"},
		EffectEnvelope:  EffectEnvelopeComplete,
		Idempotency:     Idempotent,
		Verification:    VerificationExactArtifact,
		TrustActivation: TrustActivationNotRequired,
		Recovery:        OperationRecoveryAtomic,
	})
	if err != nil {
		return nil, err
	}
	observe, err := NewOperationContract(OperationContractInput{
		Operation:       OperationObserve,
		Actuation:       ActuationNoMutation,
		Authority:       AuthorityObserve,
		EffectEnvelope:  EffectEnvelopeNotApplicable,
		Idempotency:     IdempotencyNotApplicable,
		Verification:    VerificationExactArtifact,
		TrustActivation: TrustActivationNotRequired,
		Recovery:        OperationRecoveryNotApplicable,
	})
	if err != nil {
		return nil, err
	}
	return []OperationContract{write, observe}, nil
}

// ExactSupplySubjectInput carries exact Supply facts already produced by their
// owners. RepairRecipe is present only for a Skill exact supply derived by
// canonical compatibility repair; direct and file-materialization derivations
// omit it.
type ExactSupplySubjectInput struct {
	EntityID                  entity.ID
	SubjectID                 topology.SubjectID
	ExactSupply               artifact.ExactIdentity
	ExactFileUse              *ExactFileUse
	Derivation                DerivationContract
	RepairRecipe              *skillrepair.Recipe
	SkillSetMemberCorrelation *SkillSetMemberCorrelation
}

type exactSupplyFamilyShape struct {
	kind                 entity.Kind
	label                string
	artifactKind         artifact.ArtifactKind
	requiresExactFileUse bool
}

// NewExactSupplySubjectContract constructs one canonical Supply-only resource subject.
func NewExactSupplySubjectContract(input ExactSupplySubjectInput) (LockedSubjectContract, error) {
	replay, err := exactSupplyReplayCoverage(input.Derivation)
	if err != nil {
		return LockedSubjectContract{}, err
	}
	operations, err := defaultExactArtifactOperationContracts()
	if err != nil {
		return LockedSubjectContract{}, err
	}
	contract, err := NewLockedSubjectContract(LockedSubjectContractInput{
		EntityID:                  input.EntityID,
		SubjectID:                 input.SubjectID,
		ExactSupply:               &input.ExactSupply,
		ExactFileUse:              input.ExactFileUse,
		Derivation:                &input.Derivation,
		RepairRecipe:              input.RepairRecipe,
		SkillSetMemberCorrelation: input.SkillSetMemberCorrelation,
		Ownership:                 OwnershipManifest,
		OnAbsent:                  OnAbsentApply,
		Replay:                    replay,
		OperationContracts:        operations,
	})
	if err != nil {
		return LockedSubjectContract{}, fmt.Errorf("exact Supply subject %q: %w", input.SubjectID, err)
	}
	return contract, nil
}

func exactSupplyReplayCoverage(derivation DerivationContract) (ReplayCoverage, error) {
	exclusions := []ReplayExclusion{
		{Component: "source resolver invocation", Reason: ReplayExclusionRuntimeDependency},
	}
	switch derivation.Kind() {
	case DerivationDeterministicTransform:
		return NewReplayCoverage(ReplayUnavailable, ReplayExact, ReplayExact, exclusions)
	case DerivationDirectResolution:
		return NewReplayCoverage(ReplayUnavailable, ReplayExact, ReplayNotApplicable, exclusions)
	default:
		return ReplayCoverage{}, fmt.Errorf("exact Supply derivation kind %q is unsupported", derivation.Kind())
	}
}

func validateAdmittedExactSupplySubject(contract LockedSubjectContract) error {
	for _, shape := range [...]exactSupplyFamilyShape{
		{kind: entity.KindSkill, label: "Skill", artifactKind: artifact.ArtifactKindDirectory},
		{
			kind:                 entity.KindInstructions,
			label:                "Instructions",
			artifactKind:         artifact.ArtifactKindFile,
			requiresExactFileUse: true,
		},
		{
			kind:                 entity.KindHookAsset,
			label:                "HookAsset",
			artifactKind:         artifact.ArtifactKindFile,
			requiresExactFileUse: true,
		},
	} {
		admitted, err := shape.validate(contract)
		if err != nil {
			return err
		}
		if admitted {
			return nil
		}
	}
	return fmt.Errorf("subject has no current exact Supply family admission")
}

func (shape exactSupplyFamilyShape) validate(contract LockedSubjectContract) (bool, error) {
	if contract.EntityID().Kind() != shape.kind {
		return false, nil
	}
	exactSupply, present := contract.ExactSupply()
	if !present {
		return true, fmt.Errorf("%s exact Supply identity is required", shape.label)
	}
	if exactSupply.Kind() != shape.artifactKind {
		return true, fmt.Errorf("%s exact Supply must be a %s", shape.label, shape.artifactKind)
	}
	exactFileUse, hasExactFileUse := contract.ExactFileUse()
	if hasExactFileUse != shape.requiresExactFileUse {
		if shape.requiresExactFileUse {
			return true, fmt.Errorf("%s exact Supply requires exact file use", shape.label)
		}
		return true, fmt.Errorf("%s exact Supply must not carry exact file use", shape.label)
	}
	derivation, present := contract.Derivation()
	if !present {
		return true, fmt.Errorf("%s exact Supply derivation is required", shape.label)
	}

	input := ExactSupplySubjectInput{
		EntityID:    contract.EntityID(),
		SubjectID:   contract.SubjectID(),
		ExactSupply: exactSupply,
		Derivation:  derivation,
	}
	if hasExactFileUse {
		input.ExactFileUse = &exactFileUse
	}
	if recipe, ok := contract.RepairRecipe(); ok {
		input.RepairRecipe = &recipe
	}
	if correlation, ok := contract.SkillSetMemberCorrelation(); ok {
		input.SkillSetMemberCorrelation = &correlation
	}
	expected, err := NewExactSupplySubjectContract(input)
	if err != nil {
		return true, err
	}
	if !contract.Equal(expected) {
		return true, fmt.Errorf("%s exact Supply contract does not match the admitted family refinement", shape.label)
	}
	return true, nil
}
