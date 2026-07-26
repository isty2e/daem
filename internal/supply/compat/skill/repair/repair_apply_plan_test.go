package repair

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

func TestOperationConstructorRejectsPathTraversal(t *testing.T) {
	_, err := NewReplaceBytesOperation(
		"../SKILL.md",
		0,
		[]byte("old"),
		[]byte("new"),
		artifact.HashFileContent([]byte("old")),
		artifact.HashFileContent([]byte("new")),
	)
	if err == nil {
		t.Fatal("NewReplaceBytesOperation returned nil error")
	}
	if !strings.Contains(err.Error(), "canonical relative file path") {
		t.Fatalf("error = %q, want path traversal rejection", err)
	}
}

func TestReplayFailsWhenSourceChangedAfterIdentityWasComputed(t *testing.T) {
	originalRoot := filepath.Join(t.TempDir(), "original")
	writeTestFile(t, originalRoot, "SKILL.md", "---\ndescription: Demo skill\n---\nBody\n")

	input, view := testArtifact(t, originalRoot)
	result, err := Repair(
		context.Background(),
		input,
		view,
		"review",
		[]target.Target{target.TargetCodex},
	)
	if err != nil {
		t.Fatalf("Repair returned error: %v", err)
	}
	t.Cleanup(func() { releaseResult(t, result) })
	recipe, ok := result.Recipe()
	if !ok {
		t.Fatal("Recipe() reports unchanged result")
	}

	writeTestFile(t, originalRoot, "SKILL.md", "---\ndescription: Changed\n---\nBody\n")
	_, err = Replay(context.Background(), recipe, view)
	if err == nil {
		t.Fatal("Replay returned nil error")
	}
	if !strings.Contains(err.Error(), "content hash") {
		t.Fatalf("error = %q, want hash precondition mismatch", err.Error())
	}
}
