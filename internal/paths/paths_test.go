package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveForExplicitManifestUsesManifestRoot(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "project", "daem.toml")

	paths, err := Resolve(manifestPath)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	manifestRoot := filepath.Dir(manifestPath)
	if paths.ManifestPath != manifestPath {
		t.Fatalf("ManifestPath = %q, want %q", paths.ManifestPath, manifestPath)
	}
	if paths.ManifestRoot != manifestRoot {
		t.Fatalf("ManifestRoot = %q, want %q", paths.ManifestRoot, manifestRoot)
	}
	if paths.ManifestOrigin != ManifestOriginExplicit {
		t.Fatalf("ManifestOrigin = %q, want %q", paths.ManifestOrigin, ManifestOriginExplicit)
	}
	if !paths.ProjectPlacementAllowed() {
		t.Fatal("ProjectPlacementAllowed = false, want true")
	}
	if paths.LockfilePath != filepath.Join(manifestRoot, "daem.lock.toml") {
		t.Fatalf("LockfilePath = %q", paths.LockfilePath)
	}
	if paths.StatefilePath != filepath.Join(manifestRoot, ".daem", "state.json") {
		t.Fatalf("StatefilePath = %q", paths.StatefilePath)
	}
	if paths.SourceCacheDir != filepath.Join(manifestRoot, ".daem", "cache", "sources") {
		t.Fatalf("SourceCacheDir = %q", paths.SourceCacheDir)
	}
	if paths.RecoveryDir != filepath.Join(manifestRoot, ".daem", "recovery") {
		t.Fatalf("RecoveryDir = %q", paths.RecoveryDir)
	}
	if paths.OwnershipRegistryPath != filepath.Join(paths.DataDir, "ownership", "claims.json") {
		t.Fatalf("OwnershipRegistryPath = %q", paths.OwnershipRegistryPath)
	}
}

func TestResolveImplicitUsesCWDManifestWhenPresent(t *testing.T) {
	root := t.TempDir()
	withWorkingDirectory(t, root)
	writeFile(t, filepath.Join(root, manifestFileName), "version = 1\n")
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	paths, err := Resolve("")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	if paths.ManifestPath != filepath.Join(workingDirectory, manifestFileName) {
		t.Fatalf("ManifestPath = %q", paths.ManifestPath)
	}
	if paths.ManifestRoot != workingDirectory {
		t.Fatalf("ManifestRoot = %q, want %q", paths.ManifestRoot, workingDirectory)
	}
	if paths.ManifestOrigin != ManifestOriginCWD {
		t.Fatalf("ManifestOrigin = %q, want %q", paths.ManifestOrigin, ManifestOriginCWD)
	}
	if !paths.ProjectPlacementAllowed() {
		t.Fatal("ProjectPlacementAllowed = false, want true")
	}
	if paths.StatefilePath != filepath.Join(workingDirectory, localStateDirName, "state.json") {
		t.Fatalf("StatefilePath = %q", paths.StatefilePath)
	}
}

func TestResolveImplicitDoesNotSearchParentDirectories(t *testing.T) {
	root := t.TempDir()
	configHome := filepath.Join(root, "config")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	writeFile(t, filepath.Join(root, manifestFileName), "version = 1\n")
	child := filepath.Join(root, "child")
	if err := os.MkdirAll(child, 0o700); err != nil {
		t.Fatalf("create child directory: %v", err)
	}
	withWorkingDirectory(t, child)

	paths, err := Resolve("")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	if paths.ManifestPath != filepath.Join(configHome, appDirectoryName, manifestFileName) {
		t.Fatalf("ManifestPath = %q", paths.ManifestPath)
	}
	if paths.ManifestOrigin != ManifestOriginUserDefault {
		t.Fatalf("ManifestOrigin = %q, want %q", paths.ManifestOrigin, ManifestOriginUserDefault)
	}
}

func TestResolveDefaultsUseUnixXDGRoots(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("XDG defaults are only used on Unix-like platforms")
	}

	root := t.TempDir()
	withWorkingDirectory(t, root)
	configHome := filepath.Join(root, "config")
	stateHome := filepath.Join(root, "state")
	cacheHome := filepath.Join(root, "cache")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("XDG_CACHE_HOME", cacheHome)

	paths, err := Resolve("")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	if paths.ManifestPath != filepath.Join(configHome, "daem", "daem.toml") {
		t.Fatalf("ManifestPath = %q", paths.ManifestPath)
	}
	if paths.ManifestOrigin != ManifestOriginUserDefault {
		t.Fatalf("ManifestOrigin = %q, want %q", paths.ManifestOrigin, ManifestOriginUserDefault)
	}
	if paths.ProjectPlacementAllowed() {
		t.Fatal("ProjectPlacementAllowed = true, want false")
	}
	if paths.LockfilePath != filepath.Join(configHome, "daem", "daem.lock.toml") {
		t.Fatalf("LockfilePath = %q", paths.LockfilePath)
	}
	if paths.StatefilePath != filepath.Join(stateHome, "daem", "state.json") {
		t.Fatalf("StatefilePath = %q", paths.StatefilePath)
	}
	if paths.SourceCacheDir != filepath.Join(cacheHome, "daem", "sources") {
		t.Fatalf("SourceCacheDir = %q", paths.SourceCacheDir)
	}
	if paths.RecoveryDir != filepath.Join(stateHome, "daem", "recovery") {
		t.Fatalf("RecoveryDir = %q", paths.RecoveryDir)
	}
}

func TestResolveDefaultsRejectRelativeUnixXDGRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("XDG defaults are only used on Unix-like platforms")
	}

	withWorkingDirectory(t, t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "relative-config")

	_, err := Resolve("")
	if err == nil {
		t.Fatal("Resolve returned nil error, want XDG_CONFIG_HOME diagnostic")
	}
	if !strings.Contains(err.Error(), "XDG_CONFIG_HOME must be an absolute path") {
		t.Fatalf("error = %q, want XDG_CONFIG_HOME diagnostic", err)
	}
}

func TestPathsWithDataDirKeepsOwnershipRegistryCorrelated(t *testing.T) {
	original := Paths{
		DataDir:               filepath.Join(t.TempDir(), "selected"),
		OwnershipRegistryPath: filepath.Join(t.TempDir(), "stale", "claims.json"),
		ManifestPath:          "/manifest",
	}
	physical := filepath.Join(t.TempDir(), "physical-data")
	updated, err := original.WithDataDir(physical)
	if err != nil {
		t.Fatal(err)
	}
	if updated.DataDir != physical {
		t.Fatalf("DataDir = %q, want %q", updated.DataDir, physical)
	}
	wantRegistry := filepath.Join(physical, "ownership", "claims.json")
	if updated.OwnershipRegistryPath != wantRegistry {
		t.Fatalf("OwnershipRegistryPath = %q, want %q", updated.OwnershipRegistryPath, wantRegistry)
	}
	if updated.ManifestPath != original.ManifestPath {
		t.Fatal("WithDataDir changed an unrelated path")
	}
	if original.OwnershipRegistryPath == updated.OwnershipRegistryPath {
		t.Fatal("WithDataDir mutated the receiver or retained stale registry correlation")
	}
}

func withWorkingDirectory(t *testing.T, path string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(path); err != nil {
		t.Fatalf("change working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write file %s: %v", path, err)
	}
}
