package declaration

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type fakeDeclaration struct {
	name      string
	signature string
	targets   []string
}

func TestApplyAddDeclarationReturnsStructuralOutcomes(t *testing.T) {
	original := []byte("version = 1\n\n[[widget]]\nname = \"lint\"\nsignature = \"same\"\ntargets = [\"codex\"]\n")
	codec := fakeDeclarationCodec()

	merged, err := ApplyAddDeclaration(AddEditInput[fakeDeclaration]{
		Original: original,
		Declaration: fakeDeclaration{
			name:      "lint",
			signature: "same",
			targets:   []string{"claude-code", "codex", "opencode"},
		},
		Codec: codec,
	})
	if err != nil {
		t.Fatalf("ApplyAddDeclaration merge returned error: %v", err)
	}
	if merged.Outcome != EditOutcomeMergeTargets {
		t.Fatalf("Outcome = %q, want %q", merged.Outcome, EditOutcomeMergeTargets)
	}
	if !strings.Contains(string(merged.Content), `targets = ["codex", "claude-code", "opencode"]`) {
		t.Fatalf("merged content = %s, want ordered merged values", merged.Content)
	}

	appended, err := ApplyAddDeclaration(AddEditInput[fakeDeclaration]{
		Original: original,
		Declaration: fakeDeclaration{
			name:      "format",
			signature: "same",
			targets:   []string{"codex"},
		},
		Codec: codec,
	})
	if err != nil {
		t.Fatalf("ApplyAddDeclaration append returned error: %v", err)
	}
	if appended.Outcome != EditOutcomeAppend {
		t.Fatalf("Outcome = %q, want %q", appended.Outcome, EditOutcomeAppend)
	}
	if !strings.Contains(string(appended.Content), `name = "format"`) {
		t.Fatalf("appended content = %s, want appended declaration", appended.Content)
	}
}

func TestApplyAddDeclarationRejectsDuplicateInheritedAndUnchangedValues(t *testing.T) {
	original := []byte("version = 1\n\n[[widget]]\nname = \"lint\"\nsignature = \"same\"\ntargets = [\"codex\"]\n\n[[widget]]\nname = \"inherited\"\nsignature = \"same\"\n")
	codec := fakeDeclarationCodec()

	if _, err := ApplyAddDeclaration(AddEditInput[fakeDeclaration]{
		Original: original,
		Declaration: fakeDeclaration{
			name:      "lint",
			signature: "different",
			targets:   []string{"claude-code"},
		},
		Codec: codec,
	}); err == nil || !strings.Contains(err.Error(), `duplicate widget "lint"`) {
		t.Fatalf("duplicate err = %v, want duplicate diagnostic", err)
	}

	if _, err := ApplyAddDeclaration(AddEditInput[fakeDeclaration]{
		Original: original,
		Declaration: fakeDeclaration{
			name:      "inherited",
			signature: "same",
			targets:   []string{"codex"},
		},
		Codec: codec,
	}); err == nil || !strings.Contains(err.Error(), `widget "inherited" inherits defaults`) {
		t.Fatalf("inherited err = %v, want inherited diagnostic", err)
	}

	if _, err := ApplyAddDeclaration(AddEditInput[fakeDeclaration]{
		Original: original,
		Declaration: fakeDeclaration{
			name:      "lint",
			signature: "same",
			targets:   []string{"codex"},
		},
		Codec: codec,
	}); err == nil || !strings.Contains(err.Error(), `widget "lint" already has selected values`) {
		t.Fatalf("unchanged err = %v, want unchanged diagnostic", err)
	}
}

