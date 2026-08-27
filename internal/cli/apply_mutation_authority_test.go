package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/effect/mutation"
	daempaths "github.com/isty2e/daem/internal/paths"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

func TestRunApplyDisclosedSourceDriftReturnsStalePlanWithoutEffects(t *testing.T) {
	tempDir := isolatedApplyAuthorityRoot(t)
	manifestPath, _, _ := writeApplyConfirmationFixture(t, tempDir)
	sourcePath := filepath.Join(tempDir, "instructions", "AGENTS.md")
	input := &beforeReadApplyConfirmation{
		reader: strings.NewReader("yes\n"),
		beforeRead: func() error {
			return os.WriteFile(sourcePath, []byte("changed after disclosure\n"), 0o600)
		},
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := RunWithOptions([]string{"apply", "--manifest", manifestPath}, interactiveRunOptions(input, &stdout, &stderr))
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "apply: 1 resources") || strings.Contains(stdout.String(), "Proceed with apply?") {
		t.Fatalf("stdout = %q, want disclosed stable plan", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Proceed with apply? [y/N]:") ||
		!strings.Contains(stderr.String(), "the authorized apply plan changed before apply completed") ||
		strings.Contains(stderr.String(), "stale_plan") ||
		strings.Contains(stderr.String(), sourcePath) {
		t.Fatalf("stderr = %q, want path-neutral stale-plan detail", stderr.String())
	}
	if strings.Contains(stdout.String(), "applied:") || strings.Contains(stderr.String(), "progress:") {
		t.Fatalf("stdout=%q stderr=%q, want no applied result or leaked progress", stdout.String(), stderr.String())
	}
	assertCLIPathMissing(t, filepath.Join(tempDir, "AGENTS.md"))
	assertCLIPathMissing(t, filepath.Join(tempDir, ".daem"))
	assertApplyConfirmationFileContent(t, sourcePath, "changed after disclosure\n")
}

func TestRunApplyDefaultFailureHidesRootedAuthorityPath(t *testing.T) {
	root := isolatedApplyAuthorityRoot(t)
	manifestPath, _, _ := writeApplyConfirmationFixture(t, root)
	movedRoot := root + "-moved"
	t.Cleanup(func() {
		if err := os.RemoveAll(movedRoot); err != nil {
			t.Errorf("remove moved apply root: %v", err)
		}
	})
	input := &beforeReadApplyConfirmation{
		reader: strings.NewReader("yes\n"),
		beforeRead: func() error {
			if err := os.Rename(root, movedRoot); err != nil {
				return err
			}
			return os.MkdirAll(root, 0o700)
		},
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := RunWithOptions(
		[]string{"apply", "--manifest", manifestPath},
		interactiveRunOptions(input, &stdout, &stderr),
	)
	if exitCode != 1 {
		t.Fatalf(
			"exitCode = %d, want 1; stdout=%q stderr=%q",
			exitCode,
			stdout.String(),
			stderr.String(),
		)
	}
	if !strings.Contains(
		stderr.String(),
		"apply failed: the authorized apply plan changed before apply completed",
	) {
		t.Fatalf("stderr = %q, want typed stale-plan detail", stderr.String())
	}
	for _, private := range []string{
		root,
		movedRoot,
		manifestPath,
		"rooted_path_",
	} {
		if strings.Contains(stderr.String(), private) {
			t.Fatalf("stderr discloses rooted authority evidence %q: %q", private, stderr.String())
		}
	}
	if strings.Contains(stdout.String(), "applied:") {
		t.Fatalf("stdout = %q, want no applied result", stdout.String())
	}
}

type boundedApplyFailureCause struct {
	evidence   string
	errorCalls *int
}

func (cause boundedApplyFailureCause) Error() string {
	*cause.errorCalls = *cause.errorCalls + 1
	return "unbounded apply failure must not be formatted"
}

func (cause boundedApplyFailureCause) BoundedErrorEvidence(maximumRunes int) (string, bool) {
	if len(cause.evidence) <= maximumRunes {
		return cause.evidence, false
	}
	return cause.evidence[:maximumRunes], true
}

func TestPrintApplyExecutionFailureBoundsAndSanitizesVerboseEvidence(
	t *testing.T,
) {
	errorCalls := 0
	cause := boundedApplyFailureCause{
		evidence: "private_token=boundary-secret /Users/alice/private.json " +
			strings.Repeat("x", maximumVerboseApplyFailureEvidenceRunes+1_024),
		errorCalls: &errorCalls,
	}
	var output bytes.Buffer
	printApplyFailure(
		&output,
		cause,
		applyFailureExecution,
		applyworkflow.CommandResult{ExecutionAttempted: true},
		true,
	)

	got := output.String()
	if errorCalls != 0 {
		t.Fatalf("unbounded apply error formatted %d times", errorCalls)
	}
	for _, want := range []string{
		"apply failed: apply did not complete after an effect boundary was crossed",
		"apply failure evidence:",
		"[REDACTED]",
		`\n[truncated]`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output = %q, want %q", got, want)
		}
	}
	for _, private := range []string{"boundary-secret"} {
		if strings.Contains(got, private) {
			t.Fatalf("verbose evidence discloses credential fragment %q: %q", private, got)
		}
	}
	if utf8.RuneCountInString(got) > maximumVerboseApplyFailureEvidenceRunes+256 {
		t.Fatalf("verbose failure output is not bounded: %d runes", utf8.RuneCountInString(got))
	}
}

func TestRunApplyAbandonedResidueUsesTypedFenceRefusal(t *testing.T) {
	root := isolatedApplyAuthorityRoot(t)
	manifestPath, _, _ := writeApplyConfirmationFixture(t, root)
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	residue := filepath.Join(paths.StateDir, ".daem-tmp-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := os.Mkdir(residue, 0o700); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := RunWithOptions(
		[]string{"apply", "--dry-run", "--manifest", manifestPath},
		RunOptions{Stdout: &stdout, Stderr: &stderr},
	)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "abandoned file-set residue remains") ||
		!strings.Contains(stderr.String(), "next: preserve the reported residue") {
		t.Fatalf("stderr = %q, want typed residue refusal", stderr.String())
	}
	if strings.Contains(stderr.String(), "apply was refused before effects") ||
		strings.Contains(stderr.String(), "retry the interrupted") {
		t.Fatalf("stderr = %q, want no generic refused or retry-authoring guidance", stderr.String())
	}
}

func TestRunApplyPlanningFailureUsesClosedDefaultDetail(t *testing.T) {
	root := isolatedApplyAuthorityRoot(t)
	manifestPath := filepath.Join(root, "private", "daem.toml")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := RunWithOptions(
		[]string{"apply", "--dry-run", "--manifest", manifestPath},
		RunOptions{Stdout: &stdout, Stderr: &stderr},
	)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "apply failed: apply was refused before effects") {
		t.Fatalf("stderr = %q, want closed planning detail", stderr.String())
	}
	if strings.Contains(stderr.String(), root) || strings.Contains(stderr.String(), manifestPath) {
		t.Fatalf("stderr disclosed planning path: %q", stderr.String())
	}
}

