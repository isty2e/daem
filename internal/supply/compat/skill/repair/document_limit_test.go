package repair

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
	skillcompat "github.com/isty2e/daem/internal/supply/compat/skill"
	"github.com/isty2e/daem/internal/target"
)

const testMaximumSkillDocumentBytes = int(skillcompat.MaximumSkillDocumentBytes)

func TestRepairDocumentReadsBoundUpperAndLowercase(t *testing.T) {
	exactRoot := t.TempDir()
	writeSizedTestFile(t, exactRoot, "SKILL.md", testMaximumSkillDocumentBytes)
	if _, err := skillDocumentState(context.Background(), exactRoot, "SKILL.md"); err != nil {
		t.Fatalf("read exact SKILL.md boundary: %v", err)
	}

	uppercaseRoot := t.TempDir()
	writeSparseTestFile(t, uppercaseRoot, "SKILL.md", int64(testMaximumSkillDocumentBytes+1))
	requireRepairDocumentLimit(t, mustSkillDocumentStateError(t, uppercaseRoot, "SKILL.md"))

	lowercaseRoot := t.TempDir()
	writeSparseTestFile(t, lowercaseRoot, "skill.md", int64(testMaximumSkillDocumentBytes+1))
	requireRepairDocumentLimit(t, mustSkillDocumentStateError(t, lowercaseRoot, "skill.md"))
}

func TestRepairAndReplayShareExactSkillDocumentBoundary(t *testing.T) {
	root := filepath.Join(t.TempDir(), "original")
	content := skillDocumentWithoutName(t, testMaximumSkillDocumentBytes-len("name: review\n"))
	writeTestFile(t, root, "SKILL.md", string(content))
	input, view := testArtifact(t, root)

	repaired, err := Repair(
		context.Background(),
		input,
		view,
		"review",
		[]target.Target{target.TargetCodex},
	)
	if err != nil {
		t.Fatalf("Repair exact output boundary: %v", err)
	}
	t.Cleanup(func() { releaseResult(t, repaired) })
	recipe, ok := repaired.Recipe()
	if !ok {
		t.Fatal("Repair produced no recipe")
	}
	repairedView := resultView(t, repaired)
	repairedDocument, err := skillcompat.ReadSkillDocument(
		context.Background(),
		repairedView,
		"SKILL.md",
	)
	if err != nil {
		t.Fatalf("read repaired exact boundary: %v", err)
	}
	repairedBytes := repairedDocument.Bytes()
	if len(repairedBytes) != testMaximumSkillDocumentBytes {
		t.Fatalf("repaired bytes = %d, want exact boundary", len(repairedBytes))
	}

	replayed, err := Replay(context.Background(), recipe, view)
	if err != nil {
		t.Fatalf("Replay exact output boundary: %v", err)
	}
	t.Cleanup(func() { releaseResult(t, replayed) })
	if !replayed.Identity().Equal(repaired.Identity()) {
		t.Fatalf("replay identity = %#v, want %#v", replayed.Identity(), repaired.Identity())
	}
}

func TestRepairRejectsOutputOneByteOverBeforeRecipeOrStagingPublication(t *testing.T) {
	root := filepath.Join(t.TempDir(), "original")
	content := skillDocumentWithoutName(t, testMaximumSkillDocumentBytes-len("name: review\n")+1)
	writeTestFile(t, root, "SKILL.md", string(content))
	input, view := testArtifact(t, root)
	repairTemp := t.TempDir()
	t.Setenv("TMPDIR", repairTemp)

	_, err := Repair(
		context.Background(),
		input,
		view,
		"review",
		[]target.Target{target.TargetCodex},
	)
	requireRepairDocumentLimit(t, err)
	if got := readTestFile(t, root, "SKILL.md"); got != string(content) {
		t.Fatal("failed repair mutated original SKILL.md")
	}
	entries, readErr := os.ReadDir(repairTemp)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("failed repair left staging entries: %#v", entries)
	}
}

func TestRepairRejectsOversizedInputAsTypedLimit(t *testing.T) {
	for _, relativePath := range []string{"SKILL.md", "skill.md"} {
		t.Run(relativePath, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "original")
			writeSparseTestFile(t, root, relativePath, int64(testMaximumSkillDocumentBytes+1))
			input, view := testArtifact(t, root)
			repairTemp := t.TempDir()
			t.Setenv("TMPDIR", repairTemp)

			_, err := Repair(
				context.Background(),
				input,
				view,
				"review",
				[]target.Target{target.TargetCodex},
			)
			requireRepairDocumentLimit(t, err)
			entries, readErr := os.ReadDir(repairTemp)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if len(entries) != 0 {
				t.Fatalf("failed input repair left staging entries: %#v", entries)
			}
		})
	}
}

func TestApplyRejectsOversizedSkillDocumentReplacementBeforeWrite(t *testing.T) {
	root := t.TempDir()
	input := bytes.Repeat([]byte("a"), testMaximumSkillDocumentBytes)
	writeTestFile(t, root, "SKILL.md", string(input))
	output := append(append([]byte(nil), input[:len(input)-1]...), []byte("bb")...)
	operation, err := NewReplaceBytesOperation(
		"SKILL.md",
		len(input)-1,
		[]byte("a"),
		[]byte("bb"),
		artifact.HashFileContent(input),
		artifact.HashFileContent(output),
	)
	if err != nil {
		t.Fatal(err)
	}

	err = applyOperation(context.Background(), root, operation)
	requireRepairDocumentLimit(t, err)
	if got := readTestFile(t, root, "SKILL.md"); got != string(input) {
		t.Fatal("over-limit replay primitive wrote SKILL.md before rejection")
	}
}

