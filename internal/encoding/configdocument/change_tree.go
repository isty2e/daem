package configdocument

import "context"

// A planned node is either one exact change or disjoint child operations.
// Keeping this index shared prevents per-format overlap interpretations and
// quadratic copies when many keys share one object.
type changeTree struct {
	exact       *valueChange
	children    map[string]*changeTree
	containsSet bool
}

type valueChange struct {
	value  Value
	remove bool
}

func planChanges(ctx context.Context, changes []Change, maximumDepth int) (*changeTree, error) {
	root := &changeTree{}
	for _, change := range changes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if change.path.IsRoot() || !change.remove && change.value.data == nil {
			return nil, ErrInvalidValue
		}
		keys := change.path.Segments()
		if len(keys) > maximumDepth {
			return nil, ErrLimitExceeded
		}
		copy := valueChange{value: change.value, remove: change.remove}
		if err := root.attach(keys, &changeTree{exact: &copy, containsSet: !change.remove}); err != nil {
			return nil, err
		}
	}
	return root, nil
}

func (tree *changeTree) attach(keys []string, branch *changeTree) error {
	if len(keys) == 0 || branch == nil {
		return ErrInvalidValue
	}
	node := tree
	for index, key := range keys {
		if node.exact != nil {
			return ErrOverlappingChange
		}
		node.containsSet = node.containsSet || branch.containsSet
		if node.children == nil {
			node.children = make(map[string]*changeTree)
		}
		if index == len(keys)-1 {
			if existing := node.children[key]; existing != nil && (existing.exact != nil || len(existing.children) > 0) {
				return ErrOverlappingChange
			}
			node.children[key] = branch
			return nil
		}
		if node.children[key] == nil {
			node.children[key] = &changeTree{}
		}
		node = node.children[key]
	}
	return nil
}

func updateTree(ctx context.Context, value Value, tree *changeTree) (Value, error) {
	if err := ctx.Err(); err != nil {
		return Value{}, err
	}
	if tree.exact != nil {
		return tree.exact.value, nil
	}
	members, object := value.data.(objectNode)
	if !object {
		return Value{}, ErrPathConflict
	}
	updated := make([]objectMember, 0, len(members))
	for _, member := range members {
		branch, selected := tree.children[member.key]
		if !selected {
			updated = append(updated, member)
			continue
		}
		if branch.exact != nil && branch.exact.remove {
			continue
		}
		child, err := updateTree(ctx, member.value, branch)
		if err != nil {
			return Value{}, err
		}
		updated = append(updated, objectMember{key: member.key, value: child})
	}
	for key, branch := range tree.children {
		if _, found := memberIndex(members, key); found || !branch.containsSet {
			continue
		}
		child, err := updateTree(ctx, Value{data: objectNode(nil)}, branch)
		if err != nil {
			return Value{}, err
		}
		updated = append(updated, objectMember{key: key, value: child})
	}
	return newObject(updated)
}
