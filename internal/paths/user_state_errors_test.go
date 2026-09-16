package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProjectSelectionWithoutDefaultUserAddress(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix XDG selection")
	}
	for _, fault := range []string{"relative_config", "missing_home"} {
		t.Run(fault, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
			t.Setenv("XDG_CONFIG_HOME", "relative-config")
			if fault == "missing_home" {
				t.Setenv("XDG_CONFIG_HOME", "")
				t.Setenv("HOME", "")
			}

			for _, mode := range []string{"explicit_other_name", "explicit_default_name", "cwd", "creation"} {
				t.Run(mode, func(t *testing.T) {
					project := t.TempDir()
					manifest := filepath.Join(project, manifestFileName)
					if mode == "explicit_other_name" {
						manifest = filepath.Join(project, "project.toml")
					}
					if mode != "creation" {
						writeFile(t, manifest, "version = 1\n")
					}
					withWorkingDirectory(t, project)

					var selected Paths
					var err error
					switch mode {
					case "cwd":
						selected, err = Resolve("")
					case "creation":
						selected, err = ResolveCreation("")
					default:
						selected, err = Resolve(manifest)
					}
					if err != nil {
						t.Fatal(err)
					}
					if selected.StateDir != filepath.Join(selected.ManifestRoot, localStateDirName) || selected.LegacyUserStateDir != "" {
						t.Fatalf("project storage = %#v", selected)
					}
					if !selected.ProjectPlacementAllowed() {
						t.Fatal("project placement was lost")
					}
				})
			}

			withWorkingDirectory(t, root)
			if _, err := Resolve(""); err == nil {
				t.Fatal("fallback accepted an invalid default-user address")
			}
		})
	}
}

func TestUserManifestIdentityErrorsDoNotSelectProjectStorage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix directory permissions")
	}
	root := t.TempDir()
	configHome := filepath.Join(root, "blocked", "config")
	userDir := filepath.Join(root, "user")
	for _, dir := range []string{configHome, userDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(userDir, filepath.Join(configHome, appDirectoryName)); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(userDir, manifestFileName)
	writeFile(t, manifest, "version = 1\n")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))

	selected, err := Resolve(manifest)
	if err != nil || selected.StateDir != filepath.Join(root, "state", appDirectoryName) {
		t.Fatalf("accessible user alias: %#v, %v", selected, err)
	}

	blocked := filepath.Dir(configHome)
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(blocked, 0o700); err != nil {
			t.Error(err)
		}
	})
	if _, err := os.Stat(filepath.Join(configHome, appDirectoryName)); !os.IsPermission(err) {
		t.Skipf("fixture cannot enforce inaccessible config: %v", err)
	}

	for _, project := range []string{userDir, t.TempDir()} {
		manifest := filepath.Join(project, manifestFileName)
		writeFile(t, manifest, "version = 1\n")
		withWorkingDirectory(t, project)
		for _, argument := range []string{manifest, ""} {
			if _, err := Resolve(argument); !os.IsPermission(err) {
				t.Fatalf("unknown user identity must refuse: %v", err)
			}
		}
		if _, err := ResolveCreation(""); !os.IsPermission(err) {
			t.Fatalf("creation ignored unknown user identity: %v", err)
		}
		other := filepath.Join(project, "project.toml")
		writeFile(t, other, "version = 1\n")
		selected, err := Resolve(other)
		if err != nil || selected.StateDir != filepath.Join(project, localStateDirName) {
			t.Fatalf("non-candidate depends on config access: %#v, %v", selected, err)
		}
	}
}

func TestMatchedUserManifestRejectsInvalidStorageRoots(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix XDG selection")
	}
	root := t.TempDir()
	manifest := filepath.Join(root, appDirectoryName, manifestFileName)
	if err := os.MkdirAll(filepath.Dir(manifest), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, manifest, "version = 1\n")
	t.Setenv("XDG_CONFIG_HOME", root)
	withWorkingDirectory(t, filepath.Dir(manifest))

	for _, name := range []string{"XDG_STATE_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "relative-root")
			for _, argument := range []string{manifest, ""} {
				if _, err := Resolve(argument); err == nil || !strings.Contains(err.Error(), name) {
					t.Fatalf("invalid matched-user storage %s: %v", name, err)
				}
			}
		})
	}
}