func TestApplyAddDeclarationValidatesCodecAndPropagatesRendererError(t *testing.T) {
	_, err := ApplyAddDeclaration(AddEditInput[fakeDeclaration]{
		Original:    []byte("version = 1\n"),
		Declaration: fakeDeclaration{name: "lint"},
		Codec: AddEditContract[fakeDeclaration]{
			Kind: Kind("widget"),
		},
	})
	if err == nil || !strings.Contains(err.Error(), `declaration edit codec "widget" is incomplete`) {
		t.Fatalf("err = %v, want declaration edit codec diagnostic", err)
	}

	renderErr := errors.New("render failed")
	codec := fakeDeclarationCodec()
	codec.RenderBlockWithTargets = func(string, fakeDeclaration, fakeDeclaration, Targets, ManifestHeader) (string, error) {
		return "", renderErr
	}
	_, err = ApplyAddDeclaration(AddEditInput[fakeDeclaration]{
		Original: []byte("version = 1\n\n[[widget]]\nname = \"lint\"\nsignature = \"same\"\ntargets = [\"codex\"]\n"),
		Declaration: fakeDeclaration{
			name:      "lint",
			signature: "same",
			targets:   []string{"claude-code"},
		},
		Codec: codec,
	})
	if !errors.Is(err, renderErr) {
		t.Fatalf("err = %v, want renderer error", err)
	}
}

func TestApplyTargetRemovalReturnsStructuralOutcomes(t *testing.T) {
	original := []byte("before\n[[widget]]\nname = \"lint\"\ntargets = [\"codex\", \"claude-code\"]\nafter\n")
	start := strings.Index(string(original), "[[widget]]")
	end := strings.Index(string(original), "after")

	narrowed, err := ApplyTargetRemoval(TargetRemovalInput{
		Original:        original,
		Range:           DocumentRange{Start: start, End: end},
		ExistingTargets: Targets{"codex", "claude-code"},
		SelectedTargets: Targets{"codex"},
		RenderBlockWithTargets: func(originalBlock string, remainingTargets Targets) (string, error) {
			if !strings.Contains(originalBlock, `name = "lint"`) {
				t.Fatalf("originalBlock = %q, want lint block", originalBlock)
			}
			return renderFakeBlock(fakeDeclaration{name: "lint", targets: remainingTargets.Values()}), nil
		},
	})
	if err != nil {
		t.Fatalf("ApplyTargetRemoval narrow returned error: %v", err)
	}
	if narrowed.Outcome != EditOutcomeUpdateTargets {
		t.Fatalf("Outcome = %q, want %q", narrowed.Outcome, EditOutcomeUpdateTargets)
	}
	if string(narrowed.Content) != "before\n[[widget]]\nname = \"lint\"\ntargets = [\"claude-code\"]\nafter\n" {
		t.Fatalf("content = %q, want narrowed block with surrounding content preserved", narrowed.Content)
	}

	removed, err := ApplyTargetRemoval(TargetRemovalInput{
		Original:        original,
		Range:           DocumentRange{Start: start, End: end},
		ExistingTargets: Targets{"codex", "claude-code"},
		SelectedTargets: Targets{"codex", "claude-code"},
		RenderBlockWithTargets: func(string, Targets) (string, error) {
			t.Fatal("renderer should not be called when all values are removed")
			return "", nil
		},
	})
	if err != nil {
		t.Fatalf("ApplyTargetRemoval remove returned error: %v", err)
	}
	if removed.Outcome != EditOutcomeRemove {
		t.Fatalf("Outcome = %q, want %q", removed.Outcome, EditOutcomeRemove)
	}
	if string(removed.Content) != "before\nafter\n" {
		t.Fatalf("content = %q, want removed block", removed.Content)
	}

	removedWithoutSelection, err := ApplyTargetRemoval(TargetRemovalInput{
		Original:        original,
		Range:           DocumentRange{Start: start, End: end},
		ExistingTargets: Targets{"codex", "claude-code"},
		RenderBlockWithTargets: func(string, Targets) (string, error) {
			t.Fatal("renderer should not be called when no values are selected")
			return "", nil
		},
	})
	if err != nil {
		t.Fatalf("ApplyTargetRemoval remove without selection returned error: %v", err)
	}
	if removedWithoutSelection.Outcome != EditOutcomeRemove {
		t.Fatalf("Outcome = %q, want %q", removedWithoutSelection.Outcome, EditOutcomeRemove)
	}
	if string(removedWithoutSelection.Content) != "before\nafter\n" {
		t.Fatalf("content = %q, want removed block", removedWithoutSelection.Content)
	}
}

