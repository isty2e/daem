package configdocument

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/output"
)

type rewriteRegionKind uint8

const (
	rewriteSubtree rewriteRegionKind = iota + 1
	rewriteJSONArray
)

// RewriteRegion selects comparison facts, not permission to modify a document.
// JSON array entries may be selected individually; TOML arrays remain atomic.
type RewriteRegion struct {
	kind    rewriteRegionKind
	path    output.KeyPath
	indices []int
}

func SubtreeRegion(path output.KeyPath) (RewriteRegion, error) {
	if path.IsRoot() {
		return RewriteRegion{}, fmt.Errorf("a rewrite region must identify a property")
	}
	return RewriteRegion{kind: rewriteSubtree, path: path}, nil
}

func JSONArrayRegion(path output.KeyPath, indices ...int) (RewriteRegion, error) {
	region, err := SubtreeRegion(path)
	if err != nil {
		return RewriteRegion{}, err
	}
	region.kind, region.indices = rewriteJSONArray, slices.Clone(indices)
	slices.Sort(region.indices)
	region.indices = slices.Compact(region.indices)
	if len(region.indices) != 0 && region.indices[0] < 0 {
		return RewriteRegion{}, fmt.Errorf("array rewrite indices must be non-negative")
	}
	return region, nil
}

func (region RewriteRegion) containsPropertyPath(path []string) bool {
	segments := region.path.Segments()
	return len(path) >= len(segments) && slices.Equal(segments, path[:len(segments)])
}

type rewriteSelection struct {
	fields   map[string]*rewriteSelection
	elements map[int]bool
	omit     bool
	prune    bool
}

func (selection *rewriteSelection) field(name string) *rewriteSelection {
	if selection.fields == nil {
		selection.fields = make(map[string]*rewriteSelection)
	}
	if selection.fields[name] == nil {
		selection.fields[name] = &rewriteSelection{}
	}
	return selection.fields[name]
}

// PreservesOutside compares native values and lexical comment occurrences.
// Different array positions may identify the same admitted membership change.
// Existing empty values remain protected; newly necessary parents may disappear.
func (before Document) PreservesOutside(ctx context.Context, after Document, beforeRegions, afterRegions []RewriteRegion) (bool, error) {
	if ctx == nil {
		return false, fmt.Errorf("document preservation context is required")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if before.syntax != after.syntax || before.root.data == nil || after.root.data == nil {
		return false, ErrInvalidDocument
	}
	scope := &rewriteSelection{}
	left, err := before.selectRewriteRegions(ctx, beforeRegions, scope)
	if err != nil {
		return false, err
	}
	right, err := after.selectRewriteRegions(ctx, afterRegions, scope)
	if err != nil {
		return false, err
	}
	leftValue, err := projectPreserved(ctx, before.root, before.root, left, scope)
	if err != nil {
		return false, err
	}
	rightValue, err := projectPreserved(ctx, after.root, before.root, right, scope)
	if err != nil || !leftValue.Equal(rightValue) {
		return false, err
	}
	leftComments, err := before.commentsOutside(ctx, beforeRegions)
	if err != nil {
		return false, err
	}
	rightComments, err := after.commentsOutside(ctx, afterRegions)
	if err != nil {
		return false, err
	}
	return sameComments(leftComments, rightComments), ctx.Err()
}

func (document Document) selectRewriteRegions(ctx context.Context, regions []RewriteRegion, scope *rewriteSelection) (*rewriteSelection, error) {
	selected := &rewriteSelection{}
	for index, region := range regions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if region.path.IsRoot() || region.kind != rewriteSubtree && region.kind != rewriteJSONArray {
			return nil, fmt.Errorf("invalid document rewrite region")
		}
		if regionCovered(regions, index) {
			continue
		}
		value, present, err := document.Lookup(region.path)
		if err != nil {
			return nil, err
		}
		if region.kind == rewriteJSONArray {
			if document.syntax == TOML {
				return nil, fmt.Errorf("array entry rewrite regions require JSON syntax")
			}
			values, array := value.data.(arrayNode)
			if present && !array || !present && len(region.indices) != 0 {
				return nil, ErrPathConflict
			}
			for _, item := range region.indices {
				if item < 0 || item >= len(values) {
					return nil, ErrPathConflict
				}
			}
		}
		node, parents := selected, scope
		segments := region.path.Segments()
		active := present && (region.kind == rewriteSubtree || len(region.indices) != 0)
		for item, segment := range segments {
			node = node.field(segment)
			parents = parents.field(segment)
			if active && (item != len(segments)-1 || region.kind == rewriteJSONArray) {
				parents.prune = true
			}
		}
		if region.kind == rewriteSubtree {
			node.omit = true
		} else {
			if node.elements == nil {
				node.elements = make(map[int]bool)
			}
			for _, item := range region.indices {
				node.elements[item] = true
			}
		}
	}
	return selected, nil
}

func regionCovered(regions []RewriteRegion, index int) bool {
	for other, candidate := range regions {
		if candidate.kind == rewriteSubtree && candidate.path.Contains(regions[index].path) &&
			(candidate.path.String() != regions[index].path.String() || regions[index].kind != rewriteSubtree || other < index) {
			return true
		}
	}
	return false
}

func projectPreserved(ctx context.Context, value, baseline Value, selected, scope *rewriteSelection) (Value, error) {
	if err := ctx.Err(); err != nil {
		return Value{}, err
	}
	if selected == nil && scope == nil {
		return value, nil
	}
	if selected != nil && selected.omit {
		return Value{}, nil
	}
	switch node := value.data.(type) {
	case objectNode:
		members := make(objectNode, 0, len(node))
		before, _ := baseline.data.(objectNode)
		for _, member := range node {
			var original Value
			if index, present := memberIndex(before, member.key); present {
				original = before[index].value
			}
			child, childScope := selectionField(selected, member.key), selectionField(scope, member.key)
			projected, err := projectPreserved(ctx, member.value, original, child, childScope)
			if err != nil {
				return Value{}, err
			}
			if projected.data != nil {
				members = append(members, objectMember{key: member.key, value: projected})
			}
		}
		if len(members) == 0 && scope != nil && scope.prune && !explicitEmpty(baseline) {
			return Value{}, nil
		}
		return Value{data: members}, nil
	case arrayNode:
		values := make(arrayNode, 0, len(node))
		for index, element := range node {
			if err := ctx.Err(); err != nil {
				return Value{}, err
			}
			if selected == nil || !selected.elements[index] {
				values = append(values, element)
			}
		}
		if len(values) == 0 && scope != nil && scope.prune && !explicitEmpty(baseline) {
			return Value{}, nil
		}
		return Value{data: values}, nil
	default:
		return value, nil
	}
}

func selectionField(selection *rewriteSelection, name string) *rewriteSelection {
	if selection == nil {
		return nil
	}
	return selection.fields[name]
}

func explicitEmpty(value Value) bool {
	switch node := value.data.(type) {
	case objectNode:
		return len(node) == 0
	case arrayNode:
		return len(node) == 0
	default:
		return false
	}
}
