package cli_test

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestCUXDefaultInitOmitsTemplateAndVerboseRestoresIt(t *testing.T) {
	root := t.TempDir()
	testkit.WithWorkingDirectory(t, root)

	defaultOutput := runCUXHuman(t, []string{"init", "--dry-run"})
	if strings.Contains(defaultOutput, "planned:") || strings.Contains(defaultOutput, `targets = ["codex"]`) {
		t.Fatalf("default output leaked starter content: %q", defaultOutput)
	}
	if !strings.Contains(defaultOutput, "next: rerun daem init without --dry-run") {
		t.Fatalf("default output = %q, want nearest action", defaultOutput)
	}

	verboseOutput := runCUXHuman(t, []string{"init", "--dry-run", "--verbose"})
	if !strings.Contains(verboseOutput, "planned:") || !strings.Contains(verboseOutput, `targets = ["codex"]`) {
		t.Fatalf("verbose output = %q, want starter content", verboseOutput)
	}
}

func TestCUXDefaultListOmitsProvenanceAndVerboseRestoresIt(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	testkit.WriteFile(t, root, "daem.toml", `version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]
`)

	defaultOutput := runCUXHuman(t, []string{"list", "resources", "--manifest", manifestPath})
	if !strings.Contains(defaultOutput, `resource kind=skill key="oracle" install="oracle" targets="codex" scope="project"`) {
		t.Fatalf("default output = %q, want enumerated identity", defaultOutput)
	}
	if strings.Contains(defaultOutput, "source=") || strings.Contains(defaultOutput, "group=") {
		t.Fatalf("default output leaked provenance: %q", defaultOutput)
	}

	verboseOutput := runCUXHuman(t, []string{"list", "resources", "--manifest", manifestPath, "--verbose"})
	if !strings.Contains(verboseOutput, `source="local:skills/oracle?mode=vendor"`) || !strings.Contains(verboseOutput, `group="-"`) {
		t.Fatalf("verbose output = %q, want provenance", verboseOutput)
	}
}

