package codexhostpath

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/isty2e/daem/internal/output"
)

func TestResolveHomeRootUsesDefaultAndConfiguredRoots(t *testing.T) {
	home := t.TempDir()
	workDir := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(HomeEnvironmentVariable, "")

	defaultRoot, err := ResolveHomeRoot(workDir)
	if err != nil {
		t.Fatal(err)
	}
	if defaultRoot != filepath.Join(home, ".codex") {
		t.Fatalf("default root = %q", defaultRoot)
	}

	configuredRoot := filepath.Join(t.TempDir(), "custom Codex home")
	t.Setenv(HomeEnvironmentVariable, configuredRoot)
	got, err := ResolveHomeRoot(workDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != configuredRoot {
		t.Fatalf("configured root = %q, want literal %q", got, configuredRoot)
	}
	if _, err := os.Stat(configuredRoot); !os.IsNotExist(err) {
		t.Fatalf("resolving configured root changed its existence: %v", err)
	}
}

func TestResolveHomeRootKeepsRelativeLocatorLiteral(t *testing.T) {
	workDir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "command-was-run")
	command := "$(touch " + marker + ")"
	configuredRoot := "~/$HOME/" + command
	t.Setenv(HomeEnvironmentVariable, configuredRoot)

	got, err := ResolveHomeRoot(workDir)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(workDir, "~", "$HOME", command)
	if got != want {
		t.Fatalf("configured root = %q, want %q", got, want)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("configured locator executed a command: %v", err)
	}
}

func TestResolveHomeRootUsesCurrentWorkingDirectoryWhenOmitted(t *testing.T) {
	t.Setenv(HomeEnvironmentVariable, "relative-root")
	workDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	got, err := ResolveHomeRoot("")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(workDir, "relative-root") {
		t.Fatalf("relative root = %q", got)
	}
}

func TestResolveHomeRootPreservesSymlinkSpelling(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory symlinks require additional Windows privileges")
	}
	physicalRoot := t.TempDir()
	rootParent := t.TempDir()
	configuredRoot := filepath.Join(rootParent, "codex-link")
	if err := os.Symlink(physicalRoot, configuredRoot); err != nil {
		t.Fatal(err)
	}
	t.Setenv(HomeEnvironmentVariable, configuredRoot)

	got, err := ResolveHomeRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got != configuredRoot {
		t.Fatalf("resolved root = %q, want symlink spelling %q", got, configuredRoot)
	}
}

func TestResolveHomeRootRejectsInvalidWorkingDirectories(t *testing.T) {
	for _, workDir := range []string{"relative", " /tmp ", "bad\x00path"} {
		t.Run(workDir, func(t *testing.T) {
			t.Setenv(HomeEnvironmentVariable, "relative-root")
			if _, err := ResolveHomeRoot(workDir); err == nil {
				t.Fatalf("ResolveHomeRoot(%q) succeeded", workDir)
			}
		})
	}
}

func TestResolveHomeRootRejectsInvalidDefaultHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.UserHomeDir uses Windows-specific profile handling")
	}
	t.Setenv(HomeEnvironmentVariable, "")
	t.Setenv("HOME", "relative-home")

	if _, err := ResolveHomeRoot(""); err == nil {
		t.Fatal("ResolveHomeRoot accepted a relative default home")
	}
}

func TestDestinationOverrideMatchesOnlyCodexHomeRole(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing Codex home")
	t.Setenv(HomeEnvironmentVariable, root)
	override := DestinationOverride(t.TempDir())

	codexDestination, err := output.Parse("@codex-home/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	got, matched, err := override(codexDestination)
	if err != nil {
		t.Fatal(err)
	}
	if !matched || got != filepath.Join(root, "config.toml") {
		t.Fatalf("Codex destination override = %q, %v", got, matched)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("destination resolution changed root existence: %v", err)
	}

	piDestination, err := output.Parse("@pi-agent/agents/reviewer.md")
	if err != nil {
		t.Fatal(err)
	}
	if got, matched, err := override(piDestination); err != nil || matched || got != "" {
		t.Fatalf("Pi destination override = %q, %v, %v", got, matched, err)
	}
}
