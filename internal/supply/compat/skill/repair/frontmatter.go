package repair

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	skillcompat "github.com/isty2e/daem/internal/supply/compat/skill"
	"gopkg.in/yaml.v3"
)

var safeUnquotedFrontmatterString = regexp.MustCompile(`^[A-Za-z0-9._ -]+$`)

func planAndApplyFrontmatterDelimiter(
	ctx context.Context,
	root string,
	draft *repairDraft,
) error {
	state, err := skillDocumentState(ctx, root, "SKILL.md")
	if err != nil {
		if errors.Is(err, skillcompat.ErrSkillDocumentTooLarge) {
			return err
		}
		draft.addManual(fmt.Sprintf("read SKILL.md: %v", err))
		return newManualError(*draft)
	}

	firstLineEnd := bytes.IndexByte(state.Content, '\n')
	if firstLineEnd < 0 {
		return nil
	}
	lineWithEnding := state.Content[:firstLineEnd+1]
	lineText := bytes.TrimSuffix(lineWithEnding, []byte("\n"))
	lineEnding := []byte("\n")
	if before, ok := bytes.CutSuffix(lineText, []byte("\r")); ok {
		lineText = before
		lineEnding = []byte("\r\n")
	}

	checkLine := bytes.TrimPrefix(lineText, []byte{0xef, 0xbb, 0xbf})
	if !bytes.Equal(bytes.TrimSpace(checkLine), []byte("---")) {
		return nil
	}
	if bytes.Equal(lineText, []byte("---")) {
		return nil
	}

	newLine := append([]byte{}, []byte("---")...)
	newLine = append(newLine, lineEnding...)
	operation, err := newReplaceBytesOperation(0, lineWithEnding, newLine, state.Content)
	if err != nil {
		return fmt.Errorf("construct SKILL.md delimiter repair: %w", err)
	}
	if err := applyOperation(ctx, root, operation); err != nil {
		return fmt.Errorf("apply SKILL.md delimiter repair: %w", err)
	}
	draft.operations = append(draft.operations, operation)
	return nil
}

func planAndApplyFrontmatterString(
	ctx context.Context,
	root string,
	sourceID artifact.SourceID,
	field string,
	oldValue *string,
	newValue string,
	draft *repairDraft,
) error {
	state, err := skillDocumentState(ctx, root, "SKILL.md")
	if err != nil {
		if errors.Is(err, skillcompat.ErrSkillDocumentTooLarge) {
			return err
		}
		draft.addManual(fmt.Sprintf("read SKILL.md: %v", err))
		return newManualError(*draft)
	}

	lines := splitLines(state.Content)
	if len(lines) == 0 || !isOpeningDelimiter(lines[0].Text) {
		draft.addManual(fmt.Sprintf("SKILL.md frontmatter opening delimiter must be exact before setting %s", field))
		return newManualError(*draft)
	}

	closingIndex := -1
	for index := 1; index < len(lines); index++ {
		if isDelimiter(lines[index].Text) {
			closingIndex = index
			break
		}
	}
	if closingIndex < 0 {
		draft.addManual(fmt.Sprintf("SKILL.md frontmatter closing delimiter is required before setting %s", field))
		return newManualError(*draft)
	}

	replacement := []byte(field + ": " + yamlStringScalar(newValue))
	for index := 1; index < closingIndex; index++ {
		if !bytes.HasPrefix(lines[index].Text, []byte(field+":")) {
			continue
		}
		if !isSimpleScalarLine(lines[index].Text, field) {
			draft.addManual(fmt.Sprintf("SKILL.md frontmatter field %q uses a complex YAML form; edit it manually", field))
			return newManualError(*draft)
		}
		operation, err := newSetFrontmatterStringOperation(
			field,
			oldValue,
			newValue,
			lines[index].Offset,
			lines[index].Text,
			replacement,
			state.Content,
		)
		if err != nil {
			return fmt.Errorf("construct SKILL.md frontmatter repair: %w", err)
		}
		if err := applyOperation(ctx, root, operation); err != nil {
			return fmt.Errorf("apply SKILL.md frontmatter repair: %w", err)
		}
		draft.operations = append(draft.operations, operation)
		return nil
	}

	if oldValue != nil {
		draft.addManual(fmt.Sprintf("SKILL.md frontmatter field %q uses a complex YAML form; edit it manually", field))
		return newManualError(*draft)
	}

	ending := lineEnding(lines)
	insertOffset := lines[0].Offset + len(lines[0].Text) + len(lines[0].Ending)
	newBytes := append([]byte{}, replacement...)
	newBytes = append(newBytes, ending...)
	operation, err := newSetFrontmatterStringOperation(field, nil, newValue, insertOffset, nil, newBytes, state.Content)
	if err != nil {
		return fmt.Errorf("construct SKILL.md frontmatter insertion: %w", err)
	}
	if err := applyOperation(ctx, root, operation); err != nil {
		return fmt.Errorf("apply SKILL.md frontmatter insertion: %w", err)
	}
	draft.operations = append(draft.operations, operation)
	return nil
}

