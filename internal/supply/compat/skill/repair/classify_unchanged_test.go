package repair

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	skillcompat "github.com/isty2e/daem/internal/supply/compat/skill"
	"github.com/isty2e/daem/internal/target"
)

const unchangedSkillDocument = "---\nname: review\ndescription: Demo skill\n---\nBody\n"

func TestClassifyUnchangedDoesNotRequireTemporaryStorage(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "SKILL.md", unchangedSkillDocument)
	writeTestFile(t, root, "references/large.txt", strings.Repeat("r", 2*testMaximumSkillDocumentBytes))
	input, view := testArtifact(t, root)
	t.Setenv("TMPDIR", filepath.Join(root, "absent-temp"))

	classification, err := Classify(context.Background(), input, view, "review", target.SupportedTargets())
	if err != nil || classification.Repairability() != RepairabilityNone {
		t.Fatalf("classify without temporary storage = %#v, %v", classification, err)
	}
	if err := view.Verify(context.Background(), input); err != nil {
		t.Fatalf("classification changed source: %v", err)
	}
}

func TestClassifyUnchangedVerifiesWholeInputAndAllowsFreshRetry(t *testing.T) {
	for _, changed := range []string{"SKILL.md", "references/data.txt", "executable-mode"} {
		t.Run(changed, func(t *testing.T) {
			root := t.TempDir()
			writeTestFile(t, root, "SKILL.md", unchangedSkillDocument)
			writeTestFile(t, root, "references/data.txt", "before")
			input, view := testArtifact(t, root)
			if changed == "executable-mode" {
				if err := os.Chmod(filepath.Join(root, "references/data.txt"), 0o700); err != nil {
					t.Fatal(err)
				}
			} else if changed == "SKILL.md" {
				writeTestFile(t, root, changed, unchangedSkillDocument+"After\n")
			} else {
				writeTestFile(t, root, changed, "after")
			}

			classification, err := Classify(context.Background(), input, view, "review", []target.Target{target.TargetCodex})
			if err == nil || classification.Repairability() != "" {
				t.Fatalf("stale input produced classification: %#v, %v", classification, err)
			}
			freshInput, freshView := testArtifact(t, root)
			classification, err = Classify(context.Background(), freshInput, freshView, "review", []target.Target{target.TargetCodex})
			if err != nil || classification.Repairability() != RepairabilityNone {
				t.Fatalf("fresh retry = %#v, %v", classification, err)
			}
		})
	}
}

func TestClassifyMatchesMaterializedRepair(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		content  string
	}{
		{"valid", "SKILL.md", unchangedSkillDocument},
		{"crlf", "SKILL.md", strings.ReplaceAll(unchangedSkillDocument, "\n", "\r\n")},
		{"lowercase", "skill.md", unchangedSkillDocument},
		{"bom", "SKILL.md", "\xef\xbb\xbf" + unchangedSkillDocument},
		{"padded-delimiter", "SKILL.md", " --- \t" + unchangedSkillDocument[3:]},
		{"missing-name", "SKILL.md", "---\ndescription: Demo skill\n---\n"},
		{"null-name", "SKILL.md", "---\nname: null\ndescription: Demo skill\n---\n"},
		{"different-name", "SKILL.md", strings.ReplaceAll(unchangedSkillDocument, "review", "different")},
		{"invalid-name", "SKILL.md", strings.ReplaceAll(unchangedSkillDocument, "review", "Not_Portable")},
		{"long-name", "SKILL.md", strings.ReplaceAll(unchangedSkillDocument, "review", strings.Repeat("n", 80))},
		{"missing-description", "SKILL.md", "---\nname: review\n---\n"},
		{"long-description", "SKILL.md", "---\nname: review\ndescription: " + strings.Repeat("d", 1500) + "\n---\n"},
		{"warning", "SKILL.md", "---\nname: review\ndescription: Demo skill\nunknown: value\n---\n"},
		{"malformed", "SKILL.md", "---\nname: [\n---\n"},
		{"duplicate-field", "SKILL.md", "---\nname: review\nname: again\n---\n"},
		{"no-frontmatter", "SKILL.md", "Body\n"},
		{"nested-only", "nested/SKILL.md", unchangedSkillDocument},
		{"wrong-casing", "Skill.md", unchangedSkillDocument},
	}
	targetSets := [][]target.Target{nil, target.SupportedTargets(), {target.TargetCodex, target.TargetCodex}, {target.Target("unknown")}}
	for _, selectedTarget := range target.SupportedTargets() {
		targetSets = append(targetSets, []target.Target{selectedTarget})
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			writeTestFile(t, root, testCase.filename, testCase.content)
			writeTestFile(t, root, "reference.txt", "reference\n")
			input, view := testArtifact(t, root)
			for _, targets := range targetSets {
				t.Run(fmt.Sprint(targets), func(t *testing.T) {
					assertClassificationMatchesRepair(t, context.Background(), input, view, "review", targets)
				})
			}
			if err := view.Verify(context.Background(), input); err != nil {
				t.Fatalf("classification/repair changed source: %v", err)
			}
		})
	}
}

