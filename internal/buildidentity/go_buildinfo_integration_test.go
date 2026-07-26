package buildidentity

import (
	"debug/buildinfo"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

func TestGoBuildInfoPreservesExactTagAndDirtyState(t *testing.T) {
	gitBinary, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("release provenance integration requires git: %v", err)
	}
	goBinary := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBinary); err != nil {
		t.Fatalf("current Go executable: %v", err)
	}

	root := t.TempDir()
	repository := filepath.Join(root, "repository")
	if err := os.MkdirAll(filepath.Join(repository, "cmd", "fixture"), 0o755); err != nil {
		t.Fatalf("create fixture repository: %v", err)
	}
	writeIntegrationFile(t, filepath.Join(repository, "go.mod"), "module example.com/buildinfofixture\n\ngo 1.25.0\n")
	mainPath := filepath.Join(repository, "cmd", "fixture", "main.go")
	writeIntegrationFile(t, mainPath, "package main\nfunc main() {}\n")

	runIntegrationCommand(t, repository, nil, gitBinary, "init", "--quiet")
	runIntegrationCommand(t, repository, nil, gitBinary, "config", "user.name", "Build Identity Test")
	runIntegrationCommand(t, repository, nil, gitBinary, "config", "user.email", "build-identity@example.invalid")
	runIntegrationCommand(t, repository, nil, gitBinary, "add", ".")
	commitEnvironment := map[string]string{
		"GIT_AUTHOR_DATE":    "2026-07-01T02:03:04Z",
		"GIT_COMMITTER_DATE": "2026-07-01T02:03:04Z",
	}
	runIntegrationCommand(t, repository, commitEnvironment, gitBinary, "commit", "--quiet", "-m", "fixture")
	runIntegrationCommand(t, repository, nil, gitBinary, "tag", "v1.2.3")
	revision := strings.TrimSpace(runIntegrationCommand(t, repository, nil, gitBinary, "rev-parse", "HEAD"))

	cleanBinary := filepath.Join(root, "fixture-clean")
	buildIntegrationBinary(t, repository, goBinary, cleanBinary)
	cleanInfo, err := buildinfo.ReadFile(cleanBinary)
	if err != nil {
		t.Fatalf("read clean build info: %v", err)
	}
	assertIntegrationBuildInfo(t, cleanInfo, revision, "v1.2.3", "false")

	writeIntegrationFile(t, mainPath, "package main\nfunc main() {}\n// dirty source\n")
	dirtyBinary := filepath.Join(root, "fixture-dirty")
	buildIntegrationBinary(t, repository, goBinary, dirtyBinary)
	dirtyInfo, err := buildinfo.ReadFile(dirtyBinary)
	if err != nil {
		t.Fatalf("read dirty build info: %v", err)
	}
	assertIntegrationBuildInfo(t, dirtyInfo, revision, "v1.2.3+dirty", "true")
}

func buildIntegrationBinary(t *testing.T, repository string, goBinary string, outputPath string) {
	t.Helper()
	environment := map[string]string{
		"CGO_ENABLED": "0",
		"GOFLAGS":     "",
		"GOPROXY":     "off",
		"GOSUMDB":     "off",
		"GOTOOLCHAIN": "local",
	}
	runIntegrationCommand(
		t,
		repository,
		environment,
		goBinary,
		"build",
		"-buildvcs=true",
		"-mod=readonly",
		"-trimpath",
		"-o",
		outputPath,
		"./cmd/fixture",
	)
}

func assertIntegrationBuildInfo(t *testing.T, info *debug.BuildInfo, revision string, version string, modified string) {
	t.Helper()
	if info.Path != "example.com/buildinfofixture/cmd/fixture" {
		t.Fatalf("main package = %q", info.Path)
	}
	if info.Main.Path != "example.com/buildinfofixture" || info.Main.Version != version {
		t.Fatalf("main module = %q@%q, want exact %q", info.Main.Path, info.Main.Version, version)
	}
	if info.GoVersion != runtime.Version() {
		t.Fatalf("Go version = %q, want test toolchain %q", info.GoVersion, runtime.Version())
	}
	wantSettings := map[string]string{
		"vcs":          "git",
		"vcs.revision": revision,
		"vcs.time":     "2026-07-01T02:03:04Z",
		"vcs.modified": modified,
		"GOOS":         runtime.GOOS,
		"GOARCH":       runtime.GOARCH,
		"CGO_ENABLED":  "0",
	}
	settings := make(map[string]string, len(info.Settings))
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	for key, want := range wantSettings {
		if settings[key] != want {
			t.Fatalf("build setting %s = %q, want %q", key, settings[key], want)
		}
	}
}

func runIntegrationCommand(t *testing.T, directory string, environment map[string]string, name string, args ...string) string {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = directory
	command.Env = integrationEnvironment(environment)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return string(output)
}

func integrationEnvironment(overrides map[string]string) []string {
	values := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	maps.Copy(values, overrides)
	environment := make([]string, 0, len(values))
	for key, value := range values {
		environment = append(environment, key+"="+value)
	}
	return environment
}

func writeIntegrationFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
