package cli_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/runtimeprobe"
	runtimeprobemcp "github.com/isty2e/daem/internal/assurance/runtimeprobe/mcp"
	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/execcheck"
)

func TestMCPProbeDryRunDisclosesWithoutInvokingHostCommand(t *testing.T) {
	canary := execcheck.New(t, "must-not-run-daem-test")
	project := newMCPCLIProject(t)
	writeMCPManifest(t, project.root, mcpManifestSpec{
		Command: "must-not-run-daem-test",
		Args:    []string{"--serve", "context7"},
	})
	runMCPLock(t, project)

	stdout := runMCPCLIExpect(t, 0, "probe dry-run json", "probe", "mcp-server", "context7", "--manifest", project.manifestPath, "--target", "claude-code", "--scope", "project", "--dry-run", "--json")
	execcheck.AssertClean(t, canary, "probe dry-run")
	payload := decodeMCPProbeJSONTestPayload(t, stdout)
	sideEffects := strings.Join(payload.Probe.SideEffects, "\n")
	if !strings.Contains(sideEffects, "package/cache/network") {
		t.Fatalf("side effects = %q, want package/cache/network disclosure for arbitrary locked command", sideEffects)
	}
	assertMCPProbeJSONDimension(t, stdout, "runtime_launcher", "not_probed", "RUNTIME_NOT_PROBED")
	assertMCPProbeJSONDimension(t, stdout, "protocol_initialize", "not_probed", "RUNTIME_NOT_PROBED")
	if strings.Contains(stdout, `"ready"`) || strings.Contains(stdout, `"healthy"`) {
		t.Fatalf("probe json = %s, must not contain ready/healthy aggregate", stdout)
	}
}

