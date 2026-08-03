//go:build unix

package tooling

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const focusedProbePackage = "./test/tooling/testdata/focusedprobe"

func TestFocusedRunnerCachesAndInvalidatesTrackedInputs(t *testing.T) {
	root := findRepoRoot(t)
	fixtureRoot := createFocusedFixtureRepository(t)
	temporaryRoot := t.TempDir()
	baseEnvironment := []string{
		"TMPDIR=" + temporaryRoot,
		"DAEM_FOCUSED_PROBE_VALUE=one",
	}
	first := runFocusedFixture(t, root, fixtureRoot, baseEnvironment...)
	if strings.Contains(first, "(cached)") {
		t.Fatalf("first focused probe unexpectedly used cached result:\n%s", first)
	}
	second := runFocusedFixture(t, root, fixtureRoot, baseEnvironment...)
	if !strings.Contains(second, "(cached)") {
		t.Fatalf("unchanged focused probe did not use cached result:\n%s", second)
	}

	changedEnvironment := append([]string(nil), baseEnvironment...)
	changedEnvironment[len(changedEnvironment)-1] = "DAEM_FOCUSED_PROBE_VALUE=two"
	third := runFocusedFixture(t, root, fixtureRoot, changedEnvironment...)
	if strings.Contains(third, "(cached)") {
		t.Fatalf("changed environment reused stale focused result:\n%s", third)
	}

	if err := os.WriteFile(filepath.Join(fixtureRoot, "probe", "fixture.txt"), []byte("two\n"), 0o600); err != nil {
		t.Fatalf("change focused fixture: %v", err)
	}
	fourth := runFocusedFixture(t, root, fixtureRoot, changedEnvironment...)
	if strings.Contains(fourth, "(cached)") {
		t.Fatalf("changed fixture reused stale focused result:\n%s", fourth)
	}

	if err := os.WriteFile(filepath.Join(fixtureRoot, "probe", "probe.go"), []byte("package probe\n\nconst SourceVersion = 2\n"), 0o600); err != nil {
		t.Fatalf("change focused source: %v", err)
	}
	fifth := runFocusedFixture(t, root, fixtureRoot, changedEnvironment...)
	if strings.Contains(fifth, "(cached)") {
		t.Fatalf("changed source reused stale focused result:\n%s", fifth)
	}
	assertFocusedRunnerResidueAbsent(t, temporaryRoot)
}

func TestFocusedRunnerRejectsPackagePatternsAndSubtestRegexes(t *testing.T) {
	root := findRepoRoot(t)
	for _, arguments := range [][]string{
		{"./internal/..."},
		{focusedProbePackage, "TestFocusedCacheProbe/subtest"},
	} {
		command := exec.Command(filepath.Join(root, "tools", "test-focused.sh"), arguments...)
		command.Dir = root
		output, err := command.CombinedOutput()
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) || exitError.ExitCode() != 2 {
			t.Errorf("focused arguments %v result = %v, want exit 2\n%s", arguments, err, output)
		}
	}
}

func TestFocusedRunnerRejectsConcurrentPackageExecution(t *testing.T) {
	root := findRepoRoot(t)
	temporaryRoot := t.TempDir()
	ready := filepath.Join(temporaryRoot, "ready")
	release := filepath.Join(temporaryRoot, "release")

	first := focusedProbeCommand(
		root,
		"TMPDIR="+temporaryRoot,
		"DAEM_FOCUSED_PROBE_READY="+ready,
		"DAEM_FOCUSED_PROBE_RELEASE="+release,
	)
	if err := first.Start(); err != nil {
		t.Fatalf("start first focused probe: %v", err)
	}
	waitForFocusedPath(t, ready)

	second := focusedProbeCommand(root, "TMPDIR="+temporaryRoot)
	output, err := second.CombinedOutput()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 2 {
		t.Fatalf("concurrent focused probe result = %v, want exit 2\n%s", err, output)
	}
	if !strings.Contains(string(output), "already active") {
		t.Fatalf("concurrent focused probe diagnostic = %q", output)
	}

	if err := os.WriteFile(release, []byte("release\n"), 0o600); err != nil {
		t.Fatalf("release first focused probe: %v", err)
	}
	if err := first.Wait(); err != nil {
		t.Fatalf("first focused probe failed: %v", err)
	}
	assertFocusedRunnerResidueAbsent(t, temporaryRoot)
}

