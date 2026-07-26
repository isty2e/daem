package repair

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
)

func TestRecipeReplayRoundTripsRenameOnly(t *testing.T) {
	originalRoot := filepath.Join(t.TempDir(), "original")
	expectedRoot := filepath.Join(t.TempDir(), "expected")
	writeTestFile(t, originalRoot, "skill.md", "content")
	writeTestFile(t, expectedRoot, "SKILL.md", "content")
	chmodTestPath(t, filepath.Join(originalRoot, "skill.md"), 0o751)
	chmodTestPath(t, filepath.Join(expectedRoot, "SKILL.md"), 0o751)
	operation, err := NewRenameOperation(
		"skill.md",
		"SKILL.md",
		artifact.HashFileContent([]byte("content")),
		0o751,
	)
	if err != nil {
		t.Fatalf("NewRenameOperation() error: %v", err)
	}
	assertRecipeRoundTrips(t, originalRoot, expectedRoot, []Operation{operation})
}

func TestRecipeReplayRoundTripsReplaceBytesOnly(t *testing.T) {
	originalRoot := filepath.Join(t.TempDir(), "original")
	expectedRoot := filepath.Join(t.TempDir(), "expected")
	original := []byte("prefix-before-suffix")
	expected := []byte("prefix-after-suffix")
	writeTestFile(t, originalRoot, "SKILL.md", string(original))
	writeTestFile(t, expectedRoot, "SKILL.md", string(expected))
	chmodTestPath(t, filepath.Join(originalRoot, "SKILL.md"), 0o640)
	chmodTestPath(t, filepath.Join(expectedRoot, "SKILL.md"), 0o640)
	operation, err := NewReplaceBytesOperation(
		"SKILL.md",
		len("prefix-"),
		[]byte("before"),
		[]byte("after"),
		artifact.HashFileContent(original),
		artifact.HashFileContent(expected),
	)
	if err != nil {
		t.Fatalf("NewReplaceBytesOperation() error: %v", err)
	}
	assertRecipeRoundTrips(t, originalRoot, expectedRoot, []Operation{operation})
}

func TestRecipeReplayRoundTripsFrontmatterReplacement(t *testing.T) {
	originalRoot := filepath.Join(t.TempDir(), "original")
	expectedRoot := filepath.Join(t.TempDir(), "expected")
	original := []byte("---\nname: before\ndescription: Demo\n---\n")
	expected := []byte("---\nname: after\ndescription: Demo\n---\n")
	writeTestFile(t, originalRoot, "SKILL.md", string(original))
	writeTestFile(t, expectedRoot, "SKILL.md", string(expected))
	oldValue := "before"
	offset := strings.Index(string(original), oldValue)
	operation, err := NewSetFrontmatterStringOperation(
		"SKILL.md",
		"name",
		&oldValue,
		"after",
		offset,
		[]byte(oldValue),
		[]byte("after"),
		artifact.HashFileContent(original),
		artifact.HashFileContent(expected),
	)
	if err != nil {
		t.Fatalf("NewSetFrontmatterStringOperation() error: %v", err)
	}
	assertRecipeRoundTrips(t, originalRoot, expectedRoot, []Operation{operation})
}

func TestRecipeReplayRoundTripsFrontmatterInsertion(t *testing.T) {
	originalRoot := filepath.Join(t.TempDir(), "original")
	expectedRoot := filepath.Join(t.TempDir(), "expected")
	original := []byte("---\ndescription: Demo\n---\n")
	insertion := []byte("name: review\n")
	expected := append(append([]byte(nil), original[:4]...), append(insertion, original[4:]...)...)
	writeTestFile(t, originalRoot, "SKILL.md", string(original))
	writeTestFile(t, expectedRoot, "SKILL.md", string(expected))
	operation, err := NewSetFrontmatterStringOperation(
		"SKILL.md",
		"name",
		nil,
		"review",
		4,
		nil,
		insertion,
		artifact.HashFileContent(original),
		artifact.HashFileContent(expected),
	)
	if err != nil {
		t.Fatalf("NewSetFrontmatterStringOperation() error: %v", err)
	}
	assertRecipeRoundTrips(t, originalRoot, expectedRoot, []Operation{operation})
}

