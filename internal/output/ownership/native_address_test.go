package ownership

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/output"
)

func nativeAddress(t *testing.T, path string, keys ...string) ManagedAddress {
	t.Helper()
	key, err := output.NewKeyPath(keys...)
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	address, err := NewManagedAddress(mustPathAuthority(t, path), footprint)
	if err != nil {
		t.Fatal(err)
	}
	return address
}

func TestNativeAddressAndIndexPreserveLiteralKeyBoundaries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	tests := []struct {
		name    string
		left    []string
		right   []string
		overlap bool
	}{
		{name: "slash is not a separator", left: []string{"a/b"}, right: []string{"a", "b"}},
		{name: "tilde is not decoded twice", left: []string{"a~1b"}, right: []string{"a/b"}},
		{name: "empty property is not root", left: []string{""}, right: []string{"a"}},
		{name: "empty property has descendants", left: []string{""}, right: []string{"", "child"}, overlap: true},
		{name: "dot is a literal key", left: []string{"."}, right: []string{".."}},
		{name: "unicode and controls survive", left: []string{"日本語", "line\nkey"}, right: []string{"日本語", "line\nkey"}, overlap: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			left := nativeAddress(t, path, test.left...)
			right := nativeAddress(t, path, test.right...)
			for _, pair := range [][2]ManagedAddress{{left, right}, {right, left}} {
				if pair[0].Overlaps(pair[1]) != test.overlap {
					t.Fatalf("unexpected overlap for %q and %q", test.left, test.right)
				}
				index := addressOverlapIndex{roots: make(map[string]*physicalAddressNode)}
				if _, overlap := index.insert(0, pair[0]); overlap {
					t.Fatal("first address unexpectedly overlaps an empty index")
				}
				if _, found := index.first(pair[1]); found != test.overlap {
					t.Fatal("indexed lookup lost the native key boundary")
				}
				if _, found := index.insert(1, pair[1]); found != test.overlap {
					t.Fatal("indexed insertion lost the native key boundary")
				}
			}
		})
	}

	emptyKey := nativeAddress(t, path, "")
	whole := mustAddress(t, path, "")
	if emptyKey.WholePath() || emptyKey.Equal(whole) || !whole.Overlaps(emptyKey) {
		t.Fatal("empty-property ownership was confused with whole-path ownership")
	}
	legacy := mustAddress(t, path, "/a~1b")
	if !legacy.Equal(nativeAddress(t, path, "a~1b")) || legacy.Overlaps(nativeAddress(t, path, "a/b")) {
		t.Fatal("legacy literal path was interpreted as a native pointer")
	}
	if _, err := NewManagedAddress(mustPathAuthority(t, path), output.Footprint{}); err == nil {
		t.Fatal("an uninitialized footprint acquired authority")
	}
}
