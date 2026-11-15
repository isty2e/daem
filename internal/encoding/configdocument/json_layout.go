package configdocument

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"

	"github.com/tailscale/hujson"
)

// Keep inherited indentation as a recipe until its output allowance is charged.
type jsonIndentation struct {
	prefix, unit []byte
	repeats      int
}

func (indent jsonIndentation) child() jsonIndentation {
	indent.repeats++
	return indent
}

type jsonObjectLayout struct {
	multiline                                          bool
	ending                                             []byte
	base, member                                       jsonIndentation
	baseKnown                                          bool
	first, separator, beforeColon, afterColon, closing []byte
	trailing                                           bool
}

func defaultJSONObjectLayout() jsonObjectLayout {
	base := jsonIndentation{unit: []byte("  ")}
	return jsonObjectLayout{
		multiline: true, ending: []byte("\n"), base: base, member: base.child(),
		baseKnown: true, afterColon: []byte(" "),
	}
}

func (layout jsonObjectLayout) child() jsonObjectLayout {
	return jsonObjectLayout{
		multiline: layout.multiline, ending: layout.ending,
		base: layout.member, member: layout.member.child(), baseKnown: layout.multiline,
		separator: layout.separator, beforeColon: layout.beforeColon, afterColon: layout.afterColon,
		trailing: layout.trailing,
	}
}

type jsonLayoutEvidence struct {
	raw, ending, indent []byte
	horizontal, clean   bool
}

type jsonLayoutSource struct {
	ctx      context.Context
	content  []byte
	comments []sourceSpan
}

func (source jsonLayoutSource) evidence(start, end int) (jsonLayoutEvidence, error) {
	if start < 0 || end < start || end > len(source.content) {
		return jsonLayoutEvidence{}, ErrInvalidDocument
	}
	result := jsonLayoutEvidence{raw: source.content[start:end], horizontal: true}
	comment := sort.Search(len(source.comments), func(index int) bool { return source.comments[index].end > start })
	indentStart := end
	for index, steps := start, 0; index < end; steps++ {
		if steps%4096 == 0 {
			if err := source.ctx.Err(); err != nil {
				return jsonLayoutEvidence{}, err
			}
		}
		if comment < len(source.comments) && source.comments[comment].start <= index {
			index = min(end, source.comments[comment].end)
			comment++
			result.horizontal, result.clean = false, false
			continue
		}
		switch source.content[index] {
		case '\r', '\n':
			next := index + 1
			if source.content[index] == '\r' && next < end && source.content[next] == '\n' {
				next++
			}
			result.ending = source.content[index:next]
			result.horizontal, result.clean = false, true
			index, indentStart = next, next
		case ' ', '\t':
			index++
		default:
			result.horizontal, result.clean = false, false
			index++
		}
	}
	if result.clean {
		result.indent = source.content[indentStart:end]
	}
	return result, source.ctx.Err()
}

func (source jsonLayoutSource) nameEvidence(name hujson.Value) (jsonLayoutEvidence, error) {
	return source.evidence(name.StartOffset-len(name.BeforeExtra), name.StartOffset)
}

func (source jsonLayoutSource) childLayout(parent jsonObjectLayout, name hujson.Value) (jsonObjectLayout, error) {
	layout := parent.child()
	layout.baseKnown = false
	evidence, err := source.nameEvidence(name)
	if err != nil {
		return jsonObjectLayout{}, err
	}
	if evidence.clean {
		layout.base = jsonIndentation{prefix: evidence.indent, unit: parent.member.unit}
		layout.member, layout.baseKnown = layout.base.child(), true
	}
	return layout, nil
}