func TestRunApplyOutputFailureDoesNotExposeWriterError(t *testing.T) {
	root := isolatedApplyAuthorityRoot(t)
	manifestPath, _, _ := writeApplyConfirmationFixture(t, root)
	privateCause := errors.New("write /Users/alice/private/result.json: credential=secret")
	var stderr bytes.Buffer
	exitCode := RunWithOptions(
		[]string{"apply", "--dry-run", "--manifest", manifestPath},
		RunOptions{Stdout: errorWriter{err: privateCause}, Stderr: &stderr},
	)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stderr=%q", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "output failed: command output could not be written") {
		t.Fatalf("stderr = %q, want closed output detail", stderr.String())
	}
	for _, private := range []string{"/Users/alice/private", "credential=secret"} {
		if strings.Contains(stderr.String(), private) {
			t.Fatalf("stderr disclosed writer evidence %q: %q", private, stderr.String())
		}
	}
}

func TestRunApplyJSONContentionIsActionableAndHidesLeaseRecords(t *testing.T) {
	tempDir := isolatedApplyAuthorityRoot(t)
	manifestPath, _, _ := writeApplyConfirmationFixture(t, tempDir)
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	store, err := mutation.NewStore(paths.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	domain, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{
		Path: manifestPath, Access: mutation.AccessExclusive, Effect: mutation.PathEffectDirectoryEntry,
	})
	if err != nil {
		t.Fatal(err)
	}
	holder, err := store.Acquire(context.Background(), domain)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := holder.Release(); err != nil {
			t.Errorf("release holder: %v", err)
		}
	}()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := RunWithOptions([]string{"apply", "--manifest", manifestPath, "--yes", "--json"}, RunOptions{
		Context: context.Background(), Stdout: &stdout, Stderr: &stderr, StderrIsTerminal: true,
	})
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want JSON-only diagnostic", stderr.String())
	}
	var payload struct {
		HasErrors bool `json:"has_errors"`
		Errors    []struct {
			Code    string                       `json:"code"`
			Phase   applyworkflow.FailurePhase   `json:"phase"`
			Outcome applyworkflow.FailureOutcome `json:"outcome"`
			Message string                       `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode JSON: %v; output=%q", err, stdout.String())
	}
	if !payload.HasErrors || len(payload.Errors) != 1 ||
		payload.Errors[0].Code != string(mutation.ReasonContention) ||
		payload.Errors[0].Phase != applyworkflow.FailurePhasePreflight ||
		payload.Errors[0].Outcome != applyworkflow.FailureOutcomeRefused ||
		payload.Errors[0].Message != "required mutation authority is busy" {
		t.Fatalf("contention payload = %#v", payload)
	}
	for _, private := range []string{"locks/mutation", ".lock"} {
		if strings.Contains(stdout.String(), private) {
			t.Fatalf("JSON disclosed private lease detail %q: %s", private, stdout.String())
		}
	}
	assertCLIPathMissing(t, filepath.Join(tempDir, "AGENTS.md"))
	assertCLIPathMissing(t, filepath.Join(tempDir, ".daem"))
}

func isolatedApplyAuthorityRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	return root
}

type beforeReadApplyConfirmation struct {
	reader     io.Reader
	beforeRead func() error
	read       bool
}

func (reader *beforeReadApplyConfirmation) Read(payload []byte) (int, error) {
	if !reader.read {
		reader.read = true
		if err := reader.beforeRead(); err != nil {
			return 0, err
		}
	}
	return reader.reader.Read(payload)
}
