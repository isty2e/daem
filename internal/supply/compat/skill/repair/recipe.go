package repair

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"

	"github.com/isty2e/daem/internal/supply/artifact"
)

const (
	RecipeVersion             = 1
	DerivationAlgorithmID     = "compat.skill.repair"
	DerivationExecutionDomain = "daem:compat/skill/repair"
)

// Recipe is one validated, ordered, exactly invertible skill repair.
type Recipe struct {
	version    int
	input      artifact.ExactIdentity
	output     artifact.ExactIdentity
	operations []Operation
}

// NewRecipe constructs a canonical repair recipe.
func NewRecipe(
	input artifact.ExactIdentity,
	output artifact.ExactIdentity,
	operations []Operation,
) (Recipe, error) {
	recipe := Recipe{
		version:    RecipeVersion,
		input:      input,
		output:     output,
		operations: cloneOperations(operations),
	}
	if err := recipe.Validate(); err != nil {
		return Recipe{}, err
	}
	return recipe, nil
}

// Version returns the recipe schema version.
func (recipe Recipe) Version() int { return recipe.version }

// Input returns the exact original artifact identity.
func (recipe Recipe) Input() artifact.ExactIdentity { return recipe.input }

// Output returns the exact repaired artifact identity.
func (recipe Recipe) Output() artifact.ExactIdentity { return recipe.output }

// Operations returns an independent ordered operation copy.
func (recipe Recipe) Operations() []Operation { return cloneOperations(recipe.operations) }

func (recipe Recipe) clone() Recipe {
	return Recipe{
		version:    recipe.version,
		input:      recipe.input,
		output:     recipe.output,
		operations: cloneOperations(recipe.operations),
	}
}

// Actions returns stable human-readable operation summaries.
func (recipe Recipe) Actions() []string {
	actions := make([]string, 0, len(recipe.operations))
	for _, operation := range recipe.operations {
		actions = append(actions, operation.Summary())
	}
	return actions
}

// Validate rejects malformed identities, variants, and disconnected transitions.
func (recipe Recipe) Validate() error {
	if recipe.version != RecipeVersion {
		return fmt.Errorf("repair recipe version %d is unsupported", recipe.version)
	}
	if err := recipe.input.Validate(); err != nil {
		return fmt.Errorf("repair recipe input: %w", err)
	}
	if err := recipe.output.Validate(); err != nil {
		return fmt.Errorf("repair recipe output: %w", err)
	}
	if recipe.input.SourceID() != recipe.output.SourceID() {
		return fmt.Errorf("repair recipe cannot change source id")
	}
	if recipe.input.ResolvedRef() != recipe.output.ResolvedRef() {
		return fmt.Errorf("repair recipe cannot change resolved ref")
	}
	if recipe.input.Kind() != artifact.ArtifactKindDirectory || recipe.output.Kind() != artifact.ArtifactKindDirectory {
		return fmt.Errorf("skill repair recipe requires directory artifacts")
	}
	if recipe.input.ContentHash() == recipe.output.ContentHash() {
		return fmt.Errorf("repair recipe input and output hashes must differ")
	}
	if len(recipe.operations) == 0 {
		return fmt.Errorf("repair recipe operations are required")
	}
	for index, operation := range recipe.operations {
		if err := operation.Validate(); err != nil {
			return fmt.Errorf("repair operation[%d]: %w", index, err)
		}
	}
	if err := validateOperationTransitions(recipe.operations); err != nil {
		return err
	}
	return nil
}

// Hash returns the canonical identity of every semantic recipe fact.
func (recipe Recipe) Hash() string {
	if err := recipe.Validate(); err != nil {
		return ""
	}
	digest := sha256.New()
	writeCanonicalUint64(digest, uint64(recipe.version))
	writeCanonicalIdentity(digest, recipe.input)
	writeCanonicalIdentity(digest, recipe.output)
	writeCanonicalUint64(digest, uint64(len(recipe.operations)))
	for _, operation := range recipe.operations {
		writeCanonicalOperation(digest, operation)
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil))
}

// Equal reports whether two valid recipes contain the same canonical facts.
func (recipe Recipe) Equal(other Recipe) bool {
	return recipe.Validate() == nil && other.Validate() == nil && recipe.Hash() == other.Hash()
}

// Inverse constructs the exact reverse recipe in reverse dependency order.
func (recipe Recipe) Inverse() (Recipe, error) {
	if err := recipe.Validate(); err != nil {
		return Recipe{}, err
	}
	operations := make([]Operation, 0, len(recipe.operations))
	for index := len(recipe.operations) - 1; index >= 0; index-- {
		operation, err := recipe.operations[index].Inverse()
		if err != nil {
			return Recipe{}, fmt.Errorf("invert repair operation[%d]: %w", index, err)
		}
		operations = append(operations, operation)
	}
	return NewRecipe(recipe.output, recipe.input, operations)
}

type transitionState uint8

const (
	transitionUnknown transitionState = iota
	transitionPresent
	transitionAbsent
)