func TestClassifyInputFailuresMatchRepair(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "SKILL.md", unchangedSkillDocument)
	input, view := testArtifact(t, root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assertClassificationMatchesRepair(t, nil, input, view, "review", nil)
	assertClassificationMatchesRepair(t, ctx, input, view, "review", nil)
	assertClassificationMatchesRepair(t, context.Background(), artifact.ExactIdentity{}, view, "review", nil)
	assertClassificationMatchesRepair(t, context.Background(), input, access.View{}, "review", nil)
	assertClassificationMatchesRepair(t, context.Background(), input, view, "../review", nil)
}

func TestClassifyDocumentLimitAndRepairFallback(t *testing.T) {
	for _, extra := range []int{0, 1} {
		t.Run(fmt.Sprint(extra), func(t *testing.T) {
			root := t.TempDir()
			content := unchangedSkillDocument + strings.Repeat("x", testMaximumSkillDocumentBytes-len(unchangedSkillDocument)+extra)
			writeTestFile(t, root, "SKILL.md", content)
			input, view := testArtifact(t, root)
			assertClassificationMatchesRepair(t, context.Background(), input, view, "review", []target.Target{target.TargetCodex})
		})
	}
	root := t.TempDir()
	writeTestFile(t, root, "skill.md", unchangedSkillDocument)
	input, view := testArtifact(t, root)
	t.Setenv("TMPDIR", filepath.Join(root, "absent-temp"))
	classification, err := Classify(context.Background(), input, view, "review", []target.Target{target.TargetCodex})
	if !errors.Is(err, os.ErrNotExist) || classification.Repairability() != "" {
		t.Fatalf("repair fallback hid unavailable staging: %#v, %v", classification, err)
	}
}

func TestClassificationDocumentSinkBoundsRetainedBytes(t *testing.T) {
	sink := classificationDocumentSink{}
	writer, err := sink.OpenFile("SKILL.md", 0o600, skillcompat.MaximumSkillDocumentBytes)
	if err != nil {
		t.Fatal(err)
	}
	content := bytes.Repeat([]byte("x"), testMaximumSkillDocumentBytes)
	if n, err := writer.Write(content); err != nil || n != len(content) {
		t.Fatalf("exact boundary write = %d, %v", n, err)
	}
	if n, err := writer.Write([]byte("x")); n != 0 || !errors.Is(err, skillcompat.ErrSkillDocumentTooLarge) {
		t.Fatalf("over boundary write = %d, %v", n, err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sink.document, content) {
		t.Fatal("refused write changed captured bytes")
	}
	for _, relativePath := range []string{"reference.txt", "nested/SKILL.md", "skill.md", "SKILL.md"} {
		other := classificationDocumentSink{}
		writer, err := other.OpenFile(relativePath, 0o600, skillcompat.MaximumSkillDocumentBytes+1)
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if n, err := writer.Write(content); err != nil || n != len(content) {
				t.Fatalf("discard %s = %d, %v", relativePath, n, err)
			}
		}
		if err := writer.Close(); err != nil || len(other.document) != 0 {
			t.Fatalf("noncandidate retained bytes: %d, %v", len(other.document), err)
		}
	}
}

func TestClassificationDocumentSinkRejectsObservedSizeDrift(t *testing.T) {
	const observedSize = 704_512
	sink := classificationDocumentSink{}
	writer, err := sink.OpenFile("SKILL.md", 0o600, observedSize)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(bytes.Repeat([]byte("x"), observedSize)); err != nil {
		t.Fatal(err)
	}
	if n, err := writer.Write([]byte("x")); n != 0 || err == nil {
		t.Fatalf("size drift admitted: %d, %v", n, err)
	}
	if len(sink.document) != observedSize || cap(sink.document) > testMaximumSkillDocumentBytes {
		t.Fatalf("size drift changed retained projection: len=%d cap=%d", len(sink.document), cap(sink.document))
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
}

func assertClassificationMatchesRepair(
	t *testing.T,
	ctx context.Context,
	input artifact.ExactIdentity,
	view access.View,
	installName string,
	targets []target.Target,
) {
	t.Helper()
	result, repairErr := Repair(ctx, input, view, installName, targets)
	want := Classification{repairability: RepairabilityNone}
	if manual, ok := repairErr.(ManualError); ok {
		want = Classification{repairability: RepairabilityManual, actions: manual.Actions(), manualReasons: manual.Reasons()}
		repairErr = nil
	} else if repairErr == nil {
		if recipe, ok := result.Recipe(); ok {
			want = Classification{repairability: RepairabilityMechanical, actions: recipe.Actions()}
		}
		if err := result.Release(); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Classify(ctx, input, view, installName, targets)
	if repairErr != nil {
		if err == nil || err.Error() != repairErr.Error() || got.Repairability() != "" {
			t.Fatalf("Classify = %#v, %v; want Repair error %v", got, err, repairErr)
		}
		return
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Classify = %#v, %v; want materialized Repair %#v", got, err, want)
	}
}
