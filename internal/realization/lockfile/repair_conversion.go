package lockfile

import (
	"encoding/base64"
	"fmt"

	"github.com/isty2e/daem/internal/supply/artifact"
	skillrepair "github.com/isty2e/daem/internal/supply/compat/skill/repair"
)

func repairRecipeFromDTO(dto *repairRecipeDTO) (*skillrepair.Recipe, error) {
	if dto == nil {
		return nil, nil
	}
	if dto.Version != int64(skillrepair.RecipeVersion) {
		return nil, fmt.Errorf("repair recipe version %d is unsupported", dto.Version)
	}
	input, err := exactIdentityFromDTO(dto.Input)
	if err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	output, err := exactIdentityFromDTO(dto.Output)
	if err != nil {
		return nil, fmt.Errorf("output: %w", err)
	}
	operations, err := repairOperationsFromDTO(dto.Operations)
	if err != nil {
		return nil, err
	}
	recipe, err := skillrepair.NewRecipe(input, output, operations)
	if err != nil {
		return nil, err
	}
	if recipe.Hash() != dto.RecipeHash {
		return nil, fmt.Errorf("repair recipe hash %q does not match canonical hash %q", dto.RecipeHash, recipe.Hash())
	}
	return &recipe, nil
}

func repairRecipeToDTO(recipe skillrepair.Recipe, present bool) *repairRecipeDTO {
	if !present {
		return nil
	}
	return &repairRecipeDTO{
		Version:    int64(recipe.Version()),
		RecipeHash: recipe.Hash(),
		Input:      exactIdentityToDTO(recipe.Input()),
		Output:     exactIdentityToDTO(recipe.Output()),
		Operations: repairOperationsToDTO(recipe.Operations()),
	}
}

func repairOperationsFromDTO(operations []repairOperationDTO) ([]skillrepair.Operation, error) {
	converted := make([]skillrepair.Operation, 0, len(operations))
	for index, operation := range operations {
		decoded, err := repairOperationFromDTO(operation)
		if err != nil {
			return nil, fmt.Errorf("operation[%d]: %w", index, err)
		}
		converted = append(converted, decoded)
	}
	return converted, nil
}

func repairOperationFromDTO(operation repairOperationDTO) (skillrepair.Operation, error) {
	switch skillrepair.OperationKind(operation.Kind) {
	case skillrepair.OperationRename:
		if err := requireRepairOperationShape(operation, repairOperationShape{rename: true}); err != nil {
			return skillrepair.Operation{}, err
		}
		if operation.Mode == nil || *operation.Mode < 0 || uint64(*operation.Mode) > uint64(^uint32(0)) {
			return skillrepair.Operation{}, fmt.Errorf("rename mode must be within uint32 range")
		}
		return skillrepair.NewRenameOperation(operation.From, operation.To, artifact.ContentHash(operation.FileHash), uint32(*operation.Mode))
	case skillrepair.OperationReplaceBytes:
		if err := requireRepairOperationShape(operation, repairOperationShape{replacement: true}); err != nil {
			return skillrepair.Operation{}, err
		}
		if operation.Offset == nil {
			return skillrepair.Operation{}, fmt.Errorf("replace_bytes offset is required")
		}
		oldBytes, err := decodeRepairBytes(operation.OldBytesBase64, "old_bytes_base64")
		if err != nil {
			return skillrepair.Operation{}, err
		}
		newBytes, err := decodeRepairBytes(operation.NewBytesBase64, "new_bytes_base64")
		if err != nil {
			return skillrepair.Operation{}, err
		}
		return skillrepair.NewReplaceBytesOperation(
			operation.Path, *operation.Offset, oldBytes, newBytes,
			artifact.ContentHash(operation.InputHash), artifact.ContentHash(operation.OutputHash),
		)
	case skillrepair.OperationSetFrontmatterString:
		if err := requireRepairOperationShape(operation, repairOperationShape{frontmatter: true}); err != nil {
			return skillrepair.Operation{}, err
		}
		if operation.Offset == nil {
			return skillrepair.Operation{}, fmt.Errorf("set_frontmatter_string offset is required")
		}
		oldBytes, err := decodeRepairBytes(operation.OldBytesBase64, "old_bytes_base64")
		if err != nil {
			return skillrepair.Operation{}, err
		}
		newBytes, err := decodeRepairBytes(operation.NewBytesBase64, "new_bytes_base64")
		if err != nil {
			return skillrepair.Operation{}, err
		}
		var oldValue *string
		if operation.OldValuePresent == nil {
			return skillrepair.Operation{}, fmt.Errorf("set_frontmatter_string old_value_present is required")
		}
		if *operation.OldValuePresent {
			value := operation.OldValue
			oldValue = &value
		}
		return skillrepair.NewSetFrontmatterStringOperation(
			operation.Path, operation.Field, oldValue, operation.NewValue, *operation.Offset,
			oldBytes, newBytes, artifact.ContentHash(operation.InputHash), artifact.ContentHash(operation.OutputHash),
		)
	default:
		return skillrepair.Operation{}, fmt.Errorf("unknown repair operation kind %q", operation.Kind)
	}
}

