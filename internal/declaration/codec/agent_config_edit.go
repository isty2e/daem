package codec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/isty2e/daem/internal/declaration"
	"github.com/isty2e/daem/internal/desired/agentconfig"
	"github.com/isty2e/daem/internal/encoding/toml"
)

// AppendAgentConfigDeclaration honors the existing collection's TOML form.
func AppendAgentConfigDeclaration(content []byte, config agentconfig.Config) ([]byte, string, error) {
	fields, err := agentConfigFields(config)
	if err != nil {
		return nil, "", err
	}
	array, exists, err := scanInlineAgentConfigs(content)
	if err != nil {
		return nil, "", err
	}
	block := "[[agent_config]]\n" + strings.Join(fields, "\n")
	if !exists {
		return declaration.AppendDocumentBlock(content, block+"\n"), block, nil
	}

	row := "{ " + strings.Join(fields, ", ") + " }"
	insertAt, insertion := array.appendPosition(string(content), row)
	var edits []declaration.DocumentChange
	if len(array.rows) > 0 {
		last := array.rows[len(array.rows)-1]
		if last.comma < 0 {
			if last.span.End == insertAt {
				insertion = "," + insertion
			} else {
				edits = append(edits, insertAgentConfigText(last.span.End, ","))
			}
		}
	}
	edits = append(edits, insertAgentConfigText(insertAt, insertion))
	updated, err := declaration.NewDocumentChangeSet(edits...).Apply(content)
	return updated, row, err
}

func insertAgentConfigText(at int, value string) declaration.DocumentChange {
	return declaration.NewDocumentReplacement(declaration.DocumentRange{Start: at, End: at}, []byte(value))
}

// RemoveAgentConfigDeclaration removes one semantically selected declaration and
// retains the bytes outside the row and any required array separator.
func RemoveAgentConfigDeclaration(content []byte, id string) ([]byte, error) {
	array, exists, err := scanInlineAgentConfigs(content)
	if err != nil {
		return nil, err
	}
	if exists {
		return array.remove(content, id)
	}

	after, err := AgentConfigRemoval(id).ApplyBlock(content)
	if errors.Is(err, ErrAgentConfigBlockSelection) {
		return nil, fmt.Errorf("cannot locate TOML declaration for host configuration %q", id)
	}
	return after, err
}

// AgentConfigEdit selects one declaration by ID. A replacement retains that ID.
type AgentConfigEdit struct {
	id          string
	replacement *agentconfig.Config
}

func AgentConfigRemoval(id string) AgentConfigEdit { return AgentConfigEdit{id: id} }

func AgentConfigReplacement(config agentconfig.Config) AgentConfigEdit {
	return AgentConfigEdit{id: config.ID().Name(), replacement: &config}
}

func (edit AgentConfigEdit) block(content []byte) (declaration.DocumentRange, []byte, error) {
	span, err := selectAgentConfigBlock(content, edit.id)
	if err != nil || edit.replacement == nil {
		return span, nil, err
	}
	text, err := RenderAgentConfigBlock(*edit.replacement)
	return span, []byte(text), err
}

// ApplyBlock leaves standalone comments and whitespace following the last expression intact.
func (edit AgentConfigEdit) ApplyBlock(content []byte) ([]byte, error) {
	span, replacement, err := edit.block(content)
	if err != nil {
		return nil, err
	}
	span.End, err = agentConfigBlockEnd(content, span)
	if err != nil {
		return nil, err
	}
	return declaration.NewDocumentChangeSet(declaration.NewDocumentReplacement(span, replacement)).Apply(content)
}

// MatchesCaptureAfter accepts the exact edit with or without the trailing trivia:
// earlier generation-4 writers discarded it up to the next header or EOF.
func (edit AgentConfigEdit) MatchesCaptureAfter(before, after []byte) (bool, error) {
	span, replacement, err := edit.block(before)
	if err != nil {
		return false, err
	}
	tail, ok := bytes.CutPrefix(after, before[:span.Start])
	if !ok {
		return false, nil
	}
	tail, ok = bytes.CutPrefix(tail, replacement)
	if !ok {
		return false, nil
	}
	if bytes.Equal(tail, before[span.End:]) {
		return true, nil
	}
	end, err := agentConfigBlockEnd(before, span)
	if err != nil {
		return false, err
	}
	return bytes.Equal(tail, before[end:]), nil
}

func (edit AgentConfigEdit) MatchesDeclarations(before, after []agentconfig.Config) bool {
	i := slices.IndexFunc(before, func(c agentconfig.Config) bool { return c.ID().Name() == edit.id })
	if i < 0 || i > len(after) || !slices.Equal(before[:i], after[:i]) {
		return false
	}
	tail := after[i:]
	if edit.replacement != nil {
		if len(tail) == 0 || tail[0] != *edit.replacement {
			return false
		}
		tail = tail[1:]
	}
	return slices.Equal(before[i+1:], tail)
}

func agentConfigBlockEnd(content []byte, block declaration.DocumentRange) (int, error) {
	fragment := content[block.Start:block.End]
	index, err := tomlsyntax.IndexDocument(context.Background(), fragment)
	if err != nil {
		return 0, err
	}
	expressions := index.Expressions()
	if len(expressions) == 0 {
		return 0, fmt.Errorf("cannot locate final expression in agent_config block")
	}
	return block.Start + tomlsyntax.SkipLine(string(fragment), expressions[len(expressions)-1].Span.End), nil
}

