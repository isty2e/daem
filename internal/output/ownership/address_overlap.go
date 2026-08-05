package ownership

import (
	"path/filepath"
	"strings"
)

type addressOverlapIndex struct {
	roots map[string]*physicalAddressNode
}

type physicalAddressNode struct {
	children map[string]*physicalAddressNode
	first    int
	exact    *physicalAddressClaims
}

type physicalAddressClaims struct {
	witness string
	first   int
	content contentAddressNode
}

type contentAddressNode struct {
	children map[string]*contentAddressNode
	first    int
	terminal int
}

func firstOverlappingAddress(addresses []ManagedAddress) (int, int, bool) {
	index := addressOverlapIndex{roots: make(map[string]*physicalAddressNode)}
	for addressIndex, address := range addresses {
		if existing, overlap := index.insert(addressIndex, address); overlap {
			return existing, addressIndex, true
		}
	}
	return 0, 0, false
}

func (index *addressOverlapIndex) insert(addressIndex int, address ManagedAddress) (int, bool) {
	volume, components := physicalPathComponents(address.Path())
	root := index.roots[volume]
	if root == nil {
		root = newPhysicalAddressNode()
		index.roots[volume] = root
	}

	current := root
	visited := []*physicalAddressNode{current}
	for _, component := range components {
		if current.exact != nil {
			return current.exact.first, true
		}
		next := current.children[component]
		if next == nil {
			next = newPhysicalAddressNode()
			current.children[component] = next
		}
		current = next
		visited = append(visited, current)
	}

	if current.exact == nil {
		if current.first >= 0 {
			return current.first, true
		}
		current.exact = &physicalAddressClaims{
			witness: address.PathAuthority().Witness(),
			first:   addressIndex,
			content: newContentAddressNode(),
		}
	} else if current.exact.witness != address.PathAuthority().Witness() {
		return current.exact.first, true
	}

	if existing, overlap := current.exact.content.insert(addressIndex, address.ContentPath()); overlap {
		return existing, true
	}
	for _, node := range visited {
		if node.first < 0 {
			node.first = addressIndex
		}
	}
	return 0, false
}

func (index addressOverlapIndex) first(address ManagedAddress) (int, bool) {
	volume, components := physicalPathComponents(address.Path())
	current := index.roots[volume]
	if current == nil {
		return 0, false
	}

	first := -1
	for _, component := range components {
		if current.exact != nil {
			first = earlierAddressIndex(first, current.exact.first)
		}
		next := current.children[component]
		if next == nil {
			return firstAddressIndex(first)
		}
		current = next
	}

	if current.exact != nil {
		if current.exact.witness != address.PathAuthority().Witness() {
			first = earlierAddressIndex(first, current.exact.first)
		} else if contentIndex, overlap := current.exact.content.firstOverlap(address.ContentPath()); overlap {
			first = earlierAddressIndex(first, contentIndex)
		}
	} else if current.first >= 0 {
		first = earlierAddressIndex(first, current.first)
	}
	return firstAddressIndex(first)
}

func newPhysicalAddressNode() *physicalAddressNode {
	return &physicalAddressNode{children: make(map[string]*physicalAddressNode), first: -1}
}

func newContentAddressNode() contentAddressNode {
	return contentAddressNode{children: make(map[string]*contentAddressNode), first: -1, terminal: -1}
}

func (node *contentAddressNode) insert(addressIndex int, contentPath string) (int, bool) {
	current := node
	visited := []*contentAddressNode{current}
	for _, component := range contentPathComponents(contentPath) {
		if current.terminal >= 0 {
			return current.terminal, true
		}
		next := current.children[component]
		if next == nil {
			next = &contentAddressNode{
				children: make(map[string]*contentAddressNode),
				first:    -1,
				terminal: -1,
			}
			current.children[component] = next
		}
		current = next
		visited = append(visited, current)
	}
	if current.terminal >= 0 {
		return current.terminal, true
	}
	if current.first >= 0 {
		return current.first, true
	}
	current.terminal = addressIndex
	for _, visitedNode := range visited {
		if visitedNode.first < 0 {
			visitedNode.first = addressIndex
		}
	}
	return 0, false
}

func (node *contentAddressNode) firstOverlap(contentPath string) (int, bool) {
	current := node
	first := -1
	for _, component := range contentPathComponents(contentPath) {
		if current.terminal >= 0 {
			first = earlierAddressIndex(first, current.terminal)
		}
		next := current.children[component]
		if next == nil {
			return firstAddressIndex(first)
		}
		current = next
	}
	if current.first >= 0 {
		first = earlierAddressIndex(first, current.first)
	}
	return firstAddressIndex(first)
}

func earlierAddressIndex(current int, candidate int) int {
	if current < 0 || candidate < current {
		return candidate
	}
	return current
}

func firstAddressIndex(index int) (int, bool) {
	return index, index >= 0
}

func physicalPathComponents(path string) (string, []string) {
	volume := filepath.VolumeName(path)
	relative := strings.TrimPrefix(path, volume)
	relative = strings.TrimPrefix(relative, string(filepath.Separator))
	if relative == "" {
		return volume, nil
	}
	return volume, strings.Split(relative, string(filepath.Separator))
}

func contentPathComponents(path string) []string {
	if path == "" {
		return nil
	}
	return strings.Split(strings.TrimPrefix(path, "/"), "/")
}
