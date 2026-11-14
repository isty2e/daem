package configdocument

import (
	"slices"
	"strings"

	tomlsyntax "github.com/isty2e/daem/internal/encoding/toml"
)

type inlineCoverage struct {
	exact    bool
	children map[string]*inlineCoverage
}

type inlineRendering struct {
	text              string
	displacedComments string
}

func (coverage *inlineCoverage) add(keys []string) {
	for _, key := range keys {
		if coverage.children == nil {
			coverage.children = make(map[string]*inlineCoverage)
		}
		if coverage.children[key] == nil {
			coverage.children[key] = &inlineCoverage{}
		}
		coverage = coverage.children[key]
	}
	coverage.exact = true
}

func (editor *tomlEditor) inlineTable(table tomlsyntax.InlineTable, before, after Value) (inlineRendering, error) {
	var result, rawTrivia, cleanTrivia, displaced strings.Builder
	result.WriteByte('{')
	coverage := &inlineCoverage{}
	position, kept := table.Span.Start+1, 0
	dirtyGap := false
	appendTrivia := func(span sourceSpan) error {
		if err := editor.inlineWrite(&rawTrivia, string(editor.document.content[span.start:span.end])); err != nil {
			return err
		}
		clean, err := editor.inlineTrivia(span)
		if err != nil {
			return err
		}
		return editor.inlineWrite(&cleanTrivia, clean)
	}
	for _, entry := range table.Entries {
		if err := editor.ctx.Err(); err != nil {
			return inlineRendering{}, err
		}
		if err := appendTrivia(sourceSpan{position, entry.Span.Start}); err != nil {
			return inlineRendering{}, err
		}
		position = entry.Span.End
		keys := entry.Path[len(table.Path):]
		value, present := inlineValue(after, keys)
		if !present {
			comments, err := editor.commentsWithin(sourceSpan{entry.Span.Start, entry.Span.End})
			if err != nil {
				return inlineRendering{}, err
			}
			if err := editor.inlineWrite(&displaced, comments); err != nil {
				return inlineRendering{}, err
			}
			dirtyGap = true
			continue
		}
		trivia := rawTrivia.String()
		if dirtyGap {
			trivia = cleanTrivia.String()
			if kept != 0 {
				trivia = "," + trivia
			}
		}
		if err := editor.inlineWrite(&result, trivia); err != nil {
			return inlineRendering{}, err
		}
		previous, _ := inlineValue(before, keys)
		body, err := editor.inlineEntry(entry, previous, value)
		if err != nil {
			return inlineRendering{}, err
		}
		if err := editor.inlineWrite(&result, body.text); err != nil {
			return inlineRendering{}, err
		}
		if err := editor.inlineWrite(&displaced, body.displacedComments); err != nil {
			return inlineRendering{}, err
		}
		coverage.add(keys)
		kept++
		dirtyGap = false
		rawTrivia.Reset()
		cleanTrivia.Reset()
	}
	if err := appendTrivia(sourceSpan{position, table.Span.End - 1}); err != nil {
		return inlineRendering{}, err
	}
	added, err := editor.inlineAdditions(after, coverage, nil, len(table.Path))
	if err != nil {
		return inlineRendering{}, err
	}
	suffix := rawTrivia.String()
	if len(added) != 0 || dirtyGap {
		suffix = cleanTrivia.String()
	}
	if len(added) != 0 && kept != 0 {
		if err := editor.inlineWrite(&result, ","); err != nil {
			return inlineRendering{}, err
		}
	}
	if err := editor.inlineWrite(&result, suffix); err != nil {
		return inlineRendering{}, err
	}
	for index, entry := range added {
		if index != 0 {
			if err := editor.inlineWrite(&result, ", "); err != nil {
				return inlineRendering{}, err
			}
		}
		if err := editor.inlineWrite(&result, entry); err != nil {
			return inlineRendering{}, err
		}
	}
	if err := editor.inlineWrite(&result, "}"); err != nil {
		return inlineRendering{}, err
	}
	return inlineRendering{text: result.String(), displacedComments: displaced.String()}, nil
}

