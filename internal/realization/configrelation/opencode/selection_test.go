package opencode

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/target"
)

func TestConfigDirectorySeparatesProjectAndGlobalRoots(t *testing.T) {
	root := t.TempDir()
	globalRoot := filepath.Join(root, "config", "opencode")

	project, err := ConfigDirectory(root, "", target.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, ".opencode"); project != want {
		t.Fatalf("project directory = %q, want %q", project, want)
	}
	global, err := ConfigDirectory("", globalRoot, target.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	if global != globalRoot {
		t.Fatalf("global directory = %q, want %q", global, globalRoot)
	}
}

func TestDefaultGlobalConfigRootUsesXDGOrHome(t *testing.T) {
	root := t.TempDir()
	xdg := filepath.Join(root, "xdg")
	home := filepath.Join(root, "home")

	got, err := DefaultGlobalConfigRoot(xdg, home)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(xdg, "opencode"); got != want {
		t.Fatalf("XDG root = %q, want %q", got, want)
	}
	got, err = DefaultGlobalConfigRoot("", home)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".config", "opencode"); got != want {
		t.Fatalf("home root = %q, want %q", got, want)
	}
}

func TestConfigDirectoryRejectsUnusableRootsAndScope(t *testing.T) {
	tests := []struct {
		name         string
		manifestRoot string
		globalRoot   string
		scope        target.Scope
	}{
		{name: "relative project", manifestRoot: "project", scope: target.ScopeProject},
		{
			name:         "unclean project",
			manifestRoot: t.TempDir() + string(filepath.Separator) + "..",
			scope:        target.ScopeProject,
		},
		{name: "relative global", globalRoot: "config/opencode", scope: target.ScopeGlobal},
		{name: "unknown scope", scope: target.Scope("future")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ConfigDirectory(
				test.manifestRoot,
				test.globalRoot,
				test.scope,
			); err == nil {
				t.Fatal("ConfigDirectory accepted invalid input")
			}
		})
	}
	if _, err := DefaultGlobalConfigRoot("relative", t.TempDir()); err == nil {
		t.Fatal("DefaultGlobalConfigRoot accepted relative XDG root")
	}
}
