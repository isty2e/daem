package codec

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/isty2e/daem/internal/declaration"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/encoding/toml"
)

// AppendHostConfigDeclaration honors the existing collection's TOML form.
func AppendHostConfigDeclaration(content []byte, config hostconfig.Config) ([]byte, string, error) {
	fields, err := hostConfigFields(config)
	if err != nil {
		return nil, "", err
	}
	array, exists, err := scanInlineHostConfigs(content)
	if err != nil {
		return nil, "", err
	}
	block := "[[host_config]]\n" + strings.Join(fields, "\n")
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
				edits = append(edits, insertHostConfigText(last.span.End, ","))
			}
		}
	}
	edits = append(edits, insertHostConfigText(insertAt, insertion))
	updated, err := declaration.NewDocumentChangeSet(edits...).Apply(content)
	return updated, row, err
}

func insertHostConfigText(at int, value string) declaration.DocumentChange {
	return declaration.NewDocumentReplacement(declaration.DocumentRange{Start: at, End: at}, []byte(value))
}

// RemoveHostConfigDeclaration removes one semantically selected declaration and
// retains the bytes outside the row and any required array separator.
func RemoveHostConfigDeclaration(content []byte, id string) ([]byte, error) {
	array, exists, err := scanInlineHostConfigs(content)
	if err != nil {
		return nil, err
	}
	if exists {
		return array.remove(content, id)
	}

	blocks, err := ScanHostConfigBlocks(content)
	if err != nil {
		return nil, err
	}
	for _, block := range blocks {
		if block.HostConfig.ID == id {
			end, err := hostConfigBlockEnd(content, block)
			if err != nil {
				return nil, err
			}
			span := declaration.DocumentRange{Start: block.Start, End: end}
			return declaration.NewDocumentChangeSet(declaration.NewDocumentRemoval(span)).Apply(content)
		}
	}
	return nil, fmt.Errorf("cannot locate TOML declaration for host configuration %q", id)
}

func hostConfigBlockEnd(content []byte, block HostConfigBlock) (int, error) {
	fragment := content[block.Start:block.End]
	index, err := tomlsyntax.IndexDocument(context.Background(), fragment)
	if err != nil {
		return 0, err
	}
	expressions := index.Expressions()
	if len(expressions) == 0 {
		return 0, fmt.Errorf("cannot locate final expression for host configuration %q", block.HostConfig.ID)
	}
	return block.Start + tomlsyntax.SkipLine(string(fragment), expressions[len(expressions)-1].Span.End), nil
}

type inlineHostConfigRow struct {
	span  declaration.DocumentRange
	comma int
}

type inlineHostConfigs struct {
	rows  []inlineHostConfigRow
	close int
}

func (array inlineHostConfigs) appendPosition(content, row string) (int, string) {
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

func scanInlineHostConfigs(content []byte) (inlineHostConfigs, bool, error) {
	index, err := tomlsyntax.IndexDocument(context.Background(), content)
	if err != nil {
		return inlineHostConfigs{}, false, err
	}
	for _, expression := range index.Expressions() {
		if expression.Kind != tomlsyntax.KeyValue || !slices.Equal(expression.Path, tomlsyntax.Key{"host_config"}) {
			continue
		}
		array, err := indexInlineHostConfigRows(string(content), expression.Value)
		return array, true, err
	}
	return inlineHostConfigs{}, false, nil
}

func indexInlineHostConfigRows(content string, value tomlsyntax.Span) (inlineHostConfigs, error) {
	if value.End-value.Start < 2 || content[value.Start] != '[' || content[value.End-1] != ']' {
		return inlineHostConfigs{}, fmt.Errorf("host_config is not an inline array")
	}
	array := inlineHostConfigs{close: value.End - 1}
	for cursor := value.Start + 1; cursor < array.close; {
		start := tomlsyntax.SkipSpaceAndComments(content, cursor, array.close)
		if start == array.close {
			break
		}
		end, ok := tomlsyntax.SkipValue(content, start, array.close, 0)
		if !ok || content[start] != '{' {
			return inlineHostConfigs{}, fmt.Errorf("cannot locate inline host_config table")
		}
		row := inlineHostConfigRow{span: declaration.DocumentRange{Start: start, End: end}, comma: -1}
		cursor = tomlsyntax.SkipSpaceAndComments(content, end, array.close)
		if cursor < array.close {
			if content[cursor] != ',' {
				return inlineHostConfigs{}, fmt.Errorf("cannot locate inline host_config separator")
			}
			row.comma = cursor
			cursor++
		}
		array.rows = append(array.rows, row)
	}
	return array, nil
}

func (array inlineHostConfigs) remove(content []byte, id string) ([]byte, error) {
	text := string(content)
	for index, row := range array.rows {
		// Decode exactly the table spanned by this array member, never a text
		// match on an id that may also appear in a source or comment.
		var decoded struct {
			HostConfig declaration.HostConfig `toml:"host_config"`
		}
		fragment := "host_config = " + text[row.span.Start:row.span.End]
		if _, err := tomlsyntax.Decode(fragment, &decoded); err != nil {
			return nil, err
		}
		if decoded.HostConfig.ID != id {
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
