package cli

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/runtimeprobe"
	runtimeprobemcp "github.com/isty2e/daem/internal/assurance/runtimeprobe/mcp"
	"github.com/isty2e/daem/internal/subprocess"
)

func TestRuntimeCommandsRejectBareNonInteractiveExecutionBeforeWorkspaceLookup(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		diagnostic string
	}{
		{name: "recover", args: []string{"recover"}, diagnostic: "non-interactive recovery requires --yes"},
		{name: "probe", args: []string{"probe", "mcp-server", "context7"}, diagnostic: "non-interactive probe requires --yes"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := runWithoutTerminal(test.args, &stdout, &stderr)
			if exitCode != 2 || stdout.Len() != 0 {
				t.Fatalf("exitCode = %d stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), test.diagnostic) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), test.diagnostic)
			}
			if strings.Contains(stderr.String(), "manifest") {
				t.Fatalf("stderr = %q, workspace lookup must not precede confirmation policy", stderr.String())
			}
		})
	}
}

func TestProbeTTYConfirmationDoesNotExecuteBeforeConsent(t *testing.T) {
	manifestPath := writeProbeConfirmationFixture(t)
	executor := &confirmationProbeExecutor{facts: successfulProbeFacts()}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	options := interactiveRunOptions(strings.NewReader("no\n"), &stdout, &stderr)
	options.ProbeExecutor = executor
	exitCode := RunWithOptions([]string{"probe", "mcp-server", "context7", "--manifest", manifestPath}, options)
	if exitCode != 1 || !strings.Contains(stderr.String(), "probe canceled") {
		t.Fatalf("exitCode = %d stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d, want zero before consent", executor.calls)
	}
	if !strings.Contains(stdout.String(), "probe execution: not run; awaiting confirmation") ||
		strings.Contains(stdout.String(), "rerun with --yes") ||
		strings.Contains(stdout.String(), "Proceed with probe?") {
		t.Fatalf("stdout = %q, want stable disclosed dry-run", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Proceed with probe? [y/N]") {
		t.Fatalf("stderr = %q, want prompt", stderr.String())
	}
}

func TestProbeTTYConfirmationExecutesOnceAfterConsent(t *testing.T) {
	manifestPath := writeProbeConfirmationFixture(t)
	executor := &confirmationProbeExecutor{facts: successfulProbeFacts()}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	options := interactiveRunOptions(strings.NewReader("yes\n"), &stdout, &stderr)
	options.ProbeExecutor = executor
	exitCode := RunWithOptions([]string{"probe", "mcp-server", "context7", "--manifest", manifestPath}, options)
	if exitCode != 0 || !strings.Contains(stderr.String(), "Proceed with probe? [y/N]") {
		t.Fatalf("exitCode = %d stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if executor.calls != 1 {
		t.Fatalf("executor calls = %d, want exactly one after consent", executor.calls)
	}
	if strings.Count(stdout.String(), "mcp runtime probe:") != 2 || !strings.Contains(stdout.String(), "mcp runtime probe: execute") {
		t.Fatalf("stdout = %q, want pre-consent disclosure and post-execution result", stdout.String())
	}
}

func TestProbeTTYConfirmationExecutesDisclosedRequestAfterManifestAndLockChange(t *testing.T) {
	manifestPath := writeProbeConfirmationFixture(t)
	root := filepath.Dir(manifestPath)
	executor := &confirmationProbeExecutor{facts: successfulProbeFacts()}
	input := &beforeReadApplyConfirmation{
		reader: strings.NewReader("yes\n"),
		beforeRead: func() error {
			writeApplyConfirmationFile(t, root, "daem.toml", `version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "changed-after-disclosure"
args = ["--changed"]
`)
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			if exitCode := runWithoutTerminal([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr); exitCode != 0 {
				return errors.New("rewrite lock after disclosure: " + stderr.String())
			}
			return nil
		},
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	options := interactiveRunOptions(input, &stdout, &stderr)
	options.ProbeExecutor = executor
	exitCode := RunWithOptions([]string{"probe", "mcp-server", "context7", "--manifest", manifestPath}, options)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if executor.calls != 1 || executor.request.Command != "context7-mcp" ||
		len(executor.request.Args) != 1 || executor.request.Args[0] != "--stdio" {
		t.Fatalf("executor = calls %d request %#v, want exact pre-confirmation disclosure", executor.calls, executor.request)
	}
}

func TestProbeDisclosureWriteFailureDoesNotPromptOrExecute(t *testing.T) {
	manifestPath := writeProbeConfirmationFixture(t)
	executor := &confirmationProbeExecutor{facts: successfulProbeFacts()}
	input := &countingReader{reader: strings.NewReader("yes\n")}
	formatted := 0
	stdoutErr := privateOutputFailure{calls: &formatted}
	var stderr bytes.Buffer
	options := interactiveRunOptions(input, errorWriter{err: stdoutErr}, &stderr)
	options.ProbeExecutor = executor

	exitCode := RunWithOptions([]string{"probe", "mcp-server", "context7", "--manifest", manifestPath}, options)
	if exitCode != 1 || !strings.Contains(stderr.String(), "probe failed: command output could not be written") {
		t.Fatalf("exitCode = %d stderr = %q", exitCode, stderr.String())
	}
	if formatted != 0 {
		t.Fatalf("private output error formatted %d times", formatted)
	}
	if executor.calls != 0 || input.reads != 0 || strings.Contains(stderr.String(), "Proceed with probe?") {
		t.Fatalf("executor calls = %d input reads = %d stderr = %q", executor.calls, input.reads, stderr.String())
	}
}

func TestProbeContextCancellationOverridesAffirmativeAnswer(t *testing.T) {
	manifestPath := writeProbeConfirmationFixture(t)
	executor := &confirmationProbeExecutor{facts: successfulProbeFacts()}
	ctx, cancel := context.WithCancel(context.Background())
	input := &cancelAfterRead{reader: strings.NewReader("yes\n"), cancel: cancel}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	options := interactiveRunOptions(input, &stdout, &stderr)
	options.Context = ctx
	options.ProbeExecutor = executor

	exitCode := RunWithOptions([]string{"probe", "mcp-server", "context7", "--manifest", manifestPath}, options)
	if exitCode != 1 || !strings.Contains(stderr.String(), "probe canceled: context canceled") {
		t.Fatalf("exitCode = %d stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d, want zero", executor.calls)
	}
}

type confirmationProbeExecutor struct {
	calls   int
	request runtimeprobemcp.ProbeRequest
	facts   []runtimeprobe.Fact
}

func (executor *confirmationProbeExecutor) Probe(
	_ context.Context,
	request runtimeprobemcp.ProbeRequest,
	_ subprocess.WorkingDirectoryBinder,
) ([]runtimeprobe.Fact, error) {
	executor.calls++
	executor.request = request
	return executor.facts, nil
}

func writeProbeConfirmationFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	writeApplyConfirmationFile(t, root, "daem.toml", `version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "context7-mcp"
args = ["--stdio"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := runWithoutTerminal([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr); exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("lock exitCode = %d stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	return manifestPath
}

func successfulProbeFacts() []runtimeprobe.Fact {
	return []runtimeprobe.Fact{
		{Dimension: runtimeprobe.DimensionLauncher, State: runtimeprobe.ObservedOK, Source: runtimeprobe.SourceExplicit, Freshness: runtimeprobe.FreshnessCurrent},
		{Dimension: runtimeprobe.DimensionProtocolInitialize, State: runtimeprobe.ObservedOK, Source: runtimeprobe.SourceExplicit, Freshness: runtimeprobe.FreshnessCurrent},
		{Dimension: runtimeprobe.DimensionEndpointHealth, State: runtimeprobe.NotApplicable, Reason: runtimeprobe.ReasonNotApplicable, Source: runtimeprobe.SourceExplicit, Freshness: runtimeprobe.FreshnessCurrent},
		{Dimension: runtimeprobe.DimensionAuthentication, State: runtimeprobe.Unsupported, Reason: runtimeprobe.ReasonUnsupported, Source: runtimeprobe.SourceExplicit, Freshness: runtimeprobe.FreshnessCurrent},
		{Dimension: runtimeprobe.DimensionToolInventory, State: runtimeprobe.Unsupported, Reason: runtimeprobe.ReasonUnsupported, Source: runtimeprobe.SourceExplicit, Freshness: runtimeprobe.FreshnessCurrent},
	}
}
