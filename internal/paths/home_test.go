package paths

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestPathsRetainSelectedHomeAcrossRebasing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix home and XDG selection")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	for _, kind := range []string{"CONFIG", "STATE", "CACHE", "DATA"} {
		t.Setenv("XDG_"+kind+"_HOME", filepath.Join(root, kind))
	}
	selected, err := Resolve(filepath.Join(root, "CONFIG", "daem", "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	rebased, err := selected.WithDataDir(filepath.Join(root, "new-data"))
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := selected.LegacyUserState()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", filepath.Join(root, "later-home"))
	for _, paths := range []Paths{selected, rebased, legacy} {
		if actual, err := paths.UserHomeDir(); err != nil || actual != home {
			t.Fatal("path rebasing reselected ambient home", actual, err)
		}
	}
}

func TestPathsDeferUnusedHomeFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix home and XDG selection")
	}
	root := t.TempDir()
	t.Setenv("HOME", "")
	for _, kind := range []string{"CONFIG", "STATE", "CACHE", "DATA"} {
		t.Setenv("XDG_"+kind+"_HOME", filepath.Join(root, kind))
	}
	selected, err := Resolve(filepath.Join(root, "workspace", "daem.toml"))
	if err != nil {
		t.Fatal("unrelated project resolution eagerly required home", err)
	}
	t.Setenv("HOME", filepath.Join(root, "later-home"))
	if _, err := selected.UserHomeDir(); err == nil {
		t.Fatal("home failure was replaced by ambient fallback")
	}
	if _, err := (Paths{}).UserHomeDir(); err == nil {
		t.Fatal("unselected paths acquired ambient home")
	}
}