func TestFocusedRunnerRejectsAliasedSharedBase(t *testing.T) {
	root := findRepoRoot(t)
	temporaryRoot := t.TempDir()
	outside := t.TempDir()
	base := filepath.Join(temporaryRoot, "daem-focused-test-v1")
	if err := os.Symlink(outside, base); err != nil {
		t.Fatalf("alias focused base: %v", err)
	}

	command := focusedProbeCommand(root, "TMPDIR="+temporaryRoot)
	output, err := command.CombinedOutput()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 2 {
		t.Fatalf("aliased focused base result = %v, want exit 2\n%s", err, output)
	}
	if !strings.Contains(string(output), "private directory") {
		t.Fatalf("aliased focused base diagnostic = %q", output)
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatalf("inspect aliased destination: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("aliased focused base received entries: %v", entries)
	}
}

func TestFocusedRunnerPreservesFailureAndCleansRoot(t *testing.T) {
	root := findRepoRoot(t)
	fixtureRoot := createFocusedFixtureRepository(t)
	temporaryRoot := t.TempDir()
	command := exec.Command(
		filepath.Join(root, "tools", "test-focused.sh"),
		"./probe",
		"^TestFailure$",
	)
	command.Dir = fixtureRoot
	command.Env = append(
		withoutEnvironment(os.Environ(), "GOENV", "GOFLAGS", "GOWORK", "TMPDIR"),
		"TMPDIR="+temporaryRoot,
	)
	output, err := command.CombinedOutput()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 1 {
		t.Fatalf("focused failure result = %v, want exit 1\n%s", err, output)
	}
	if !strings.Contains(string(output), "FAIL") {
		t.Fatalf("focused failure output = %q, want Go test failure", output)
	}
	assertFocusedRunnerResidueAbsent(t, temporaryRoot)
}

func TestFocusedRunnerForwardsTerminationAndCleansRoot(t *testing.T) {
	root := findRepoRoot(t)
	temporaryRoot := t.TempDir()
	ready := filepath.Join(temporaryRoot, "ready")
	release := filepath.Join(temporaryRoot, "never-released")
	command := focusedProbeCommand(
		root,
		"TMPDIR="+temporaryRoot,
		"DAEM_FOCUSED_PROBE_READY="+ready,
		"DAEM_FOCUSED_PROBE_RELEASE="+release,
	)
	if err := command.Start(); err != nil {
		t.Fatalf("start focused signal probe: %v", err)
	}
	waitForFocusedPath(t, ready)
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("terminate focused runner: %v", err)
	}
	err := command.Wait()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 143 {
		t.Fatalf("terminated focused runner result = %v, want exit 143", err)
	}
	assertFocusedRunnerResidueAbsent(t, temporaryRoot)
}

func runFocusedFixture(t *testing.T, root string, fixtureRoot string, environment ...string) string {
	t.Helper()
	command := exec.Command(
		filepath.Join(root, "tools", "test-focused.sh"),
		"./probe",
		"^TestFixture$",
	)
	command.Dir = fixtureRoot
	command.Env = append(
		withoutEnvironment(os.Environ(), "GOENV", "GOFLAGS", "GOWORK", "TMPDIR"),
		"GOENV="+filepath.Join(root, "internal", "archguard", "testdata", "hostile-go.env"),
		"GOFLAGS=-run=^NoFocusedTestsShouldMatch$",
		"GOWORK="+filepath.Join(root, "internal", "archguard", "testdata", "missing.work"),
	)
	command.Env = append(command.Env, environment...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("focused probe failed: %v\n%s", err, output)
	}
	return string(output)
}

func focusedProbeCommand(root string, environment ...string) *exec.Cmd {
	command := exec.Command(
		filepath.Join(root, "tools", "test.sh"),
		"focused",
		focusedProbePackage,
		"^TestFocusedCacheProbe$",
	)
	command.Dir = root
	command.Env = append(
		withoutEnvironment(os.Environ(), "GOENV", "GOFLAGS", "GOWORK", "TMPDIR"),
		"GOENV="+filepath.Join(root, "internal", "archguard", "testdata", "hostile-go.env"),
		"GOFLAGS=-run=^NoFocusedTestsShouldMatch$",
		"GOWORK="+filepath.Join(root, "internal", "archguard", "testdata", "missing.work"),
	)
	command.Env = append(command.Env, environment...)
	return command
}

func createFocusedFixtureRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod":              "module example.com/focusedprobe\n\ngo 1.25.0\n",
		"probe/probe.go":      "package probe\n\nconst SourceVersion = 1\n",
		"probe/fixture.txt":   "one\n",
		"probe/probe_test.go": "package probe\n\nimport (\"os\"; \"testing\")\n\nfunc TestFixture(t *testing.T) {\n if _, err := os.ReadFile(\"fixture.txt\"); err != nil { t.Fatal(err) }\n _ = os.Getenv(\"DAEM_FOCUSED_PROBE_VALUE\")\n _ = SourceVersion\n}\n\nfunc TestFailure(t *testing.T) { t.Fatal(\"expected failure\") }\n",
	}
	for relative, content := range files {
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("create focused fixture parent: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write focused fixture %s: %v", relative, err)
		}
	}
	for _, arguments := range [][]string{
		{"init"},
		{"config", "user.email", "daem@example.invalid"},
		{"config", "user.name", "Daem Test"},
		{"add", "."},
		{"commit", "-m", "fixture"},
	} {
		command := exec.Command("git", arguments...)
		command.Dir = root
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v for focused fixture: %v\n%s", arguments, err, output)
		}
	}
	fixture := filepath.Join(root, "probe", "fixture.txt")
	old := time.Now().Add(-5 * time.Second)
	if err := os.Chtimes(fixture, old, old); err != nil {
		t.Fatalf("age focused fixture: %v", err)
	}
	return root
}

func waitForFocusedPath(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !os.IsNotExist(err) {
			t.Fatalf("observe focused marker: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func assertFocusedRunnerResidueAbsent(t *testing.T, temporaryRoot string) {
	t.Helper()
	path := filepath.Join(temporaryRoot, "daem-focused-test-v1")
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("focused runner residue at %s: %v", path, err)
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatalf("could not find repository root from %s", directory)
		}
		directory = parent
	}
}

func withoutEnvironment(environment []string, names ...string) []string {
	excluded := make(map[string]struct{}, len(names))
	for _, name := range names {
		excluded[name] = struct{}{}
	}
	filtered := make([]string, 0, len(environment))
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		if _, skip := excluded[name]; !skip {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}
