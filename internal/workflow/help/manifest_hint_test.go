package help

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManifestHintPathsKeepSelectionFamiliesDistinct(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	for _, variable := range []string{"CONFIG", "STATE", "CACHE", "DATA"} {
		t.Setenv("XDG_"+variable+"_HOME", filepath.Join(root, variable))
	}
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(cwd, "daem.toml")
	user := filepath.Join(root, "CONFIG", "daem")
	if err := os.MkdirAll(user, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(user, "daem.toml"), []byte("version=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	userManifest := filepath.Join(user, "daem.toml")
	if got := ImplicitManifestHintPath(""); got != userManifest {
		t.Fatalf("implicit user hint target=%q, want %q", got, userManifest)
	}
	if got := ImplicitManifestHintPath(userManifest); got != "" {
		t.Fatalf("explicit user selector admitted for elision: %q", got)
	}
	if got := CreationManifestHintPath(); got != manifest {
		t.Fatalf("creation target=%q, want absent cwd path %q", got, manifest)
	}

	if err := os.WriteFile(manifest, []byte("version=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ImplicitManifestHintPath(""); got != manifest {
		t.Fatalf("existing cwd target=%q, want %q", got, manifest)
	}
	if got := CreationManifestHintPath(); got != manifest {
		t.Fatalf("existing creation target=%q, want %q", got, manifest)
	}
	if got := ImplicitManifestHintPath("./daem.toml"); got != manifest {
		t.Fatalf("relative explicit cwd target=%q, want %q", got, manifest)
	}

	t.Setenv("XDG_DATA_HOME", "invalid-relative-root")
	if existing, creation := ImplicitManifestHintPath(""), CreationManifestHintPath(); existing != "" || creation != "" {
		t.Fatalf("unavailable observation retained selection evidence: existing=%q creation=%q", existing, creation)
	}
}
