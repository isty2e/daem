package testkit

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func RepositoryRoot(t testing.TB) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	for candidate := filepath.Dir(file); ; candidate = filepath.Dir(candidate) {
		if _, err := os.Stat(filepath.Join(candidate, "go.mod")); err == nil {
			return candidate
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			t.Fatalf("repository root not found from %q", file)
		}
	}
}

func WriteFile(t *testing.T, root string, relativePath string, content string) {
	t.Helper()

	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}

func AuthoringTransactionDir(stateDir string) string {
	return filepath.Join(stateDir, "metadata-transaction")
}

func SetDefaultRootEnv(t *testing.T, root string) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", filepath.Join(root, "appdata", "roaming"))
		t.Setenv("LOCALAPPDATA", filepath.Join(root, "appdata", "local"))
		return
	}

	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
}

func SetDataRootEnv(t *testing.T, root string) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", filepath.Join(root, "appdata", "local"))
		return
	}
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
}

// RunWithIsolatedDefaultRoots runs one test package with private user roots.
func RunWithIsolatedDefaultRoots(m *testing.M) int {
	root, err := os.MkdirTemp("", "daem-test-roots-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create isolated test roots: %v\n", err)
		return 1
	}

	type environmentSetting struct {
		name  string
		value string
	}
	settings := []environmentSetting{
		{name: "HOME", value: filepath.Join(root, "home")},
		{name: "CODEX_HOME", value: filepath.Join(root, "home", ".codex")},
		{name: "CLAUDE_CONFIG_DIR", value: filepath.Join(root, "home", ".claude")},
	}
	for _, directory := range []string{
		filepath.Join(root, "home"),
		filepath.Join(root, "home", ".codex"),
		filepath.Join(root, "home", ".claude"),
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			fmt.Fprintf(os.Stderr, "create isolated test directory %s: %v\n", directory, err)
			_ = os.RemoveAll(root)
			return 1
		}
	}
	if runtime.GOOS == "windows" {
		settings = append(
			settings,
			environmentSetting{name: "USERPROFILE", value: filepath.Join(root, "home")},
			environmentSetting{name: "APPDATA", value: filepath.Join(root, "appdata", "roaming")},
			environmentSetting{name: "LOCALAPPDATA", value: filepath.Join(root, "appdata", "local")},
		)
	} else {
		settings = append(
			settings,
			environmentSetting{name: "XDG_CONFIG_HOME", value: filepath.Join(root, "config")},
			environmentSetting{name: "XDG_STATE_HOME", value: filepath.Join(root, "state")},
			environmentSetting{name: "XDG_CACHE_HOME", value: filepath.Join(root, "cache")},
			environmentSetting{name: "XDG_DATA_HOME", value: filepath.Join(root, "data")},
		)
	}
	for _, setting := range settings {
		if err := os.Setenv(setting.name, setting.value); err != nil {
			fmt.Fprintf(os.Stderr, "set isolated test root %s: %v\n", setting.name, err)
			_ = os.RemoveAll(root)
			return 1
		}
	}

	exitCode := m.Run()
	if err := os.RemoveAll(root); err != nil && exitCode == 0 {
		fmt.Fprintf(os.Stderr, "remove isolated test roots: %v\n", err)
		return 1
	}
	return exitCode
}

func WithWorkingDirectory(t *testing.T, path string) {
	t.Helper()

	originalWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	if err := os.Chdir(path); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalWorkingDirectory); err != nil {
			t.Fatalf("Chdir cleanup returned error: %v", err)
		}
	})
}

func AssertFileContent(t *testing.T, path string, want string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %q returned error: %v", path, err)
	}
	if string(content) != want {
		t.Fatalf("content of %q = %q, want %q", path, content, want)
	}
}

func ReadFile(t testing.TB, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	return content
}

func AssertPathMissing(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("path %q exists or stat returned unexpected error: %v", path, err)
	}
}

func AssertDirectoryEntryMissingExact(t *testing.T, root string, name string) {
	t.Helper()

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir %q returned error: %v", root, err)
	}
	for _, entry := range entries {
		if entry.Name() == name {
			t.Fatalf("directory %q has exact entry %q", root, name)
		}
	}
}

func AssertDirectoryEntryExistsExact(t *testing.T, root string, name string) {
	t.Helper()

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir %q returned error: %v", root, err)
	}
	for _, entry := range entries {
		if entry.Name() == name {
			return
		}
	}
	t.Fatalf("directory %q is missing exact entry %q", root, name)
}

func AssertOutputLine(t *testing.T, output string, want string) {
	t.Helper()

	for _, line := range strings.Split(strings.TrimRight(output, "\n"), "\n") {
		if line == want {
			return
		}
	}
	t.Fatalf("output = %q, want line %q", output, want)
}

func AssertContainsInOrder(t testing.TB, text string, orderedSubstrings ...string) {
	t.Helper()
	offset := 0
	for _, substring := range orderedSubstrings {
		index := strings.Index(text[offset:], substring)
		if index < 0 {
			t.Fatalf("text = %q, want %q after byte offset %d", text, substring, offset)
		}
		offset += index + len(substring)
	}
}

func AssertNoRecoveryArtifacts(t *testing.T, root string) {
	t.Helper()

	for _, path := range []string{
		filepath.Join(root, ".daem", "snapshots"),
		filepath.Join(root, ".daem", "recovery"),
	} {
		entries, err := os.ReadDir(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatalf("ReadDir %q returned error: %v", path, err)
		}
		if len(entries) != 0 {
			t.Fatalf("%q contains recovery artifacts: %#v", path, entries)
		}
	}
}
