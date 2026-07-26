package clipresent

import (
	"bytes"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/runtimeprobe"
	runtimeprobemcp "github.com/isty2e/daem/internal/assurance/runtimeprobe/mcp"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	probeworkflow "github.com/isty2e/daem/internal/workflow/probe"
)

func TestMCPProbeReportFromPreservesEvidenceAndRedactsEnvironment(t *testing.T) {
	t.Setenv("A_HOST_API_KEY", "secret-value")
	t.Setenv("Z_HOST_TOKEN", "token-value")

	subject, err := topology.NewSubjectID(topology.SubjectProjection, "claude-project-mcp", "context7")
	if err != nil {
		t.Fatalf("NewSubjectID returned error: %v", err)
	}
	runtime, err := runtimeprobe.FoldFacts([]runtimeprobe.Fact{
		{
			Dimension:       runtimeprobe.DimensionLauncher,
			State:           runtimeprobe.ObservedFailed,
			Reason:          runtimeprobe.ReasonObservedFailed,
			Source:          runtimeprobe.SourceExplicit,
			Freshness:       runtimeprobe.FreshnessCurrent,
			SanitizedDetail: "redacted launch failure",
		},
		{
			Dimension:       runtimeprobe.DimensionProtocolInitialize,
			State:           runtimeprobe.Blocked,
			Reason:          runtimeprobe.ReasonBlocked,
			Source:          runtimeprobe.SourceExplicit,
			Freshness:       runtimeprobe.FreshnessCurrent,
			SanitizedDetail: "missing child environment source",
		},
	})
	if err != nil {
		t.Fatalf("FoldFacts returned error: %v", err)
	}
	args := []string{"-y", "@upstash/context7-mcp"}
	sideEffects := []string{"launch selected locked MCP server"}
	result := probeworkflow.CommandResult{
		ManifestPath:     "/workspace/daem.toml",
		LockfilePath:     "/workspace/daem.lock",
		ServerName:       "context7",
		Target:           target.TargetClaudeCode,
		Scope:            target.ScopeProject,
		Mode:             probeworkflow.ModeExecute,
		Timeout:          500 * time.Millisecond,
		Subject:          subject,
		WorkingDirectory: "/workspace",
		ProbeRequest: runtimeprobemcp.ProbeRequest{
			Transport: runtimeprobemcp.TransportStdio,
			Command:   "npx",
			Args:      args,
			Env: map[string]string{
				"SERVER_TOKEN":    "Z_HOST_TOKEN",
				"SERVER_API_KEY":  "A_HOST_API_KEY",
				"SERVER_API_COPY": "A_HOST_API_KEY",
			},
		},
		Runtime:     runtime,
		SideEffects: sideEffects,
	}

	report := MCPProbeReportFrom(result)
	if report.Timeout != "500ms" || report.TimeoutSeconds != 1 {
		t.Fatalf("timeout = %q seconds=%d, want 500ms and 1", report.Timeout, report.TimeoutSeconds)
	}
	wantBindings := []MCPProbeEnvironmentBinding{
		{ChildName: "SERVER_API_COPY", HostSourceName: "A_HOST_API_KEY"},
		{ChildName: "SERVER_API_KEY", HostSourceName: "A_HOST_API_KEY"},
		{ChildName: "SERVER_TOKEN", HostSourceName: "Z_HOST_TOKEN"},
	}
	if !slices.Equal(report.Probe.EnvBindings, wantBindings) {
		t.Fatalf("env bindings = %#v, want exact sorted child-to-host mappings", report.Probe.EnvBindings)
	}
	if !slices.Equal(report.Probe.Args, args) || !slices.Equal(report.Probe.SideEffects, sideEffects) {
		t.Fatalf("probe disclosure = %#v, want exact args and side effects", report.Probe)
	}
	if len(report.Dimensions) != 5 ||
		report.Dimensions[0].Dimension != "runtime_launcher" ||
		report.Dimensions[0].Detail != "redacted launch failure" ||
		report.Dimensions[4].Dimension != "tool_inventory" {
		t.Fatalf("runtime dimensions = %#v, want exact canonical order and detail", report.Dimensions)
	}
	if !report.HasErrors {
		t.Fatal("execute report did not preserve failed launcher outcome")
	}
	var human bytes.Buffer
	PrintMCPProbeReportWithOptions(&human, report, MCPProbeHumanOptions{})
	if !strings.Contains(human.String(), `detail="redacted launch failure"`) {
		t.Fatalf("default failed probe output = %q, want sanitized cause", human.String())
	}
	if !strings.Contains(human.String(), `detail="missing child environment source"`) {
		t.Fatalf("default blocked probe output = %q, want sanitized cause", human.String())
	}
	for _, binding := range []string{
		`SERVER_API_COPY<-A_HOST_API_KEY`,
		`SERVER_API_KEY<-A_HOST_API_KEY`,
		`SERVER_TOKEN<-Z_HOST_TOKEN`,
	} {
		if !strings.Contains(human.String(), binding) {
			t.Fatalf("probe disclosure = %q, want binding %q", human.String(), binding)
		}
	}

	args[0] = "mutated"
	sideEffects[0] = "mutated"
	if report.Probe.Args[0] == "mutated" || report.Probe.SideEffects[0] == "mutated" {
		t.Fatalf("report retained mutable workflow slices: %#v", report.Probe)
	}
	rendered := reportString(report)
	for _, forbidden := range []string{"secret-value", "token-value"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("report leaked resolved environment value %q: %s", forbidden, rendered)
		}
	}
}

func TestMCPProbeInteractiveDisclosureDoesNotRecommendRerunningWithYes(t *testing.T) {
	report := MCPProbeReportFrom(probeworkflow.CommandResult{
		Mode:    probeworkflow.ModeDryRun,
		Timeout: time.Second,
	})
	var output bytes.Buffer
	PrintMCPProbeReportWithOptions(&output, report, MCPProbeHumanOptions{AwaitingConfirmation: true})
	if !strings.Contains(output.String(), "awaiting confirmation") || strings.Contains(output.String(), "rerun with --yes") {
		t.Fatalf("interactive disclosure = %q", output.String())
	}
}

func TestMCPProbeReportFromZeroEvidenceUsesClosedDefaultRows(t *testing.T) {
	report := MCPProbeReportFrom(probeworkflow.CommandResult{
		Mode:    probeworkflow.ModeDryRun,
		Timeout: 0,
	})
	if report.Timeout != "0s" || report.TimeoutSeconds != 0 {
		t.Fatalf("zero timeout = %q seconds=%d", report.Timeout, report.TimeoutSeconds)
	}
	if len(report.Dimensions) != 5 {
		t.Fatalf("zero runtime dimensions = %#v, want five", report.Dimensions)
	}
	for _, dimension := range report.Dimensions {
		if dimension.State != string(runtimeprobe.NotProbed) ||
			dimension.Reason != string(runtimeprobe.ReasonNotProbed) ||
			dimension.Source != "" ||
			dimension.Freshness != "" ||
			dimension.Detail != "" {
			t.Fatalf("zero runtime dimension = %#v, want not_probed without invented evidence", dimension)
		}
	}
	if report.HasErrors {
		t.Fatal("dry-run report claimed runtime errors without active evidence")
	}
}

func reportString(report MCPProbeReport) string {
	var builder strings.Builder
	if err := PrintMCPProbeJSON(&builder, report); err != nil {
		panic(err)
	}
	return builder.String()
}
