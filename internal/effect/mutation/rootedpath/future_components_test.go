//go:build darwin || linux || windows

package rootedpath

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestFutureReferentComponentsPreserveExistingDirectoryAndSuffix(t *testing.T) {
	root := referentTestRoot(t)
	file := filepath.Join(root, "settings.json")
	if err := os.WriteFile(file, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		path   string
		suffix []string
	}{
		{"existing directory", root, nil},
		{"existing entry", file, []string{"settings.json"}},
		{"missing suffix", filepath.Join(root, "future", "settings.json"), []string{"future", "settings.json"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			anchor, suffix, err := ResolveFutureReferentComponentsBounded(test.path, 256, &referentTestBudget{limit: 10_000})
			if err != nil || anchor != root || !slices.Equal(suffix, test.suffix) {
				t.Fatalf("physical selection = %q + %q, %v; want %q + %q", anchor, suffix, err, root, test.suffix)
			}
		})
	}
}

func TestFutureReferentComponentsRejectWithoutPartialSelection(t *testing.T) {
	for _, selected := range []string{"", "relative", filepath.Join(referentTestRoot(t), "file")} {
		anchor, suffix, err := ResolveFutureReferentComponentsBounded(selected, 256, &referentTestBudget{})
		if err == nil || anchor != "" || suffix != nil {
			t.Fatalf("failed traversal supplied partial selection: %q + %q, %v", anchor, suffix, err)
		}
	}
}
