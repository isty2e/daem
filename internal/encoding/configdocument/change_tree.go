package configdocument

import "context"

// A planned node is either one exact change or disjoint child operations.
// Keeping this index shared prevents per-format overlap interpretations and
// quadratic copies when many keys share one object.
type changeTree struct {
	exact       *Change
	children    map[string]*changeTree
	containsSet bool
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
		node := root
		for _, key := range keys {
			if node.exact != nil {
				return nil, ErrOverlappingChange
			}
			node.containsSet = node.containsSet || !change.remove
			if node.children == nil {
				node.children = make(map[string]*changeTree)
			}
			if node.children[key] == nil {
				node.children[key] = &changeTree{}
			}
			node = node.children[key]
		}
		if node.exact != nil || len(node.children) > 0 {
			return nil, ErrOverlappingChange
		}
		copy := change
		node.exact = &copy
		node.containsSet = !change.remove
	}
	return root, nil
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