func TestMCPProbeDryRunReportsExactTimeoutAndSideEffectEnvelope(t *testing.T) {
	project := newMCPCLIProject(t)
	writeMCPManifest(t, project.root, mcpManifestSpec{
		Command: "npx",
		Args:    []string{"-y", "@example/server"},
	})
	runMCPLock(t, project)

	stdout := runMCPCLIExpect(t, 0, "probe dry-run timeout json", "probe", "mcp-server", "context7", "--manifest", project.manifestPath, "--target", "claude-code", "--scope", "project", "--timeout", "500ms", "--dry-run", "--json")
	var payload struct {
		Timeout        string `json:"timeout"`
		TimeoutSeconds int    `json:"timeout_seconds"`
		Probe          struct {
			SideEffects []string `json:"side_effects"`
		} `json:"probe"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("decode probe json: %v\n%s", err, stdout)
	}
	if payload.Timeout != "500ms" || payload.TimeoutSeconds != 1 {
		t.Fatalf("timeout = %q timeout_seconds=%d, want exact 500ms and rounded-up 1 second", payload.Timeout, payload.TimeoutSeconds)
	}
	sideEffects := strings.Join(payload.Probe.SideEffects, "\n")
	for _, want := range []string{"process environment", "network calls", "auth", "trust", "session", "timeout", "redaction", "package/cache/network"} {
		if !strings.Contains(sideEffects, want) {
			t.Fatalf("side effects = %q, want disclosure containing %q", sideEffects, want)
		}
	}
}

func TestMCPProbeValidatesConsentModesAndInfersUniqueSelectors(t *testing.T) {
	project := newMCPCLIProject(t)
	writeMCPManifest(t, project.root, mcpManifestSpec{Command: "node", Args: []string{}})
	runMCPLock(t, project)

	invalid := []struct {
		name string
		args []string
	}{
		{
			name: "json requires noninteractive mode",
			args: []string{"probe", "mcp-server", "context7", "--manifest", project.manifestPath, "--json"},
		},
		{
			name: "both modes",
			args: []string{"probe", "mcp-server", "context7", "--manifest", project.manifestPath, "--target", "claude-code", "--scope", "project", "--dry-run", "--yes"},
		},
	}

	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			exitCode, stdout, stderr := runMCPCLI(t, test.args...)
			if exitCode != 2 {
				t.Fatalf("exitCode=%d stdout=%q stderr=%q, want misuse exit 2", exitCode, stdout, stderr)
			}
		})
	}

	for _, args := range [][]string{
		{"probe", "mcp-server", "context7", "--manifest", project.manifestPath, "--dry-run"},
		{"probe", "mcp-server", "context7", "--manifest", project.manifestPath, "--scope", "project", "--dry-run"},
		{"probe", "mcp-server", "context7", "--manifest", project.manifestPath, "--target", "claude-code", "--dry-run"},
	} {
		exitCode, stdout, stderr := runMCPCLI(t, args...)
		if exitCode != 0 || stderr != "" {
			t.Fatalf("inferred selector args=%q exitCode=%d stdout=%q stderr=%q", args, exitCode, stdout, stderr)
		}
		if !strings.Contains(stdout, "target=claude-code scope=project") {
			t.Fatalf("stdout=%q, want inferred locked row", stdout)
		}
	}
}

func TestMCPProbeYesUsesInjectedExecutorAndReportsDimensions(t *testing.T) {
	project := newMCPCLIProject(t)
	writeMCPManifest(t, project.root, mcpManifestSpec{
		Command: "node",
		Args:    []string{"server.js"},
		Env:     map[string]string{"API_TOKEN": "HOST_TOKEN"},
	})
	runMCPLock(t, project)
	executor := &fakeCLIRuntimeProbeExecutor{
		facts: observedOKMCPProbeFacts(),
	}
	type contextKey struct{}
	rootContext := context.WithValue(context.Background(), contextKey{}, "root")

	exitCode, stdout, stderr := runMCPCLIWithOptions(t, []string{"probe", "mcp-server", "context7", "--manifest", project.manifestPath, "--target", "claude-code", "--scope", "project", "--yes", "--json"}, clipkg.RunOptions{
		Context:       rootContext,
		ProbeExecutor: executor,
	})
	if exitCode != 0 || stderr != "" {
		t.Fatalf("probe yes exitCode=%d stdout=%q stderr=%q, want success JSON", exitCode, stdout, stderr)
	}
	if !executor.called {
		t.Fatal("probe yes did not call injected executor")
	}
	if executor.context == nil || executor.context.Value(contextKey{}) != "root" {
		t.Fatal("probe executor did not receive the RunOptions root context")
	}
	if executor.request.Command != "node" ||
		!stringSlicesEqualTest(executor.request.Args, []string{"server.js"}) ||
		executor.request.Env["API_TOKEN"] != "HOST_TOKEN" {
		t.Fatalf("probe request = %#v, want locked command/args/env refs", executor.request)
	}
	payload := decodeMCPProbeJSONTestPayload(t, stdout)
	if len(payload.Probe.EnvBindings) != 1 ||
		payload.Probe.EnvBindings[0].ChildName != "API_TOKEN" ||
		payload.Probe.EnvBindings[0].HostSourceName != "HOST_TOKEN" {
		t.Fatalf("probe env bindings = %#v, want exact API_TOKEN<-HOST_TOKEN disclosure", payload.Probe.EnvBindings)
	}
	assertMCPProbeJSONDimension(t, stdout, "runtime_launcher", "observed_ok", "")
	assertMCPProbeJSONDimension(t, stdout, "protocol_initialize", "observed_ok", "")
	assertMCPProbeJSONDimension(t, stdout, "endpoint_health", "not_applicable", "RUNTIME_NOT_APPLICABLE")
	assertMCPProbeJSONDimension(t, stdout, "runtime_authentication", "unsupported", "RUNTIME_UNSUPPORTED")
	assertMCPProbeJSONDimension(t, stdout, "tool_inventory", "unsupported", "RUNTIME_UNSUPPORTED")
	if strings.Contains(stdout, `"ready"`) || strings.Contains(stdout, `"healthy"`) {
		t.Fatalf("probe json = %s, must not contain ready/healthy aggregate", stdout)
	}
}

func TestMCPProbeOpenCodeDryRunDisclosesProjectionDerivedRequest(t *testing.T) {
	project := newMCPCLIProject(t)
	writeMCPManifest(t, project.root, mcpManifestSpec{
		Target:  "opencode",
		Command: "npx",
		Args:    []string{"-y", "@example/server"},
	})
	runMCPLock(t, project)

	stdout := runMCPCLIExpect(t, 0, "opencode probe dry-run json", "probe", "mcp-server", "context7", "--manifest", project.manifestPath, "--target", "opencode", "--scope", "project", "--dry-run", "--json")
	payload := decodeMCPProbeJSONTestPayload(t, stdout)

	if payload.Subject.Namespace != "opencode.project.mcp-server" || payload.Subject.Name != "context7" {
		t.Fatalf("subject = %#v, want OpenCode project MCP projection", payload.Subject)
	}
	if payload.Probe.Transport != "stdio" ||
		payload.Probe.Command != "npx" ||
		!stringSlicesEqualTest(payload.Probe.Args, []string{"-y", "@example/server"}) ||
		len(payload.Probe.EnvBindings) != 0 ||
		payload.Probe.WorkDir != project.root {
		t.Fatalf("probe disclosure = %#v, want OpenCode locked command argv without env refs in project workdir", payload.Probe)
	}
	sideEffects := strings.Join(payload.Probe.SideEffects, "\n")
	for _, want := range []string{"workdir", "process environment", "network calls", "auth", "trust", "session", "timeout", "redaction", "package/cache/network"} {
		if !strings.Contains(sideEffects, want) {
			t.Fatalf("side effects = %q, want disclosure containing %q", sideEffects, want)
		}
	}
	for _, forbidden := range []string{"environment variables", "env values"} {
		if strings.Contains(sideEffects, forbidden) {
			t.Fatalf("side effects = %q, must not contain OpenCode env lookup disclosure %q", sideEffects, forbidden)
		}
	}
	assertMCPProbeJSONDimension(t, stdout, "runtime_launcher", "not_probed", "RUNTIME_NOT_PROBED")
	assertMCPProbeJSONDimension(t, stdout, "protocol_initialize", "not_probed", "RUNTIME_NOT_PROBED")
}

func TestMCPProbeOpenCodeYesUsesExactProjectionWithoutDelegatePlan(t *testing.T) {
	project := newMCPCLIProject(t)
	writeMCPManifest(t, project.root, mcpManifestSpec{
		Target:  "opencode",
		Command: "node",
		Args:    []string{"server.js"},
	})
	runMCPLock(t, project)
	executor := &fakeCLIRuntimeProbeExecutor{
		facts: observedOKMCPProbeFacts(),
	}

	exitCode, stdout, stderr := runMCPCLIWithOptions(t, []string{"probe", "mcp-server", "context7", "--manifest", project.manifestPath, "--target", "opencode", "--scope", "project", "--yes", "--json"}, clipkg.RunOptions{
		ProbeExecutor: executor,
	})
	if exitCode != 0 || stderr != "" {
		t.Fatalf("opencode probe yes exitCode=%d stdout=%q stderr=%q, want success JSON", exitCode, stdout, stderr)
	}
	if !executor.called {
		t.Fatal("opencode probe yes did not call injected executor")
	}
	if _, err := os.Stat(filepath.Join(project.root, "opencode.json")); !os.IsNotExist(err) {
		t.Fatalf("opencode.json stat err = %v, want probe execution to leave host config absent", err)
	}
	if executor.request.Command != "node" ||
		!stringSlicesEqualTest(executor.request.Args, []string{"server.js"}) ||
		len(executor.request.Env) != 0 {
		t.Fatalf("probe request = %#v, want exact OpenCode projection command argv without delegate env", executor.request)
	}
	payload := decodeMCPProbeJSONTestPayload(t, stdout)
	if payload.Probe.WorkDir != project.root {
		t.Fatalf("probe workdir = %q, want selected root %q", payload.Probe.WorkDir, project.root)
	}
	if len(payload.Probe.EnvBindings) != 0 {
		t.Fatalf("env_bindings = %#v, want none for OpenCode local command projection", payload.Probe.EnvBindings)
	}
	assertMCPProbeJSONDimension(t, stdout, "runtime_launcher", "observed_ok", "")
	assertMCPProbeJSONDimension(t, stdout, "protocol_initialize", "observed_ok", "")
	if strings.Contains(stdout, `"ready"`) || strings.Contains(stdout, `"healthy"`) {
		t.Fatalf("probe json = %s, must not contain ready/healthy aggregate", stdout)
	}
}

func TestMCPProbeOpenCodeDoesNotExecuteStaleLock(t *testing.T) {
	project := newMCPCLIProject(t)
	writeMCPManifest(t, project.root, mcpManifestSpec{
		Target:  "opencode",
		Command: "node",
		Args:    []string{"server.js"},
	})
	runMCPLock(t, project)
	writeMCPManifest(t, project.root, mcpManifestSpec{
		Target:  "opencode",
		Command: "node",
		Args:    []string{"server.js", "--changed"},
	})
	executor := &fakeCLIRuntimeProbeExecutor{}

	exitCode, stdout, stderr := runMCPCLIWithOptions(t, []string{"probe", "mcp-server", "context7", "--manifest", project.manifestPath, "--target", "opencode", "--scope", "project", "--yes", "--json"}, clipkg.RunOptions{
		ProbeExecutor: executor,
	})
	if exitCode != 1 || stdout != "" || !strings.Contains(stderr, "stale") {
		t.Fatalf("opencode stale probe exitCode=%d stdout=%q stderr=%q, want stale error", exitCode, stdout, stderr)
	}
	if executor.called {
		t.Fatal("opencode stale lock called probe executor")
	}
}

func TestMCPProbeOpenCodeRejectsClaudeOnlyLockForSameNameServer(t *testing.T) {
	project := newMCPCLIProject(t)
	writeMCPManifest(t, project.root, mcpManifestSpec{
		Command: "claude-server",
		Args:    []string{"--cc"},
	})
	runMCPLock(t, project)
	writeMCPManifest(t, project.root, mcpManifestSpec{
		Target:  "opencode",
		Command: "opencode-server",
		Args:    []string{"--oc"},
	})
	executor := &fakeCLIRuntimeProbeExecutor{}

	exitCode, stdout, stderr := runMCPCLIWithOptions(t, []string{"probe", "mcp-server", "context7", "--manifest", project.manifestPath, "--target", "opencode", "--scope", "project", "--yes", "--json"}, clipkg.RunOptions{
		ProbeExecutor: executor,
	})
	if exitCode != 1 ||
		stdout != "" ||
		!strings.Contains(stderr, "locked MCP subject projection/opencode.project.mcp-server") ||
		!strings.Contains(stderr, "missing") {
		t.Fatalf("cross-target opencode probe exitCode=%d stdout=%q stderr=%q, want missing OpenCode locked subject", exitCode, stdout, stderr)
	}
	if executor.called {
		t.Fatal("same-name Claude-only lock called OpenCode probe executor")
	}
}

func TestMCPProbeOpenCodeRejectsInvalidCurrentManifestBeforeExecutor(t *testing.T) {
	tests := []struct {
		name    string
		rewrite func(t *testing.T, root string)
		want    string
	}{
		{
			name: "env refs unsupported",
			rewrite: func(t *testing.T, root string) {
				writeMCPManifest(t, root, mcpManifestSpec{
					Target:  "opencode",
					Command: "node",
					Args:    []string{"server.js"},
					Env:     map[string]string{"API_TOKEN": "HOST_TOKEN"},
				})
			},
			want: "OpenCode MCP projection does not support env",
		},
		{
			name: "non stdio transport unsupported",
			rewrite: func(t *testing.T, root string) {
				writeOpenCodeMCPManifestWithTransport(t, root, "sse")
			},
			want: "unsupported MCP transport",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			project := newMCPCLIProject(t)
			writeMCPManifest(t, project.root, mcpManifestSpec{
				Target:  "opencode",
				Command: "node",
				Args:    []string{"server.js"},
			})
			runMCPLock(t, project)
			test.rewrite(t, project.root)
			executor := &fakeCLIRuntimeProbeExecutor{}

			exitCode, stdout, stderr := runMCPCLIWithOptions(t, []string{"probe", "mcp-server", "context7", "--manifest", project.manifestPath, "--target", "opencode", "--scope", "project", "--yes", "--json"}, clipkg.RunOptions{
				ProbeExecutor: executor,
			})
			if exitCode != 1 || stdout != "" || !strings.Contains(stderr, test.want) {
				t.Fatalf("invalid opencode probe exitCode=%d stdout=%q stderr=%q, want %q", exitCode, stdout, stderr, test.want)
			}
			if executor.called {
				t.Fatal("invalid current OpenCode manifest called probe executor")
			}
		})
	}
}

func TestMCPProbeOpenCodeRejectsMissingLockBeforeExecutor(t *testing.T) {
	project := newMCPCLIProject(t)
	writeMCPManifest(t, project.root, mcpManifestSpec{
		Target:  "opencode",
		Command: "node",
		Args:    []string{"server.js"},
	})
	executor := &fakeCLIRuntimeProbeExecutor{}

	exitCode, stdout, stderr := runMCPCLIWithOptions(t, []string{"probe", "mcp-server", "context7", "--manifest", project.manifestPath, "--target", "opencode", "--scope", "project", "--yes", "--json"}, clipkg.RunOptions{
		ProbeExecutor: executor,
	})
	if exitCode != 1 || stdout != "" || !strings.Contains(stderr, "run lock") {
		t.Fatalf("missing lock opencode probe exitCode=%d stdout=%q stderr=%q, want run lock guidance", exitCode, stdout, stderr)
	}
	if executor.called {
		t.Fatal("missing lock called probe executor")
	}
}

func TestMCPProbeOpenCodeMissingRunnerReturnsDimensionalFailure(t *testing.T) {
	project := newMCPCLIProject(t)
	writeMCPManifest(t, project.root, mcpManifestSpec{
		Target:  "opencode",
		Command: "definitely-missing-daem-mcp-runner-rt05",
		Args:    []string{},
	})
	runMCPLock(t, project)

	exitCode, stdout, stderr := runMCPCLI(t, "probe", "mcp-server", "context7", "--manifest", project.manifestPath, "--target", "opencode", "--scope", "project", "--yes", "--json")
	if exitCode != 1 || stderr != "" {
		t.Fatalf("missing runner opencode probe exitCode=%d stdout=%q stderr=%q, want failed JSON only", exitCode, stdout, stderr)
	}
	payload := decodeMCPProbeJSONTestPayload(t, stdout)
	if !payload.HasErrors {
		t.Fatalf("payload = %#v, want has_errors=true", payload)
	}
	assertMCPProbeJSONDimension(t, stdout, "runtime_launcher", "observed_failed", "RUNTIME_OBSERVED_FAILED")
	if !strings.Contains(runtimeDimensionDetail(payload, "runtime_launcher"), "missing runner") {
		t.Fatalf("launcher detail = %#v, want missing runner", payload.Dimensions)
	}
	assertMCPProbeJSONDimension(t, stdout, "protocol_initialize", "not_probed", "RUNTIME_NOT_PROBED")
}

func TestMCPProbeDefaultRunnerLaunchesFromSelectedProjectRoot(t *testing.T) {
	project := newMCPCLIProject(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	if err := os.Symlink(executable, filepath.Join(project.root, "mcp-probe-helper")); err != nil {
		t.Fatalf("create PATH probe helper: %v", err)
	}
	physicalRoot, err := filepath.EvalSymlinks(project.root)
	if err != nil {
		t.Fatalf("resolve project root: %v", err)
	}
	t.Setenv("DAEM_MCP_CLI_EXPECTED_CWD", physicalRoot)
	t.Setenv("PATH", project.root+string(os.PathListSeparator)+os.Getenv("PATH"))
	writeMCPManifest(t, project.root, mcpManifestSpec{
		Target:  "opencode",
		Command: "mcp-probe-helper",
		Args:    []string{"-test.run=^TestMCPCLIRuntimeProbeHelperProcess$"},
	})
	runMCPLock(t, project)

	exitCode, stdout, stderr := runMCPCLI(
		t,
		"probe", "mcp-server", "context7",
		"--manifest", project.manifestPath,
		"--target", "opencode",
		"--scope", "project",
		"--yes",
		"--json",
	)
	if exitCode != 0 || stderr != "" {
		t.Fatalf("default probe exitCode=%d stdout=%q stderr=%q, want success JSON", exitCode, stdout, stderr)
	}
	assertMCPProbeJSONDimension(t, stdout, "runtime_launcher", "observed_ok", "")
	assertMCPProbeJSONDimension(t, stdout, "protocol_initialize", "observed_ok", "")
}

func TestMCPCLIRuntimeProbeHelperProcess(t *testing.T) {
	expectedCWD := os.Getenv("DAEM_MCP_CLI_EXPECTED_CWD")
	if expectedCWD == "" {
		return
	}
	cwd, err := os.Getwd()
	if err != nil || cwd != expectedCWD {
		os.Exit(81)
	}
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		os.Exit(82)
	}
	var request struct {
		ID     int `json:"id"`
		Params struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"params"`
	}
	if err := json.Unmarshal([]byte(line), &request); err != nil ||
		request.ID != 1 ||
		request.Params.ProtocolVersion == "" {
		os.Exit(83)
	}
	fmt.Printf(
		`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":%q,"capabilities":{},"serverInfo":{"name":"cli-test","version":"1"}}}`+"\n",
		request.Params.ProtocolVersion,
	)
	notification, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(notification, "notifications/initialized") {
		os.Exit(84)
	}
}