func repairOperationsToDTO(operations []skillrepair.Operation) []repairOperationDTO {
	converted := make([]repairOperationDTO, 0, len(operations))
	for _, operation := range operations {
		switch operation.Kind() {
		case skillrepair.OperationRename:
			body, _ := operation.Rename()
			mode := int(body.Mode())
			converted = append(converted, repairOperationDTO{
				Kind: string(operation.Kind()), From: body.From(), To: body.To(),
				FileHash: string(body.FileHash()), Mode: &mode,
			})
		case skillrepair.OperationReplaceBytes:
			body, _ := operation.ReplaceBytes()
			offset := body.Offset()
			converted = append(converted, repairOperationDTO{
				Kind: string(operation.Kind()), Path: body.Path(), Offset: &offset,
				OldBytesBase64: base64.StdEncoding.EncodeToString(body.Old()),
				NewBytesBase64: base64.StdEncoding.EncodeToString(body.New()),
				InputHash:      string(body.InputHash()), OutputHash: string(body.OutputHash()),
			})
		case skillrepair.OperationSetFrontmatterString:
			body, _ := operation.SetFrontmatterString()
			offset := body.Offset()
			oldValue, oldValuePresent := body.OldValue()
			oldValuePresentValue := oldValuePresent
			converted = append(converted, repairOperationDTO{
				Kind: string(operation.Kind()), Path: body.Path(), Field: body.Field(),
				OldValue: oldValue, OldValuePresent: &oldValuePresentValue, NewValue: body.NewValue(), Offset: &offset,
				OldBytesBase64: base64.StdEncoding.EncodeToString(body.Old()),
				NewBytesBase64: base64.StdEncoding.EncodeToString(body.New()),
				InputHash:      string(body.InputHash()), OutputHash: string(body.OutputHash()),
			})
		}
	}
	return converted
}

func decodeRepairBytes(value string, field string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("%s must be base64: %w", field, err)
	}
	return decoded, nil
}

type repairOperationShape struct {
	rename      bool
	replacement bool
	frontmatter bool
}

func requireRepairOperationShape(operation repairOperationDTO, shape repairOperationShape) error {
	if !shape.rename && (operation.From != "" || operation.To != "" || operation.FileHash != "" || operation.Mode != nil) {
		return fmt.Errorf("rename-only fields must be omitted for %s operations", operation.Kind)
	}
	if !shape.replacement && !shape.frontmatter &&
		(operation.Path != "" || operation.Offset != nil || operation.OldBytesBase64 != "" ||
			operation.NewBytesBase64 != "" || operation.InputHash != "" || operation.OutputHash != "") {
		return fmt.Errorf("replacement fields must be omitted for %s operations", operation.Kind)
	}
	if !shape.frontmatter &&
		(operation.Field != "" || operation.OldValue != "" || operation.OldValuePresent != nil || operation.NewValue != "") {
		return fmt.Errorf("frontmatter-only fields must be omitted for %s operations", operation.Kind)
	}
	if shape.frontmatter && operation.OldValuePresent == nil {
		return fmt.Errorf("old_value_present is required for %s operations", operation.Kind)
	}
	if shape.frontmatter && operation.OldValue != "" && !*operation.OldValuePresent {
		return fmt.Errorf("old_value_present is required when old_value is set")
	}
	return nil
}
