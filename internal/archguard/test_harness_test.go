package archguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryGoTestHarnessEnvironment(t *testing.T) {
	if os.Getenv("DAEM_TEST_HARNESS") != "1" {
		t.Fatal("repository guards must run through tools/test-go.sh")
	}

	testRoot := requireHarnessAbsolutePath(t, "DAEM_TEST_ROOT")
	for _, name := range []string{
		"HOME",
		"XDG_CACHE_HOME",
		"XDG_CONFIG_HOME",
		"XDG_DATA_HOME",
		"XDG_STATE_HOME",
	} {
		assertHarnessPathInside(t, name, testRoot)
	}
	for _, name := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "PI_CODING_AGENT_DIR"} {
		if value, present := os.LookupEnv(name); present {
			t.Errorf("%s = %q, want optional host override absent", name, value)
		}
	}

	if original := os.Getenv("DAEM_TEST_ORIGINAL_HOME"); original != "" && original == os.Getenv("HOME") {
		t.Fatalf("HOME = %q, want isolation from invoking HOME", original)
	}
	for current, original := range map[string]string{
		"GOCACHE":    "DAEM_TEST_ORIGINAL_GOCACHE",
		"GOMODCACHE": "DAEM_TEST_ORIGINAL_GOMODCACHE",
		"GOPATH":     "DAEM_TEST_ORIGINAL_GOPATH",
	} {
		if got, want := os.Getenv(current), os.Getenv(original); got == "" || got != want {
			t.Errorf("%s = %q, want preserved value %q", current, got, want)
		}
	}
}

func TestRepositoryGoTestEntrypointsUseHarness(t *testing.T) {
	root := findRepoRoot(t)
	assertFileContainsExactly(t, filepath.Join(root, ".pre-commit-config.yaml"), "entry: tools/test-go.sh ./internal/archguard -count=1", 1)
	assertFileContainsExactly(t, filepath.Join(root, ".github", "workflows", "ci.yml"), "tools/test-go.sh", 3)
	assertFileContainsExactly(t, filepath.Join(root, ".github", "workflows", "release-artifact.yml"), "tools/test-go.sh", 1)
	assertFileContainsExactly(t, filepath.Join(root, "CONTRIBUTING.md"), "tools/test-go.sh", 2)

	info, err := os.Stat(filepath.Join(root, "tools", "test-go.sh"))
	if err != nil {
		t.Fatalf("stat Go test harness: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatal("tools/test-go.sh must be executable")
	}
}

func requireHarnessAbsolutePath(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" || !filepath.IsAbs(value) || filepath.Clean(value) != value {
		t.Fatalf("%s = %q, want a canonical absolute path", name, value)
	}
	return value
}

func assertHarnessPathInside(t *testing.T, name string, root string) {
	t.Helper()
	value := requireHarnessAbsolutePath(t, name)
	relative, err := filepath.Rel(root, value)
	if err != nil {
		t.Fatalf("compare %s with test root: %v", name, err)
	}
	if relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatalf("%s = %q, want a descendant of %q", name, value, root)
	}
}

func assertFileContainsExactly(t *testing.T, path string, fragment string, want int) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if got := strings.Count(string(content), fragment); got != want {
		t.Fatalf("%s contains %q %d times, want %d", path, fragment, got, want)
	}
}