func TestMCPProbeDoesNotExecuteStaleLock(t *testing.T) {
	project := newMCPCLIProject(t)
	writeMCPManifest(t, project.root, mcpManifestSpec{
		Command: "node",
		Args:    []string{"server.js"},
	})
	runMCPLock(t, project)
	writeMCPManifest(t, project.root, mcpManifestSpec{
		Command: "node",
		Args:    []string{"server.js", "--changed"},
	})
	executor := &fakeCLIRuntimeProbeExecutor{}

	exitCode, stdout, stderr := runMCPCLIWithOptions(t, []string{"probe", "mcp-server", "context7", "--manifest", project.manifestPath, "--target", "claude-code", "--scope", "project", "--yes", "--json"}, clipkg.RunOptions{
		ProbeExecutor: executor,
	})
	if exitCode != 1 || stdout != "" || !strings.Contains(stderr, "stale") {
		t.Fatalf("stale probe exitCode=%d stdout=%q stderr=%q, want stale error", exitCode, stdout, stderr)
	}
	if executor.called {
		t.Fatal("stale lock called probe executor")
	}
}

func TestMCPProbeYesFailureReturnsDimensionalJSON(t *testing.T) {
	project := newMCPCLIProject(t)
	writeMCPManifest(t, project.root, mcpManifestSpec{
		Command: "node",
		Args:    []string{"server.js"},
	})
	runMCPLock(t, project)
	executor := &fakeCLIRuntimeProbeExecutor{
		facts: []runtimeprobe.Fact{
			{
				Dimension: runtimeprobe.DimensionLauncher,
				State:     runtimeprobe.ObservedOK,
				Source:    runtimeprobe.SourceExplicit,
				Freshness: runtimeprobe.FreshnessCurrent,
			},
			{
				Dimension:       runtimeprobe.DimensionProtocolInitialize,
				State:           runtimeprobe.ObservedFailed,
				Reason:          runtimeprobe.ReasonObservedFailed,
				Source:          runtimeprobe.SourceExplicit,
				Freshness:       runtimeprobe.FreshnessCurrent,
				SanitizedDetail: "initialize error",
			},
		},
	}

	exitCode, stdout, stderr := runMCPCLIWithOptions(t, []string{"probe", "mcp-server", "context7", "--manifest", project.manifestPath, "--target", "claude-code", "--scope", "project", "--yes", "--json"}, clipkg.RunOptions{
		ProbeExecutor: executor,
	})
	if exitCode != 1 || stderr != "" {
		t.Fatalf("probe yes failure exitCode=%d stdout=%q stderr=%q, want failed JSON", exitCode, stdout, stderr)
	}
	assertMCPProbeJSONDimension(t, stdout, "runtime_launcher", "observed_ok", "")
	assertMCPProbeJSONDimension(t, stdout, "protocol_initialize", "observed_failed", "RUNTIME_OBSERVED_FAILED")
	var payload struct {
		HasErrors bool `json:"has_errors"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("decode probe json: %v", err)
	}
	if !payload.HasErrors {
		t.Fatalf("probe json = %s, want has_errors=true", stdout)
	}
}

type fakeCLIRuntimeProbeExecutor struct {
	called  bool
	context context.Context
	request runtimeprobemcp.ProbeRequest
	facts   []runtimeprobe.Fact
	err     error
}

func (executor *fakeCLIRuntimeProbeExecutor) Probe(
	ctx context.Context,
	request runtimeprobemcp.ProbeRequest,
	_ subprocess.WorkingDirectoryBinder,
) ([]runtimeprobe.Fact, error) {
	executor.called = true
	executor.context = ctx
	executor.request = request
	return executor.facts, executor.err
}

func observedOKMCPProbeFacts() []runtimeprobe.Fact {
	return []runtimeprobe.Fact{
		{
			Dimension: runtimeprobe.DimensionLauncher,
			State:     runtimeprobe.ObservedOK,
			Source:    runtimeprobe.SourceExplicit,
			Freshness: runtimeprobe.FreshnessCurrent,
		},
		{
			Dimension: runtimeprobe.DimensionProtocolInitialize,
			State:     runtimeprobe.ObservedOK,
			Source:    runtimeprobe.SourceExplicit,
			Freshness: runtimeprobe.FreshnessCurrent,
		},
		{
			Dimension: runtimeprobe.DimensionEndpointHealth,
			State:     runtimeprobe.NotApplicable,
			Reason:    runtimeprobe.ReasonNotApplicable,
			Source:    runtimeprobe.SourceExplicit,
			Freshness: runtimeprobe.FreshnessCurrent,
		},
		{
			Dimension: runtimeprobe.DimensionAuthentication,
			State:     runtimeprobe.Unsupported,
			Reason:    runtimeprobe.ReasonUnsupported,
			Source:    runtimeprobe.SourceExplicit,
			Freshness: runtimeprobe.FreshnessCurrent,
		},
		{
			Dimension: runtimeprobe.DimensionToolInventory,
			State:     runtimeprobe.Unsupported,
			Reason:    runtimeprobe.ReasonUnsupported,
			Source:    runtimeprobe.SourceExplicit,
			Freshness: runtimeprobe.FreshnessCurrent,
		},
	}
}

func assertMCPProbeJSONDimension(t *testing.T, content string, dimension string, state string, reason string) {
	t.Helper()
	var payload struct {
		Dimensions []struct {
			Dimension string `json:"dimension"`
			State     string `json:"state"`
			Reason    string `json:"reason"`
		} `json:"dimensions"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		t.Fatalf("decode probe json: %v\n%s", err, content)
	}
	for _, got := range payload.Dimensions {
		if got.Dimension != dimension {
			continue
		}
		if got.State != state || got.Reason != reason {
			t.Fatalf("%s dimension = %#v, want state=%q reason=%q", dimension, got, state, reason)
		}
		return
	}
	t.Fatalf("dimensions = %#v, want %s", payload.Dimensions, dimension)
}

