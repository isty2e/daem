package access

import "testing"

func TestTreeStructureLimitOwnsDescendantEntryAndDirectoryDepthBounds(t *testing.T) {
	limit, err := NewTreeStructureLimit(100_000, 64)
	if err != nil {
		t.Fatal(err)
	}
	if limit.maximumEntries != 100_000 || limit.maximumDepth != 64 {
		t.Fatalf(
			"tree structure limit = entries:%d depth:%d",
			limit.maximumEntries,
			limit.maximumDepth,
		)
	}
	if err := limit.validate(); err != nil {
		t.Fatalf("validate returned error: %v", err)
	}
}

func TestTreeStructureLimitRejectsInvalidBounds(t *testing.T) {
	for _, test := range []struct {
		name    string
		entries int
		depth   int
	}{
		{name: "negative entries", entries: -1, depth: 1},
		{name: "negative depth", entries: 1, depth: -1},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewTreeStructureLimit(test.entries, test.depth); err == nil {
				t.Fatal("NewTreeStructureLimit accepted invalid bounds")
			}
		})
	}
	if limit, err := NewTreeStructureLimit(0, 0); err != nil {
		t.Fatalf("NewTreeStructureLimit rejected an empty-tree bound: %v", err)
	} else if err := limit.validate(); err != nil {
		t.Fatalf("empty-tree limit validation failed: %v", err)
	}
	if err := (TreeStructureLimit{}).validate(); err == nil {
		t.Fatal("zero TreeStructureLimit passed validation")
	}
}
