package record

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/output"
)

func TestRecoveryFootprintScanMatchesNativeScopeOracle(t *testing.T) {
	candidates := []output.Footprint{output.WholePathFootprint()}
	for _, pointer := range []string{
		"/", "//child", "/a", "/a!", "/a!!", "/a/b", "/a/b/c", "/a~1b", "/a~01b",
		"/日", "/旦", "/日/child", "/\x00", "/\x00/child",
		strings.Repeat("/", 4096) + "a", strings.Repeat("/", 4096) + "b",
	} {
		key, err := output.ParseKeyPath(pointer)
		if err != nil {
			t.Fatal(err)
		}
		footprint, err := output.NewKeyFootprint(key)
		if err != nil {
			t.Fatal(err)
		}
		candidates = append(candidates, footprint)
	}
	random := rand.New(rand.NewSource(4))
	for iteration := range 300 {
		footprints := make([]output.Footprint, 1+random.Intn(8))
		for index := range footprints {
			footprints[index] = candidates[random.Intn(len(candidates))]
		}
		overlaps := false
		for right, footprint := range footprints {
			for left := range right {
				overlaps = overlaps || footprints[left].Overlaps(footprint)
			}
		}
		if err := validateNonOverlappingRecoveryFootprints(footprints); (err != nil) != overlaps {
			t.Fatalf("iteration %d overlap refusal = %v, want %v", iteration, err, overlaps)
		}
	}
}

func TestRecoveryFootprintScanRetainsNonadjacentAncestors(t *testing.T) {
	var footprints []output.Footprint
	for _, keys := range [][]string{{"a"}, {"a!"}, {"a!!"}, {"a", "b"}} {
		key, err := output.NewKeyPath(keys...)
		if err != nil {
			t.Fatal(err)
		}
		footprint, err := output.NewKeyFootprint(key)
		if err != nil {
			t.Fatal(err)
		}
		footprints = append(footprints, footprint)
	}
	if err := validateNonOverlappingRecoveryFootprints(footprints); err == nil {
		t.Fatal("raw-prefix neighbors hid the later key descendant")
	}
	if err := validateNonOverlappingRecoveryFootprints(footprints[1:]); err != nil {
		t.Fatalf("raw-prefix siblings were treated as ancestors: %v", err)
	}
}
