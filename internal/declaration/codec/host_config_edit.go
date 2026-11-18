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
	var edits []declaration.DocumentChange
	if len(array.rows) > 0 {
		last := array.rows[len(array.rows)-1]
		if last.comma < 0 {
			edits = append(edits, insertHostConfigText(last.span.End, ","))
		} else {
			row += ","
		}
		row = " " + row
	}
	edits = append(edits, insertHostConfigText(array.close, row))
	updated, err := declaration.NewDocumentChangeSet(edits...).Apply(content)
	return updated, block, err
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
			span := declaration.DocumentRange{Start: block.Start, End: hostConfigBlockEnd(content, block)}
			return declaration.NewDocumentChangeSet(declaration.NewDocumentRemoval(span)).Apply(content)
		}
	}
	return nil, fmt.Errorf("cannot locate TOML declaration for host configuration %q", id)
}

func hostConfigBlockEnd(content []byte, block HostConfigBlock) int {
	end := block.End
	for end > block.Start {
		lineEnd := end
		if content[lineEnd-1] == '\n' {
			lineEnd--
		}
		lineStart := block.Start + bytes.LastIndexByte(content[block.Start:lineEnd], '\n') + 1
		trimmed := bytes.TrimSpace(content[lineStart:lineEnd])
		if len(trimmed) != 0 && trimmed[0] != '#' {
			break
		}
		end = lineStart
	}
	return end
}

type inlineHostConfigRow struct {
	span  declaration.DocumentRange
	comma int
}

type inlineHostConfigs struct {
	rows  []inlineHostConfigRow
	close int
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
	for index, row := range array.rows {
		// Decode exactly the table spanned by this array member, never a text
		// match on an id that may also appear in a source or comment.
		var decoded struct {
			HostConfig declaration.HostConfig `toml:"host_config"`
		}
		fragment := "host_config = " + string(content[row.span.Start:row.span.End])
		if _, err := tomlsyntax.Decode(fragment, &decoded); err != nil {
			return nil, err
		}
		if decoded.HostConfig.ID != id {
			continue
		}

		edits := []declaration.DocumentChange{declaration.NewDocumentRemoval(row.span)}
		comma := row.comma
		if comma < 0 && index > 0 {
			comma = array.rows[index-1].comma
		}
		if comma >= 0 {
			edits = append(edits, declaration.NewDocumentRemoval(declaration.DocumentRange{Start: comma, End: comma + 1}))
		}
		return declaration.NewDocumentChangeSet(edits...).Apply(content)
	}
	return nil, fmt.Errorf("cannot locate inline TOML declaration for host configuration %q", id)
}
