package archguard

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestRepositoryGoTestHarnessEnvironment(t *testing.T) {
	if os.Getenv("DAEM_TEST_HARNESS") != "1" {
		t.Fatal("repository guards must run through tools/test-go.sh")
	}

	testRoot := requireHarnessAbsolutePath(t, "DAEM_TEST_ROOT")
	packageRoot := requireHarnessAbsolutePath(t, "DAEM_TEST_PACKAGE_ROOT")
	assertPathDescendsFrom(t, "DAEM_TEST_PACKAGE_ROOT", packageRoot, testRoot)
	for _, name := range []string{
		"HOME",
		"XDG_CACHE_HOME",
		"XDG_CONFIG_HOME",
		"XDG_DATA_HOME",
		"XDG_STATE_HOME",
	} {
		assertHarnessPathInside(t, name, packageRoot)
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
	if got := os.Getenv("GOENV"); got != "off" {
		t.Errorf("GOENV = %q, want off", got)
	}
	if got := os.Getenv("GOWORK"); got != "off" {
		t.Errorf("GOWORK = %q, want off", got)
	}
	if got, present := os.LookupEnv("GOFLAGS"); present {
		t.Errorf("GOFLAGS = %q, want absent", got)
	}
}

func TestRepositoryGoTestEntrypointsUseHarness(t *testing.T) {
	root := findRepoRoot(t)
	assertFileContainsExactly(t, filepath.Join(root, ".pre-commit-config.yaml"), "entry: tools/test.sh repository", 1)
	assertFileContainsExactly(t, filepath.Join(root, ".github", "workflows", "ci.yml"), "tools/test.sh", 3)
	assertFileContainsExactly(t, filepath.Join(root, ".github", "workflows", "release-artifact.yml"), "tools/test.sh", 1)
	assertFileContainsExactly(t, filepath.Join(root, "CONTRIBUTING.md"), "tools/test.sh", 6)

	laneInfo, err := os.Stat(filepath.Join(root, "tools", "test.sh"))
	if err != nil {
		t.Fatalf("stat test lane owner: %v", err)
	}
	if laneInfo.Mode()&0o111 == 0 {
		t.Fatal("tools/test.sh must be executable")
	}
	focusedInfo, err := os.Stat(filepath.Join(root, "tools", "test-focused.sh"))
	if err != nil {
		t.Fatalf("stat focused test runner: %v", err)
	}
	if focusedInfo.Mode()&0o111 == 0 {
		t.Fatal("tools/test-focused.sh must be executable")
	}
	raceProofInfo, err := os.Stat(filepath.Join(root, "tools", "test-race-proof.sh"))
	if err != nil {
		t.Fatalf("stat race detector proof: %v", err)
	}
	if raceProofInfo.Mode()&0o111 == 0 {
		t.Fatal("tools/test-race-proof.sh must be executable")
	}
	assertFileContainsExactly(t, filepath.Join(root, "tools", "test.sh"), "tools/test-race-proof.sh", 1)
	assertFileContainsExactly(t, filepath.Join(root, "tools", "test.sh"), "GORACE=atexit_sleep_ms=0", 1)
	assertFileContainsExactly(t, filepath.Join(root, "tools", "test-race-proof.sh"), "GORACE=atexit_sleep_ms=0", 1)

	info, err := os.Stat(filepath.Join(root, "tools", "test-go.sh"))
	if err != nil {
		t.Fatalf("stat Go test harness: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatal("tools/test-go.sh must be executable")
	}
	execInfo, err := os.Stat(filepath.Join(root, "tools", "test-exec.sh"))
	if err != nil {
		t.Fatalf("stat package test wrapper: %v", err)
	}
	if execInfo.Mode()&0o111 == 0 {
		t.Fatal("tools/test-exec.sh must be executable")
	}
}

func TestRepositoryGoTestPackageWrapperUsesDistinctRoots(t *testing.T) {
	root := findRepoRoot(t)
	wrapper := filepath.Join(root, "tools", "test-exec.sh")
	testRoot := t.TempDir()

	type result struct {
		output string
		err    error
	}
	results := make(chan result, 2)
	var started sync.WaitGroup
	started.Add(2)
	for range 2 {
		go func() {
			started.Done()
			started.Wait()
			command := exec.Command(wrapper, "sh", "-c", `printf '%s\n%s\n' "$HOME" "$XDG_STATE_HOME"`)
			command.Env = append(
				withoutEnvironment(os.Environ(), "DAEM_TEST_ROOT"),
				"DAEM_TEST_HARNESS=1",
				"DAEM_TEST_ROOT="+testRoot,
			)
			output, err := command.CombinedOutput()
			results <- result{output: string(output), err: err}
		}()
	}

	homes := make(map[string]struct{}, 2)
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatalf("package wrapper failed: %v\n%s", result.err, result.output)
		}
		lines := strings.Split(strings.TrimSpace(result.output), "\n")
		if len(lines) != 2 {
			t.Fatalf("package wrapper output = %q, want HOME and XDG state root", result.output)
		}
		assertPathDescendsFrom(t, "wrapper HOME", lines[0], testRoot)
		assertPathDescendsFrom(t, "wrapper XDG_STATE_HOME", lines[1], testRoot)
		homes[lines[0]] = struct{}{}
	}
	if len(homes) != 2 {
		t.Fatalf("concurrent package wrappers shared HOME: %v", homes)
	}
}

