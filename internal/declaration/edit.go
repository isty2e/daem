package declaration

import (
	"fmt"
	"slices"
)

// EditOutcome names the structural effect of a declaration document edit.
type EditOutcome string

const (
	EditOutcomeAppend        EditOutcome = "append"
	EditOutcomeMergeTargets  EditOutcome = "target_merge"
	EditOutcomeRemove        EditOutcome = "remove"
	EditOutcomeUpdateTargets EditOutcome = "target_update"
)

// EditResult is a byte-preserving declaration document edit result.
type EditResult struct {
	Content []byte
	Outcome EditOutcome
}

// EditBlock is one decoded declaration value plus its byte range.
type EditBlock[T any] struct {
	Range DocumentRange
	Value T
}

// AddEditContract supplies declaration-family facts needed by the generic add-or-merge operation.
type AddEditContract[T any] struct {
	Kind                   Kind
	Scan                   func([]byte) ([]EditBlock[T], error)
	Key                    func(T) (Key, error)
	ExplicitTargets        func(T) Targets
	SameIdentity           func(existing T, incoming T, header ManifestHeader) bool
	RenderBlock            func(T) string
	RenderBlockWithTargets func(originalBlock string, existing T, incoming T, mergedTargets Targets, header ManifestHeader) (string, error)
	DuplicateError         func(Key) error
	AlreadyExistsError     func(Key) error
	InheritsTargetsError   func(Key) error
	AlreadyHasTargetsError func(Key) error
}

// AddEditInput describes one add-or-merge declaration edit.
type AddEditInput[T any] struct {
	Original    []byte
	Header      ManifestHeader
	Declaration T
	Codec       AddEditContract[T]
}

// ApplyAddDeclaration appends a declaration or merges explicit target values into a matching block.
func ApplyAddDeclaration[T any](input AddEditInput[T]) (EditResult, error) {
	if input.Codec.Scan == nil || input.Codec.Key == nil || input.Codec.ExplicitTargets == nil ||
		input.Codec.SameIdentity == nil || input.Codec.RenderBlock == nil || input.Codec.RenderBlockWithTargets == nil {
		return EditResult{}, fmt.Errorf("declaration edit codec %q is incomplete", input.Codec.Kind)
	}

	incomingKey, err := input.Codec.Key(input.Declaration)
	if err != nil {
		return EditResult{}, err
	}
	blocks, err := input.Codec.Scan(input.Original)
	if err != nil {
		return EditResult{}, err
	}

	for _, block := range blocks {
		existingKey, err := input.Codec.Key(block.Value)
		if err != nil {
			return EditResult{}, err
		}
		if existingKey != incomingKey {
			continue
		}
		if !input.Codec.SameIdentity(block.Value, input.Declaration, input.Header) {
			return EditResult{}, declarationError(input.Codec.DuplicateError, incomingKey, "duplicate declaration %s %q")
		}

		incomingTargets := input.Codec.ExplicitTargets(input.Declaration).Values()
		if len(incomingTargets) == 0 {
			return EditResult{}, declarationError(input.Codec.AlreadyExistsError, incomingKey, "%s %q already exists")
		}
		existingTargets := input.Codec.ExplicitTargets(block.Value).Values()
		if len(existingTargets) == 0 {
			return EditResult{}, declarationError(input.Codec.InheritsTargetsError, incomingKey, "%s %q inherits manifest targets")
		}

		mergedTargets := Targets(mergeStringValues(existingTargets, incomingTargets))
		if slices.Equal(existingTargets, mergedTargets.Values()) {
			return EditResult{}, declarationError(input.Codec.AlreadyHasTargetsError, incomingKey, "%s %q already has the selected targets")
		}

		updatedBlock, err := input.Codec.RenderBlockWithTargets(
			string(input.Original[block.Range.Start:block.Range.End]),
			block.Value,
			input.Declaration,
			mergedTargets,
			input.Header,
		)
		if err != nil {
			return EditResult{}, err
		}

		return EditResult{
			Content: ReplaceDocumentRange(input.Original, block.Range, []byte(updatedBlock)),
			Outcome: EditOutcomeMergeTargets,
		}, nil
	}

	return EditResult{
		Content: AppendDocumentBlock(input.Original, input.Codec.RenderBlock(input.Declaration)),
		Outcome: EditOutcomeAppend,
	}, nil
}

func declarationError(build func(Key) error, key Key, fallback string) error {
	if build != nil {
		return build(key)
	}
	return fmt.Errorf(fallback, key.Kind, key.Name)
}

// TargetRemovalInput describes the common remove-or-narrow edit.
type TargetRemovalInput struct {
	Original                  []byte
	Range                     DocumentRange
	ExistingTargets           Targets
	SelectedTargets           Targets
	NoSelectedTargetsError    func() error
	RenderBlockWithTargets    func(originalBlock string, remainingTargets Targets) (string, error)
	BeforeTargetReplace       func(originalBlock string) string
	AllowPartialTargetRemoval func(remainingTargets Targets) error
}

// ApplyTargetRemoval removes a declaration block or narrows its explicit target set.
func ApplyTargetRemoval(input TargetRemovalInput) (EditResult, error) {
	if len(input.SelectedTargets) == 0 {
		return EditResult{
			Content: RemoveDocumentRange(input.Original, input.Range),
			Outcome: EditOutcomeRemove,
		}, nil
	}

	remainingTargets, removed := RemoveTargets(input.ExistingTargets, input.SelectedTargets)
	if !removed {
		if input.NoSelectedTargetsError != nil {
			return EditResult{}, input.NoSelectedTargetsError()
		}
		return EditResult{}, fmt.Errorf("selected target values are not present")
	}
	if len(remainingTargets) == 0 {
		return EditResult{
			Content: RemoveDocumentRange(input.Original, input.Range),
			Outcome: EditOutcomeRemove,
		}, nil
	}
	if input.AllowPartialTargetRemoval != nil {
		if err := input.AllowPartialTargetRemoval(remainingTargets); err != nil {
			return EditResult{}, err
		}
	}
	if input.RenderBlockWithTargets == nil {
		return EditResult{}, fmt.Errorf("target renderer is required")
	}

	block := string(input.Original[input.Range.Start:input.Range.End])
	if input.BeforeTargetReplace != nil {
		block = input.BeforeTargetReplace(block)
	}
	updatedBlock, err := input.RenderBlockWithTargets(block, remainingTargets)
	if err != nil {
		return EditResult{}, err
	}

	return EditResult{
		Content: ReplaceDocumentRange(input.Original, input.Range, []byte(updatedBlock)),
		Outcome: EditOutcomeUpdateTargets,
	}, nil
}

// RemoveTargets subtracts selected target values from existing target values.
func RemoveTargets(existing Targets, selected Targets) (Targets, bool) {
	selectedSet := make(map[string]struct{}, len(selected))
	for _, target := range selected {
		selectedSet[target] = struct{}{}
	}
	removed := false
	remaining := make(Targets, 0, len(existing))
	for _, target := range existing {
		if _, ok := selectedSet[target]; ok {
			removed = true
			continue
		}
		remaining = append(remaining, target)
	}
	return remaining, removed
}

func mergeStringValues(existing []string, additions []string) []string {
	merged := append([]string{}, existing...)
	seen := make(map[string]struct{}, len(existing)+len(additions))
	for _, value := range existing {
		seen[value] = struct{}{}
	}
	for _, value := range additions {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		merged = append(merged, value)
	}
	return merged
}
