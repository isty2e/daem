package cli_test

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/effect/mutation"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/test/testkit"
)

const (
	workspaceMutationCLIHelperEnv    = "GO_WANT_WORKSPACE_MUTATION_CLI_HELPER"
	workspaceMutationCLIHelperRunArg = "-test.run=^TestWorkspaceMutationCLIHelperProcess$"
)

type workspaceMutationHelper struct {
	command *exec.Cmd
	args    []string
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	stderr  *bytes.Buffer
	done    chan error
}

func TestConcurrentAddSkillsSerializeManifestAndLockfile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	manifestPath := filepath.Join(root, "daem.toml")
	if err := os.WriteFile(manifestPath, []byte("version = 1\ntargets = [\"codex\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	alphaPath := writeConcurrentSkillFixture(t, root, "alpha")
	betaPath := writeConcurrentSkillFixture(t, root, "beta")

	alpha, beta, alphaErr, betaErr := runBlockedWorkspaceMutationPair(t, manifestPath, []string{
		"add", "skill", alphaPath,
		"--manifest", manifestPath, "--target", "codex",
	}, []string{
		"add", "skill", betaPath,
		"--manifest", manifestPath, "--target", "codex",
	})
	if (alphaErr == nil) == (betaErr == nil) {
		t.Fatalf("concurrent results = alpha:%v beta:%v; want one winner and one stale", alphaErr, betaErr)
	}
	staleHelper := alpha
	if betaErr != nil {
		staleHelper = beta
	}
	if !strings.Contains(staleHelper.stderr.String(), "stale_snapshot") {
		t.Fatalf("losing helper stderr = %q, want stale_snapshot", staleHelper.stderr.String())
	}
	var retryStdout bytes.Buffer
	var retryStderr bytes.Buffer
	if exitCode := clipkg.RunWithOptions(staleHelper.args, clipkg.RunOptions{
		Context: context.Background(), Stdout: &retryStdout, Stderr: &retryStderr,
	}); exitCode != 0 {
		t.Fatalf("stale retry exitCode=%d stderr=%s", exitCode, retryStderr.String())
	}

	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, section := range []string{`name = "alpha"`, `name = "beta"`} {
		if !strings.Contains(string(manifest), section) {
			t.Fatalf("final manifest missing %s:\n%s", section, manifest)
		}
	}
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := lockfile.Load(t.Context(), paths.LockfilePath)
	if err != nil {
		t.Fatalf("load final lockfile: %v", err)
	}
	if len(testkit.LockedSkills(t, loaded)) != 2 {
		t.Fatalf("locked skills = %d, want 2", len(testkit.LockedSkills(t, loaded)))
	}
}

func TestConcurrentAddAndLockNeverPublishMismatchedLockfile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	manifestPath := filepath.Join(root, "daem.toml")
	if err := os.WriteFile(manifestPath, []byte("version = 1\ntargets = [\"codex\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(root, "project.md")
	if err := os.WriteFile(sourcePath, []byte("project\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	add, lock, addErr, lockErr := runBlockedWorkspaceMutationPair(t, manifestPath, []string{
		"add", "instruction", "project", sourcePath,
		"--manifest", manifestPath, "--target", "codex",
	}, []string{"lock", "--manifest", manifestPath})
	if addErr != nil {
		t.Fatalf("add failed: %v; stderr=%s", addErr, add.stderr.String())
	}
	if lockErr != nil && !strings.Contains(lock.stderr.String(), "stale_snapshot") {
		t.Fatalf("lock failed without stale classification: %v; stderr=%s", lockErr, lock.stderr.String())
	}
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := lockfile.Load(t.Context(), paths.LockfilePath)
	if err != nil {
		t.Fatalf("load final lockfile: %v", err)
	}
	if len(testkit.LockedInstructions(t, loaded)) != 1 {
		t.Fatalf("locked instructions = %d, want 1", len(testkit.LockedInstructions(t, loaded)))
	}
}

func TestConcurrentAddAndRemoveRequireExplicitStaleRetry(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	manifestPath := filepath.Join(root, "daem.toml")
	writeConcurrentSkillFixture(t, root, "alpha")
	betaPath := writeConcurrentSkillFixture(t, root, "beta")
	manifest := `version = 1
targets = ["codex"]

[[skill]]
name = "alpha"
source = { path = "skills/alpha", mode = "vendor" }
targets = ["codex"]
`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	var baselineStdout bytes.Buffer
	var baselineStderr bytes.Buffer
	if exitCode := clipkg.RunWithOptions([]string{"lock", "--manifest", manifestPath}, clipkg.RunOptions{
		Context: context.Background(), Stdout: &baselineStdout, Stderr: &baselineStderr,
	}); exitCode != 0 {
		t.Fatalf("baseline lock exitCode=%d stderr=%s", exitCode, baselineStderr.String())
	}

	add, remove, addErr, removeErr := runBlockedWorkspaceMutationPair(t, manifestPath, []string{
		"add", "skill", betaPath,
		"--manifest", manifestPath, "--target", "codex",
	}, []string{
		"remove", "skill", "alpha", "--manifest", manifestPath,
	})
	if (addErr == nil) == (removeErr == nil) {
		t.Fatalf("concurrent results = add:%v remove:%v; want one winner and one stale", addErr, removeErr)
	}
	stale := add
	if removeErr != nil {
		stale = remove
	}
	if !strings.Contains(stale.stderr.String(), "stale_snapshot") {
		t.Fatalf("losing helper stderr = %q, want stale_snapshot", stale.stderr.String())
	}
	var retryStdout bytes.Buffer
	var retryStderr bytes.Buffer
	if exitCode := clipkg.RunWithOptions(stale.args, clipkg.RunOptions{
		Context: context.Background(), Stdout: &retryStdout, Stderr: &retryStderr,
	}); exitCode != 0 {
		t.Fatalf("stale retry exitCode=%d stderr=%s", exitCode, retryStderr.String())
	}
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), `name = "alpha"`) || !strings.Contains(string(content), `name = "beta"`) {
		t.Fatalf("unexpected final manifest:\n%s", content)
	}
}