func newReplaceBytesOperation(
	offset int,
	oldBytes []byte,
	newBytes []byte,
	input []byte,
) (Operation, error) {
	if offset < 0 || offset > len(input) || len(oldBytes) > len(input)-offset {
		return Operation{}, fmt.Errorf("repair byte range is outside SKILL.md")
	}
	if err := checkSkillDocumentReplacementSize(len(input), len(oldBytes), len(newBytes)); err != nil {
		return Operation{}, err
	}
	output := replaceAt(input, offset, oldBytes, newBytes)
	if err := skillcompat.CheckSkillDocumentSize(int64(len(output))); err != nil {
		return Operation{}, err
	}
	return NewReplaceBytesOperation(
		"SKILL.md",
		offset,
		oldBytes,
		newBytes,
		artifact.HashFileContent(input),
		artifact.HashFileContent(output),
	)
}

func newSetFrontmatterStringOperation(
	field string,
	oldValue *string,
	newValue string,
	offset int,
	oldBytes []byte,
	newBytes []byte,
	input []byte,
) (Operation, error) {
	if offset < 0 || offset > len(input) || len(oldBytes) > len(input)-offset {
		return Operation{}, fmt.Errorf("repair byte range is outside SKILL.md")
	}
	if err := checkSkillDocumentReplacementSize(len(input), len(oldBytes), len(newBytes)); err != nil {
		return Operation{}, err
	}
	output := replaceAt(input, offset, oldBytes, newBytes)
	if err := skillcompat.CheckSkillDocumentSize(int64(len(output))); err != nil {
		return Operation{}, err
	}
	return NewSetFrontmatterStringOperation(
		"SKILL.md",
		field,
		oldValue,
		newValue,
		offset,
		oldBytes,
		newBytes,
		artifact.HashFileContent(input),
		artifact.HashFileContent(output),
	)
}

func loadFrontmatter(
	ctx context.Context,
	root string,
	sourceID artifact.SourceID,
) (skillcompat.SkillFrontmatter, error) {
	view, err := access.OpenView(root)
	if err != nil {
		return skillcompat.SkillFrontmatter{}, err
	}
	return skillcompat.LoadSkillFrontmatter(ctx, view, sourceID)
}

type line struct {
	Text   []byte
	Ending []byte
	Offset int
}

func splitLines(content []byte) []line {
	lines := make([]line, 0)
	offset := 0
	for len(content) != 0 {
		lineEnd := bytes.IndexByte(content, '\n')
		if lineEnd < 0 {
			lines = append(lines, line{Text: append([]byte{}, content...), Offset: offset})
			break
		}
		text := append([]byte{}, content[:lineEnd]...)
		ending := []byte("\n")
		if before, ok := bytes.CutSuffix(text, []byte("\r")); ok {
			text = before
			ending = []byte("\r\n")
		}
		lines = append(lines, line{Text: text, Ending: ending, Offset: offset})
		offset += lineEnd + 1
		content = content[lineEnd+1:]
	}
	return lines
}

func lineEnding(lines []line) []byte {
	for _, line := range lines {
		if len(line.Ending) != 0 {
			return append([]byte{}, line.Ending...)
		}
	}
	return []byte("\n")
}

func isOpeningDelimiter(line []byte) bool {
	return isDelimiter(bytes.TrimPrefix(line, []byte{0xef, 0xbb, 0xbf}))
}

func isDelimiter(line []byte) bool {
	return bytes.Equal(bytes.TrimRight(line, " \t"), []byte("---"))
}

func isSimpleScalarLine(line []byte, field string) bool {
	afterField := strings.TrimSpace(string(line[len(field)+1:]))
	if afterField == "" {
		return false
	}
	if strings.HasPrefix(afterField, "|") || strings.HasPrefix(afterField, ">") {
		return false
	}
	return true
}

func yamlStringScalar(value string) string {
	if safeUnquotedFrontmatterString.MatchString(value) &&
		!strings.Contains(value, " #") &&
		plainYAMLStringPreservesValue(value) {
		return value
	}
	return strconv.Quote(value)
}

func plainYAMLStringPreservesValue(value string) bool {
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(value), &document); err != nil ||
		document.Kind != yaml.DocumentNode ||
		len(document.Content) != 1 {
		return false
	}
	scalar := document.Content[0]
	return scalar.Kind == yaml.ScalarNode && scalar.Tag == "!!str" && scalar.Value == value
}
