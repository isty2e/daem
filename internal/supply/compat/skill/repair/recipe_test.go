package repair

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

func TestOperationRejectsMalformedVariants(t *testing.T) {
	tests := map[string]Operation{
		"zero": {},
		"unknown": {
			kind:   "unknown",
			rename: &RenameOperation{from: "a", to: "b"},
		},
		"missing body": {kind: OperationRename},
		"multiple bodies": {
			kind:         OperationRename,
			rename:       &RenameOperation{from: "a", to: "b"},
			replaceBytes: &ReplaceBytesOperation{path: "a"},
		},
	}
	for name, operation := range tests {
		t.Run(name, func(t *testing.T) {
			if err := operation.Validate(); err == nil {
				t.Fatal("Validate() returned nil error")
			}
		})
	}
}

func TestOperationConstructorsCloneMutableInputs(t *testing.T) {
	oldBytes := []byte("old")
	newBytes := []byte("new")
	operation, err := NewReplaceBytesOperation(
		"SKILL.md",
		0,
		oldBytes,
		newBytes,
		artifact.HashFileContent(oldBytes),
		artifact.HashFileContent(newBytes),
	)
	if err != nil {
		t.Fatalf("NewReplaceBytesOperation returned error: %v", err)
	}
	oldBytes[0] = 'x'
	newBytes[0] = 'y'
	body, ok := operation.ReplaceBytes()
	if !ok {
		t.Fatal("ReplaceBytes() reports wrong variant")
	}
	if string(body.Old()) != "old" || string(body.New()) != "new" {
		t.Fatalf("operation bytes = %q -> %q, want cloned old -> new", body.Old(), body.New())
	}

	returned := body.Old()
	returned[0] = 'z'
	bodyAgain, _ := operation.ReplaceBytes()
	if string(bodyAgain.Old()) != "old" {
		t.Fatalf("operation bytes changed through getter: %q", bodyAgain.Old())
	}
}

func TestOperationConstructorsRejectPathsOutsideRepairRegistry(t *testing.T) {
	hash := artifact.HashFileContent([]byte("content"))
	if _, err := NewRenameOperation("notes.md", "NOTES.md", hash, 0o600); err == nil {
		t.Fatal("NewRenameOperation accepted a non-skill casing pair")
	}
	if _, err := NewReplaceBytesOperation(
		"notes.md",
		0,
		[]byte("old"),
		[]byte("new"),
		artifact.HashFileContent([]byte("old")),
		artifact.HashFileContent([]byte("new")),
	); err == nil {
		t.Fatal("NewReplaceBytesOperation accepted a non-SKILL.md path")
	}
}

func TestRecipeRejectsDisconnectedOrderedTransitions(t *testing.T) {
	input, output := testRecipeIdentities(t)
	rename, err := NewRenameOperation("skill.md", "SKILL.md", artifact.HashFileContent([]byte("before")), 0o600)
	if err != nil {
		t.Fatalf("NewRenameOperation returned error: %v", err)
	}
	replace, err := NewReplaceBytesOperation(
		"SKILL.md",
		0,
		[]byte("other"),
		[]byte("after"),
		artifact.HashFileContent([]byte("other")),
		artifact.HashFileContent([]byte("after")),
	)
	if err != nil {
		t.Fatalf("NewReplaceBytesOperation returned error: %v", err)
	}
	_, err = NewRecipe(input, output, []Operation{rename, replace})
	if err == nil {
		t.Fatal("NewRecipe returned nil error")
	}
	if !strings.Contains(err.Error(), "prior postcondition") {
		t.Fatalf("error = %q, want disconnected-transition rejection", err)
	}
}

