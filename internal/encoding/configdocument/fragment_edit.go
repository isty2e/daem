package configdocument

import (
	"context"
	"slices"

	"github.com/isty2e/daem/internal/output"
)

// EditFragment preserves leaf ownership when removing the last selected child:
// newly empty structural parents disappear, but explicit empty values remain.
func EditFragment(ctx context.Context, document Document, changes []Change) (Document, error) {
	if ctx == nil || !document.root.IsObject() || document.syntax.validate() != nil {
		return Document{}, ErrInvalidDocument
	}

	tree, err := planChanges(ctx, changes, document.limits.MaximumDepth)
	if err != nil {
		return Document{}, err
	}
	expected, err := updateTree(ctx, document.root, tree)
	if err != nil {
		return Document{}, err
	}
	pruned, _, err := newlyEmptyFragmentParents(ctx, document.root, expected, tree, nil)
	if err != nil {
		return Document{}, err
	}
	if len(pruned) == 0 {
		return Edit(ctx, document, changes)
	}

	prunedTree, err := planChanges(ctx, pruned, document.limits.MaximumDepth)
	if err != nil {
		return Document{}, err
	}
	retained := make([]Change, 0, len(changes)+len(pruned))
	for _, change := range changes {
		if err := ctx.Err(); err != nil {
			return Document{}, err
		}
		if !hasPlannedAncestor(prunedTree, change.path) {
			retained = append(retained, change)
		}
	}
	return Edit(ctx, document, append(retained, pruned...))
}

func hasPlannedAncestor(tree *changeTree, path output.KeyPath) bool {
	for _, segment := range path.Segments() {
		if tree.exact != nil {
			return true
		}
		tree = tree.children[segment]
		if tree == nil {
			return false
		}
	}
	return tree.exact != nil
}

func newlyEmptyFragmentParents(ctx context.Context, before, after Value, tree *changeTree, segments []string) ([]Change, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if tree.exact != nil || !before.IsObject() || !after.IsObject() {
		return nil, false, nil
	}

	var changes []Change
	removedChildren := 0
	members := after.data.(objectNode)
	for _, member := range before.data.(objectNode) {
		branch, selected := tree.children[member.key]
		if !selected {
			continue
		}
		index, present := memberIndex(members, member.key)
		if !present {
			continue
		}
		children, removed, err := newlyEmptyFragmentParents(ctx, member.value, members[index].value, branch, append(slices.Clone(segments), member.key))
		if err != nil {
			return nil, false, err
		}
		if removed {
			removedChildren++
		}
		changes = append(changes, children...)
	}

	if len(segments) != 0 && !before.IsAtomic() && removedChildren == len(members) {
		path, err := output.NewKeyPath(segments...)
		if err != nil {
			return nil, false, err
		}
		change, err := Remove(path)
		return []Change{change}, true, err
	}
	return changes, false, nil
}
