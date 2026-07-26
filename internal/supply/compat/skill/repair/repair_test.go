package repair

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/target"
)

func TestRepairBuildsRecipeAndReplaysBothDirections(t *testing.T) {
	originalRoot := filepath.Join(t.TempDir(), "original")
	writeTestFile(t, originalRoot, "skill.md", " ---   \ndescription: Demo skill\n---\nBody\n")

	input, originalView := testArtifact(t, originalRoot)
	result, err := Repair(
		context.Background(),
		input,
		originalView,
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
	if operations := recipe.Operations(); len(operations) != 3 {
		t.Fatalf("operations = %#v, want rename, delimiter, name insert", operations)
	}
	if recipe.Hash() == "" {
		t.Fatal("Recipe.Hash() is empty")
	}
	repairedView := resultView(t, result)
	repairedContent := readTestViewFile(t, repairedView, "SKILL.md")
	if !strings.HasPrefix(repairedContent, "---\nname: review\ndescription: Demo skill\n---\n") {
		t.Fatalf("repaired SKILL.md = %q", repairedContent)
	}
	if directoryHasEntry(t, originalRoot, "SKILL.md") {
		t.Fatal("Repair mutated original source")
	}

	replayed, err := Replay(context.Background(), recipe, originalView)
	if err != nil {
		t.Fatalf("Replay returned error: %v", err)
	}
	t.Cleanup(func() { releaseResult(t, replayed) })
	if !replayed.Identity().Equal(result.Identity()) {
		t.Fatalf("replayed identity = %#v, want %#v", replayed.Identity(), result.Identity())
	}
	if replayedContent := readTestViewFile(t, resultView(t, replayed), "SKILL.md"); replayedContent != repairedContent {
		t.Fatalf("replayed content = %q, want %q", replayedContent, repairedContent)
	}

	inverse, err := recipe.Inverse()
	if err != nil {
		t.Fatalf("Inverse returned error: %v", err)
	}
	restored, err := Replay(context.Background(), inverse, repairedView)
	if err != nil {
		t.Fatalf("inverse Replay returned error: %v", err)
	}
	t.Cleanup(func() { releaseResult(t, restored) })
	if !restored.Identity().Equal(input) {
		t.Fatalf("restored identity = %#v, want %#v", restored.Identity(), input)
	}

	forwardAgain, err := Replay(context.Background(), recipe, resultView(t, restored))
	if err != nil {
		t.Fatalf("second forward Replay returned error: %v", err)
	}
	t.Cleanup(func() { releaseResult(t, forwardAgain) })
	if !forwardAgain.Identity().Equal(result.Identity()) {
		t.Fatalf("second forward identity = %#v, want %#v", forwardAgain.Identity(), result.Identity())
	}
}

func TestRepairAlignsStrictNameWithoutTouchingReferences(t *testing.T) {
	originalRoot := filepath.Join(t.TempDir(), "original")
	writeTestFile(t, originalRoot, "SKILL.md", "---\nname: other\ndescription: Demo skill\n---\nBody\n")
	writeTestFile(t, originalRoot, "scripts/run.sh", "#!/bin/sh\necho ok\n")
	writeTestFile(t, originalRoot, "references/guide.md", "reference\n")

	input, view := testArtifact(t, originalRoot)
	result, err := Repair(
		context.Background(),
		input,
		view,
		"review",
		[]target.Target{target.TargetOpenCode},
	)
	if err != nil {
		t.Fatalf("Repair returned error: %v", err)
	}
	t.Cleanup(func() { releaseResult(t, result) })
	recipe, ok := result.Recipe()
	if !ok {
		t.Fatal("Recipe() reports unchanged result")
	}
	operations := recipe.Operations()
	if len(operations) != 1 || operations[0].Kind() != OperationSetFrontmatterString {
		t.Fatalf("operations = %#v, want one set_frontmatter_string", operations)
	}
	repairedView := resultView(t, result)
	if content := readTestViewFile(t, repairedView, "SKILL.md"); !strings.Contains(content, "name: review\n") {
		t.Fatalf("repaired SKILL.md = %q, want aligned name", content)
	}
	if script := readTestViewFile(t, repairedView, "scripts/run.sh"); script != "#!/bin/sh\necho ok\n" {
		t.Fatalf("script content changed: %q", script)
	}
	if reference := readTestViewFile(t, repairedView, "references/guide.md"); reference != "reference\n" {
		t.Fatalf("reference content changed: %q", reference)
	}
}

func TestRepairRecordsPresentNullNameAsExistingOldScalar(t *testing.T) {
	originalRoot := filepath.Join(t.TempDir(), "original")
	writeTestFile(t, originalRoot, "SKILL.md", "---\nname: null\ndescription: Demo skill\n---\n")
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
	recipe, present := result.Recipe()
	if !present || len(recipe.Operations()) != 1 {
		t.Fatalf("recipe = %#v, want one name repair", recipe)
	}
	body, ok := recipe.Operations()[0].SetFrontmatterString()
	if !ok {
		t.Fatalf("operation = %#v, want set_frontmatter_string", recipe.Operations()[0])
	}
	oldValue, oldValuePresent := body.OldValue()
	if !oldValuePresent || oldValue != "" {
		t.Fatalf("old value = %q, %t, want present normalized null", oldValue, oldValuePresent)
	}
	if content := readTestViewFile(t, resultView(t, result), "SKILL.md"); !strings.Contains(content, "name: review\n") {
		t.Fatalf("repaired SKILL.md = %q, want review name", content)
	}
}

func TestRepairRemovesBOMWhenNormalizingDelimiter(t *testing.T) {
	originalRoot := filepath.Join(t.TempDir(), "original")
	writeTestFile(t, originalRoot, "skill.md", "\xef\xbb\xbf ---   \r\ndescription: Demo skill\r\n---\r\nBody\r\n")

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
	if !ok || len(recipe.Operations()) != 3 {
		t.Fatalf("recipe = %#v, want rename, delimiter, name insert", recipe)
	}
	repairedContent := readTestViewFile(t, resultView(t, result), "SKILL.md")
	if !strings.HasPrefix(repairedContent, "---\r\nname: review\r\ndescription: Demo skill\r\n---\r\n") {
		t.Fatalf("repaired SKILL.md = %q, want BOM removed and CRLF preserved", repairedContent)
	}
}

func TestRepairRejectsUnsafeInstallNameBeforeCopying(t *testing.T) {
	originalRoot := filepath.Join(t.TempDir(), "original")
	writeTestFile(t, originalRoot, "SKILL.md", "---\nname: review\ndescription: Demo skill\n---\n")
	input, view := testArtifact(t, originalRoot)
	_, err := Repair(
		context.Background(),
		input,
		view,
		"../review",
		[]target.Target{target.TargetCodex},
	)
	if err == nil {
		t.Fatal("Repair returned nil error")
	}
	if !strings.Contains(err.Error(), "safe single-segment skill name") {
		t.Fatalf("error = %q, want safe install name diagnostic", err)
	}
	if content := readTestFile(t, originalRoot, "SKILL.md"); !strings.Contains(content, "name: review") {
		t.Fatalf("source content changed: %q", content)
	}
}

func TestRepairRejectsControlCharacterInstallNameBeforeCopying(t *testing.T) {
	originalRoot := filepath.Join(t.TempDir(), "original")
	writeTestFile(t, originalRoot, "SKILL.md", "---\nname: review\ndescription: Demo skill\n---\n")
	input, view := testArtifact(t, originalRoot)

	_, err := Repair(
		context.Background(),
		input,
		view,
		"review\ninjected",
		[]target.Target{target.TargetCodex},
	)
	if err == nil {
		t.Fatal("Repair returned nil error")
	}
	if !strings.Contains(err.Error(), "safe single-segment skill name") {
		t.Fatalf("error = %q, want unsafe control-character rejection", err)
	}
	if content := readTestFile(t, originalRoot, "SKILL.md"); content != "---\nname: review\ndescription: Demo skill\n---\n" {
		t.Fatalf("source content changed before install-name rejection: %q", content)
	}
}

func TestRepairQuotesYAMLAmbiguousInstallName(t *testing.T) {
	originalRoot := filepath.Join(t.TempDir(), "original")
	writeTestFile(t, originalRoot, "SKILL.md", "---\ndescription: Demo skill\n---\n")
	input, view := testArtifact(t, originalRoot)

	result, err := Repair(
		context.Background(),
		input,
		view,
		"null",
		[]target.Target{target.TargetCodex},
	)
	if err != nil {
		t.Fatalf("Repair returned error: %v", err)
	}
	t.Cleanup(func() { releaseResult(t, result) })
	content := readTestViewFile(t, resultView(t, result), "SKILL.md")
	if !strings.Contains(content, "name: \"null\"\n") {
		t.Fatalf("repaired SKILL.md = %q, want YAML string scalar", content)
	}
}

func TestRepairRefusesMissingDescription(t *testing.T) {
	originalRoot := filepath.Join(t.TempDir(), "original")
	writeTestFile(t, originalRoot, "SKILL.md", "---\nname: review\n---\nBody\n")

	input, view := testArtifact(t, originalRoot)
	_, err := Repair(
		context.Background(),
		input,
		view,
		"review",
		[]target.Target{target.TargetCodex},
	)
	if err == nil {
		t.Fatal("Repair returned nil error")
	}
	var manual ManualError
	if !errors.As(err, &manual) {
		t.Fatalf("error = %T %v, want ManualError", err, err)
	}
	if len(manual.Reasons()) == 0 || !strings.Contains(manual.Error(), "description is required") {
		t.Fatalf("manual error = %v, want description guidance", manual)
	}
}

func TestRepairRefusesComplexFrontmatterName(t *testing.T) {
	originalRoot := filepath.Join(t.TempDir(), "original")
	writeTestFile(t, originalRoot, "SKILL.md", "---\nname:\n  nested: review\ndescription: Demo skill\n---\nBody\n")

	input, view := testArtifact(t, originalRoot)
	_, err := Repair(
		context.Background(),
		input,
		view,
		"review",
		[]target.Target{target.TargetOpenCode},
	)
	if err == nil {
		t.Fatal("Repair returned nil error")
	}
	var manual ManualError
	if !errors.As(err, &manual) {
		t.Fatalf("error = %T %v, want ManualError", err, err)
	}
	if !strings.Contains(manual.Error(), "must be a string") {
		t.Fatalf("manual error = %v, want complex name refusal", manual)
	}
}

func TestRepairReturnsOriginalViewWhenNoOperationIsNeeded(t *testing.T) {
	originalRoot := filepath.Join(t.TempDir(), "original")
	writeTestFile(t, originalRoot, "SKILL.md", "---\nname: review\ndescription: Demo skill\n---\n")
	input, view := testArtifact(t, originalRoot)

	result, err := Repair(context.Background(), input, view, "review", []target.Target{target.TargetCodex})
	if err != nil {
		t.Fatalf("Repair returned error: %v", err)
	}
	if _, ok := result.Recipe(); ok {
		t.Fatal("Recipe() returned a recipe for unchanged result")
	}
	if !result.Identity().Equal(input) {
		t.Fatalf("identity = %#v, want %#v", result.Identity(), input)
	}
	if err := result.Release(); err != nil {
		t.Fatalf("unchanged Release() error: %v", err)
	}
}
