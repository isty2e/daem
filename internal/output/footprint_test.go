package output_test

import (
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/output"
)

func footprint(t *testing.T, keys ...string) output.Footprint {
	t.Helper()
	path, err := output.NewKeyPath(keys...)
	if err != nil {
		t.Fatal(err)
	}
	value, err := output.NewKeyFootprint(path)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestFootprintSeparatesRootEmptyKeyAndDocument(t *testing.T) {
	whole := output.WholePathFootprint()
	emptyKey := footprint(t, "")
	if whole.Kind() != output.FootprintPath || emptyKey.Kind() != output.FootprintKey || whole.Equal(emptyKey) {
		t.Fatal("document and empty property ownership collapsed")
	}
	if key, ok := emptyKey.Key(); !ok || key.String() != "/" {
		t.Fatal("empty property path lost")
	}
	if _, ok := whole.Key(); ok {
		t.Fatal("document authority exposed as root key ownership")
	}
	if _, err := output.NewKeyFootprint(output.KeyPath{}); !errors.Is(err, output.ErrInvalidFootprint) {
		t.Fatalf("root selector: %v", err)
	}
	if (output.Footprint{}).Contains(whole) || (output.Footprint{}).Overlaps(whole) || (output.Footprint{}).Equal(output.Footprint{}) {
		t.Fatal("uninitialized footprint grants authority")
	}
	if !whole.Contains(emptyKey) || emptyKey.Contains(whole) {
		t.Fatal("whole-document containment is not directional")
	}
}

func TestLegacyFootprintConversionNeverDecodesEscapes(t *testing.T) {
	for _, test := range []struct {
		legacy output.ContentPath
		keys   []string
	}{
		{"/a~1b", []string{"a~1b"}},
		{"/x/~0", []string{"x", "~0"}},
		{"/100%25ready", []string{"100%25ready"}},
		{"/literal.dot/back\\slash", []string{"literal.dot", "back\\slash"}},
	} {
		converted, err := output.FootprintFromLegacy(test.legacy)
		if err != nil {
			t.Fatal(err)
		}
		if !converted.Equal(footprint(t, test.keys...)) {
			t.Errorf("reinterpreted legacy content path %q", test.legacy)
		}
		legacy, err := converted.LegacyContentPath()
		if err != nil || legacy != test.legacy {
			t.Errorf("legacy round trip %q: %q, %v", test.legacy, legacy, err)
		}
	}
	legacy, err := output.FootprintFromLegacy("/a~1b")
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Equal(footprint(t, "a/b")) {
		t.Fatal("legacy literal tilde became a native slash")
	}
	whole, err := output.FootprintFromLegacy("")
	if err != nil || !whole.Equal(output.WholePathFootprint()) {
		t.Fatalf("legacy whole document: %v", err)
	}
	for _, invalid := range []output.ContentPath{"/", "/a//b", "/a/../b", "/a/", "/\x00", "/\xff"} {
		if _, err := output.FootprintFromLegacy(invalid); !errors.Is(err, output.ErrInvalidFootprint) {
			t.Errorf("invalid legacy path %q: %v", invalid, err)
		}
	}
}

func TestNativeFootprintRefusesLossyLegacyEncoding(t *testing.T) {
	for _, keys := range [][]string{{""}, {"a/b"}, {"a", ""}, {"."}, {".."}, {"line\nkey"}} {
		value := footprint(t, keys...)
		if _, err := value.LegacyContentPath(); !errors.Is(err, output.ErrNotLegacyFootprint) {
			t.Errorf("lossy legacy encoding admitted for %q: %v", keys, err)
		}
	}
	legacy, err := footprint(t, "~1").LegacyContentPath()
	if err != nil || legacy != "/~1" {
		t.Fatalf("literal tilde round trip: %q, %v", legacy, err)
	}
}

func TestFootprintOverlapPreservesLegacyRelations(t *testing.T) {
	paths := []output.ContentPath{"", "/a", "/a/b", "/ab", "/a-b", "/a~1b", "/x/~0", "/x/~0/child"}
	for _, left := range paths {
		for _, right := range paths {
			leftFootprint, err := output.FootprintFromLegacy(left)
			if err != nil {
				t.Fatal(err)
			}
			rightFootprint, err := output.FootprintFromLegacy(right)
			if err != nil {
				t.Fatal(err)
			}
			if leftFootprint.Overlaps(rightFootprint) != left.Overlaps(right) {
				t.Errorf("legacy overlap changed: %q and %q", left, right)
			}
		}
	}
	root := footprint(t, "a/b")
	child := footprint(t, "a/b", "child")
	other := footprint(t, "a", "b")
	if !root.Contains(child) || child.Contains(root) || root.Overlaps(other) {
		t.Fatal("native key ancestry reinterpreted slashes")
	}
}

func TestFootprintComparisonIsTotalAndConsistentWithIdentity(t *testing.T) {
	values := []output.Footprint{output.WholePathFootprint(), footprint(t, ""), footprint(t, "a"), footprint(t, "a", "b"), footprint(t, "a/b")}
	for _, left := range values {
		for _, right := range values {
			forward := output.CompareFootprints(left, right)
			backward := output.CompareFootprints(right, left)
			if forward != -backward || (forward == 0) != left.Equal(right) {
				t.Fatal("footprint ordering differs from identity")
			}
		}
	}
}
