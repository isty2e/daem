package configdocument

import "github.com/isty2e/daem/internal/output"

type resolutionKind uint8

const (
	resolutionAbsent resolutionKind = iota
	resolutionPresent
	resolutionBlocked
)

// Resolution distinguishes a missing property from traversal blocked by a
// non-object, including a scalar/array document root.
type Resolution struct {
	kind     resolutionKind
	value    Value
	ancestor output.KeyPath
}

func (resolution Resolution) Absent() bool { return resolution.kind == resolutionAbsent }

func (resolution Resolution) Value() (Value, bool) {
	return resolution.value, resolution.kind == resolutionPresent
}

func (resolution Resolution) BlockedBy() (output.KeyPath, bool) {
	return resolution.ancestor, resolution.kind == resolutionBlocked
}

func (document Document) Resolve(path output.KeyPath) (Resolution, error) {
	if document.root.data == nil {
		return Resolution{}, ErrInvalidDocument
	}
	if !document.exists {
		return Resolution{}, nil
	}

	value := document.root
	keys := path.Segments()
	for position, key := range keys {
		members, object := value.data.(objectNode)
		if !object {
			ancestor, err := output.NewKeyPath(keys[:position]...)
			if err != nil {
				return Resolution{}, err
			}
			return Resolution{kind: resolutionBlocked, ancestor: ancestor}, nil
		}
		index, found := memberIndex(members, key)
		if !found {
			return Resolution{}, nil
		}
		value = members[index].value
	}
	return Resolution{kind: resolutionPresent, value: value}, nil
}