func (source jsonLayoutSource) objectLayout(tree *hujson.Value, inherited jsonObjectLayout, root bool) (jsonObjectLayout, error) {
	object := tree.Value.(*hujson.Object)
	layout := inherited
	if root {
		leading, err := source.evidence(0, tree.StartOffset)
		if err != nil {
			return jsonObjectLayout{}, err
		}
		trailing, err := source.evidence(tree.EndOffset, len(source.content))
		if err != nil {
			return jsonObjectLayout{}, err
		}
		if len(trailing.ending) != 0 {
			layout.ending = trailing.ending
		} else if len(leading.ending) != 0 {
			layout.ending = leading.ending
		}
	}
	closing, err := source.evidence(tree.EndOffset-1-len(object.AfterExtra), tree.EndOffset-1)
	if err != nil {
		return jsonObjectLayout{}, err
	}
	if len(object.Members) == 0 && len(object.AfterExtra) == 0 {
		return layout, nil
	}

	layout.multiline = false
	layout.first, layout.separator, layout.closing = nil, nil, nil
	var keyIndent, keyEnding, boundaryEnding []byte
	cleanKey := false
	for index, member := range object.Members {
		evidence, err := source.nameEvidence(member.Name)
		if err != nil {
			return jsonObjectLayout{}, err
		}
		if len(evidence.ending) != 0 {
			layout.multiline, boundaryEnding = true, evidence.ending
		}
		if evidence.clean {
			keyIndent, keyEnding, cleanKey = evidence.indent, evidence.ending, true
		}
		if evidence.horizontal {
			layout.separator = evidence.raw
			if index == 0 {
				layout.first = evidence.raw
			}
		}
	}
	if len(object.Members) == 0 {
		layout.multiline = len(closing.ending) != 0
		boundaryEnding = closing.ending
		if closing.horizontal {
			layout.first, layout.separator = closing.raw, closing.raw
		}
	}
	if closing.horizontal {
		layout.closing = closing.raw
	}
	if cleanKey {
		layout.ending = keyEnding
	} else if len(boundaryEnding) != 0 {
		layout.ending = boundaryEnding
	}
	if closing.clean {
		layout.base = jsonIndentation{prefix: closing.indent, unit: inherited.member.unit}
		layout.baseKnown = true
	}
	step := inherited.member.unit
	if cleanKey && layout.baseKnown && layout.base.repeats == 0 && len(keyIndent) > len(layout.base.prefix) && bytes.HasPrefix(keyIndent, layout.base.prefix) {
		step = keyIndent[len(layout.base.prefix):]
	}
	if cleanKey {
		layout.member = jsonIndentation{prefix: keyIndent, unit: step}
	} else {
		if layout.base.repeats == 0 {
			layout.base.unit = step
		}
		layout.member = layout.base.child()
	}

	if root || len(object.Members) != 0 {
		layout.beforeColon, layout.afterColon = nil, nil
		if layout.multiline {
			layout.afterColon = []byte(" ")
		}
	}
	if len(object.Members) != 0 {
		last := object.Members[len(object.Members)-1]
		before, err := source.evidence(last.Name.EndOffset, last.Name.EndOffset+len(last.Name.AfterExtra))
		if err != nil {
			return jsonObjectLayout{}, err
		}
		after, err := source.evidence(last.Value.StartOffset-len(last.Value.BeforeExtra), last.Value.StartOffset)
		if err != nil {
			return jsonObjectLayout{}, err
		}
		if before.horizontal {
			layout.beforeColon = before.raw
		}
		if after.horizontal {
			layout.afterColon = after.raw
		}
		layout.trailing = last.Value.AfterExtra != nil
	}
	return layout, nil
}

func (renderer *literalRenderer) jsonExtra(prefix []byte, indent jsonIndentation) (hujson.Extra, error) {
	remaining := renderer.remaining
	if int64(len(prefix)) > remaining {
		return nil, ErrLimitExceeded
	}
	remaining -= int64(len(prefix))
	if int64(len(indent.prefix)) > remaining {
		return nil, ErrLimitExceeded
	}
	remaining -= int64(len(indent.prefix))
	if indent.repeats != 0 && int64(len(indent.unit)) > remaining/int64(indent.repeats) {
		return nil, ErrLimitExceeded
	}
	size := int64(len(prefix)) + int64(len(indent.prefix)) + int64(len(indent.unit))*int64(indent.repeats)
	if size > int64(int(^uint(0)>>1)) {
		return nil, ErrLimitExceeded
	}
	if err := renderer.charge(size); err != nil {
		return nil, err
	}
	if size == 0 {
		return nil, nil
	}
	extra := make(hujson.Extra, int(size))
	position := copy(extra, prefix)
	position += copy(extra[position:], indent.prefix)
	for index := range indent.repeats {
		if index%4096 == 0 {
			if err := renderer.ctx.Err(); err != nil {
				return nil, err
			}
		}
		position += copy(extra[position:], indent.unit)
	}
	return extra, nil
}

