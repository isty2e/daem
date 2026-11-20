package lockfile

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	"github.com/isty2e/daem/internal/encoding/toml"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lock"
)

const (
	lockfileVersion6    int64 = 6
	v6TranslationTarget int64 = 7
)

// ErrUnusableV6 reports a recognized schema 6 file whose contents cannot be
// used as a locked plan.
var ErrUnusableV6 = errors.New("cannot use schema 6 lockfile as a locked plan")

func loadV6Content(ctx context.Context, content []byte) (lock.File, error) {
	if err := admitLockfileStructure(ctx, content); err != nil {
		return lock.File{}, v6ReadFailure(ctx, err, "invalid TOML structure")
	}
	legacy, err := decodeLockfileDTO[v6FileDTO](ctx, content)
	if err != nil {
		return lock.File{}, v6ReadFailure(ctx, err, "invalid or unrecognized fields")
	}
	dto, err := legacy.toV7(ctx)
	if err != nil {
		return lock.File{}, v6ReadFailure(ctx, err, "invalid aggregate path")
	}

	contributions, err := currentContributions(ctx)
	if err != nil {
		return lock.File{}, err
	}
	file, err := validatedSnapshotFromDTO(ctx, dto, contributions)
	if err != nil {
		return lock.File{}, v6ReadFailure(ctx, err, "locked values failed validation")
	}
	return file, nil
}

func v6ReadFailure(ctx context.Context, cause error, detail string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) ||
		tomlsyntax.IsStructureLimit(cause) || errors.Is(cause, configdocument.ErrLimitExceeded) ||
		errors.Is(cause, jsonstrict.ErrMaximumDepthExceeded) {
		return cause
	}
	return fmt.Errorf("%w: %s", ErrUnusableV6, detail)
}

func (legacy v6FileDTO) toV7(ctx context.Context) (fileDTO, error) {
	var subjects []lockedSubjectDTO
	if legacy.Locked.Subjects != nil {
		subjects = make([]lockedSubjectDTO, len(legacy.Locked.Subjects))
	}
	for i, row := range legacy.Locked.Subjects {
		if err := ctx.Err(); err != nil {
			return fileDTO{}, err
		}
		subject, err := row.toV7()
		if err != nil {
			return fileDTO{}, err
		}
		subjects[i] = subject
	}

	return fileDTO{
		Version: v6TranslationTarget,
		Locked: lockedSectionDTO{
			Subjects: subjects,
			OrderConstraints: translateV6Rows(legacy.Locked.OrderConstraints, func(row v6LockedOrderConstraintDTO) lockedOrderConstraintDTO {
				return lockedOrderConstraintDTO{
					ClassID: row.ClassID, ContractVersion: row.ContractVersion, RuntimeMeaning: row.RuntimeMeaning,
					Members: translateV6Rows(row.Members, func(member v6LockedOrderMemberDTO) lockedOrderMemberDTO {
						return lockedOrderMemberDTO(member)
					}),
				}
			}),
		},
	}, nil
}

func (row v6LockedSubjectDTO) toV7() (lockedSubjectDTO, error) {
	realization, err := row.Realization.toV7()
	if err != nil {
		return lockedSubjectDTO{}, err
	}

	return lockedSubjectDTO{
		EntityID: row.EntityID, SubjectID: row.SubjectID,
		ExactSupply: (*exactIdentityDTO)(row.ExactSupply), ExactFileUse: (*exactFileUseDTO)(row.ExactFileUse),
		Realization: realization, Derivation: row.Derivation.toV7(), RepairRecipe: row.RepairRecipe.toV7(),
		DelegatePlan: row.DelegatePlan.toV7(), MCPEnvironmentSources: row.MCPEnvironmentSources,
		MCPProviderContribution: row.MCPProviderContribution, SkillSetMember: (*skillSetMemberCorrelationDTO)(row.SkillSetMember),
		Ownership: row.Ownership, OnAbsent: row.OnAbsent,
		Replay: replayCoverageDTO{
			Invocation: row.Replay.Invocation, Outcome: row.Replay.Outcome, Derivation: row.Replay.Derivation,
			Exclusions: translateV6Rows(row.Replay.Exclusions, func(exclusion v6ReplayExclusionDTO) replayExclusionDTO {
				return replayExclusionDTO(exclusion)
			}),
		},
		Operations: translateV6Rows(row.Operations, func(operation v6OperationContractDTO) operationContractDTO {
			return operationContractDTO(operation)
		}),
	}, nil
}