func TestApplyTargetRemovalRejectsMissingValuesAndPartialGuard(t *testing.T) {
	original := []byte("[[widget]]\nname = \"lint\"\ntargets = [\"codex\"]\n")

	_, err := ApplyTargetRemoval(TargetRemovalInput{
		Original:        original,
		Range:           DocumentRange{Start: 0, End: len(original)},
		ExistingTargets: Targets{"codex"},
		SelectedTargets: Targets{"claude-code"},
		NoSelectedTargetsError: func() error {
			return errors.New("selected values not present")
		},
	})
	if err == nil || err.Error() != "selected values not present" {
		t.Fatalf("err = %v, want selected values diagnostic", err)
	}

	guardErr := errors.New("partial edit rejected")
	_, err = ApplyTargetRemoval(TargetRemovalInput{
		Original:        original,
		Range:           DocumentRange{Start: 0, End: len(original)},
		ExistingTargets: Targets{"codex", "claude-code"},
		SelectedTargets: Targets{"codex"},
		AllowPartialTargetRemoval: func(Targets) error {
			return guardErr
		},
		RenderBlockWithTargets: func(string, Targets) (string, error) {
			t.Fatal("renderer should not be called after guard failure")
			return "", nil
		},
	})
	if !errors.Is(err, guardErr) {
		t.Fatalf("err = %v, want guard error", err)
	}
}

func TestApplyTargetRemovalPreprocessesBlockAndRequiresRenderer(t *testing.T) {
	original := []byte("[[widget]]\nname = \"lint\"\ntargets = [\"codex\", \"claude-code\"]\n")

	_, err := ApplyTargetRemoval(TargetRemovalInput{
		Original:        original,
		Range:           DocumentRange{Start: 0, End: len(original)},
		ExistingTargets: Targets{"codex", "claude-code"},
		SelectedTargets: Targets{"codex"},
	})
	if err == nil || err.Error() != "target renderer is required" {
		t.Fatalf("err = %v, want renderer diagnostic", err)
	}

	seenBlock := ""
	_, err = ApplyTargetRemoval(TargetRemovalInput{
		Original:        original,
		Range:           DocumentRange{Start: 0, End: len(original)},
		ExistingTargets: Targets{"codex", "claude-code"},
		SelectedTargets: Targets{"codex"},
		BeforeTargetReplace: func(originalBlock string) string {
			return strings.ReplaceAll(originalBlock, "lint", "checked")
		},
		RenderBlockWithTargets: func(originalBlock string, remainingTargets Targets) (string, error) {
			seenBlock = originalBlock
			return renderFakeBlock(fakeDeclaration{name: "checked", targets: remainingTargets.Values()}), nil
		},
	})
	if err != nil {
		t.Fatalf("ApplyTargetRemoval returned error: %v", err)
	}
	if !strings.Contains(seenBlock, `name = "checked"`) {
		t.Fatalf("seenBlock = %q, want preprocessed block", seenBlock)
	}
}

func TestRemoveTargetsPreservesRemainingOrderAndCopies(t *testing.T) {
	existing := Targets{"codex", "claude-code", "opencode"}
	remaining, removed := RemoveTargets(existing, Targets{"claude-code", "claude-code"})
	if !removed {
		t.Fatal("removed = false, want true")
	}
	if !reflect.DeepEqual(remaining.Values(), []string{"codex", "opencode"}) {
		t.Fatalf("remaining = %#v, want codex/opencode", remaining.Values())
	}

	remaining[0] = "mutated"
	if existing[0] != "codex" {
		t.Fatalf("existing = %#v, want original values preserved", existing)
	}

	unchanged, removed := RemoveTargets(existing, Targets{"missing"})
	if removed {
		t.Fatal("removed = true, want false")
	}
	if !reflect.DeepEqual(unchanged.Values(), existing.Values()) {
		t.Fatalf("unchanged = %#v, want copy of original", unchanged.Values())
	}
}

