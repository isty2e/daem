package artifact

import (
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
)

const (
	MaximumInstalledTreeEntries       = 100_000
	MaximumInstalledTreeDepth         = 64
	MaximumInstalledTreeBytes   int64 = 4 << 30
)

// TreeMembers names the descendants shipped in a directory artifact, in hash order.
// A trailing slash denotes a directory. The zero value means no inventory is available.
type TreeMembers struct {
	entries []string
}

// TreeInventory binds a release's relative paths to the digests from the same traversal.
type TreeInventory struct {
	PathHashes
	Members TreeMembers
}

func NewTreeMembers(entries []string) (TreeMembers, error) {
	if entries == nil {
		return TreeMembers{}, nil
	}
	if len(entries) > MaximumInstalledTreeEntries {
		return TreeMembers{}, fmt.Errorf("installed tree exceeds %d entries", MaximumInstalledTreeEntries)
	}
	directories := map[string]bool{".": true}
	previous := ""
	for _, entry := range entries {
		name, directory := strings.CutSuffix(entry, "/")
		if !fs.ValidPath(name) || name == "." || strings.ContainsRune(name, 0) || !directories[path.Dir(name)] || compareDirectoryHashPaths(previous, name) >= 0 {
			return TreeMembers{}, fmt.Errorf("invalid or unordered installed tree member %q", entry)
		}
		if directory {
			if strings.Count(entry, "/") > MaximumInstalledTreeDepth {
				return TreeMembers{}, fmt.Errorf("installed directory exceeds depth %d", MaximumInstalledTreeDepth)
			}
			directories[name] = true
		}
		previous = name
	}
	return TreeMembers{entries: slices.Clone(entries)}, nil
}

func (m TreeMembers) Known() bool       { return m.entries != nil }
func (m TreeMembers) Entries() []string { return slices.Clone(m.entries) }
func (m TreeMembers) Equal(other TreeMembers) bool {
	return m.Known() == other.Known() && slices.Equal(m.entries, other.entries)
}

func (m TreeMembers) Includes(name string) bool {
	_, found := m.lookup(name)
	return found
}

func (m TreeMembers) lookup(name string) (int, bool) {
	return slices.BinarySearchFunc(m.entries, name, func(entry, target string) int {
		return compareDirectoryHashPaths(strings.TrimSuffix(entry, "/"), target)
	})
}
