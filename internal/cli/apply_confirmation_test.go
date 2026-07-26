package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

func TestRunApplyPromptsAndAppliesWhenInteractiveConfirmationAccepted(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, _, instructionHash := writeApplyConfirmationFixture(t, tempDir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := RunWithOptions([]string{"apply", "--manifest", manifestPath}, interactiveRunOptions(strings.NewReader("yes\n"), &stdout, &stderr))
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "Proceed with apply? [y/N]:") {
		t.Fatalf("stderr = %q, want prompt", stderr.String())
	}
	for _, want := range []string{
		"apply: 1 resources",
		"applied: 1 actions",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	assertApplyConfirmationFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "shared instructions\n")
	state, err := statefile.Load(t.Context(), filepath.Join(tempDir, ".daem", "state.json"))
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	assertApplyConfirmationStateResource(t, state, "codex", "AGENTS.md", instructionHash)
}

func TestRunApplyPromptCancellationDoesNotMutate(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, _, _ := writeApplyConfirmationFixture(t, tempDir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := RunWithOptions([]string{"apply", "--manifest", manifestPath}, interactiveRunOptions(strings.NewReader("no\n"), &stdout, &stderr))
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if !strings.Contains(stdout.String(), "apply: 1 resources") || strings.Contains(stdout.String(), "Proceed with apply?") {
		t.Fatalf("stdout = %q, want stable plan without prompt", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Proceed with apply? [y/N]:") || !strings.Contains(stderr.String(), "apply canceled") {
		t.Fatalf("stderr = %q, want cancellation diagnostic", stderr.String())
	}
	assertCLIPathMissing(t, filepath.Join(tempDir, "AGENTS.md"))
	assertCLIPathMissing(t, filepath.Join(tempDir, ".daem"))
}

func TestRunApplyInteractivePlanErrorDoesNotPrompt(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, _, _ := writeApplyConfirmationFixture(t, tempDir)
	writeApplyConfirmationFile(t, tempDir, "AGENTS.md", "manual content\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := RunWithOptions([]string{"apply", "--manifest", manifestPath}, interactiveRunOptions(strings.NewReader("yes\n"), &stdout, &stderr))
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if strings.Contains(stdout.String(), "Proceed with apply?") {
		t.Fatalf("stdout = %q, did not want prompt", stdout.String())
	}
	if !strings.Contains(stderr.String(), "plan contains error action") {
		t.Fatalf("stderr = %q, want plan error diagnostic", stderr.String())
	}
	assertApplyConfirmationFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "manual content\n")
}

func TestRunApplyInteractiveNoopPlanDoesNotPrompt(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, lockfilePath, instructionHash := writeApplyConfirmationFixture(t, tempDir)
	writeApplyConfirmationFile(t, tempDir, "AGENTS.md", "shared instructions\n")
	writeApplyConfirmationStatefile(
		t,
		filepath.Join(tempDir, ".daem", "state.json"),
		applyConfirmationSnapshot(t, applyConfirmationInstructionState(t, lockfilePath, instructionHash)),
	)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := RunWithOptions([]string{"apply", "--manifest", manifestPath}, interactiveRunOptions(strings.NewReader("yes\n"), &stdout, &stderr))
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if strings.Contains(stdout.String(), "Proceed with apply?") {
		t.Fatalf("stdout = %q, did not want prompt for noop plan", stdout.String())
	}
	if !strings.Contains(stdout.String(), "apply: 1 resources") || !strings.Contains(stdout.String(), "applied: 0 actions") {
		t.Fatalf("stdout = %q, want preview and zero applied actions", stdout.String())
	}
}

func TestRunApplyOrdinaryDelegationPromptsForNoopProjectionWithDelegateSubject(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, _ := writeMCPApplyConfirmationFixture(t, tempDir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := RunWithOptions([]string{"apply", "--manifest", manifestPath, "--target", "claude-code", "--yes"}, RunOptions{
		Stdout:              &stdout,
		Stderr:              &stderr,
		ApplyExecuteOptions: successfulDelegateApplyOptions(),
	})
	if exitCode != 0 {
		t.Fatalf("initial apply exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("initial apply stderr = %q, want empty", stderr.String())
	}
	state, err := statefile.Load(t.Context(), filepath.Join(tempDir, ".daem", "state.json"))
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	attempts := state.DelegateAttempts()
	if len(attempts) != 1 || attempts[0].Status() != durableattempt.DelegateStatusSucceeded {
		t.Fatalf("delegate attempts = %#v, want one successful ordinary attempt", attempts)
	}

	stdout.Reset()
	stderr.Reset()
	options := interactiveRunOptions(strings.NewReader("no\n"), &stdout, &stderr)
	options.ApplyExecuteOptions = successfulDelegateApplyOptions()
	exitCode = RunWithOptions([]string{"apply", "--manifest", manifestPath, "--target", "claude-code"}, options)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want cancellation exit 1", exitCode)
	}
	for _, want := range []string{
		"apply: 1 resources",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	if !strings.Contains(stderr.String(), "Proceed with apply? [y/N]:") || !strings.Contains(stderr.String(), "apply canceled") {
		t.Fatalf("stderr = %q, want cancellation diagnostic", stderr.String())
	}
	state, err = statefile.Load(t.Context(), filepath.Join(tempDir, ".daem", "state.json"))
	if err != nil {
		t.Fatalf("statefile.Load returned error after cancellation: %v", err)
	}
	attempts = state.DelegateAttempts()
	if len(attempts) != 1 {
		t.Fatalf("delegate attempts after cancellation = %#v, want prior attempt unchanged", attempts)
	}
}

func TestRunApplyOrdinaryDelegationStaleProjectionDoesNotPrompt(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, _ := writeMCPApplyConfirmationFixture(t, tempDir)
	writeApplyConfirmationFile(t, tempDir, "daem.toml", `
version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "must-not-run-daem-test"
args = ["--serve", "context7", "--changed"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := RunWithOptions(
		[]string{"apply", "--manifest", manifestPath, "--target", "claude-code"},
		interactiveRunOptions(strings.NewReader("yes\n"), &stdout, &stderr),
	)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want stale-lock failure", exitCode)
	}
	if strings.Contains(stdout.String(), "Proceed with apply?") {
		t.Fatalf("stdout = %q, did not want prompt for blocked stale projection", stdout.String())
	}
	if !strings.Contains(stderr.String(), "stale_lock") {
		t.Fatalf("stderr = %q, want stale lock diagnostic", stderr.String())
	}
}

func successfulDelegateApplyOptions() applyworkflow.ExecuteOptions {
	return applyworkflow.ExecuteOptions{
		DelegateExecutor: delegate.NewExecutor(delegate.Options{
			Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
				return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
			},
		}),
	}
}

func interactiveRunOptions(input io.Reader, stdout io.Writer, stderr io.Writer) RunOptions {
	return RunOptions{
		Stdin:                input,
		Stdout:               stdout,
		Stderr:               stderr,
		StdinIsTerminal:      true,
		StdoutIsTerminal:     true,
		StderrIsTerminal:     true,
		ReadConfirmationLine: readConfirmationLineFromReader,
	}
}

func writeApplyConfirmationFixture(t *testing.T, root string) (string, string, string) {
	t.Helper()

	manifestPath := filepath.Join(root, "daem.toml")
	lockfilePath := filepath.Join(root, "daem.lock.toml")
	writeApplyConfirmationFile(t, root, "instructions/AGENTS.md", "shared instructions\n")
	instructionHash := hashApplyConfirmationPath(t, filepath.Join(root, "instructions/AGENTS.md"))
	writeApplyConfirmationFile(t, root, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
targets = ["codex"]
`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := runWithoutTerminal([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("lock exitCode = %d stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}

	return manifestPath, lockfilePath, instructionHash
}

func writeMCPApplyConfirmationFixture(t *testing.T, root string) (string, string) {
	t.Helper()

	manifestPath := filepath.Join(root, "daem.toml")
	lockfilePath := filepath.Join(root, "daem.lock.toml")
	writeApplyConfirmationFile(t, root, "daem.toml", `
version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "must-not-run-daem-test"
args = ["--serve", "context7"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithoutTerminal([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("lock exitCode = %d stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("lock stderr = %q, want empty", stderr.String())
	}

	return manifestPath, lockfilePath
}

func writeApplyConfirmationFile(t *testing.T, root string, relativePath string, content string) {
	t.Helper()

	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}

func writeApplyConfirmationStatefile(t *testing.T, path string, snapshot durable.Snapshot) {
	t.Helper()

	content, err := statefile.Marshal(snapshot)
	if err != nil {
		t.Fatalf("statefile.Marshal returned error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}

func assertApplyConfirmationFileContent(t *testing.T, path string, want string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %q returned error: %v", path, err)
	}
	if string(content) != want {
		t.Fatalf("content of %q = %q, want %q", path, content, want)
	}
}

func hashApplyConfirmationPath(t *testing.T, path string) string {
	t.Helper()

	contentHash, artifactKind, err := access.HashPath(context.Background(), path)
	if err != nil {
		t.Fatalf("HashPath returned error: %v", err)
	}
	if artifactKind != artifact.ArtifactKindFile {
		t.Fatalf("artifactKind = %q, want %s", artifactKind, artifact.ArtifactKindFile)
	}

	return string(contentHash)
}

func assertApplyConfirmationStateResource(t *testing.T, snapshot durable.Snapshot, selectedTarget string, path string, contentHash string) {
	t.Helper()
	for _, state := range snapshot.ManagedPaths() {
		entityID, entityBacked := topologyprojection.EntityID(state.Subject())
		if !entityBacked || entityID.Kind() != entity.KindInstructions ||
			entityID.Name() != "project" || state.Scope() != "project" || string(state.Destination()) != path {
			continue
		}
		consumers := state.ConsumerTargets()
		if len(consumers) != 1 || string(consumers[0]) != selectedTarget ||
			state.ContentKind() != realization.PathProjectionFile || string(state.ContentHash()) != contentHash {
			t.Fatalf("managed path state = %#v", state)
		}
		return
	}
	t.Fatalf("managed Instructions state target=%q path=%q not found in %#v", selectedTarget, path, snapshot.ManagedPaths())
}

func applyConfirmationInstructionState(t *testing.T, lockfilePath string, contentHash string) durable.ManagedPathState {
	t.Helper()
	locked, err := lockfile.Load(lockfilePath)
	if err != nil {
		t.Fatalf("load confirmation lockfile: %v", err)
	}
	for _, contract := range locked.Locked.Subjects() {
		entityID := contract.EntityID()
		realization, realized := contract.Realization()
		projection, managedPath := realization.ManagedPathProjection()
		if !realized || !managedPath || entityID.Kind() != entity.KindInstructions || entityID.Name() != "project" {
			continue
		}
		state, err := durable.NewManagedPathState(
			contract.SubjectID(),
			projection.ConsumerTargets(),
			projection.Scope(),
			output.Destination(projection.Destination()),
			artifact.ContentHash(contentHash),
			projection.ContentKind(),
			projection.PermissionPolicy(),
			0,
		)
		if err != nil {
			t.Fatalf("durable.NewManagedPathState returned error: %v", err)
		}
		return state
	}
	t.Fatal("confirmation lockfile has no Instructions projection")
	return durable.ManagedPathState{}
}

func applyConfirmationSnapshot(t *testing.T, managedPaths ...durable.ManagedPathState) durable.Snapshot {
	t.Helper()
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: managedPaths})
	if err != nil {
		t.Fatalf("durable.NewSnapshot returned error: %v", err)
	}
	return snapshot
}

func assertCLIPathMissing(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("path %q exists or stat failed unexpectedly: %v", path, err)
	}
}