func TestRecipeHashIncludesOrderAndPreconditionBytes(t *testing.T) {
	input, output := testRecipeIdentities(t)
	contentHash := artifact.HashFileContent([]byte("content"))
	first, err := NewRenameOperation("skill.md", "SKILL.md", contentHash, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewRenameOperation("SKILL.md", "skill.md", contentHash, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	ordered, err := NewRecipe(input, output, []Operation{first, second})
	if err != nil {
		t.Fatalf("NewRecipe(ordered) returned error: %v", err)
	}
	reversed, err := NewRecipe(input, output, []Operation{second, first})
	if err != nil {
		t.Fatalf("NewRecipe(reversed) returned error: %v", err)
	}
	if ordered.Hash() == reversed.Hash() {
		t.Fatal("recipe hash does not include operation order")
	}

	replacement := mustReplaceOperation(t, []byte("a"), []byte("b"))
	replacementRecipe, err := NewRecipe(input, output, []Operation{replacement})
	if err != nil {
		t.Fatal(err)
	}
	changed := replacementRecipe.clone()
	changed.operations[0].replaceBytes.old = []byte("x")
	if replacementRecipe.Hash() == changed.Hash() {
		t.Fatal("recipe hash does not include precondition bytes")
	}
}

func TestRecipeEqualRequiresValidCanonicalFacts(t *testing.T) {
	input, output := testRecipeIdentities(t)
	operation := mustReplaceOperation(t, []byte("before"), []byte("after"))
	left, err := NewRecipe(input, output, []Operation{operation})
	if err != nil {
		t.Fatal(err)
	}
	right, err := NewRecipe(input, output, []Operation{operation})
	if err != nil {
		t.Fatal(err)
	}
	inverse, err := left.Inverse()
	if err != nil {
		t.Fatal(err)
	}

	if !left.Equal(right) || !right.Equal(left) {
		t.Fatal("identical valid recipes are not symmetrically equal")
	}
	if left.Equal(inverse) {
		t.Fatal("inverse recipe compared equal to forward recipe")
	}
	if left.Equal(Recipe{}) || (Recipe{}).Equal(left) {
		t.Fatal("invalid zero recipe compared equal")
	}
}

func TestFrontmatterInsertionInverseLowersToExactReplacement(t *testing.T) {
	input := []byte("---\ndescription: Demo\n---\n")
	inserted := []byte("name: review\n")
	operation, err := NewSetFrontmatterStringOperation(
		"SKILL.md",
		"name",
		nil,
		"review",
		4,
		nil,
		inserted,
		artifact.HashFileContent(input),
		artifact.HashFileContent(append(append([]byte(nil), input[:4]...), append(inserted, input[4:]...)...)),
	)
	if err != nil {
		t.Fatalf("NewSetFrontmatterStringOperation returned error: %v", err)
	}
	inverse, err := operation.Inverse()
	if err != nil {
		t.Fatalf("Inverse returned error: %v", err)
	}
	if inverse.Kind() != OperationReplaceBytes {
		t.Fatalf("inverse kind = %q, want replace_bytes", inverse.Kind())
	}
	body, ok := inverse.ReplaceBytes()
	if !ok || len(body.New()) != 0 || string(body.Old()) != string(inserted) {
		t.Fatalf("inverse replacement = %#v, want exact insertion removal", body)
	}
}

func TestRepairSucceedsWithReadOnlySourceDirectories(t *testing.T) {
	originalRoot := filepath.Join(t.TempDir(), "original")
	writeTestFile(t, originalRoot, "skill.md", "---\ndescription: Demo skill\n---\n")
	t.Cleanup(func() { _ = chmodTestDirectoryTree(originalRoot, 0o700) })
	if err := chmodTestDirectoryTree(originalRoot, 0o555); err != nil {
		t.Fatalf("chmod source directory: %v", err)
	}
	input, view := testArtifact(t, originalRoot)

	result, err := Repair(context.Background(), input, view, "review", []target.Target{target.TargetCodex})
	if err != nil {
		t.Fatalf("Repair returned error for read-only source: %v", err)
	}
	t.Cleanup(func() { releaseResult(t, result) })
	if !strings.Contains(readTestViewFile(t, resultView(t, result), "SKILL.md"), "name: review") {
		t.Fatal("repaired output does not contain inserted name")
	}
}

func testRecipeIdentities(t *testing.T) (artifact.ExactIdentity, artifact.ExactIdentity) {
	t.Helper()
	input, err := artifact.NewExactIdentity(
		"source:test",
		"revision",
		artifact.ArtifactKindDirectory,
		artifact.HashFileContent([]byte("input")),
	)
	if err != nil {
		t.Fatalf("NewExactIdentity(input) returned error: %v", err)
	}
	output, err := artifact.NewExactIdentity(
		"source:test",
		"revision",
		artifact.ArtifactKindDirectory,
		artifact.HashFileContent([]byte("output")),
	)
	if err != nil {
		t.Fatalf("NewExactIdentity(output) returned error: %v", err)
	}
	return input, output
}

func mustReplaceOperation(t *testing.T, oldBytes []byte, newBytes []byte) Operation {
	t.Helper()
	operation, err := NewReplaceBytesOperation(
		"SKILL.md",
		0,
		oldBytes,
		newBytes,
		artifact.HashFileContent(oldBytes),
		artifact.HashFileContent(newBytes),
	)
	if err != nil {
		t.Fatalf("NewReplaceBytesOperation returned error: %v", err)
	}
	return operation
}
