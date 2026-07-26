package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

const (
	FileMaterializationAlgorithmID      = "artifact.file-materialization"
	FileMaterializationAlgorithmVersion = "v1"
	FileMaterializationExecutionDomain  = "daem:artifact/file-materialization"
)

// FileMaterialization is one exact deterministic change from source file mode
// semantics to required output file mode semantics.
type FileMaterialization struct {
	input      ExactIdentity
	output     ExactIdentity
	executable bool
	recipeHash string
}

// NewFileMaterialization verifies input bytes and constructs the exact output identity.
func NewFileMaterialization(
	input ExactIdentity,
	content []byte,
	sourceExecutable bool,
	requiredExecutable bool,
) (FileMaterialization, error) {
	if err := input.Validate(); err != nil {
		return FileMaterialization{}, fmt.Errorf("file materialization input: %w", err)
	}
	if input.Kind() != ArtifactKindFile {
		return FileMaterialization{}, fmt.Errorf("file materialization requires a file artifact")
	}
	if got := HashFileContentWithExecutable(content, sourceExecutable); got != input.ContentHash() {
		return FileMaterialization{}, fmt.Errorf("file materialization bytes do not match input identity")
	}
	output, err := NewExactIdentity(
		input.SourceID(),
		input.ResolvedRef(),
		input.Kind(),
		HashFileContentWithExecutable(content, requiredExecutable),
	)
	if err != nil {
		return FileMaterialization{}, fmt.Errorf("file materialization output: %w", err)
	}
	return FileMaterialization{
		input:      input,
		output:     output,
		executable: requiredExecutable,
		recipeHash: fileMaterializationRecipeHash(requiredExecutable),
	}, nil
}

// InputIdentity returns the exact source identity before mode normalization.
func (materialization FileMaterialization) InputIdentity() ExactIdentity {
	return materialization.input
}

// OutputIdentity returns the exact file identity after mode normalization.
func (materialization FileMaterialization) OutputIdentity() ExactIdentity {
	return materialization.output
}

// Executable reports the required output executable-bit class.
func (materialization FileMaterialization) Executable() bool { return materialization.executable }

// RecipeHash returns the canonical identity of the concrete mode-normalization recipe.
func (materialization FileMaterialization) RecipeHash() string { return materialization.recipeHash }

// ChangesIdentity reports whether materialization changes the exact file identity.
func (materialization FileMaterialization) ChangesIdentity() bool {
	return !materialization.input.Equal(materialization.output)
}

func fileMaterializationRecipeHash(executable bool) string {
	digest := sha256.New()
	writeRecord(digest, "daem-file-materialization-recipe-v1", executableLabel(executable))
	return hashAlgorithm + ":" + hex.EncodeToString(digest.Sum(nil))
}
