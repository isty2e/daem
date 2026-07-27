package hostpath

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/output"
)

func mustDestination(t testing.TB, value string) output.Destination {
	t.Helper()
	destination, err := output.Parse(value)
	if err != nil {
		t.Fatalf("output.Parse(%q) returned error: %v", value, err)
	}
	return destination
}

func TestResolverExpandsProjectDestination(t *testing.T) {
	root := t.TempDir()
	resolver := NewResolver(root)

	got, err := resolver.Resolve(mustDestination(t, "nested/skill"))
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	want := filepath.Join(root, "nested", "skill")
	if got != want {
		t.Fatalf("Resolve = %q, want %q", got, want)
	}
}

func TestResolverExpandsHomeDestinationFromCurrentEnvironment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	got, err := NewResolver(t.TempDir()).Resolve(mustDestination(t, "~/agents/skills"))
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	want := filepath.Join(home, "agents", "skills")
	if got != want {
		t.Fatalf("Resolve = %q, want %q", got, want)
	}
}

func TestResolverRejectsMalformedHomeRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.UserHomeDir normalizes USERPROFILE differently on Windows")
	}
	t.Setenv("HOME", " relative ")

	_, err := NewResolver(t.TempDir()).Resolve(mustDestination(t, "~/agents/skills"))
	if err == nil || !strings.Contains(err.Error(), "trimmed absolute path") {
		t.Fatalf("Resolve error = %v, want malformed home-root rejection", err)
	}
}

func TestResolverUsesSelectedManagedDataRoot(t *testing.T) {
	projectRoot := t.TempDir()
	firstDataRoot := filepath.Join(t.TempDir(), "data-one")
	secondDataRoot := filepath.Join(t.TempDir(), "data-two")
	destination := mustDestination(t, "@data/hook-assets/example")

	first, err := NewResolverWithManagedDataRoot(projectRoot, firstDataRoot).Resolve(destination)
	if err != nil {
		t.Fatalf("first Resolve returned error: %v", err)
	}
	second, err := NewResolverWithManagedDataRoot(projectRoot, secondDataRoot).Resolve(destination)
	if err != nil {
		t.Fatalf("second Resolve returned error: %v", err)
	}
	if first != filepath.Join(firstDataRoot, "hook-assets", "example") {
		t.Fatalf("first Resolve = %q", first)
	}
	if second != filepath.Join(secondDataRoot, "hook-assets", "example") {
		t.Fatalf("second Resolve = %q", second)
	}
	if first == second {
		t.Fatal("data-root resolution ignored the operation-selected root")
	}
}

func TestResolverRejectsMissingOrUnsafeManagedDataRoot(t *testing.T) {
	projectRoot := t.TempDir()
	destination := mustDestination(t, "@data/hook-assets/example")
	for _, dataRoot := range []string{"", " relative ", "relative", string(filepath.Separator)} {
		_, err := NewResolverWithManagedDataRoot(projectRoot, dataRoot).Resolve(destination)
		if err == nil {
			t.Fatalf("Resolve accepted data root %q", dataRoot)
		}
	}
}

func TestResolverRejectsMissingOrRelativeProjectRoot(t *testing.T) {
	for _, projectRoot := range []string{"", " relative ", "relative"} {
		_, err := NewResolver(projectRoot).Resolve(mustDestination(t, "AGENTS.md"))
		if err == nil || !strings.Contains(err.Error(), "project root") {
			t.Fatalf("Resolve with project root %q error = %v, want root rejection", projectRoot, err)
		}
	}
}

func TestResolverRejectsZeroDestination(t *testing.T) {
	dataRoot := filepath.Join(t.TempDir(), "data")
	var destination output.Destination
	if _, err := NewResolverWithManagedDataRoot(t.TempDir(), dataRoot).Resolve(destination); err == nil {
		t.Fatal("Resolve accepted a zero destination")
	}
}

func TestResolverDoesNotRequireUnusedRoots(t *testing.T) {
	projectRoot := t.TempDir()
	if _, err := NewResolver(projectRoot).Resolve(mustDestination(t, "AGENTS.md")); err != nil {
		t.Fatalf("project Resolve required an unused data root: %v", err)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if _, err := NewResolver("").Resolve(mustDestination(t, "~/AGENTS.md")); err != nil {
		t.Fatalf("home Resolve required an unused project root: %v", err)
	}
}

func TestResolverUsesCurrentHomeOnEveryResolution(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows caches USERPROFILE through os.UserHomeDir")
	}
	firstHome := t.TempDir()
	secondHome := t.TempDir()
	resolver := NewResolver(t.TempDir())
	t.Setenv("HOME", firstHome)

	first, err := resolver.Resolve(mustDestination(t, "~/AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("HOME", secondHome); err != nil {
		t.Fatal(err)
	}
	second, err := resolver.Resolve(mustDestination(t, "~/AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if first != filepath.Join(firstHome, "AGENTS.md") || second != filepath.Join(secondHome, "AGENTS.md") {
		t.Fatalf("home resolutions = %q, %q", first, second)
	}
}