func (layout jsonObjectLayout) writeMember(member *hujson.ObjectMember, first bool, renderer *literalRenderer) error {
	prefix, indent := layout.separator, jsonIndentation{}
	if first {
		prefix = layout.first
	}
	if layout.multiline {
		prefix, indent = layout.ending, layout.member
	}
	var err error
	member.Name.BeforeExtra, err = renderer.jsonExtra(prefix, indent)
	if err != nil {
		return err
	}
	member.Name.AfterExtra, err = renderer.jsonExtra(layout.beforeColon, jsonIndentation{})
	if err != nil {
		return err
	}
	member.Value.BeforeExtra, err = renderer.jsonExtra(layout.afterColon, jsonIndentation{})
	return err
}

func (layout jsonObjectLayout) writeClosing(object *hujson.Object, renderer *literalRenderer) error {
	prefix, indent := layout.closing, jsonIndentation{}
	if layout.multiline {
		prefix, indent = layout.ending, layout.base
	}
	var err error
	object.AfterExtra, err = renderer.jsonExtra(prefix, indent)
	return err
}

func normalizeJSONObjectComma(object *hujson.Object, trailing bool) {
	if len(object.Members) == 0 {
		return
	}
	last := &object.Members[len(object.Members)-1].Value
	if trailing && last.AfterExtra == nil {
		last.AfterExtra = hujson.Extra{}
	} else if !trailing && last.AfterExtra != nil {
		object.AfterExtra = append(last.AfterExtra, object.AfterExtra...)
		last.AfterExtra = nil
	}
}

func (editor jsonTreeEditor) annotateObjects(tree *hujson.Value, original *hujson.Value, inherited jsonObjectLayout, depth int) error {
	if err := editor.renderer.ctx.Err(); err != nil {
		return err
	}
	object, ok := tree.Value.(*hujson.Object)
	if !ok || len(object.Members) == 0 {
		return nil
	}
	layout := inherited
	var oldMembers map[string]hujson.Value
	if original != nil {
		if old, ok := original.Value.(*hujson.Object); ok {
			var err error
			layout, err = editor.source.objectLayout(original, inherited, depth == 0)
			if err != nil {
				return err
			}
			if len(old.Members) != 0 {
				oldMembers = make(map[string]hujson.Value, len(old.Members))
			}
			for _, member := range old.Members {
				if err := editor.renderer.ctx.Err(); err != nil {
					return err
				}
				var key string
				if err := json.Unmarshal(member.Name.Value.(hujson.Literal), &key); err != nil {
					return ErrInvalidDocument
				}
				oldMembers[key] = member.Value
			}
		}
	}
	for index := range object.Members {
		member := &object.Members[index]
		if err := layout.writeMember(member, index == 0, editor.renderer); err != nil {
			return err
		}
		var key string
		if err := json.Unmarshal(member.Name.Value.(hujson.Literal), &key); err != nil {
			return ErrInvalidDocument
		}
		childLayout := layout.child()
		var oldValue *hujson.Value
		if old, found := oldMembers[key]; found {
			oldValue = &old
		}
		if err := editor.annotateObjects(&member.Value, oldValue, childLayout, depth+1); err != nil {
			return err
		}
	}
	if err := layout.writeClosing(object, editor.renderer); err != nil {
		return err
	}
	if layout.trailing {
		if err := editor.renderer.charge(1); err != nil {
			return err
		}
	}
	normalizeJSONObjectComma(object, layout.trailing)
	return nil
}
