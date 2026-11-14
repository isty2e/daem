package ownership

import (
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/output"
)

type addressOverlapIndex struct {
	roots  map[string]*physicalAddressNode
	legacy map[string]*compatibilityPathNode
	native map[string]*compatibilityPathNode
}

type compatibilityPathNode struct {
	children map[string]*compatibilityPathNode
	first    int
	terminal int
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

// Edges retain canonical pointer bytes. Terminals may have non-slash children:
// /a and /a! are disjoint even though their radix paths are nested.
type contentAddressNode struct {
	prefix   string
	children map[byte]*contentAddressNode
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
	comparison, native := address.PathAuthority().LegacyComparisonKey()
	if comparison != "" {
		opposite := index.native
		if native {
			opposite = index.legacy
		}
		if existing, overlap := firstCompatibilityPath(opposite, comparison, true); overlap {
			return existing, true
		}
	}
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

	if existing, overlap := current.exact.content.insert(addressIndex, address.Footprint()); overlap {
		return existing, true
	}
	for _, node := range visited {
		if node.first < 0 {
			node.first = addressIndex
		}
	}
	if comparison != "" {
		if native {
			if index.native == nil {
				index.native = make(map[string]*compatibilityPathNode)
			}
			markCompatibilityPath(index.native, comparison, addressIndex)
		} else {
			if index.legacy == nil {
				index.legacy = make(map[string]*compatibilityPathNode)
			}
			markCompatibilityPath(index.legacy, comparison, addressIndex)
		}
	}
	return 0, false
}

func markCompatibilityPath(roots map[string]*compatibilityPathNode, path string, addressIndex int) {
	volume, components := physicalPathComponents(path)
	current := roots[volume]
	if current == nil {
		current = &compatibilityPathNode{children: make(map[string]*compatibilityPathNode), first: addressIndex, terminal: -1}
		roots[volume] = current
	}
	for _, component := range components {
		next := current.children[component]
		if next == nil {
			next = &compatibilityPathNode{children: make(map[string]*compatibilityPathNode), first: addressIndex, terminal: -1}
			current.children[component] = next
		}
		current = next
	}
	if current.terminal < 0 {
		current.terminal = addressIndex
	}
}

func firstCompatibilityPath(roots map[string]*compatibilityPathNode, path string, descendants bool) (int, bool) {
	volume, components := physicalPathComponents(path)
	current := roots[volume]
	first := -1
	for depth := 0; current != nil; depth++ {
		first = earlierAddressIndex(first, current.terminal)
		if depth == len(components) {
			if descendants {
				first = earlierAddressIndex(first, current.first)
			}
			break
		}
		current = current.children[components[depth]]
	}
	return firstAddressIndex(first)
}

func (index addressOverlapIndex) first(address ManagedAddress) (int, bool) {
	first := -1
	if existing, found := index.firstFootprint(address); found {
		first = existing
	}
	comparison, native := address.PathAuthority().LegacyComparisonKey()
	if existing, found := index.firstComparison(comparison, native, true); found {
		first = earlierAddressIndex(first, existing)
	}
	return firstAddressIndex(first)
}

func (index addressOverlapIndex) firstProvisional(candidate pathauthority.Provisional) (int, bool) {
	first := -1
	if existing, found := index.firstPhysicalAncestor(candidate.CandidateKey()); found {
		first = existing
	}
	comparison, native := candidate.LegacyComparisonKey()
	if existing, found := index.firstComparison(comparison, native, false); found {
		first = earlierAddressIndex(first, existing)
	}
	return firstAddressIndex(first)
}

func (index addressOverlapIndex) firstComparison(comparison string, native, descendants bool) (int, bool) {
	if comparison == "" {
		return 0, false
	}
	opposite := index.native
	if native {
		opposite = index.legacy
	}
	return firstCompatibilityPath(opposite, comparison, descendants)
}

func (index addressOverlapIndex) firstFootprint(address ManagedAddress) (int, bool) {
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
		} else if contentIndex, overlap := current.exact.content.firstOverlap(address.Footprint()); overlap {
			first = earlierAddressIndex(first, contentIndex)
		}
	} else if current.first >= 0 {
		first = earlierAddressIndex(first, current.first)
	}
	return firstAddressIndex(first)
}

func (index addressOverlapIndex) firstPhysicalAncestor(path string) (int, bool) {
	volume, components := physicalPathComponents(path)
	current := index.roots[volume]
	first := -1
	for depth := 0; current != nil; depth++ {
		if current.exact != nil {
			first = earlierAddressIndex(first, current.exact.first)
		}
		if depth == len(components) {
			break
		}
		current = current.children[components[depth]]
	}
	return firstAddressIndex(first)
}

func newPhysicalAddressNode() *physicalAddressNode {
	return &physicalAddressNode{children: make(map[string]*physicalAddressNode), first: -1}
}

func newContentAddressNode() contentAddressNode {
	return contentAddressNode{first: -1, terminal: -1}
}

func (node *contentAddressNode) insert(addressIndex int, footprint output.Footprint) (int, bool) {
	if existing, overlap := node.firstOverlap(footprint); overlap {
		return existing, true
	}
	remaining := footprintPointer(footprint)
	current := node
	visited := []*contentAddressNode{current}
	for remaining != "" {
		if current.children == nil {
			current.children = make(map[byte]*contentAddressNode)
		}
		next := current.children[remaining[0]]
		if next == nil {
			next = &contentAddressNode{prefix: remaining, first: addressIndex, terminal: addressIndex}
			current.children[remaining[0]] = next
			current = next
			visited = append(visited, current)
			break
		}

		common := commonPrefixBytes(next.prefix, remaining)
		if common < len(next.prefix) {
			branch := &contentAddressNode{
				prefix: next.prefix[:common], first: next.first, terminal: -1,
				children: make(map[byte]*contentAddressNode),
			}
			next.prefix = next.prefix[common:]
			branch.children[next.prefix[0]] = next
			current.children[remaining[0]] = branch
			next = branch
		}
		remaining = remaining[common:]
		current = next
		visited = append(visited, current)
	}
	current.terminal = addressIndex
	for _, entry := range visited {
		entry.first = earlierAddressIndex(entry.first, addressIndex)
	}
	return 0, false
}

func (node *contentAddressNode) firstOverlap(footprint output.Footprint) (int, bool) {
	remaining := footprintPointer(footprint)
	current := node
	for {
		if current.terminal >= 0 && (remaining == "" || remaining[0] == '/') {
			return current.terminal, true
		}
		if remaining == "" {
			if descendants := current.children['/']; descendants != nil {
				return firstAddressIndex(descendants.first)
			}
			return 0, false
		}
		next := current.children[remaining[0]]
		if next == nil {
			return 0, false
		}
		if strings.HasPrefix(remaining, next.prefix) {
			remaining = remaining[len(next.prefix):]
			current = next
			continue
		}
		if strings.HasPrefix(next.prefix, remaining) && next.prefix[len(remaining)] == '/' {
			return firstAddressIndex(next.first)
		}
		return 0, false
	}
}

func commonPrefixBytes(left, right string) int {
	end := min(len(left), len(right))
	index := 0
	for index < end && left[index] == right[index] {
		index++
	}
	return index
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

func footprintPointer(footprint output.Footprint) string {
	key, _ := footprint.Key()
	return key.String()
}
