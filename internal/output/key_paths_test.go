package output_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/output"
)

func TestTopmostKeyPathsPreservesLiteralAncestryAndInputs(t *testing.T) {
	for _, name := range []string{"a", "", "a/b", "~", ".", "..", "line\n\x00", "日本語"} {
		parent := newTopmostKey(t, name)
		child := newTopmostKey(t, name, "child")
		lookalike := newTopmostKey(t, name+"!")
		input := []output.KeyPath{child, lookalike, parent, child, parent}
		before := slices.Clone(input)
		actual := output.TopmostKeyPaths(input)
		if len(actual) != 2 || !slices.Contains(actual, parent) || !slices.Contains(actual, lookalike) || !slices.Equal(input, before) {
			t.Fatal("topmost key selection lost literal identity or mutated its input")
		}
		if len(output.TopmostKeyPaths([]output.KeyPath{parent, child, {}})) != 1 || !output.TopmostKeyPaths([]output.KeyPath{parent, {}})[0].IsRoot() {
			t.Fatal("root selection was confused with a non-root key")
		}
	}
	deep, err := output.ParseKeyPath(strings.Repeat("/deep", 1024))
	if err != nil {
		t.Fatal(err)
	}
	child, err := deep.Append("child")
	if err != nil {
		t.Fatal(err)
	}
	if actual := output.TopmostKeyPaths([]output.KeyPath{child, deep}); len(actual) != 1 || actual[0] != deep {
		t.Fatal("topmost query introduced a key-depth cutoff")
	}
}

func newTopmostKey(t testing.TB, parts ...string) output.KeyPath {
	t.Helper()
	key, err := output.NewKeyPath(parts...)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
