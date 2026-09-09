package mutation

import (
	"errors"
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

func TestPathSelectionResolverSharesAncestorObservations(t *testing.T) {
	root := t.TempDir()
	resolutions, directoryChecks := 0, 0
	resolve := newPathSelectionResolver(
		func(path string) (string, error) {
			resolutions++
			return filepath.EvalSymlinks(path)
		},
		func(path string) error {
			directoryChecks++
			return requireDirectoryAncestor(path)
		},
	)
	for range 2 {
		for _, path := range []string{
			root,
			filepath.Join(root, "missing", "one"),
			filepath.Join(root, "missing", "two"),
			filepath.Join(root, "other", "three"),
		} {
			want, err := resolveDeepestExisting(path)
			if err != nil {
				t.Fatal(err)
			}
			got, err := resolve(path)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("resolve(%q) = %#v, %v; want %#v", path, got, err, want)
			}
		}
	}
	if resolutions != 1 || directoryChecks != 1 {
		t.Fatalf("ancestor observations: resolutions=%d directories=%d; want 1, 1", resolutions, directoryChecks)
	}
}

func TestPathSelectionResolverDoesNotCacheFailures(t *testing.T) {
	for _, failResolution := range []bool{true, false} {
		name := "directory"
		if failResolution {
			name = "resolution"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			failure := errors.New("observation unavailable")
			attempts := 0
			resolve := newPathSelectionResolver(
				func(path string) (string, error) {
					if failResolution {
						attempts++
						if attempts == 1 {
							return "", failure
						}
					}
					return filepath.EvalSymlinks(path)
				},
				func(path string) error {
					if !failResolution {
						attempts++
						if attempts == 1 {
							return failure
						}
					}
					return requireDirectoryAncestor(path)
				},
			)
			path := filepath.Join(root, "missing", "one")
			if got, err := resolve(path); !errors.Is(err, failure) || !reflect.DeepEqual(got, pathSelection{}) {
				t.Fatalf("failed observation = %#v, %v; want no selection and %v", got, err, failure)
			}
			for _, path := range []string{path, filepath.Join(root, "missing", "two")} {
				want, err := resolveDeepestExisting(path)
				if err != nil {
					t.Fatal(err)
				}
				got, err := resolve(path)
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("retry(%q) = %#v, %v; want %#v", path, got, err, want)
				}
			}
			if attempts != 2 {
				t.Fatalf("observation attempts = %d; want failed and successful attempts", attempts)
			}
		})
	}
}

func TestPathSelectionResolverFreshPassAfterPublication(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "future", "child")
	before, err := newPathSelectionResolver(filepath.EvalSymlinks, requireDirectoryAncestor)(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	after, err := newPathSelectionResolver(filepath.EvalSymlinks, requireDirectoryAncestor)(path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := resolveDeepestExisting(path)
	if err != nil || !reflect.DeepEqual(after, want) || reflect.DeepEqual(before, after) {
		t.Fatalf("fresh selection = %#v, %v; want %#v, different from %#v", after, err, want, before)
	}
}

func TestPathIdentityObserverPreservesAncestorAndSymlinkResults(t *testing.T) {
	root := t.TempDir()
	stored := filepath.Join(root, "Stored")
	if err := os.Mkdir(stored, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(stored, "file")
	if err := os.WriteFile(file, []byte("contents"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, link := range []struct{ target, name string }{
		{"Stored", "directory-link"},
		{filepath.Join("Stored", "file"), "file-link"},
		{"absent", "dangling"},
		{"loop", "loop"},
	} {
		if err := os.Symlink(link.target, filepath.Join(root, link.name)); err != nil {
			t.Skipf("symlink fixture unavailable: %v", err)
		}
	}
	observe := newPathIdentityObserver()
	for range 2 {
		for _, suffix := range []string{
			"Stored", "Stored/file", "Stored/file/child", "directory-link",
			"directory-link/missing/one", "directory-link/missing/two",
			"file-link", "file-link/child", "dangling", "dangling/child", "loop",
		} {
			path := filepath.Join(root, filepath.FromSlash(suffix))
			for _, effect := range []PathEffect{PathEffectDirectoryEntry, PathEffectReferent} {
				want, wantErr := canonicalPathIdentity(path, effect)
				got, gotErr := observe(path, effect)
				if (wantErr == nil) != (gotErr == nil) ||
					(wantErr != nil && gotErr.Error() != wantErr.Error()) || !reflect.DeepEqual(got, want) {
					t.Fatalf("observe(%q, %v) = %#v, %v; want %#v, %v", path, effect, got, gotErr, want, wantErr)
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
