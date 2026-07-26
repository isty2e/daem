package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
	daempaths "github.com/isty2e/daem/internal/paths"
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
	if !strings.Contains(stderr.String(), "Proceed with apply? [y/N]:") || !strings.Contains(stderr.String(), "stale_plan") || strings.Contains(stderr.String(), "stale_snapshot") {
		t.Fatalf("stderr = %q, want stale_plan only", stderr.String())
	}
	if strings.Contains(stdout.String(), "applied:") || strings.Contains(stderr.String(), "progress:") {
		t.Fatalf("stdout=%q stderr=%q, want no applied result or leaked progress", stdout.String(), stderr.String())
	}
	assertCLIPathMissing(t, filepath.Join(tempDir, "AGENTS.md"))
	assertCLIPathMissing(t, filepath.Join(tempDir, ".daem"))
	assertApplyConfirmationFileContent(t, sourcePath, "changed after disclosure\n")
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
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode JSON: %v; output=%q", err, stdout.String())
	}
	if !payload.HasErrors || len(payload.Errors) != 1 || payload.Errors[0].Code != string(mutation.ReasonContention) ||
		!strings.Contains(payload.Errors[0].Message, "is busy; retry after the other daem operation finishes") {
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
