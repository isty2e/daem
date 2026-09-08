//go:build darwin || linux

package access

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
)

func TestViewsFollowFilesystemCaseLookupForAbsoluteRoots(t *testing.T) {
	for _, constructor := range []struct {
		name string
		open func(string) (View, error)
	}{
		{name: "parent-resolved", open: OpenView},
		{name: "no-follow", open: OpenNoFollowView},
	} {
		for _, kind := range []artifact.ArtifactKind{artifact.ArtifactKindFile, artifact.ArtifactKindDirectory} {
			for _, spelling := range []struct {
				name   string
				parent string
				root   string
			}{
				{name: "exact", parent: "StoredParent", root: "StoredRoot"},
				{name: "root-case", parent: "StoredParent", root: "storedroot"},
				{name: "ancestor-case", parent: "storedparent", root: "StoredRoot"},
				{name: "both-case", parent: "storedparent", root: "storedroot"},
			} {
				t.Run(fmt.Sprintf("%s/%s/%s", constructor.name, kind, spelling.name), func(t *testing.T) {
					base := resolvedAccessTestRoot(t)
					storedParent := filepath.Join(base, "StoredParent")
					storedRoot := filepath.Join(storedParent, "StoredRoot")
					selectedRoot := filepath.Join(base, spelling.parent, spelling.root)
					readPath := "."
					if kind == artifact.ArtifactKindDirectory {
						readPath = "Nested/Exact"
					}
					contentPath := filepath.Join(storedRoot, readPath)
					writeAccessTestFile(t, contentPath, []byte("original\n"))
					identity := accessTestIdentity(t, storedRoot)
					storedInfo, err := os.Stat(storedRoot)
					if err != nil {
						t.Fatal(err)
					}
					selectedInfo, lookupErr := os.Stat(selectedRoot)
					view, err := constructor.open(selectedRoot)
					if errors.Is(lookupErr, fs.ErrNotExist) {
						if !errors.Is(err, fs.ErrNotExist) {
							t.Fatalf("Open missing native spelling error = %v, want fs.ErrNotExist", err)
						}
						t.Log("native namespace rejects the absent case variant")
						return
					}
					if lookupErr != nil {
						t.Fatalf("native lookup: %v", lookupErr)
					}
					if !os.SameFile(storedInfo, selectedInfo) {
						t.Fatal("case lookup selected a different fixture object")
					}
					if err != nil {
						t.Fatalf("Open existing native spelling: %v", err)
					}
					if view.Kind() != kind {
						t.Fatalf("Kind = %q, want %q", view.Kind(), kind)
					}
					content, err := view.ReadFile(t.Context(), readPath, 64)
					if err != nil || string(content.Bytes()) != "original\n" {
						t.Fatalf("ReadFile = %q, %v", content.Bytes(), err)
					}
					if got, err := view.Hash(t.Context()); err != nil || got != identity.ContentHash() {
						t.Fatalf("Hash = %q, %v; want %q", got, err, identity.ContentHash())
					}
					if kind == artifact.ArtifactKindDirectory {
						entries, err := view.ReadDirectory(t.Context(), ".")
						if err != nil || len(entries) != 1 || entries[0].Name() != "Nested" {
							t.Fatalf("ReadDirectory = %v, %v", entries, err)
						}
						for _, relative := range []string{"nested/Exact", "Nested/exact"} {
							if _, err := view.ReadFile(t.Context(), relative, 64); err == nil {
								t.Fatalf("ReadFile accepted non-exact relative spelling %q", relative)
							}
						}
					}

					if err := os.Rename(storedParent, filepath.Join(base, "retained")); err != nil {
						t.Fatal(err)
					}
					writeAccessTestFile(t, contentPath, []byte("original\n"))
					replacementInfo, err := os.Stat(selectedRoot)
					if err != nil || os.SameFile(selectedInfo, replacementInfo) {
						t.Fatalf("replacement did not establish a different native object: %v", err)
					}
					if _, err := constructor.open(selectedRoot); err != nil {
						t.Fatalf("fresh capture rejected replacement: %v", err)
					}
					if _, err := view.Hash(t.Context()); err == nil {
						t.Fatal("captured view accepted same-byte replacement through its root spelling")
					}
				})
			}
		}
	}
}

func TestViewsKeepCaseDistinctAbsoluteRootsSeparate(t *testing.T) {
	base := resolvedAccessTestRoot(t)
	upper := filepath.Join(base, "Root")
	lower := filepath.Join(base, "root")
	writeAccessTestFile(t, upper, []byte("upper\n"))
	if _, err := os.Stat(lower); err == nil {
		t.Skip("fixture namespace resolves the case variant; same-object coverage runs separately")
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("probe case lookup: %v", err)
	}
	writeAccessTestFile(t, lower, []byte("lower\n"))

	for _, constructor := range []struct {
		name string
		open func(string) (View, error)
	}{
		{name: "parent-resolved", open: OpenView},
		{name: "no-follow", open: OpenNoFollowView},
	} {
		for _, selection := range []struct {
			path    string
			content string
		}{
			{path: upper, content: "upper\n"},
			{path: lower, content: "lower\n"},
		} {
			t.Run(constructor.name+"/"+filepath.Base(selection.path), func(t *testing.T) {
				view, err := constructor.open(selection.path)
				if err != nil {
					t.Fatal(err)
				}
				content, err := view.ReadFile(t.Context(), ".", 64)
				if err != nil || string(content.Bytes()) != selection.content {
					t.Fatalf("ReadFile = %q, %v; want %q", content.Bytes(), err, selection.content)
				}
			})
		}
	}
}
