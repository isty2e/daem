package native

import (
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

func TestCapturedLocationsAreIndependentOfLaterEnvironmentChanges(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CODEX_HOME", "before")
	locations, err := CaptureLocations(work)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", "after")
	reference, err := configuration.RootReferenceForScope(target.TargetCodex, target.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	location, err := locations.Resolve(reference)
	if err != nil || location.Path() != filepath.Join(work, "before") || location.Source() != "CODEX_HOME" {
		t.Fatal("captured locator changed with the ambient environment", location, err)
	}
	if _, err := os.Stat(location.Path()); !os.IsNotExist(err) {
		t.Fatal("resolving a locator created its root", err)
	}
}

func TestNativeLocatorContractsKeepDistinctAnchorsAndLiteralParents(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	values := map[string]string{"CODEX_HOME": "link/../codex", "PI_CODING_AGENT_DIR": "~/custom/pi"}
	locations, err := NewLocations(work, home, values)
	if err != nil {
		t.Fatal(err)
	}
	values["CODEX_HOME"] = "changed"
	contracts, err := configuration.NativeRootContracts()
	if err != nil {
		t.Fatal(err)
	}
	for _, contract := range contracts {
		location, err := locations.Resolve(contract.Reference())
		if err != nil {
			t.Fatal(err)
		}
		var want string
		switch contract.Reference().Identity() {
		case "pi-agent":
			want = filepath.Join(home, "custom", "pi")
		case "codex-home":
			want = work + string(filepath.Separator) + "link/../codex"
		case "claude-preferences":
			want = filepath.Join(home, ".claude")
		case "claude-account":
			want = home
		case "opencode-configuration":
			want = filepath.Join(home, ".config", "opencode")
		case "antigravity-preferences":
			want = filepath.Join(home, ".gemini", "antigravity-cli")
		case "antigravity-shared":
			want = filepath.Join(home, ".gemini", "config")
		default:
			t.Fatalf("unexamined locator contract %q", contract.Reference().Identity())
		}
		if location.Path() != want {
			t.Errorf("%s = %q, want %q", contract.Reference().Identity(), location.Path(), want)
		}
	}
}

func TestNativeLocationsRejectIncompleteOrDifferentLocatorContracts(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	locations, err := NewLocations(work, home, map[string]string{"CODEX_HOME": "bad\x00location"})
	if err != nil {
		t.Fatal(err)
	}
	reference, _ := configuration.RootReferenceForScope(target.TargetCodex, target.ScopeGlobal)
	if _, err := locations.Resolve(reference); err == nil || strings.Contains(err.Error(), "bad") {
		t.Fatal("invalid locator was admitted or disclosed", err)
	}
	foreign, err := output.NewRootReference(reference.Identity(), "different-location-v1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := locations.Resolve(foreign); err == nil {
		t.Fatal("matching identity substituted a different locator contract")
	}
	if _, err := (Locations{}).Resolve(reference); err == nil {
		t.Fatal("zero context invented a location")
	}
}

func TestPiLocatorUsesLexicalParentsWhileCodexPreservesFilesystemParents(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Win32 parent components are lexical before reparse traversal")
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(base, "actual", "inner"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "actual", "inner"), filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}
	literal := "link/../agent"
	locations, err := NewLocations(base, t.TempDir(), map[string]string{"PI_CODING_AGENT_DIR": literal, "CODEX_HOME": literal})
	if err != nil {
		t.Fatal(err)
	}
	pi, _ := configuration.RootReferenceForScope(target.TargetPi, target.ScopeGlobal)
	codex, _ := configuration.RootReferenceForScope(target.TargetCodex, target.ScopeGlobal)
	piLocation, err := locations.Resolve(pi)
	if err != nil || piLocation.Path() != filepath.Join(base, "agent") {
		t.Fatal("Pi's Node path join was replaced by physical parent interpretation", piLocation, err)
	}
	codexLocation, err := locations.Resolve(codex)
	if err != nil || codexLocation.Path() != base+"/"+literal {
		t.Fatal("Codex locator was lexically cleaned before filesystem traversal", codexLocation, err)
	}
}

func TestPiLocatorFileURLsAndSpacesKeepTheHostContract(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	filePath := filepath.Join(home, "agent directory")
	fileURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(filePath)}).String()
	if runtime.GOOS == "windows" {
		fileURL = (&url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(filePath)}).String()
	}
	for _, input := range []struct{ value, want string }{
		{fileURL, filePath},
		{" relative directory ", filepath.Join(work, " relative directory ")},
		{"~/pi home", filepath.Join(home, "pi home")},
	} {
		got, err := PiAgentDirectory(input.value, work, home)
		if err != nil || got != input.want {
			t.Fatalf("Pi locator = %q, %v; want %q", got, err, input.want)
		}
	}
	for _, value := range []string{"file://%zz", "file:///tmp/a%2fb", "file://user@localhost/path", "file:///tmp/a%00b"} {
		if _, err := PiAgentDirectory(value, work, home); err == nil {
			t.Fatal("invalid Pi file locator was admitted")
		}
	}
}

func TestPiWindowsShellNormalizationUsesTheUpstreamGrammar(t *testing.T) {
	for _, input := range []struct{ value, want string }{
		{"/c", `C:\`},
		{"/mnt/d/work", `D:\work`},
		{"/cygdrive/z/a/b", `Z:\a\b`},
		{"//host/share", "//host/share"},
		{`/c/a\b`, `/c/a\b`},
		{"/case/path", "/case/path"},
	} {
		if got := normalizePiWindowsShellPath(input.value); got != input.want {
			t.Fatalf("shell locator %q = %q; want %q", input.value, got, input.want)
		}
	}
}