func TestRecipeReplayRoundTripsMixedOrderedOperations(t *testing.T) {
	originalRoot := filepath.Join(t.TempDir(), "original")
	expectedRoot := filepath.Join(t.TempDir(), "expected")
	original := []byte(" ---   \ndescription: Demo\n---\n")
	normalized := []byte("---\ndescription: Demo\n---\n")
	insertion := []byte("name: review\n")
	expected := append(append([]byte(nil), normalized[:4]...), append(insertion, normalized[4:]...)...)
	writeTestFile(t, originalRoot, "skill.md", string(original))
	writeTestFile(t, expectedRoot, "SKILL.md", string(expected))
	chmodTestPath(t, filepath.Join(originalRoot, "skill.md"), 0o754)
	chmodTestPath(t, filepath.Join(expectedRoot, "SKILL.md"), 0o754)
	rename, err := NewRenameOperation("skill.md", "SKILL.md", artifact.HashFileContent(original), 0o754)
	if err != nil {
		t.Fatalf("NewRenameOperation() error: %v", err)
	}
	replace, err := NewReplaceBytesOperation(
		"SKILL.md",
		0,
		[]byte(" ---   "),
		[]byte("---"),
		artifact.HashFileContent(original),
		artifact.HashFileContent(normalized),
	)
	if err != nil {
		t.Fatalf("NewReplaceBytesOperation() error: %v", err)
	}
	set, err := NewSetFrontmatterStringOperation(
		"SKILL.md",
		"name",
		nil,
		"review",
		4,
		nil,
		insertion,
		artifact.HashFileContent(normalized),
		artifact.HashFileContent(expected),
	)
	if err != nil {
		t.Fatalf("NewSetFrontmatterStringOperation() error: %v", err)
	}
	assertRecipeRoundTrips(t, originalRoot, expectedRoot, []Operation{rename, replace, set})
}

func assertRecipeRoundTrips(
	t *testing.T,
	originalRoot string,
	expectedRoot string,
	operations []Operation,
) {
	t.Helper()
	input, originalView := testArtifact(t, originalRoot)
	_, expectedView := testArtifact(t, expectedRoot)
	expectedHash, err := expectedView.Hash(context.Background())
	if err != nil {
		t.Fatalf("expected Hash() error: %v", err)
	}
	output, err := artifact.NewExactIdentity(
		input.SourceID(),
		input.ResolvedRef(),
		artifact.ArtifactKindDirectory,
		expectedHash,
	)
	if err != nil {
		t.Fatalf("NewExactIdentity(output) error: %v", err)
	}
	recipe, err := NewRecipe(input, output, operations)
	if err != nil {
		t.Fatalf("NewRecipe() error: %v", err)
	}

	forward, err := Replay(context.Background(), recipe, originalView)
	if err != nil {
		t.Fatalf("forward Replay() error: %v", err)
	}
	t.Cleanup(func() { releaseResult(t, forward) })
	forwardView := resultView(t, forward)
	assertTestViewsEqual(t, expectedView, forwardView)

	inverse, err := recipe.Inverse()
	if err != nil {
		t.Fatalf("Inverse() error: %v", err)
	}
	restored, err := Replay(context.Background(), inverse, forwardView)
	if err != nil {
		t.Fatalf("inverse Replay() error: %v", err)
	}
	t.Cleanup(func() { releaseResult(t, restored) })
	restoredView := resultView(t, restored)
	assertTestViewsEqual(t, originalView, restoredView)

	forwardAgain, err := Replay(context.Background(), recipe, restoredView)
	if err != nil {
		t.Fatalf("second forward Replay() error: %v", err)
	}
	t.Cleanup(func() { releaseResult(t, forwardAgain) })
	assertTestViewsEqual(t, expectedView, resultView(t, forwardAgain))
}

func chmodTestPath(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("Chmod(%q) error: %v", path, err)
	}
}
