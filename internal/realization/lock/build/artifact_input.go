package build

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
)

// resolvedArtifactInput carries S/A-owned artifact facts into lock assembly.
type resolvedArtifactInput struct {
	entityID   entity.ID
	sourceSpec source.Source
	resolution acquisition.Resolution
}

func resolvedArtifactInputFromSourceTaskResult(result sourceTaskResult) (resolvedArtifactInput, error) {
	if result.err != nil {
		return resolvedArtifactInput{}, result.err
	}
	return newResolvedArtifactInput(result.task.entityID, result.task.sourceSpec, result.resolution)
}

func newResolvedArtifactInput(
	entityID entity.ID,
	sourceSpec source.Source,
	resolution acquisition.Resolution,
) (resolvedArtifactInput, error) {
	input := resolvedArtifactInput{
		entityID:   entityID,
		sourceSpec: sourceSpec,
		resolution: resolution,
	}
	if err := input.validate(); err != nil {
		return resolvedArtifactInput{}, err
	}
	return input, nil
}

func (input resolvedArtifactInput) validate() error {
	if err := validateEntityID(input.entityID, "resolved artifact entity"); err != nil {
		return err
	}
	if err := input.resolution.Validate(input.sourceSpec); err != nil {
		return fmt.Errorf("resolved artifact for %s: %w", formatEntityID(input.entityID), err)
	}
	return nil
}

func (input resolvedArtifactInput) matches(entityID entity.ID) error {
	if input.entityID != entityID {
		return fmt.Errorf("resolved artifact entity %s does not match candidate %s", formatEntityID(input.entityID), formatEntityID(entityID))
	}
	return input.validate()
}

func (input resolvedArtifactInput) exactArtifact(
	entityID entity.ID,
) (artifact.ExactIdentity, access.View, error) {
	if err := input.matches(entityID); err != nil {
		return artifact.ExactIdentity{}, access.View{}, err
	}
	return input.resolution.Identity(), input.resolution.View(), nil
}
