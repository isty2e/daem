package adopt

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/target"
)

func TestDiscoveryLocationsFiltersScopeAndSortsByPriorityThenPath(t *testing.T) {
	got := DiscoveryLocations(target.TargetClaudeCode, entity.KindSkill, target.ScopeGlobal)
	for _, location := range got {
		if location.Scope() != target.ScopeGlobal {
			t.Fatalf("DiscoveryLocations() included scope %q, want %q", location.Scope(), target.ScopeGlobal)
		}
	}
	for index := 1; index < len(got); index++ {
		previous := got[index-1]
		current := got[index]
		if previous.Priority() > current.Priority() ||
			(previous.Priority() == current.Priority() && previous.Path() > current.Path()) {
			t.Fatalf("DiscoveryLocations() is not sorted at %d: %#v before %#v", index, previous, current)
		}
	}
	if len(got) > 0 {
		originalPath := got[0].Path()
		got = nil
		again := DiscoveryLocations(target.TargetClaudeCode, entity.KindSkill, target.ScopeGlobal)
		if len(again) == 0 || again[0].Path() != originalPath {
			t.Fatalf("DiscoveryLocations() returned aliased profile state")
		}
	}
}

func TestCleanProjectDestinationRejectsEscapesAndBackslashes(t *testing.T) {
	testCases := []string{"", ".", "..", "../skill", "a/../../skill", `skills\name`}
	for _, value := range testCases {
		t.Run(value, func(t *testing.T) {
			if _, err := CleanProjectDestination(value); err == nil {
				t.Fatalf("CleanProjectDestination(%q) succeeded, want error", value)
			}
		})
	}
}

func TestCleanProjectDestinationNormalizesSlashRelativePath(t *testing.T) {
	got, err := CleanProjectDestination("skills/./nested/../name")
	if err != nil {
		t.Fatalf("CleanProjectDestination() error = %v", err)
	}
	if got != "skills/name" {
		t.Fatalf("CleanProjectDestination() = %q, want %q", got, "skills/name")
	}
}

func TestResolveDestinationRejectsAbsoluteAndEmptyPaths(t *testing.T) {
	for _, value := range []string{"", "   ", filepath.Join(string(os.PathSeparator), "tmp", "skill")} {
		if _, err := ResolveDestination(value); err == nil {
			t.Fatalf("ResolveDestination(%q) succeeded, want error", value)
		}
	}
}

func TestResolveDestinationExpandsHomeRelativePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}

	got, err := ResolveDestination("~/agents/config.json")
	if err != nil {
		t.Fatalf("ResolveDestination() error = %v", err)
	}
	want := filepath.Join(home, "agents", "config.json")
	if got != want {
		t.Fatalf("ResolveDestination() = %q, want %q", got, want)
	}
}

func TestResolveDestinationCleansProjectRelativePath(t *testing.T) {
	got, err := ResolveDestination("agents/./nested/../config.json")
	if err != nil {
		t.Fatalf("ResolveDestination() error = %v", err)
	}
	want := filepath.Clean(filepath.FromSlash("agents/config.json"))
	if got != want {
		t.Fatalf("ResolveDestination() = %q, want %q", got, want)
	}
}

func TestLocationPathPreservesRelativeAndCleansAbsolutePaths(t *testing.T) {
	relative, err := LocationPath("skills/nested")
	if err != nil {
		t.Fatalf("LocationPath(relative) error = %v", err)
	}
	if relative != filepath.FromSlash("skills/nested") {
		t.Fatalf("LocationPath(relative) = %q", relative)
	}

	absoluteInput := filepath.Join(string(os.PathSeparator), "tmp", "nested", "..", "skill")
	absolute, err := LocationPath(absoluteInput)
	if err != nil {
		t.Fatalf("LocationPath(absolute) error = %v", err)
	}
	if absolute != filepath.Clean(absoluteInput) {
		t.Fatalf("LocationPath(absolute) = %q, want %q", absolute, filepath.Clean(absoluteInput))
	}
}

func TestPathExistsUsesLstatAndPropagatesOtherErrors(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing")
	exists, err := PathExists(missing)
	if err != nil || exists {
		t.Fatalf("PathExists(missing) = (%v, %v), want (false, nil)", exists, err)
	}

	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	exists, err = PathExists(file)
	if err != nil || !exists {
		t.Fatalf("PathExists(file) = (%v, %v), want (true, nil)", exists, err)
	}

	if runtime.GOOS != "windows" {
		link := filepath.Join(root, "link")
		if err := os.Symlink(missing, link); err != nil {
			t.Fatal(err)
		}
		exists, err = PathExists(link)
		if err != nil || !exists {
			t.Fatalf("PathExists(dangling symlink) = (%v, %v), want (true, nil)", exists, err)
		}
	}
}

func TestCleanProjectDestinationErrorQuotesInput(t *testing.T) {
	_, err := CleanProjectDestination("../outside")
	if err == nil || !strings.Contains(err.Error(), `"../outside"`) {
		t.Fatalf("CleanProjectDestination() error = %v, want quoted input", err)
	}
}
