package ownership

import (
	"math/rand"
	"path/filepath"
	"sort"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
)

func TestAddressOverlapIndexMatchesPairwiseSemantics(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "overlap-oracle")
	paths := []string{
		root,
		filepath.Join(root, "a"),
		filepath.Join(root, "a", "child"),
		filepath.Join(root, "a-other"),
		filepath.Join(root, "b"),
	}
	contentPaths := []string{"", "/alpha", "/alpha/child", "/alpha-other", "/beta"}
	addresses := make([]ManagedAddress, 0, len(paths)*len(contentPaths)+1)
	for _, path := range paths {
		for _, contentPath := range contentPaths {
			address, err := NewManagedAddress(pathtest.Exact(path), contentPath)
			if err != nil {
				t.Fatal(err)
			}
			addresses = append(addresses, address)
		}
	}
	alternateWitness, err := NewManagedAddress(pathtest.DarwinCaseSensitive(paths[1]), "/disjoint")
	if err != nil {
		t.Fatal(err)
	}
	addresses = append(addresses, alternateWitness)

	random := rand.New(rand.NewSource(1))
	for iteration := range 500 {
		size := 1 + random.Intn(12)
		selection := make([]ManagedAddress, size)
		for index := range selection {
			selection[index] = addresses[random.Intn(len(addresses))]
		}
		random.Shuffle(len(selection), func(left int, right int) {
			selection[left], selection[right] = selection[right], selection[left]
		})
		_, _, got := firstOverlappingAddress(selection)
		want := pairwiseAddressOverlap(selection)
		if got != want {
			t.Fatalf("iteration %d overlap = %t, want %t for %#v", iteration, got, want, selection)
		}
	}
}

func pairwiseAddressOverlap(addresses []ManagedAddress) bool {
	for right, address := range addresses {
		for left := range right {
			if addresses[left].Overlaps(address) {
				return true
			}
		}
	}
	return false
}

func TestAddressOverlapLookupMatchesPairwiseSemantics(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "overlap-lookup-oracle")
	paths := []string{
		root,
		filepath.Join(root, "a"),
		filepath.Join(root, "a", "child"),
		filepath.Join(root, "a-other"),
		filepath.Join(root, "b"),
	}
	contentPaths := []string{"", "/alpha", "/alpha/child", "/alpha-other", "/beta"}
	candidates := make([]ManagedAddress, 0, len(paths)*len(contentPaths)+1)
	for _, path := range paths {
		for _, contentPath := range contentPaths {
			address, err := NewManagedAddress(pathtest.Exact(path), contentPath)
			if err != nil {
				t.Fatal(err)
			}
			candidates = append(candidates, address)
		}
	}
	alternateWitness, err := NewManagedAddress(pathtest.DarwinCaseSensitive(paths[1]), "/disjoint")
	if err != nil {
		t.Fatal(err)
	}
	candidates = append(candidates, alternateWitness)

	random := rand.New(rand.NewSource(2))
	for iteration := range 500 {
		existing := make([]ManagedAddress, 0, 12)
		permutation := random.Perm(len(candidates))
		for _, candidateIndex := range permutation {
			candidate := candidates[candidateIndex]
			if _, overlap := pairwiseFirstOverlap(existing, candidate); overlap {
				continue
			}
			existing = append(existing, candidate)
			if len(existing) == 12 {
				break
			}
		}
		sort.Slice(existing, func(left int, right int) bool {
			return existing[left].Less(existing[right])
		})
		index := addressOverlapIndex{roots: make(map[string]*physicalAddressNode)}
		for addressIndex, address := range existing {
			if _, overlap := index.insert(addressIndex, address); overlap {
				t.Fatalf("iteration %d generated overlapping index", iteration)
			}
		}
		for _, candidate := range candidates {
			gotIndex, got := index.first(candidate)
			wantIndex, want := pairwiseFirstOverlap(existing, candidate)
			if got != want || gotIndex != wantIndex {
				t.Fatalf(
					"iteration %d first overlap = (%d, %t), want (%d, %t)",
					iteration,
					gotIndex,
					got,
					wantIndex,
					want,
				)
			}
		}
	}
}

func pairwiseFirstOverlap(addresses []ManagedAddress, candidate ManagedAddress) (int, bool) {
	for index, address := range addresses {
		if address.Overlaps(candidate) {
			return index, true
		}
	}
	return 0, false
}
