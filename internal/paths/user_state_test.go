package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestUserManifestSelectionSharesStorageWithoutChangingPlacement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix XDG selection")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	manifest := filepath.Join(root, "config", "daem", manifestFileName)
	if err := os.MkdirAll(filepath.Dir(manifest), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, manifest, "version = 1\n")
	withWorkingDirectory(t, root)
	fallback, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	explicit, err := Resolve(manifest)
	if err != nil {
		t.Fatal(err)
	}
	withWorkingDirectory(t, filepath.Dir(manifest))
	cwd, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	for _, selected := range []Paths{fallback, explicit, cwd} {
		if selected.StatefilePath != fallback.StatefilePath || selected.RecoveryDir != fallback.RecoveryDir || selected.SourceCacheDir != fallback.SourceCacheDir {
			t.Fatalf("selection %q has split storage: %#v", selected.ManifestOrigin, selected)
		}
		if selected.LegacyUserStateDir != filepath.Join(filepath.Dir(manifest), ".daem") {
			t.Fatalf("legacy = %q", selected.LegacyUserStateDir)
		}
	}
	if fallback.ProjectPlacementAllowed() || !explicit.ProjectPlacementAllowed() || !cwd.ProjectPlacementAllowed() {
		t.Fatal("storage selection changed placement admission")
	}
	if explicit.ManifestOrigin != ManifestOriginExplicit || cwd.ManifestOrigin != ManifestOriginCWD || fallback.ManifestOrigin != ManifestOriginUserDefault {
		t.Fatal("selection provenance collapsed")
	}

	withWorkingDirectory(t, root)
	created, err := ResolveCreation("")
	if err != nil {
		t.Fatal(err)
	}
	if created.ManifestPath != filepath.Join(root, manifestFileName) || created.StateDir != filepath.Join(root, ".daem") {
		t.Fatalf("creation fell back to user storage: %#v", created)
	}
	project, err := Resolve(filepath.Join(root, "project", manifestFileName))
	if err != nil {
		t.Fatal(err)
	}
	if project.StateDir != filepath.Join(root, "project", ".daem") || project.LegacyUserStateDir != "" {
		t.Fatalf("project = %#v", project)
	}
}

func TestUserManifestDirectoryAliasesDoNotCollapseOtherEntries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix XDG selection")
	}
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	manifest := filepath.Join(root, "config", "daem", manifestFileName)
	if err := os.MkdirAll(filepath.Dir(manifest), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, manifest, "version = 1\n")
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(filepath.Dir(manifest), alias); err != nil {
		t.Fatal(err)
	}
	selected, err := Resolve(filepath.Join(alias, manifestFileName))
	if err != nil {
		t.Fatal(err)
	}
	if selected.StateDir != filepath.Join(root, "state", "daem") {
		t.Fatalf("directory alias split user storage: %#v", selected)
	}

	for _, name := range []string{"copy.toml", "linked.toml"} {
		other := filepath.Join(filepath.Dir(manifest), name)
		if name == "copy.toml" {
			if err := os.Link(manifest, other); err != nil {
				t.Fatal(err)
			}
		} else if err := os.Symlink(manifest, other); err != nil {
			t.Fatal(err)
		}
		selected, err := Resolve(other)
		if err != nil {
			t.Fatal(err)
		}
		if selected.StateDir != filepath.Join(filepath.Dir(manifest), ".daem") {
			t.Fatalf("distinct entry collapsed into user storage: %#v", selected)
		}
	}
	caseAlias := filepath.Join(filepath.Dir(manifest), "DAEM.TOML")
	if _, err := os.Lstat(caseAlias); err == nil {
		selected, err := Resolve(caseAlias)
		if err != nil {
			t.Fatal(err)
		}
		if selected.StateDir != filepath.Join(root, "state", "daem") {
			t.Fatal("case alias split user storage")
		}
	} else if os.IsNotExist(err) {
		if err := os.Link(manifest, caseAlias); err != nil {
			t.Fatal(err)
		}
		selected, err := Resolve(caseAlias)
		if err != nil {
			t.Fatal(err)
		}
		if selected.StateDir != filepath.Join(filepath.Dir(manifest), ".daem") {
			t.Fatal("case-distinct hardlink collapsed into user storage")
		}
	} else {
		t.Fatal(err)
	}
}