type inlineAgentConfigRow struct {
	span  declaration.DocumentRange
	comma int
}

type inlineAgentConfigs struct {
	rows  []inlineAgentConfigRow
	close int
}

func (array inlineAgentConfigs) appendPosition(content, row string) (int, string) {
	if len(array.rows) > 0 && array.rows[len(array.rows)-1].comma >= 0 {
		row += ","
	}
	closeLine := strings.LastIndexByte(content[:array.close], '\n') + 1
	if closeLine == 0 || tomlsyntax.SkipHorizontalSpace(content, closeLine, array.close) != array.close {
		if len(array.rows) > 0 {
			row = " " + row
		}
		return array.close, row
	}
	indent := content[closeLine:array.close] + "  "
	if len(array.rows) > 0 {
		last := array.rows[len(array.rows)-1].span.Start
		line := strings.LastIndexByte(content[:last], '\n') + 1
		if tomlsyntax.SkipHorizontalSpace(content, line, last) == last {
			indent = content[line:last]
		}
	}
	newline := "\n"
	if closeLine >= 2 && content[closeLine-2] == '\r' {
		newline = "\r\n"
	}
	return closeLine, indent + row + newline
}

func scanInlineAgentConfigs(content []byte) (inlineAgentConfigs, bool, error) {
	index, err := tomlsyntax.IndexDocument(context.Background(), content)
	if err != nil {
		return inlineAgentConfigs{}, false, err
	}
	for _, expression := range index.Expressions() {
		if expression.Kind != tomlsyntax.KeyValue || !slices.Equal(expression.Path, tomlsyntax.Key{"agent_config"}) {
			continue
		}
		array, err := indexInlineAgentConfigRows(string(content), expression.Value)
		return array, true, err
	}
	return inlineAgentConfigs{}, false, nil
}

func indexInlineAgentConfigRows(content string, value tomlsyntax.Span) (inlineAgentConfigs, error) {
	if value.End-value.Start < 2 || content[value.Start] != '[' || content[value.End-1] != ']' {
		return inlineAgentConfigs{}, fmt.Errorf("agent_config is not an inline array")
	}
	array := inlineAgentConfigs{close: value.End - 1}
	for cursor := value.Start + 1; cursor < array.close; {
		start := tomlsyntax.SkipSpaceAndComments(content, cursor, array.close)
		if start == array.close {
			break
		}
		end, ok := tomlsyntax.SkipValue(content, start, array.close, 0)
		if !ok || content[start] != '{' {
			return inlineAgentConfigs{}, fmt.Errorf("cannot locate inline agent_config table")
		}
		row := inlineAgentConfigRow{span: declaration.DocumentRange{Start: start, End: end}, comma: -1}
		cursor = tomlsyntax.SkipSpaceAndComments(content, end, array.close)
		if cursor < array.close {
			if content[cursor] != ',' {
				return inlineAgentConfigs{}, fmt.Errorf("cannot locate inline agent_config separator")
			}
			row.comma = cursor
			cursor++
		}
		array.rows = append(array.rows, row)
	}
	return array, nil
}

func (array inlineAgentConfigs) remove(content []byte, id string) ([]byte, error) {
	text := string(content)
	for index, row := range array.rows {
		// Decode exactly the table spanned by this array member, never a text
		// match on an id that may also appear in a source or comment.
		var decoded struct {
			AgentConfig declaration.AgentConfig `toml:"agent_config"`
		}
		fragment := "agent_config = " + text[row.span.Start:row.span.End]
		if _, err := tomlsyntax.Decode(fragment, &decoded); err != nil {
			return nil, err
		}
		if decoded.AgentConfig.ID != id {
			continue
		}

		comma := row.comma
		if comma < 0 && index > 0 {
			comma = array.rows[index-1].comma
		}
		span := row.span
		if comma >= span.End && !bytes.ContainsAny(content[span.End:comma], "\r\n") {
			span.End = comma + 1
			comma = -1
		}
		span = wholeInlineRowLine(text, span)
		edits := []declaration.DocumentChange{declaration.NewDocumentRemoval(span)}
		if comma >= 0 {
			edits = append(edits, declaration.NewDocumentRemoval(declaration.DocumentRange{Start: comma, End: comma + 1}))
		}
		return declaration.NewDocumentChangeSet(edits...).Apply(content)
	}
	return nil, fmt.Errorf("cannot locate inline TOML declaration for host configuration %q", id)
}

func wholeInlineRowLine(content string, span declaration.DocumentRange) declaration.DocumentRange {
	start := strings.LastIndexByte(content[:span.Start], '\n') + 1
	if tomlsyntax.SkipHorizontalSpace(content, start, span.Start) != span.Start {
		return span
	}
	tail := tomlsyntax.SkipHorizontalSpace(content, span.End, len(content))
	end := tomlsyntax.SkipTrailingCommentAndNewline(content, tail)
	if end > tail && content[end-1] == '\n' {
		return declaration.DocumentRange{Start: start, End: end}
	}
	return span
}
