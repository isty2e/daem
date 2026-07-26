package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestRunPublishesCompleteDeterministicDirectory(t *testing.T) {
	fixture := buildReleasepackFixture(t)
	firstOutput := filepath.Join(fixture.root, "artifact-first")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := run(fixture.args(firstOutput), &stdout, &stderr); exitCode != 0 {
		t.Fatalf("run exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	assertPublishedDirectory(t, firstOutput, stdout.String())

	secondOutput := filepath.Join(fixture.root, "artifact-second")
	stdout.Reset()
	if exitCode := run(fixture.args(secondOutput), &stdout, &stderr); exitCode != 0 {
		t.Fatalf("second run exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	firstArchive := readOnlyArtifactFile(t, firstOutput, fixture.archiveName())
	secondArchive := readOnlyArtifactFile(t, secondOutput, fixture.archiveName())
	if !bytes.Equal(firstArchive, secondArchive) {
		t.Fatal("different output directories changed archive bytes")
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := run(fixture.args(firstOutput), &stdout, &stderr); exitCode != 1 {
		t.Fatalf("overwrite run exit=%d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "already exists") || stdout.Len() != 0 {
		t.Fatalf("overwrite stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if !bytes.Equal(firstArchive, readOnlyArtifactFile(t, firstOutput, fixture.archiveName())) {
		t.Fatal("refused overwrite changed existing archive")
	}
}

func TestRunRejectsUnsafeInputsWithoutPublishing(t *testing.T) {
	fixture := buildReleasepackFixture(t)
	directoryInput := filepath.Join(fixture.root, "directory-input")
	if err := os.Mkdir(directoryInput, 0o755); err != nil {
		t.Fatalf("create directory input: %v", err)
	}
	symlinkInput := filepath.Join(fixture.root, "symlink-input")
	if err := os.Symlink(fixture.binaryPath, symlinkInput); err != nil {
		t.Fatalf("create symlink input: %v", err)
	}
	nonGoInput := filepath.Join(fixture.root, "not-go")
	writeReleasepackFile(t, nonGoInput, "not a Go executable")

	tests := []struct {
		name       string
		binaryPath string
		want       string
	}{
		{name: "directory", binaryPath: directoryInput, want: "not a regular file"},
		{name: "symlink", binaryPath: symlinkInput, want: "is a symlink"},
		{name: "non go", binaryPath: nonGoInput, want: "read executable build identity"},
		{name: "missing", binaryPath: filepath.Join(fixture.root, "missing"), want: "no such file"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			outputDir := filepath.Join(fixture.root, "rejected-"+strings.ReplaceAll(test.name, " ", "-"))
			args := fixture.args(outputDir)
			args[1] = test.binaryPath
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			if exitCode := run(args, &stdout, &stderr); exitCode != 1 {
				t.Fatalf("run exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
			if stdout.Len() != 0 || !strings.Contains(strings.ToLower(stderr.String()), test.want) {
				t.Fatalf("stdout=%q stderr=%q, want %q", stdout.String(), stderr.String(), test.want)
			}
			if _, err := os.Lstat(outputDir); !os.IsNotExist(err) {
				t.Fatalf("rejected input published output: %v", err)
			}
		})
	}
}

func TestRunRejectsExistingOutputForms(t *testing.T) {
	fixture := buildReleasepackFixture(t)
	existingFile := filepath.Join(fixture.root, "existing-file")
	writeReleasepackFile(t, existingFile, "sentinel")
	existingDirectory := filepath.Join(fixture.root, "existing-directory")
	if err := os.Mkdir(existingDirectory, 0o755); err != nil {
		t.Fatalf("create existing directory: %v", err)
	}
	existingSymlink := filepath.Join(fixture.root, "existing-symlink")
	if err := os.Symlink(existingDirectory, existingSymlink); err != nil {
		t.Fatalf("create existing symlink: %v", err)
	}
	for _, outputDir := range []string{existingFile, existingDirectory, existingSymlink} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		if exitCode := run(fixture.args(outputDir), &stdout, &stderr); exitCode != 1 {
			t.Fatalf("run(%q) exit=%d, want 1", outputDir, exitCode)
		}
		if stdout.Len() != 0 || !strings.Contains(stderr.String(), "already exists") {
			t.Fatalf("run(%q) stdout=%q stderr=%q", outputDir, stdout.String(), stderr.String())
		}
	}
	content, err := os.ReadFile(existingFile)
	if err != nil || string(content) != "sentinel" {
		t.Fatalf("existing file changed: content=%q err=%v", content, err)
	}
}

func TestRunRejectsInvalidArgumentsBeforeFileAccess(t *testing.T) {
	fixture := buildReleasepackFixture(t)
	valid := fixture.args(filepath.Join(fixture.root, "unused"))
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing", args: nil, want: "--binary is required"},
		{name: "unknown flag", args: []string{"--unknown"}, want: "flag provided but not defined"},
		{name: "positional", args: append(append([]string(nil), valid...), "extra"), want: "unexpected positional argument"},
		{name: "invalid target", args: replaceReleasepackArgument(valid, "--goos", "Linux"), want: "invalid target"},
		{name: "invalid version", args: replaceReleasepackArgument(valid, "--version", "1.2.3"), want: "invalid release requirement"},
		{name: "short revision", args: replaceReleasepackArgument(valid, "--revision", "short"), want: "invalid release requirement"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			if exitCode := run(test.args, &stdout, &stderr); exitCode != 2 {
				t.Fatalf("run exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
			if stdout.Len() != 0 || !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("stdout=%q stderr=%q, want %q", stdout.String(), stderr.String(), test.want)
			}
		})
	}
}

func TestRunReportsWriterFailureAfterCompletePublication(t *testing.T) {
	fixture := buildReleasepackFixture(t)
	outputDir := filepath.Join(fixture.root, "artifact-writer-failure")
	want := errors.New("stdout closed")
	var stderr bytes.Buffer
	if exitCode := run(fixture.args(outputDir), releasepackErrorWriter{err: want}, &stderr); exitCode != 1 {
		t.Fatalf("run exit=%d stderr=%q", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "report outputs: stdout closed") {
		t.Fatalf("stderr=%q", stderr.String())
	}
	assertPublishedDirectory(t, outputDir, "")
}

type releasepackErrorWriter struct{ err error }

func (writer releasepackErrorWriter) Write([]byte) (int, error) { return 0, writer.err }

type releasepackFixture struct {
	root       string
	binaryPath string
	revision   string
}

func (fixture releasepackFixture) args(outputDir string) []string {
	return []string{
		"--binary", fixture.binaryPath,
		"--output-dir", outputDir,
		"--version", "v1.2.3",
		"--revision", fixture.revision,
		"--go-version", runtime.Version(),
		"--goos", runtime.GOOS,
		"--goarch", runtime.GOARCH,
	}
}

func (fixture releasepackFixture) archiveName() string {
	return "daem_1.2.3_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
}

func buildReleasepackFixture(t *testing.T) releasepackFixture {
	t.Helper()
	gitBinary, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("releasepack integration requires git: %v", err)
	}
	root := t.TempDir()
	repository := filepath.Join(root, "repository")
	mainPath := filepath.Join(repository, "cmd", "daem", "main.go")
	if err := os.MkdirAll(filepath.Dir(mainPath), 0o755); err != nil {
		t.Fatalf("create fixture repository: %v", err)
	}
	writeReleasepackFile(t, filepath.Join(repository, "go.mod"), "module github.com/isty2e/daem\n\ngo 1.25.0\n")
	writeReleasepackFile(t, mainPath, "package main\nfunc main() {}\n")
	runReleasepackCommand(t, repository, nil, gitBinary, "init", "--quiet")
	runReleasepackCommand(t, repository, nil, gitBinary, "config", "user.name", "Releasepack Test")
	runReleasepackCommand(t, repository, nil, gitBinary, "config", "user.email", "releasepack@example.invalid")
	runReleasepackCommand(t, repository, nil, gitBinary, "add", ".")
	commitEnvironment := map[string]string{
		"GIT_AUTHOR_DATE":    "2026-07-01T02:03:04Z",
		"GIT_COMMITTER_DATE": "2026-07-01T02:03:04Z",
	}
	runReleasepackCommand(t, repository, commitEnvironment, gitBinary, "commit", "--quiet", "-m", "fixture")
	runReleasepackCommand(t, repository, nil, gitBinary, "tag", "v1.2.3")
	revision := strings.TrimSpace(runReleasepackCommand(t, repository, nil, gitBinary, "rev-parse", "HEAD"))
	binaryPath := filepath.Join(root, "daem")
	goBinary := filepath.Join(runtime.GOROOT(), "bin", "go")
	buildEnvironment := map[string]string{
		"CGO_ENABLED": "0", "GOFLAGS": "", "GOPROXY": "off", "GOSUMDB": "off", "GOTOOLCHAIN": "local",
	}
	runReleasepackCommand(t, repository, buildEnvironment, goBinary, "build", "-buildvcs=true", "-mod=readonly", "-trimpath", "-o", binaryPath, "./cmd/daem")
	return releasepackFixture{root: root, binaryPath: binaryPath, revision: revision}
}

func assertPublishedDirectory(t *testing.T, outputDir string, stdout string) {
	t.Helper()
	directoryInfo, err := os.Stat(outputDir)
	if err != nil || !directoryInfo.IsDir() || directoryInfo.Mode().Perm() != 0o755 {
		t.Fatalf("output directory info=%v err=%v", directoryInfo, err)
	}
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatalf("read output directory: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	if len(names) != 2 || !strings.HasSuffix(names[0], ".tar.gz") || !strings.HasSuffix(names[1], ".tar.gz.sha256") {
		t.Fatalf("output names = %#v", names)
	}
	archive := readOnlyArtifactFile(t, outputDir, names[0])
	checksum := readOnlyArtifactFile(t, outputDir, names[1])
	digest := sha256.Sum256(archive)
	wantSidecar := hex.EncodeToString(digest[:]) + "  " + names[0] + "\n"
	if string(checksum) != wantSidecar {
		t.Fatalf("checksum = %q, want %q", checksum, wantSidecar)
	}
	if stdout != "" {
		for _, want := range []string{filepath.Join(outputDir, names[0]), filepath.Join(outputDir, names[1]), hex.EncodeToString(digest[:])} {
			if !strings.Contains(stdout, want) {
				t.Fatalf("stdout=%q, want %q", stdout, want)
			}
		}
	}
}

func readOnlyArtifactFile(t *testing.T, directory string, name string) []byte {
	t.Helper()
	path := filepath.Join(directory, name)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 {
		t.Fatalf("artifact file %s info=%v err=%v", path, info, err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read artifact file %s: %v", path, err)
	}
	return content
}

func replaceReleasepackArgument(args []string, flagName string, value string) []string {
	replaced := append([]string(nil), args...)
	for index := 0; index+1 < len(replaced); index++ {
		if replaced[index] == flagName {
			replaced[index+1] = value
			return replaced
		}
	}
	return replaced
}

func runReleasepackCommand(t *testing.T, directory string, overrides map[string]string, name string, args ...string) string {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = directory
	command.Env = releasepackEnvironment(overrides)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return string(output)
}

func releasepackEnvironment(overrides map[string]string) []string {
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

func writeReleasepackFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