type mcpProbeJSONTestPayload struct {
	Subject struct {
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	} `json:"subject"`
	Probe struct {
		Transport   string   `json:"transport"`
		Command     string   `json:"command"`
		Args        []string `json:"args"`
		EnvBindings []struct {
			ChildName      string `json:"child_name"`
			HostSourceName string `json:"host_source_name"`
		} `json:"env_bindings"`
		WorkDir     string   `json:"workdir"`
		SideEffects []string `json:"side_effects"`
	} `json:"probe"`
	Dimensions []struct {
		Dimension string `json:"dimension"`
		State     string `json:"state"`
		Reason    string `json:"reason"`
		Detail    string `json:"detail"`
	} `json:"dimensions"`
	HasErrors bool `json:"has_errors"`
}

func decodeMCPProbeJSONTestPayload(t *testing.T, content string) mcpProbeJSONTestPayload {
	t.Helper()
	var payload mcpProbeJSONTestPayload
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		t.Fatalf("decode probe json: %v\n%s", err, content)
	}
	return payload
}

func runtimeDimensionDetail(payload mcpProbeJSONTestPayload, dimension string) string {
	for _, got := range payload.Dimensions {
		if got.Dimension == dimension {
			return got.Detail
		}
	}
	return ""
}

func stringSlicesEqualTest(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func writeOpenCodeMCPManifestWithTransport(t *testing.T, root string, transport string) {
	t.Helper()
	testkit.WriteFile(t, root, "daem.toml", `version = 1
targets = ["opencode"]

[[mcp_server]]
name = "context7"
transport = "`+transport+`"
command = "node"
args = ["server.js"]
`)
}