func TestCUXDefaultAuthoringOmitsManifestBlockAndVerboseRestoresIt(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	sourcePath := filepath.Join(root, "instructions", "AGENTS.md")
	testkit.WriteFile(t, root, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")
	testkit.WriteFile(t, filepath.Dir(sourcePath), filepath.Base(sourcePath), "instructions\n")
	args := []string{"add", "instruction", "project", sourcePath, "--manifest", manifestPath, "--target", "codex", "--dry-run"}

	defaultOutput := runCUXHuman(t, args)
	if strings.Contains(defaultOutput, "planned:") || strings.Contains(defaultOutput, `[instructions.`) {
		t.Fatalf("default output leaked manifest block: %q", defaultOutput)
	}
	if !strings.Contains(defaultOutput, "lockfile: would write") {
		t.Fatalf("default output = %q, want derived lock outcome", defaultOutput)
	}

	verboseOutput := runCUXHuman(t, append(args, "--verbose"))
	if !strings.Contains(verboseOutput, "planned:") || !strings.Contains(verboseOutput, `[instructions.`) {
		t.Fatalf("verbose output = %q, want manifest block", verboseOutput)
	}
}

func TestCUXDefaultImportOmitsCleanRowsAndVerboseRestoresThem(t *testing.T) {
	root := t.TempDir()
	testkit.WithWorkingDirectory(t, root)
	testkit.WriteFile(t, root, "AGENTS.md", "instructions\n")
	manifestPath := filepath.Join(root, "imported.toml")
	args := []string{"import", "--target", "codex", "--manifest", manifestPath, "--dry-run"}

	defaultOutput := runCUXHuman(t, args)
	if strings.Contains(defaultOutput, "scan resource=") || strings.Contains(defaultOutput, "import resource=") {
		t.Fatalf("default output leaked exhaustive import rows: %q", defaultOutput)
	}
	if !strings.Contains(defaultOutput, "summary:") || !strings.Contains(defaultOutput, "destination:") {
		t.Fatalf("default output = %q, want compact summary", defaultOutput)
	}

	verboseOutput := runCUXHuman(t, append(args, "--verbose"))
	if !strings.Contains(verboseOutput, "scan resource=") || !strings.Contains(verboseOutput, "import resource=") {
		t.Fatalf("verbose output = %q, want exhaustive rows", verboseOutput)
	}
}

func TestCUXDefaultLockOmitsUnchangedAndHashes(t *testing.T) {
	root := t.TempDir()
	manifestPath, _, _ := testkit.WriteLockDeltaFixture(t, root)

	defaultOutput := runCUXHuman(t, []string{"lock", "--manifest", manifestPath, "--dry-run"})
	for _, forbidden := range []string{"lockfile.subject.unchanged:", "source_id=", "content_hash=", "resolved_ref="} {
		if strings.Contains(defaultOutput, forbidden) {
			t.Fatalf("default output = %q, did not want %q", defaultOutput, forbidden)
		}
	}

	verboseOutput := runCUXHuman(t, []string{"lock", "--manifest", manifestPath, "--dry-run", "--verbose"})
	if !strings.Contains(verboseOutput, "lockfile.subject.unchanged:") || !strings.Contains(verboseOutput, "content_hash=") {
		t.Fatalf("verbose output = %q, want unchanged identities and hashes", verboseOutput)
	}
}

func TestCUXDefaultStatusOmitsInternalActionEvidence(t *testing.T) {
	root := t.TempDir()
	manifestPath, _, _ := testkit.WriteInstructionApplyFixture(t, root)

	defaultOutput := runCUXHuman(t, []string{"status", "--manifest", manifestPath})
	if !strings.Contains(defaultOutput, "add managed output") {
		t.Fatalf("default output = %q, want pending mutation", defaultOutput)
	}
	for _, forbidden := range []string{"mode=", "reason=", "content_path=", "statefile:"} {
		if strings.Contains(defaultOutput, forbidden) {
			t.Fatalf("default output = %q, did not want %q", defaultOutput, forbidden)
		}
	}

	verboseOutput := runCUXHuman(t, []string{"status", "--manifest", manifestPath, "--verbose"})
	if !strings.Contains(verboseOutput, "mode=copy") || !strings.Contains(verboseOutput, "reason=missing_output") {
		t.Fatalf("verbose output = %q, want causal evidence", verboseOutput)
	}
}

func TestCUXDefaultDoctorCountsSuccessfulChecks(t *testing.T) {
	root := t.TempDir()
	testkit.WithWorkingDirectory(t, root)
	defaultOutput := runCUXHuman(t, []string{"doctor"})
	if !strings.Contains(defaultOutput, "doctor:") || !strings.Contains(defaultOutput, "ok=") {
		t.Fatalf("default output = %q, want severity totals", defaultOutput)
	}
	if strings.Contains(defaultOutput, "\nok ") {
		t.Fatalf("default output leaked successful rows: %q", defaultOutput)
	}

	verboseOutput := runCUXHuman(t, []string{"doctor", "--verbose"})
	if !strings.Contains(verboseOutput, "\nok ") {
		t.Fatalf("verbose output = %q, want successful rows", verboseOutput)
	}
}

func TestCUXDefaultRecoverOmitsBackupInternals(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	paths, currentState, _, _, _ := captureCLIRecoveryUpdateJournal(t, manifestPath)
	testkit.WriteStatefile(t, paths.StatefilePath, currentState)
	testkit.WriteFile(t, root, "AGENTS.md", "new instructions\n")

	defaultOutput := runCUXHuman(t, []string{"recover", "--manifest", manifestPath, "--dry-run"})
	if strings.Contains(defaultOutput, "backup=") || strings.Contains(defaultOutput, "operation directory:") {
		t.Fatalf("default output leaked recovery internals: %q", defaultOutput)
	}

	verboseOutput := runCUXHuman(t, []string{"recover", "--manifest", manifestPath, "--dry-run", "--verbose"})
	if !strings.Contains(verboseOutput, `backup="files/000001"`) || !strings.Contains(verboseOutput, "operation directory:") {
		t.Fatalf("verbose output = %q, want recovery evidence", verboseOutput)
	}
}

func TestCUXProbeDisclosureStaysVisibleWithoutVerboseInternals(t *testing.T) {
	root := t.TempDir()
	testkit.SetDefaultRootEnv(t, filepath.Join(root, "home"))
	manifestPath := filepath.Join(root, "daem.toml")
	lockfilePath := filepath.Join(root, "daem.lock.toml")
	testkit.WriteFile(t, root, "daem.toml", `version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "context7-mcp"
args = ["--stdio"]
`)
	var lockStdout bytes.Buffer
	var lockStderr bytes.Buffer
	if exitCode := testkit.RunCLI([]string{"lock", "--manifest", manifestPath}, &lockStdout, &lockStderr); exitCode != 0 {
		t.Fatalf("lock exitCode=%d stdout=%q stderr=%q", exitCode, lockStdout.String(), lockStderr.String())
	}

	args := []string{"probe", "mcp-server", "context7", "--manifest", manifestPath, "--dry-run"}
	defaultOutput := runCUXHuman(t, args)
	for _, required := range []string{`command="context7-mcp"`, `args=["--stdio"]`, "side effects:", "future skip authority"} {
		if !strings.Contains(defaultOutput, required) {
			t.Fatalf("default output = %q, want %q", defaultOutput, required)
		}
	}
	if strings.Contains(defaultOutput, "  manifest:") || strings.Contains(defaultOutput, "  lockfile:") {
		t.Fatalf("default output leaked verbose paths: %q", defaultOutput)
	}

	verboseOutput := runCUXHuman(t, append(args, "--verbose"))
	if !strings.Contains(verboseOutput, "  manifest: "+manifestPath) || !strings.Contains(verboseOutput, "  lockfile: "+lockfilePath) {
		t.Fatalf("verbose output = %q, want exact workspace paths", verboseOutput)
	}
}

func TestCUXStableOutputFailureFailsCommand(t *testing.T) {
	root := t.TempDir()
	testkit.WithWorkingDirectory(t, root)
	var stderr bytes.Buffer
	exitCode := testkit.RunCLI([]string{"init", "--dry-run"}, failingStableWriter{}, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want stable output failure", exitCode)
	}
	if !strings.Contains(stderr.String(), "output failed: stable output closed") {
		t.Fatalf("stderr = %q, want bounded output failure", stderr.String())
	}
}

func TestCUXStableOutputFailureDoesNotHideNonCleanCheckIdentity(t *testing.T) {
	root := t.TempDir()
	manifestPath, _, _ := testkit.WriteInstructionApplyFixture(t, root)
	var stderr bytes.Buffer

	exitCode := testkit.RunCLI(
		[]string{"status", "--manifest", manifestPath, "--check"},
		failingStableWriter{},
		&stderr,
	)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want non-clean status identity", exitCode)
	}
	if !strings.Contains(stderr.String(), "output failed: stable output closed") {
		t.Fatalf("stderr = %q, want bounded output failure", stderr.String())
	}
}

func runCUXHuman(t *testing.T, args []string) string {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunCLI(args, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("args=%q exitCode=%d stdout=%q stderr=%q", args, exitCode, stdout.String(), stderr.String())
	}
	return stdout.String()
}

type failingStableWriter struct{}

func (failingStableWriter) Write([]byte) (int, error) {
	return 0, errors.New("stable output closed")
}
