package ownership

import (
	"math/rand"
	"path/filepath"
	"testing"
)

func TestNativeIndexMatchesScopeOracleAcrossInsertionOrders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	candidates := []ManagedAddress{mustAddress(t, path, "")}
	for _, keys := range [][]string{
		{""},
		{"", "child"},
		{"", "child!"},
		{"a"},
		{"a!"},
		{"a!!"},
		{"a", "b"},
		{"a", "b!"},
		{"a", "b", "c"},
		{"a/b"},
		{"a~1b"},
		{"~"},
		{"~0"},
		{"~1"},
		{"."},
		{".."},
		{"0"},
		{"00"},
		{"\x00"},
		{"\x00", "nested"},
		{"日"},
		{"日", "nested"},
		{"旦"},
		{"日本語"},
	} {
		candidates = append(candidates, nativeAddress(t, path, keys...))
	}
	random := rand.New(rand.NewSource(3))
	for iteration := range 200 {
		index := addressOverlapIndex{roots: make(map[string]*physicalAddressNode)}
		var existing []ManagedAddress
		for _, position := range random.Perm(len(candidates)) {
			candidate := candidates[position]
			wantPosition, want := pairwiseFirstOverlap(existing, candidate)
			gotPosition, got := index.insert(len(existing), candidate)
			if got != want || got && gotPosition != wantPosition {
				t.Fatalf("iteration %d insertion overlap = (%d, %v), want (%d, %v)",
					iteration, gotPosition, got, wantPosition, want)
			}
			if !got {
				existing = append(existing, candidate)
			}
			for _, query := range candidates {
				wantPosition, want := pairwiseFirstOverlap(existing, query)
				gotPosition, got := index.first(query)
				if got != want || got && gotPosition != wantPosition {
					t.Fatalf("iteration %d lookup overlap = (%d, %v), want (%d, %v)",
						iteration, gotPosition, got, wantPosition, want)
				}
			}
		}
	}
}
