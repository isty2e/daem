package pathauthority_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
)

func TestExactRequiresVersionedSemanticsWitness(t *testing.T) {
	key := filepath.Join(t.TempDir(), "state.json")
	for _, witness := range []string{"", "darwin-case-v1:x", "future-v1:"} {
		if _, err := pathauthority.NewExact(key, witness); err == nil {
			t.Fatalf("NewExact(%q) accepted malformed witness", witness)
		}
	}
}

func TestExactRejectsNonUTF8Key(t *testing.T) {
	key := filepath.Join(t.TempDir(), string([]byte{'b', 'a', 'd', 0xff}))
	if _, err := pathauthority.NewExact(key, "exact-v1:"); err == nil {
		t.Fatal("NewExact accepted a path key that cannot round-trip through JSON")
	}
}

func TestExactDarwinWitnessMustCoverEveryPathComponent(t *testing.T) {
	key := string(filepath.Separator) + filepath.Join("one", "two")
	if _, err := pathauthority.NewExact(key, "darwin-case-v1:ss"); err != nil {
		t.Fatalf("NewExact returned error: %v", err)
	}
	if _, err := pathauthority.NewExact(key, "darwin-case-v1:s"); err == nil {
		t.Fatal("NewExact accepted incomplete Darwin witness")
	}
}

func TestFoldedWitnessesRequireFoldedKeys(t *testing.T) {
	key := filepath.Join(t.TempDir(), "Mixed")
	if _, err := pathauthority.NewExact(key, "windows-fold-v1:"); err == nil {
		t.Fatal("NewExact accepted mixed-case Windows-fold key")
	}
	darwinWitness := "darwin-case-v1:"
	for _, component := range splitRootedPath(key) {
		if component == "Mixed" {
			darwinWitness += "i"
		} else {
			darwinWitness += "s"
		}
	}
	if _, err := pathauthority.NewExact(key, darwinWitness); err == nil {
		t.Fatal("NewExact accepted mixed-case insensitive Darwin component")
	}
}

func TestExactEqualityIncludesSemanticsWitness(t *testing.T) {
	key := filepath.Join(t.TempDir(), "managed")
	exact, err := pathauthority.NewExact(key, "exact-v1:")
	if err != nil {
		t.Fatalf("NewExact exact: %v", err)
	}
	darwin, err := pathauthority.NewExact(
		key,
		"darwin-case-v1:"+strings.Repeat("s", len(splitRootedPath(key))),
	)
	if err != nil {
		t.Fatalf("NewExact Darwin: %v", err)
	}
	if exact.Equal(darwin) {
		t.Fatal("authorities with different semantics witnesses compare equal")
	}
	if exact.Compare(darwin) == 0 || exact.Compare(darwin) != -darwin.Compare(exact) {
		t.Fatal("authority order ignored differing semantics witnesses")
	}
	if !exact.Contains(darwin) || !darwin.Contains(exact) {
		t.Fatal("same-key semantic drift must retain conservative path overlap")
	}
}

func splitRootedPath(path string) []string {
	root := filepath.VolumeName(path) + string(filepath.Separator)
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." {
		return nil
	}
	return strings.Split(relative, string(filepath.Separator))
}
