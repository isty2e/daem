package build

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/hookasset"
	"github.com/isty2e/daem/internal/desired/instructions"
	"github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
)

// lockAssemblyInput is the lock/build boundary between phase-owned facts and snapshot assembly.
type lockAssemblyInput struct {
	Skills       []skillLockAssemblyInput
	HookAssets   []hookAssetLockAssemblyInput
	Instructions []instructionLockAssemblyInput
}

type skillLockAssemblyInput struct {
	value               skill.Skill
	skillSetDeclaration *skill.SkillSetDeclarationIdentity
	artifact            resolvedArtifactInput
	task                assemblyTaskRef
}

type instructionLockAssemblyInput struct {
	value    instructions.Instructions
	artifact resolvedArtifactInput
	task     assemblyTaskRef
}

type hookAssetLockAssemblyInput struct {
	value    hookasset.HookAsset
	artifact resolvedArtifactInput
	task     assemblyTaskRef
}

type assemblyTaskRef struct {
	taskID          acquisition.RequestID
	stage           EventStage
	ordinal         int
	entityID        entity.ID
	skillGroupIndex *int
}

func newSkillLockAssemblyInputs(
	skills []lockableSkill,
	results []sourceTaskResult,
) ([]skillLockAssemblyInput, error) {
	if len(skills) != len(results) {
		return nil, fmt.Errorf("lock assembly got %d skill candidates for %d resolved skill artifacts", len(skills), len(results))
	}

	inputs := make([]skillLockAssemblyInput, 0, len(skills))
	for index, lockable := range skills {
		resolved, err := resolvedArtifactInputFromSourceTaskResult(results[index])
		if err != nil {
			return nil, err
		}
		input, err := newSkillLockAssemblyInput(lockable, resolved, assemblyTaskRefFromSourceTask(results[index].task))
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, input)
	}
	return inputs, nil
}

func newInstructionLockAssemblyInputs(
	instructionResources []instructions.Instructions,
	results []sourceTaskResult,
) ([]instructionLockAssemblyInput, error) {
	if len(instructionResources) != len(results) {
		return nil, fmt.Errorf("lock assembly got %d instructions candidates for %d resolved instructions artifacts", len(instructionResources), len(results))
	}

	inputs := make([]instructionLockAssemblyInput, 0, len(instructionResources))
	for index, instruction := range instructionResources {
		resolved, err := resolvedArtifactInputFromSourceTaskResult(results[index])
		if err != nil {
			return nil, err
		}
		input, err := newInstructionLockAssemblyInput(instruction, resolved, assemblyTaskRefFromSourceTask(results[index].task))
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, input)
	}
	return inputs, nil
}

func newHookAssetLockAssemblyInputs(
	hookAssets []hookasset.HookAsset,
	results []sourceTaskResult,
) ([]hookAssetLockAssemblyInput, error) {
	if len(hookAssets) != len(results) {
		return nil, fmt.Errorf("lock assembly got %d hook asset candidates for %d resolved hook asset artifacts", len(hookAssets), len(results))
	}

	inputs := make([]hookAssetLockAssemblyInput, 0, len(hookAssets))
	for index, asset := range hookAssets {
		resolved, err := resolvedArtifactInputFromSourceTaskResult(results[index])
		if err != nil {
			return nil, err
		}
		input, err := newHookAssetLockAssemblyInput(asset, resolved, assemblyTaskRefFromSourceTask(results[index].task))
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, input)
	}
	return inputs, nil
}

func newHookAssetLockAssemblyInput(
	asset hookasset.HookAsset,
	resolved resolvedArtifactInput,
	task assemblyTaskRef,
) (hookAssetLockAssemblyInput, error) {
	input := hookAssetLockAssemblyInput{value: asset, artifact: resolved, task: task}
	if err := input.validate(); err != nil {
		return hookAssetLockAssemblyInput{}, err
	}
	return input, nil
}

func newSkillLockAssemblyInput(
	lockable lockableSkill,
	resolved resolvedArtifactInput,
	task assemblyTaskRef,
) (skillLockAssemblyInput, error) {
	input := skillLockAssemblyInput{
		value:               lockable.Resource,
		skillSetDeclaration: cloneSkillSetDeclarationIdentity(lockable.SkillSetDeclaration),
		artifact:            resolved,
		task:                task,
	}
	if err := input.validate(); err != nil {
		return skillLockAssemblyInput{}, err
	}
	return input, nil
}