func (editor *tomlEditor) inlineEntry(entry tomlsyntax.Expression, before, after Value) (inlineRendering, error) {
	content := editor.document.content
	if before.Equal(after) {
		return inlineRendering{text: string(content[entry.Span.Start:entry.Span.End])}, nil
	}
	prefix := string(content[entry.Span.Start:entry.Value.Start])
	if table, inline := editor.index.inlines[entry.Value.Start]; inline && before.IsObject() && after.IsObject() {
		value, err := editor.inlineTable(table, before, after)
		if err != nil {
			return inlineRendering{}, err
		}
		return inlineRendering{text: prefix + value.text, displacedComments: value.displacedComments}, nil
	}
	var value strings.Builder
	if err := editor.renderer.value(&value, after, len(entry.Path)); err != nil {
		return inlineRendering{}, err
	}
	comments, err := editor.commentsWithin(sourceSpan{entry.Value.Start, entry.Value.End})
	if err != nil {
		return inlineRendering{}, err
	}
	return inlineRendering{text: prefix + value.String(), displacedComments: comments}, nil
}

func (editor *tomlEditor) inlineAdditions(value Value, coverage *inlineCoverage, prefix []string, depth int) ([]string, error) {
	var result []string
	members, object := value.data.(objectNode)
	if !object {
		return nil, ErrInvalidValue
	}
	for _, member := range members {
		if err := editor.ctx.Err(); err != nil {
			return nil, err
		}
		branch := coverage.children[member.key]
		if branch != nil && branch.exact {
			continue
		}
		keys := append(slices.Clone(prefix), member.key)
		if branch != nil {
			entries, err := editor.inlineAdditions(member.value, branch, keys, depth)
			if err != nil {
				return nil, err
			}
			result = append(result, entries...)
			continue
		}
		var entry strings.Builder
		for index, key := range keys {
			if index != 0 {
				if err := editor.renderer.raw(&entry, "."); err != nil {
					return nil, err
				}
			}
			if err := editor.renderer.key(&entry, key); err != nil {
				return nil, err
			}
		}
		if err := editor.renderer.raw(&entry, " = "); err != nil {
			return nil, err
		}
		if err := editor.renderer.value(&entry, member.value, depth+len(keys)); err != nil {
			return nil, err
		}
		result = append(result, entry.String())
	}
	return result, nil
}

func inlineValue(value Value, keys []string) (Value, bool) {
	for _, key := range keys {
		members, object := value.data.(objectNode)
		if !object {
			return Value{}, false
		}
		index, present := memberIndex(members, key)
		if !present {
			return Value{}, false
		}
		value = members[index].value
	}
	return value, true
}

func (editor *tomlEditor) inlineTrivia(span sourceSpan) (string, error) {
	var result strings.Builder
	position := span.start
	start, _ := slices.BinarySearchFunc(editor.index.comments, span.start, func(comment sourceSpan, offset int) int { return comment.start - offset })
	for _, comment := range editor.index.comments[start:] {
		if comment.start >= span.end {
			break
		}
		if err := editor.inlineWrite(&result, strings.ReplaceAll(string(editor.document.content[position:comment.start]), ",", "")); err != nil {
			return "", err
		}
		if err := editor.inlineWrite(&result, string(editor.document.content[comment.start:comment.end])); err != nil {
			return "", err
		}
		position = comment.end
	}
	if err := editor.inlineWrite(&result, strings.ReplaceAll(string(editor.document.content[position:span.end]), ",", "")); err != nil {
		return "", err
	}
	return result.String(), editor.ctx.Err()
}

func (editor *tomlEditor) inlineWrite(result *strings.Builder, text string) error {
	if int64(result.Len())+int64(len(text)) > editor.document.limits.MaximumBytes {
		return ErrLimitExceeded
	}
	result.WriteString(text)
	return editor.ctx.Err()
}