func (row *v6RealizationDTO) toV7() (*realizationDTO, error) {
	if row == nil {
		return nil, nil
	}
	result := realizationDTO{
		ManagedPath:       (*managedPathProjectionDTO)(row.ManagedPath),
		DelegatedRelation: (*delegatedRelationDTO)(row.DelegatedRelation),
	}
	if old := row.ManagedAggregate; old != nil {
		path, err := aggregate.ParseContentPath(old.ContentPath)
		if err != nil {
			return nil, err
		}
		footprint, err := output.FootprintFromLegacy(output.ContentPath(path))
		if err != nil {
			return nil, err
		}
		key, _ := footprint.Key() // ParseContentPath requires a nonempty projection.
		result.ManagedAggregate = &managedAggregateContributionDTO{
			PlacementID: old.PlacementID, Target: old.Target, Scope: old.Scope, AggregateRoot: old.AggregateRoot,
			KeyPath: key.String(), MergeUnit: old.MergeUnit, ContributionCardinality: old.ContributionCardinality,
			SiblingRetention: old.SiblingRetention, SiblingPreservation: old.SiblingPreservation,
			Equivalence: old.Equivalence, CanonicalContribution: old.CanonicalContribution,
			CodecContract: old.CodecContract, ComparedFields: old.ComparedFields,
		}
	}
	return &result, nil
}

func (row *v6DerivationDTO) toV7() *derivationDTO {
	if row == nil {
		return nil
	}
	result := derivationDTO{DirectResolution: (*exactIdentityDTO)(row.DirectResolution)}
	if transform := row.DeterministicTransform; transform != nil {
		result.DeterministicTransform = &deterministicTransformDTO{
			InputIdentity: exactIdentityDTO(transform.InputIdentity), RecipeHash: transform.RecipeHash,
			AlgorithmID: transform.AlgorithmID, AlgorithmVersion: transform.AlgorithmVersion,
			ExecutionDomain: transform.ExecutionDomain, ExpectedOutputIdentity: exactIdentityDTO(transform.ExpectedOutputIdentity),
		}
	}
	return &result
}

func (row *v6RepairRecipeDTO) toV7() *repairRecipeDTO {
	if row == nil {
		return nil
	}
	return &repairRecipeDTO{
		Version: row.Version, RecipeHash: row.RecipeHash,
		Input: exactIdentityDTO(row.Input), Output: exactIdentityDTO(row.Output),
		Operations: translateV6Rows(row.Operations, func(operation v6RepairOperationDTO) repairOperationDTO {
			return repairOperationDTO(operation)
		}),
	}
}

func (row *v6DelegatePlanDTO) toV7() *delegatePlanDTO {
	if row == nil {
		return nil
	}
	return &delegatePlanDTO{
		IdentityKey: row.IdentityKey, RunnerKind: row.RunnerKind, Command: row.Command, Args: row.Args,
		PinPolicy: row.PinPolicy,
		Env: translateV6Rows(row.Env, func(reference v6DelegateEnvDTO) delegateEnvDTO {
			return delegateEnvDTO(reference)
		}),
		Packages: translateV6Rows(row.Packages, func(pkg v6DelegatePackageDTO) delegatePackageDTO {
			return delegatePackageDTO(pkg)
		}),
	}
}

func translateV6Rows[Legacy, Current any](rows []Legacy, translate func(Legacy) Current) []Current {
	// Canonicality distinguishes omitted lists from explicitly empty ones.
	if rows == nil {
		return nil
	}
	result := make([]Current, len(rows))
	for i, row := range rows {
		result[i] = translate(row)
	}
	return result
}