func newInstructionLockAssemblyInput(
	instruction instructions.Instructions,
	resolved resolvedArtifactInput,
	task assemblyTaskRef,
) (instructionLockAssemblyInput, error) {
	input := instructionLockAssemblyInput{value: instruction, artifact: resolved, task: task}
	if err := input.validate(); err != nil {
		return instructionLockAssemblyInput{}, err
	}
	return input, nil
}

func (input lockAssemblyInput) validate() error {
	seen := make(map[entity.ID]struct{}, len(input.Skills)+len(input.HookAssets)+len(input.Instructions))
	for _, skillInput := range input.Skills {
		if err := skillInput.validate(); err != nil {
			return err
		}
		if _, exists := seen[skillInput.value.ID()]; exists {
			return fmt.Errorf("duplicate lock assembly subject %q", skillInput.value.ID())
		}
		seen[skillInput.value.ID()] = struct{}{}
	}
	for _, hookAssetInput := range input.HookAssets {
		if err := hookAssetInput.validate(); err != nil {
			return err
		}
		if _, exists := seen[hookAssetInput.value.ID()]; exists {
			return fmt.Errorf("duplicate lock assembly subject %q", hookAssetInput.value.ID())
		}
		seen[hookAssetInput.value.ID()] = struct{}{}
	}
	for _, instructionInput := range input.Instructions {
		if err := instructionInput.validate(); err != nil {
			return err
		}
		if _, exists := seen[instructionInput.value.ID()]; exists {
			return fmt.Errorf("duplicate lock assembly subject %q", instructionInput.value.ID())
		}
		seen[instructionInput.value.ID()] = struct{}{}
	}
	return nil
}

func (input skillLockAssemblyInput) validate() error {
	if err := input.value.Validate(); err != nil {
		return fmt.Errorf("skill lock assembly subject: %w", err)
	}
	if input.skillSetDeclaration != nil {
		if err := input.skillSetDeclaration.Validate(); err != nil {
			return fmt.Errorf("skill lock assembly declaration identity: %w", err)
		}
	}
	return validateAssemblyCorrelation(input.value.ID(), input.artifact, input.task, "skill")
}

func (input instructionLockAssemblyInput) validate() error {
	if err := input.value.Validate(); err != nil {
		return fmt.Errorf("instructions lock assembly subject: %w", err)
	}
	return validateAssemblyCorrelation(input.value.ID(), input.artifact, input.task, "instructions")
}

func (input hookAssetLockAssemblyInput) validate() error {
	if err := input.value.Validate(); err != nil {
		return fmt.Errorf("hook asset lock assembly subject: %w", err)
	}
	return validateAssemblyCorrelation(input.value.ID(), input.artifact, input.task, "hook asset")
}

func validateAssemblyCorrelation(
	entityID entity.ID,
	resolved resolvedArtifactInput,
	task assemblyTaskRef,
	label string,
) error {
	if err := resolved.matches(entityID); err != nil {
		return err
	}
	if task.entityID != entityID {
		return fmt.Errorf("%s lock assembly task id %s does not match subject %s", label, formatEntityID(task.entityID), formatEntityID(entityID))
	}
	return nil
}

func cloneSkillSetDeclarationIdentity(
	identity *skill.SkillSetDeclarationIdentity,
) *skill.SkillSetDeclarationIdentity {
	if identity == nil {
		return nil
	}
	cloned := *identity
	return &cloned
}

func cloneIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func (input skillLockAssemblyInput) event(kind EventKind, err error) Event {
	return input.task.event(kind, err)
}

func (input instructionLockAssemblyInput) event(kind EventKind, err error) Event {
	return input.task.event(kind, err)
}

func (input hookAssetLockAssemblyInput) event(kind EventKind, err error) Event {
	return input.task.event(kind, err)
}

func (ref assemblyTaskRef) event(kind EventKind, err error) Event {
	return Event{
		Kind:            kind,
		TaskID:          ref.taskID,
		Stage:           ref.stage,
		Ordinal:         ref.ordinal,
		EntityID:        ref.entityID,
		SkillGroupIndex: cloneIntPointer(ref.skillGroupIndex),
		Err:             err,
	}
}

func assemblyTaskRefFromSourceTask(task sourceTask) assemblyTaskRef {
	return assemblyTaskRef{
		taskID:          task.id().requestID(),
		stage:           task.stage,
		ordinal:         task.ordinal,
		entityID:        task.entityID,
		skillGroupIndex: task.skillGroupIndex(),
	}
}

func validateEntityID(id entity.ID, context string) error {
	if err := id.Validate(); err != nil {
		return fmt.Errorf("%s: %w", context, err)
	}
	return nil
}

func formatEntityID(id entity.ID) string {
	return fmt.Sprintf("%s/%s", id.Kind(), id.Name())
}