type fileTransition struct {
	state     transitionState
	hash      artifact.ContentHash
	mode      uint32
	modeKnown bool
}

func validateOperationTransitions(operations []Operation) error {
	states := make(map[string]fileTransition)
	for index, operation := range operations {
		switch operation.kind {
		case OperationRename:
			body := operation.rename
			from := states[body.from]
			if from.state == transitionAbsent {
				return fmt.Errorf("repair operation[%d] rename source %q is absent after prior operations", index, body.from)
			}
			if from.state == transitionPresent &&
				(from.hash != body.fileHash || (from.modeKnown && from.mode != body.mode)) {
				return fmt.Errorf("repair operation[%d] rename source %q does not match prior postcondition", index, body.from)
			}
			to := states[body.to]
			if to.state == transitionPresent {
				return fmt.Errorf("repair operation[%d] rename destination %q is already present", index, body.to)
			}
			states[body.from] = fileTransition{state: transitionAbsent}
			states[body.to] = fileTransition{
				state:     transitionPresent,
				hash:      body.fileHash,
				mode:      body.mode,
				modeKnown: true,
			}
		case OperationReplaceBytes:
			body := operation.replaceBytes
			if err := advanceReplacementTransition(states, body.path, body.inputHash, body.outputHash, index); err != nil {
				return err
			}
		case OperationSetFrontmatterString:
			body := operation.setFrontmatterString
			if err := advanceReplacementTransition(states, body.path, body.inputHash, body.outputHash, index); err != nil {
				return err
			}
		}
	}
	return nil
}

func advanceReplacementTransition(
	states map[string]fileTransition,
	path string,
	inputHash artifact.ContentHash,
	outputHash artifact.ContentHash,
	index int,
) error {
	state := states[path]
	if state.state == transitionAbsent {
		return fmt.Errorf("repair operation[%d] target %q is absent after prior operations", index, path)
	}
	if state.state == transitionPresent && state.hash != inputHash {
		return fmt.Errorf("repair operation[%d] target %q input hash does not match prior postcondition", index, path)
	}
	state.state = transitionPresent
	state.hash = outputHash
	states[path] = state
	return nil
}

type canonicalWriter interface {
	Write([]byte) (int, error)
}

func writeCanonicalIdentity(writer canonicalWriter, identity artifact.ExactIdentity) {
	writeCanonicalString(writer, string(identity.SourceID()))
	writeCanonicalString(writer, string(identity.ResolvedRef()))
	writeCanonicalString(writer, string(identity.Kind()))
	writeCanonicalString(writer, string(identity.ContentHash()))
}

func writeCanonicalOperation(writer canonicalWriter, operation Operation) {
	writeCanonicalString(writer, string(operation.kind))
	switch operation.kind {
	case OperationRename:
		body := operation.rename
		writeCanonicalString(writer, body.from)
		writeCanonicalString(writer, body.to)
		writeCanonicalString(writer, string(body.fileHash))
		writeCanonicalUint64(writer, uint64(body.mode))
	case OperationReplaceBytes:
		body := operation.replaceBytes
		writeCanonicalReplacement(writer, body.path, body.offset, body.old, body.new, body.inputHash, body.outputHash)
	case OperationSetFrontmatterString:
		body := operation.setFrontmatterString
		writeCanonicalReplacement(writer, body.path, body.offset, body.old, body.new, body.inputHash, body.outputHash)
		writeCanonicalString(writer, body.field)
		if body.oldValue == nil {
			writeCanonicalUint64(writer, 0)
		} else {
			writeCanonicalUint64(writer, 1)
			writeCanonicalString(writer, *body.oldValue)
		}
		writeCanonicalString(writer, body.newValue)
	}
}

func writeCanonicalReplacement(
	writer canonicalWriter,
	path string,
	offset int,
	oldBytes []byte,
	newBytes []byte,
	inputHash artifact.ContentHash,
	outputHash artifact.ContentHash,
) {
	writeCanonicalString(writer, path)
	writeCanonicalUint64(writer, uint64(offset))
	writeCanonicalBytes(writer, oldBytes)
	writeCanonicalBytes(writer, newBytes)
	writeCanonicalString(writer, string(inputHash))
	writeCanonicalString(writer, string(outputHash))
}

func writeCanonicalString(writer canonicalWriter, value string) {
	writeCanonicalBytes(writer, []byte(value))
}

func writeCanonicalBytes(writer canonicalWriter, value []byte) {
	writeCanonicalUint64(writer, uint64(len(value)))
	_, _ = writer.Write(value)
}

func writeCanonicalUint64(writer canonicalWriter, value uint64) {
	var buffer [8]byte
	binary.BigEndian.PutUint64(buffer[:], value)
	_, _ = writer.Write(buffer[:])
}

func cloneOperations(operations []Operation) []Operation {
	cloned := make([]Operation, 0, len(operations))
	for _, operation := range operations {
		cloned = append(cloned, operation.clone())
	}
	return cloned
}
