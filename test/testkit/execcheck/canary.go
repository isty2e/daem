package execcheck

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type Canary struct {
	dir      string
	commands []string
}

func New(t testing.TB, commands ...string) Canary {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable canary is not portable to Windows")
	}
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	markerDir := filepath.Join(root, "markers")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("create canary bin dir: %v", err)
	}
	if err := os.MkdirAll(markerDir, 0o700); err != nil {
		t.Fatalf("create canary marker dir: %v", err)
	}
	for _, command := range commands {
		writeExecutable(t, binDir, command)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DAEM_TEST_EXECUTABLE_CANARY_DIR", markerDir)
	return Canary{dir: markerDir, commands: append([]string(nil), commands...)}
}

func AssertClean(t testing.TB, canary Canary, label string) {
	t.Helper()
	entries, err := os.ReadDir(canary.dir)
	if err != nil {
		t.Fatalf("%s read canary dir: %v", label, err)
	}
	if len(entries) == 0 {
		return
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	t.Fatalf("%s invoked forbidden command(s): %s", label, strings.Join(names, ", "))
}

func AssertInvoked(t testing.TB, canary Canary, command string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(canary.dir, command+".invoked"))
	if err != nil {
		t.Fatalf("read canary marker for %q: %v", command, err)
	}
	want := "invoked " + command + "\n"
	if string(content) != want {
		t.Fatalf("canary marker for %q = %q, want %q", command, content, want)
	}
}

func writeExecutable(t testing.TB, binDir string, command string) {
	t.Helper()
	script := `#!/bin/sh
set -eu
marker_dir="${DAEM_TEST_EXECUTABLE_CANARY_DIR:?missing canary dir}"
name="$(basename "$0")"
printf 'invoked %s\n' "$name" > "$marker_dir/$name.invoked"
exit 97
`
	path := filepath.Join(binDir, command)
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write canary executable %q: %v", command, err)
	}
}
