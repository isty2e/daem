package output_test

import (
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/output"
)

func TestKeyPathRoundTripPreservesNativeKeys(t *testing.T) {
	cases := [][]string{
		nil,
		{""},
		{"", ""},
		{"projects", "/Users/example/project"},
		{"a.b", "..", ".", "~1", "~", "a/b", `a\b`},
		{"a ", " leading", "0", "日本語", "line\nbreak", "nul\x00key"},
	}
	for _, keys := range cases {
		path, err := output.NewKeyPath(keys...)
		if err != nil {
			t.Fatalf("construct %q: %v", keys, err)
		}
		parsed, err := output.ParseKeyPath(path.String())
		if err != nil {
			t.Fatalf("parse %q: %v", path.String(), err)
		}
		if parsed != path || !slices.Equal(parsed.Segments(), keys) {
			t.Fatalf("round trip %q produced %q", keys, parsed.Segments())
		}
	}
}

func TestKeyPathRejectsInvalidPointers(t *testing.T) {
	for _, pointer := range []string{"a/b", "#/a", " /a", "/a~", "/a~2", "/~x", "/\xff"} {
		if _, err := output.ParseKeyPath(pointer); err == nil {
			t.Errorf("accepted invalid pointer %q", pointer)
		}
	}
	if _, err := output.NewKeyPath("valid", "\xff"); err == nil {
		t.Fatal("accepted invalid UTF-8 key")
	}
}

func TestKeyPathUnescapesOnlyOnce(t *testing.T) {
	path, err := output.ParseKeyPath("/~01/~10/~00")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := path.Segments(), []string{"~1", "/0", "~0"}; !slices.Equal(got, want) {
		t.Fatalf("decoded keys = %q, want %q", got, want)
	}
}

func TestKeyPathContainmentUsesNativeHierarchy(t *testing.T) {
	cases := []struct {
		parent, child string
		contains      bool
	}{
		{"", "/a", true},
		{"", "", true},
		{"/a", "/a", true},
		{"/a", "/a/b", true},
		{"/a", "/ab", false},
		{"/a", "/a~1b", false},
		{"/a~1b", "/a~1b/c", true},
		{"/a", "/a/", true},
		{"/", "//child", true},
		{"/", "/child", false},
		{"/a/..", "/b", false},
	}
	for _, item := range cases {
		parent, err := output.ParseKeyPath(item.parent)
		if err != nil {
			t.Fatal(err)
		}
		child, err := output.ParseKeyPath(item.child)
		if err != nil {
			t.Fatal(err)
		}
		if parent.Contains(child) != item.contains {
			t.Errorf("%q contains %q = %v", parent, child, !item.contains)
		}
		if got := parent.Overlaps(child); got != child.Overlaps(parent) {
			t.Errorf("overlap is asymmetric for %q and %q", parent, child)
		}
	}
}

func TestKeyPathAppendAndParentDistinguishRootAndEmptyKey(t *testing.T) {
	var root output.KeyPath
	if !root.IsRoot() {
		t.Fatal("zero path is not the document root")
	}
	if _, ok := root.Parent(); ok {
		t.Fatal("root has a parent")
	}

	emptyKey, err := root.Append("")
	if err != nil {
		t.Fatal(err)
	}
	if emptyKey.IsRoot() || emptyKey.String() != "/" {
		t.Fatalf("empty key confused with root: %q", emptyKey)
	}
	child, err := emptyKey.Append("a/b")
	if err != nil {
		t.Fatal(err)
	}
	parent, ok := child.Parent()
	if !ok || parent != emptyKey {
		t.Fatalf("parent of %q = %q, %v", child, parent, ok)
	}
	if _, err := child.Append("\xff"); err == nil {
		t.Fatal("append accepted invalid UTF-8")
	}
}

func TestKeyPathOverlapMatchesExactNativeAncestry(t *testing.T) {
	cases := []struct {
		left, right string
		want        bool
	}{
		{"/a", "/a/b", true},
		{"/a/b", "/a", true},
		{"/a", "/a~1b", false},
		{"", "/", true},
		{"/", "/a", false},
		{"/a", "/ab", false},
	}
	for _, item := range cases {
		left, err := output.ParseKeyPath(item.left)
		if err != nil {
			t.Fatal(err)
		}
		right, err := output.ParseKeyPath(item.right)
		if err != nil {
			t.Fatal(err)
		}
		if got := left.Overlaps(right); got != item.want {
			t.Errorf("overlap(%q, %q) = %v, want %v", left, right, got, item.want)
		}
	}
}

func TestKeyPathSegmentsCannotMutateIdentity(t *testing.T) {
	keys := []string{"a/b", "~key"}
	path, err := output.NewKeyPath(keys...)
	if err != nil {
		t.Fatal(err)
	}
	keys[0] = "changed input"
	segments := path.Segments()
	segments[1] = "changed output"
	if got, want := path.Segments(), []string{"a/b", "~key"}; !slices.Equal(got, want) {
		t.Fatalf("key identity changed through caller slice: %q", got)
	}
}