func writeConcurrentSkillFixture(t *testing.T, root string, name string) string {
	t.Helper()
	path := filepath.Join(root, "skills", name)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf("---\nname: %s\ndescription: Concurrent fixture.\n---\n", name)
	if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestConcurrentLockWritersSerialize(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	manifestPath := filepath.Join(root, "daem.toml")
	if err := os.WriteFile(manifestPath, []byte("version = 1\ntargets = [\"codex\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	left, right, leftErr, rightErr := runBlockedWorkspaceMutationPair(
		t,
		manifestPath,
		[]string{"lock", "--manifest", manifestPath},
		[]string{"lock", "--manifest", manifestPath},
	)
	if leftErr != nil || rightErr != nil {
		t.Fatalf("lock pair errors = %v/%v; stderr=%q/%q", leftErr, rightErr, left.stderr.String(), right.stderr.String())
	}
}

func TestConcurrentImportMergeAndAuthoringRequireExplicitStaleRetry(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	codexHome := filepath.Join(root, "codex-home")
	t.Setenv("CODEX_HOME", codexHome)
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, "AGENTS.md"), []byte("global\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, "daem.toml")
	if err := os.WriteFile(manifestPath, []byte("version = 1\ntargets = [\"codex\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	projectPath := filepath.Join(root, "project.md")
	if err := os.WriteFile(projectPath, []byte("project\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	importer, author, importErr, authorErr := runBlockedWorkspaceMutationPair(t, manifestPath, []string{
		"import", "--target", "codex", "--scope", "global",
		"--manifest", manifestPath, "--merge",
	}, []string{
		"add", "instruction", "project", projectPath,
		"--manifest", manifestPath, "--target", "codex",
	})
	if (importErr == nil) == (authorErr == nil) {
		t.Fatalf("concurrent results = import:%v author:%v; want one winner and one stale", importErr, authorErr)
	}
	stale := importer
	if authorErr != nil {
		stale = author
	}
	if !strings.Contains(stale.stderr.String(), "stale_snapshot") {
		t.Fatalf("losing helper stderr = %q, want stale_snapshot", stale.stderr.String())
	}
	var retryStdout bytes.Buffer
	var retryStderr bytes.Buffer
	if exitCode := clipkg.RunWithOptions(stale.args, clipkg.RunOptions{
		Context: context.Background(), Stdout: &retryStdout, Stderr: &retryStderr,
	}); exitCode != 0 {
		t.Fatalf("stale retry exitCode=%d stderr=%s", exitCode, retryStderr.String())
	}
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, section := range []string{"[instructions.codex_global]", `[instructions."project"]`} {
		if !strings.Contains(string(content), section) {
			t.Fatalf("final manifest missing %s:\n%s", section, content)
		}
	}
	importedSource := filepath.Join(root, "daem.d", "instructions", "codex-global.md")
	if source, err := os.ReadFile(importedSource); err != nil || string(source) != "global\n" {
		t.Fatalf("imported source = %q, %v", source, err)
	}
}

func TestConcurrentInitForceAndAuthoringSerializeManifestEntry(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	manifestPath := filepath.Join(root, "daem.toml")
	if err := os.WriteFile(manifestPath, []byte("version = 1\ntargets = [\"codex\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(root, "project.md")
	if err := os.WriteFile(sourcePath, []byte("project\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	initializer, author, initErr, authorErr := runBlockedWorkspaceMutationPair(t, manifestPath, []string{
		"init", "--manifest", manifestPath, "--force",
	}, []string{
		"add", "instruction", "project", sourcePath,
		"--manifest", manifestPath, "--target", "codex",
	})
	if initErr != nil || authorErr != nil {
		t.Fatalf("serialized init/authoring errors = %v/%v; stderr=%q/%q", initErr, authorErr, initializer.stderr.String(), author.stderr.String())
	}
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), `[instructions."project"]`) {
		paths, err := daempaths.Resolve(manifestPath)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := lockfile.Load(t.Context(), paths.LockfilePath)
		if err != nil {
			t.Fatalf("load authoring-winner lockfile: %v", err)
		}
		if len(testkit.LockedInstructions(t, loaded)) != 1 {
			t.Fatalf("authoring-winner locked instructions = %d, want 1", len(testkit.LockedInstructions(t, loaded)))
		}
		return
	}
	if got := string(content); got != "version = 1\ntargets = [\"codex\"]\n" {
		t.Fatalf("init-winner manifest is not the complete init template:\n%s", content)
	}
}

func runBlockedWorkspaceMutationPair(
	t *testing.T,
	manifestPath string,
	leftArgs []string,
	rightArgs []string,
) (*workspaceMutationHelper, *workspaceMutationHelper, error, error) {
	t.Helper()
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	store, err := mutation.NewStore(paths.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	manifestDomain, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{
		Path: manifestPath, Access: mutation.AccessExclusive, Effect: mutation.PathEffectDirectoryEntry,
	})
	if err != nil {
		t.Fatal(err)
	}
	holder, err := store.Acquire(context.Background(), manifestDomain)
	if err != nil {
		t.Fatal(err)
	}
	holderReleased := false
	defer func() {
		if !holderReleased {
			_ = holder.Release()
		}
	}()

	left := startWorkspaceMutationHelper(t, leftArgs)
	right := startWorkspaceMutationHelper(t, rightArgs)
	t.Cleanup(left.kill)
	t.Cleanup(right.kill)
	left.start(t)
	right.start(t)
	select {
	case err := <-left.done:
		t.Fatalf("left helper completed while manifest holder was active: %v; stderr=%s", err, left.stderr.String())
	case err := <-right.done:
		t.Fatalf("right helper completed while manifest holder was active: %v; stderr=%s", err, right.stderr.String())
	case <-time.After(150 * time.Millisecond):
	}
	if err := holder.Release(); err != nil {
		t.Fatal(err)
	}
	holderReleased = true
	return left, right, waitWorkspaceMutationHelper(t, left), waitWorkspaceMutationHelper(t, right)
}

func TestWorkspaceMutationCLIHelperProcess(t *testing.T) {
	if os.Getenv(workspaceMutationCLIHelperEnv) != "1" {
		return
	}
	separator := -1
	for index, arg := range os.Args {
		if arg == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		fmt.Fprintln(os.Stderr, "workspace mutation helper arguments are missing")
		os.Exit(2)
	}
	fmt.Fprintln(os.Stdout, "ready")
	if _, err := bufio.NewReader(os.Stdin).ReadByte(); err != nil {
		fmt.Fprintf(os.Stderr, "workspace mutation helper barrier: %v\n", err)
		os.Exit(2)
	}
	exitCode := clipkg.RunWithOptions(os.Args[separator+1:], clipkg.RunOptions{
		Context: context.Background(), Stdout: os.Stdout, Stderr: os.Stderr,
	})
	os.Exit(exitCode)
}

func startWorkspaceMutationHelper(t *testing.T, args []string) *workspaceMutationHelper {
	t.Helper()
	commandArgs := append([]string{workspaceMutationCLIHelperRunArg, "--"}, args...)
	command := exec.Command(os.Args[0], commandArgs...)
	command.Env = append(os.Environ(), workspaceMutationCLIHelperEnv+"=1")
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdoutPipe, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr := &bytes.Buffer{}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	helper := &workspaceMutationHelper{
		command: command,
		args:    append([]string(nil), args...),
		stdin:   stdin,
		stdout:  bufio.NewReader(stdoutPipe),
		stderr:  stderr,
		done:    make(chan error, 1),
	}
	ready, err := helper.stdout.ReadString('\n')
	if err != nil || ready != "ready\n" {
		helper.kill()
		t.Fatalf("helper readiness = %q, %v; stderr=%s", ready, err, stderr.String())
	}
	return helper
}

func (helper *workspaceMutationHelper) start(t *testing.T) {
	t.Helper()
	go func() {
		_, _ = io.ReadAll(helper.stdout)
		helper.done <- helper.command.Wait()
	}()
	if _, err := helper.stdin.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	if err := helper.stdin.Close(); err != nil {
		t.Fatal(err)
	}
}

func (helper *workspaceMutationHelper) kill() {
	if helper != nil && helper.command != nil && helper.command.Process != nil {
		_ = helper.command.Process.Kill()
	}
}

func waitWorkspaceMutationHelper(t *testing.T, helper *workspaceMutationHelper) error {
	t.Helper()
	select {
	case err := <-helper.done:
		return err
	case <-time.After(15 * time.Second):
		helper.kill()
		t.Fatalf("helper timed out; stderr=%s", helper.stderr.String())
		return context.DeadlineExceeded
	}
}