func TestRepositoryGoTestPackageWrapperPreservesFailureAndCleansRoot(t *testing.T) {
	root := findRepoRoot(t)
	wrapper := filepath.Join(root, "tools", "test-exec.sh")
	testRoot := t.TempDir()
	command := exec.Command(wrapper, "sh", "-c", `mkdir -p "$HOME/residue"; exit 7`)
	command.Env = append(
		withoutEnvironment(os.Environ(), "DAEM_TEST_ROOT"),
		"DAEM_TEST_HARNESS=1",
		"DAEM_TEST_ROOT="+testRoot,
	)
	err := command.Run()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 7 {
		t.Fatalf("package wrapper failure = %v, want exit 7", err)
	}
	entries, err := os.ReadDir(testRoot)
	if err != nil {
		t.Fatalf("read wrapper test root: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("package wrapper retained temporary roots after failure: %v", entries)
	}
}

func TestRepositoryGoTestHarnessRejectsExecOverride(t *testing.T) {
	root := findRepoRoot(t)
	for _, arguments := range [][]string{
		{"-exec=/bin/true", "./internal/archguard"},
		{"-exec", "/bin/true", "./internal/archguard"},
		{"--exec=/bin/true", "./internal/archguard"},
		{"--exec", "/bin/true", "./internal/archguard"},
	} {
		command := exec.Command(filepath.Join(root, "tools", "test-go.sh"), arguments...)
		command.Dir = root
		output, err := command.CombinedOutput()
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) || exitError.ExitCode() != 2 {
			t.Errorf("harness exec override %v result = %v, want exit 2\n%s", arguments, err, output)
			continue
		}
		if !strings.Contains(string(output), "owns -exec/--exec") {
			t.Errorf("harness exec override %v diagnostic = %q, want ownership explanation", arguments, output)
		}
	}
}

func TestRepositoryGoTestHarnessRejectsHostTestPolicy(t *testing.T) {
	root := findRepoRoot(t)
	tests := []struct {
		name            string
		goEnvironment   string
		explicitGoFlags string
	}{
		{
			name:            "selection flags",
			goEnvironment:   "GOFLAGS=-run=^NoRepositoryTestsShouldMatch$\n",
			explicitGoFlags: "-run=^NoRepositoryTestsShouldMatch$",
		},
		{
			name:          "bootstrap output mode",
			goEnvironment: "GOFLAGS=-json\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			hostGoEnvironment := filepath.Join(t.TempDir(), "go.env")
			if err := os.WriteFile(hostGoEnvironment, []byte(test.goEnvironment), 0o600); err != nil {
				t.Fatalf("write hostile GOENV: %v", err)
			}

			command := exec.Command(
				filepath.Join(root, "tools", "test-go.sh"),
				"-mod=readonly",
				"-run=^TestRepositoryGoTestHostPolicyProbe$",
				"-count=1",
				"-v",
				"./internal/archguard",
			)
			command.Dir = root
			command.Env = append(
				withoutEnvironment(os.Environ(), "GOENV", "GOFLAGS", "GOWORK"),
				"DAEM_TEST_HOST_POLICY_PROBE=1",
				"GOENV="+hostGoEnvironment,
				"GOWORK="+filepath.Join(t.TempDir(), "missing.work"),
			)
			if test.explicitGoFlags != "" {
				command.Env = append(command.Env, "GOFLAGS="+test.explicitGoFlags)
			}
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("isolated harness probe failed: %v\n%s", err, output)
			}
			if !strings.Contains(string(output), "=== RUN   TestRepositoryGoTestHostPolicyProbe") {
				t.Fatalf("host policy suppressed mandatory probe:\n%s", output)
			}
		})
	}
}

func TestRepositoryGoTestHostPolicyProbe(t *testing.T) {
	if os.Getenv("DAEM_TEST_HOST_POLICY_PROBE") != "1" {
		t.Skip("runs only in the nested host-policy harness probe")
	}
	if os.Getenv("GOENV") != "off" || os.Getenv("GOWORK") != "off" {
		t.Fatalf("Go policy was not normalized: GOENV=%q GOWORK=%q", os.Getenv("GOENV"), os.Getenv("GOWORK"))
	}
	if value, present := os.LookupEnv("GOFLAGS"); present {
		t.Fatalf("GOFLAGS = %q, want absent", value)
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
	assertPathDescendsFrom(t, name, value, root)
}

func assertPathDescendsFrom(t *testing.T, name string, value string, root string) {
	t.Helper()
	relative, err := filepath.Rel(root, value)
	if err != nil {
		t.Fatalf("compare %s with test root: %v", name, err)
	}
	if relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatalf("%s = %q, want a descendant of %q", name, value, root)
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