func TestOperationRejectsOversizedReplacementOperand(t *testing.T) {
	oversized := bytes.Repeat([]byte("x"), testMaximumSkillDocumentBytes+1)
	_, err := NewReplaceBytesOperation(
		"SKILL.md",
		0,
		nil,
		oversized,
		artifact.HashFileContent(nil),
		artifact.HashFileContent(oversized),
	)
	requireRepairDocumentLimit(t, err)
}

func TestSkillDocumentReplacementSizeBoundary(t *testing.T) {
	if err := checkSkillDocumentReplacementSize(
		testMaximumSkillDocumentBytes,
		1,
		1,
	); err != nil {
		t.Fatalf("exact output boundary: %v", err)
	}
	requireRepairDocumentLimit(t, checkSkillDocumentReplacementSize(
		testMaximumSkillDocumentBytes,
		1,
		2,
	))
}

func TestApplyRejectsOversizedRenameCandidateBeforeDestinationMutation(t *testing.T) {
	root := t.TempDir()
	writeSparseTestFile(t, root, "skill.md", int64(testMaximumSkillDocumentBytes+1))
	operation, err := NewRenameOperation(
		"skill.md",
		"SKILL.md",
		artifact.HashFileContent(nil),
		0o600,
	)
	if err != nil {
		t.Fatal(err)
	}

	err = applyOperation(context.Background(), root, operation)
	requireRepairDocumentLimit(t, err)
	if !directoryHasEntry(t, root, "skill.md") || directoryHasEntry(t, root, "SKILL.md") {
		t.Fatal("over-limit rename mutated document paths before rejection")
	}
}

func TestRepairAcceptsLargeUnrelatedAttachmentAndExactLowercaseRename(t *testing.T) {
	root := filepath.Join(t.TempDir(), "original")
	prefix := []byte("---\nname: review\ndescription: Demo\n---\n")
	document := append(prefix, bytes.Repeat([]byte("x"), testMaximumSkillDocumentBytes-len(prefix))...)
	writeTestFile(t, root, "skill.md", string(document))
	writeSizedTestFile(t, root, "assets/model.bin", testMaximumSkillDocumentBytes+1)
	input, view := testArtifact(t, root)

	result, err := Repair(
		context.Background(),
		input,
		view,
		"review",
		[]target.Target{target.TargetCodex},
	)
	if err != nil {
		t.Fatalf("Repair lowercase exact boundary with large attachment: %v", err)
	}
	t.Cleanup(func() { releaseResult(t, result) })
	recipe, ok := result.Recipe()
	if !ok || len(recipe.Operations()) != 1 || recipe.Operations()[0].Kind() != OperationRename {
		t.Fatalf("recipe = %#v, want one casing rename", recipe)
	}
}

func TestReplayRejectsSourceReplacedByOversizedDocumentWithoutStagingResidue(t *testing.T) {
	root := filepath.Join(t.TempDir(), "original")
	writeTestFile(t, root, "SKILL.md", "---\ndescription: Demo\n---\n")
	input, view := testArtifact(t, root)
	repaired, err := Repair(
		context.Background(),
		input,
		view,
		"review",
		[]target.Target{target.TargetCodex},
	)
	if err != nil {
		t.Fatal(err)
	}
	recipe, ok := repaired.Recipe()
	if !ok {
		t.Fatal("Repair produced no recipe")
	}
	releaseResult(t, repaired)

	writeSparseTestFile(t, root, "SKILL.md", int64(testMaximumSkillDocumentBytes+1))
	replayTemp := t.TempDir()
	t.Setenv("TMPDIR", replayTemp)
	if _, err := Replay(context.Background(), recipe, view); err == nil {
		t.Fatal("Replay accepted source replaced after recipe planning")
	}
	entries, err := os.ReadDir(replayTemp)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed replay left staging entries: %#v", entries)
	}
}

func skillDocumentWithoutName(t *testing.T, size int) []byte {
	t.Helper()
	prefix := []byte("---\ndescription: Demo\n---\n")
	if size < len(prefix) {
		t.Fatalf("requested document size %d is below prefix size %d", size, len(prefix))
	}
	return append(prefix, bytes.Repeat([]byte("x"), size-len(prefix))...)
}

func writeSizedTestFile(t *testing.T, root string, relativePath string, size int) {
	t.Helper()
	writeTestFile(t, root, relativePath, string(bytes.Repeat([]byte("x"), size)))
}

func writeSparseTestFile(t *testing.T, root string, relativePath string, size int64) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(size); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func mustSkillDocumentStateError(t *testing.T, root string, relativePath string) error {
	t.Helper()
	_, err := skillDocumentState(context.Background(), root, relativePath)
	if err == nil {
		t.Fatalf("skillDocumentState(%q) returned nil error", relativePath)
	}
	return err
}

func requireRepairDocumentLimit(t *testing.T, err error) {
	t.Helper()
	var limitErr *skillcompat.SkillDocumentLimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("error = %v, want SkillDocumentLimitError", err)
	}
	if limitErr.Limit() != int64(testMaximumSkillDocumentBytes) ||
		limitErr.Observed() != int64(testMaximumSkillDocumentBytes+1) {
		t.Fatalf(
			"limit error = limit %d observed %d",
			limitErr.Limit(),
			limitErr.Observed(),
		)
	}
}