func fakeDeclarationCodec() AddEditContract[fakeDeclaration] {
	return AddEditContract[fakeDeclaration]{
		Kind: Kind("widget"),
		Scan: func(content []byte) ([]EditBlock[fakeDeclaration], error) {
			text := string(content)
			blocks := make([]EditBlock[fakeDeclaration], 0)
			offset := 0
			for {
				relativeStart := strings.Index(text[offset:], "[[widget]]")
				if relativeStart < 0 {
					break
				}
				start := offset + relativeStart
				next := strings.Index(text[start+1:], "\n[[widget]]")
				end := len(text)
				if next >= 0 {
					end = start + 1 + next
				}
				block := text[start:end]
				blocks = append(blocks, EditBlock[fakeDeclaration]{
					Range: DocumentRange{Start: start, End: end},
					Value: fakeDeclaration{
						name:      between(block, `name = "`, `"`),
						signature: between(block, `signature = "`, `"`),
						targets:   fakeTargets(block),
					},
				})
				offset = end
			}
			return blocks, nil
		},
		Key: func(value fakeDeclaration) (Key, error) {
			return Key{Kind: Kind("widget"), Name: value.name}, nil
		},
		ExplicitTargets: func(value fakeDeclaration) Targets {
			return Targets(value.targets)
		},
		SameIdentity: func(existing fakeDeclaration, incoming fakeDeclaration, _ ManifestHeader) bool {
			return existing.signature == incoming.signature
		},
		RenderBlock: func(value fakeDeclaration) string {
			return renderFakeBlock(value)
		},
		RenderBlockWithTargets: func(_ string, existing fakeDeclaration, _ fakeDeclaration, mergedTargets Targets, _ ManifestHeader) (string, error) {
			existing.targets = mergedTargets.Values()
			return renderFakeBlock(existing), nil
		},
		DuplicateError: func(key Key) error {
			return fmt.Errorf("duplicate %s %q", key.Kind, key.Name)
		},
		AlreadyExistsError: func(key Key) error {
			return fmt.Errorf("%s %q already exists", key.Kind, key.Name)
		},
		InheritsTargetsError: func(key Key) error {
			return fmt.Errorf("%s %q inherits defaults", key.Kind, key.Name)
		},
		AlreadyHasTargetsError: func(key Key) error {
			return fmt.Errorf("%s %q already has selected values", key.Kind, key.Name)
		},
	}
}

func renderFakeBlock(value fakeDeclaration) string {
	lines := []string{
		"[[widget]]",
		fmt.Sprintf("name = %q", value.name),
	}
	if value.signature != "" {
		lines = append(lines, fmt.Sprintf("signature = %q", value.signature))
	}
	lines = append(lines, "targets = "+literalArray(value.targets))
	return strings.Join(lines, "\n") + "\n"
}

func fakeTargets(block string) []string {
	start := strings.Index(block, `targets = [`)
	if start < 0 {
		return nil
	}
	start += len(`targets = [`)
	end := strings.Index(block[start:], `]`)
	if end < 0 {
		return nil
	}
	content := strings.TrimSpace(block[start : start+end])
	if content == "" {
		return nil
	}
	parts := strings.Split(content, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.Unquote(strings.TrimSpace(part))
		if err != nil {
			continue
		}
		values = append(values, value)
	}
	return values
}

func literalArray(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, strconv.Quote(value))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func between(value string, prefix string, suffix string) string {
	start := strings.Index(value, prefix)
	if start < 0 {
		return ""
	}
	start += len(prefix)
	end := strings.Index(value[start:], suffix)
	if end < 0 {
		return ""
	}
	return value[start : start+end]
}
