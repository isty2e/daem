package mutation

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPathIdentityObserverMatchesIndependentObservations(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"Stored", "Caf\u00e9", "Cafe\u0301"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	paths := []string{
		root,
		filepath.Join(root, "Stored"),
		filepath.Join(root, "Stored", "missing", "one"),
		filepath.Join(root, "Stored", "missing", "two"),
		filepath.Join(root, "Caf\u00e9"),
		filepath.Join(root, "Cafe\u0301"),
		filepath.Join(root, "Future", "Caf\u00e9"),
		filepath.Join(root, "Future", "Cafe\u0301"),
	}
	observe := newPathIdentityObserver()
	for range 2 {
		for _, path := range paths {
			for _, effect := range []PathEffect{PathEffectDirectoryEntry, PathEffectReferent} {
				want, err := canonicalPathIdentity(path, effect)
				if err != nil {
					t.Fatal(err)
				}
				got, err := observe(path, effect)
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("observe(%q, %v) = %#v, %v; want %#v", path, effect, got, err, want)
				}
			}
		}
	}
}

func TestSelectPathDoesNotMutateSharedMissingSuffix(t *testing.T) {
	missing := make([]string, 1, 4)
	missing[0] = "future"
	root := t.TempDir()
	resolve := func(string) (pathSelection, error) {
		return pathSelection{anchorPath: root, missingComponents: missing}, nil
	}
	first, err := selectPathWithResolver(filepath.Join(root, "one"), PathEffectDirectoryEntry, resolve)
	if err != nil {
		t.Fatal(err)
	}
	second, err := selectPathWithResolver(filepath.Join(root, "two"), PathEffectDirectoryEntry, resolve)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.missingComponents, []string{"future", "one"}) ||
		!reflect.DeepEqual(second.missingComponents, []string{"future", "two"}) ||
		!reflect.DeepEqual(missing, []string{"future"}) {
		t.Fatalf("shared suffix changed: first=%v second=%v source=%v", first, second, missing)
	}
}

func TestPathIdentityObserverPreservesInvalidInputErrors(t *testing.T) {
	observe := newPathIdentityObserver()
	for _, request := range []struct {
		path   string
		effect PathEffect
	}{
		{"", PathEffectReferent},
		{"a\x00b", PathEffectDirectoryEntry},
		{t.TempDir(), PathEffect(0)},
	} {
		_, want := canonicalPathIdentity(request.path, request.effect)
		_, got := observe(request.path, request.effect)
		if want == nil || got == nil || got.Error() != want.Error() {
			t.Fatalf("error = %v; want %v", got, want)
		}
	}
}
