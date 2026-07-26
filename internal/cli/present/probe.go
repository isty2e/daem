package clipresent

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/isty2e/daem/internal/assurance/runtimeprobe"
	probeworkflow "github.com/isty2e/daem/internal/workflow/probe"
)

const mcpProbeJSONSchemaVersion = 1

// MCPProbeReport is the public presentation DTO for explicit runtime probes.
type MCPProbeReport struct {
	SchemaVersion  int                 `json:"schema_version"`
	Command        string              `json:"command"`
	Mode           string              `json:"mode"`
	ManifestPath   string              `json:"manifest_path"`
	LockfilePath   string              `json:"lockfile_path"`
	ServerName     string              `json:"server_name"`
	Target         string              `json:"target"`
	Scope          string              `json:"scope"`
	Subject        MCPSubject          `json:"subject"`
	Timeout        string              `json:"timeout"`
	TimeoutSeconds int                 `json:"timeout_seconds"`
	Probe          MCPProbeDisclosure  `json:"probe"`
	Dimensions     []MCPProbeDimension `json:"dimensions"`
	HasErrors      bool                `json:"has_errors"`
}

// MCPProbeDisclosure reports exact side-effect disclosure without secret values.
type MCPProbeDisclosure struct {
	Transport   string                       `json:"transport"`
	Command     string                       `json:"command"`
	Args        []string                     `json:"args"`
	EnvBindings []MCPProbeEnvironmentBinding `json:"env_bindings"`
	WorkDir     string                       `json:"workdir"`
	SideEffects []string                     `json:"side_effects"`
}

// MCPProbeEnvironmentBinding identifies one child environment name and its host source without a value.
type MCPProbeEnvironmentBinding struct {
	ChildName      string `json:"child_name"`
	HostSourceName string `json:"host_source_name"`
}

// MCPProbeDimension is one runtime-readiness evidence axis.
type MCPProbeDimension struct {
	Dimension string `json:"dimension"`
	State     string `json:"state"`
	Reason    string `json:"reason,omitempty"`
	Source    string `json:"source,omitempty"`
	Freshness string `json:"freshness,omitempty"`
	Detail    string `json:"detail,omitempty"`
	isFailure bool
}

// MCPProbeReportFrom projects one canonical probe result into the public report contract.
func MCPProbeReportFrom(result probeworkflow.CommandResult) MCPProbeReport {
	return MCPProbeReport{
		SchemaVersion:  mcpProbeJSONSchemaVersion,
		Command:        "probe",
		Mode:           string(result.Mode),
		ManifestPath:   result.ManifestPath,
		LockfilePath:   result.LockfilePath,
		ServerName:     result.ServerName,
		Target:         string(result.Target),
		Scope:          string(result.Scope),
		Subject:        MCPSubject{Kind: string(result.Subject.Kind()), Namespace: result.Subject.Namespace(), Name: result.ServerName},
		Timeout:        result.Timeout.String(),
		TimeoutSeconds: probeTimeoutSeconds(result.Timeout),
		Probe: MCPProbeDisclosure{
			Transport:   string(result.ProbeRequest.Transport),
			Command:     result.ProbeRequest.Command,
			Args:        append([]string(nil), result.ProbeRequest.Args...),
			EnvBindings: probeEnvironmentBindings(result.ProbeRequest.Env),
			WorkDir:     result.WorkingDirectory,
			SideEffects: append([]string(nil), result.SideEffects...),
		},
		Dimensions: runtimeProbeDimensions(result.Runtime),
		HasErrors:  result.HasRuntimeErrors(),
	}
}

func probeTimeoutSeconds(timeout time.Duration) int {
	if timeout <= 0 {
		return 0
	}
	seconds := int(timeout / time.Second)
	if timeout%time.Second != 0 {
		seconds++
	}
	return seconds
}

func probeEnvironmentBindings(values map[string]string) []MCPProbeEnvironmentBinding {
	childNames := make([]string, 0, len(values))
	for childName := range values {
		childNames = append(childNames, childName)
	}
	sort.Strings(childNames)

	result := make([]MCPProbeEnvironmentBinding, 0, len(values))
	for _, childName := range childNames {
		result = append(result, MCPProbeEnvironmentBinding{
			ChildName:      childName,
			HostSourceName: values[childName],
		})
	}
	return result
}

func probeEnvironmentBindingStrings(bindings []MCPProbeEnvironmentBinding) []string {
	result := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		result = append(result, binding.ChildName+"<-"+binding.HostSourceName)
	}
	return result
}

func runtimeProbeDimensions(observation runtimeprobe.Observation) []MCPProbeDimension {
	return []MCPProbeDimension{
		runtimeProbeDimension(runtimeprobe.DimensionLauncher, observation.Launcher()),
		runtimeProbeDimension(runtimeprobe.DimensionProtocolInitialize, observation.ProtocolInitialize()),
		runtimeProbeDimension(runtimeprobe.DimensionAuthentication, observation.Authentication()),
		runtimeProbeDimension(runtimeprobe.DimensionEndpointHealth, observation.EndpointHealth()),
		runtimeProbeDimension(runtimeprobe.DimensionToolInventory, observation.ToolInventory()),
	}
}

func runtimeProbeDimension(
	dimension runtimeprobe.Dimension,
	observation runtimeprobe.ReadinessObservation,
) MCPProbeDimension {
	return MCPProbeDimension{
		Dimension: string(dimension),
		State:     string(observation.State()),
		Reason:    string(observation.Reason()),
		Source:    string(observation.Source()),
		Freshness: string(observation.Freshness()),
		Detail:    observation.SanitizedDetail(),
		isFailure: observation.IsFailure(),
	}
}

// PrintMCPProbeReportWithOptions writes a human-readable explicit probe report.
func PrintMCPProbeReportWithOptions(output io.Writer, report MCPProbeReport, options MCPProbeHumanOptions) {
	fmt.Fprintf(output, "mcp runtime probe: %s\n", report.Mode)
	fmt.Fprintf(output, "  server: %s target=%s scope=%s\n", report.ServerName, report.Target, report.Scope)
	fmt.Fprintf(output, "  subject: %q\n", mcpProbeSubjectString(report.Subject))
	if options.Verbose {
		fmt.Fprintf(output, "  manifest: %s\n", Escape(report.ManifestPath))
		fmt.Fprintf(output, "  lockfile: %s\n", Escape(report.LockfilePath))
	}
	fmt.Fprintf(output, "  timeout: %s\n", report.Timeout)
	fmt.Fprintln(output, "side effects:")
	for _, effect := range report.Probe.SideEffects {
		fmt.Fprintf(output, "  - %s\n", effect)
	}
	fmt.Fprintf(
		output,
		"probe request: transport=%s command=%q args=%s env_bindings=%s workdir=%q\n",
		report.Probe.Transport,
		report.Probe.Command,
		quotedList(report.Probe.Args),
		quotedList(probeEnvironmentBindingStrings(report.Probe.EnvBindings)),
		report.Probe.WorkDir,
	)
	if report.Mode == "dry-run" {
		if options.AwaitingConfirmation {
			fmt.Fprintln(output, "probe execution: not run; awaiting confirmation to launch the disclosed MCP server request")
		} else {
			fmt.Fprintln(output, "probe execution: not run; rerun with --yes to launch the selected MCP server")
		}
		fmt.Fprintln(output, "probe success would be current best-effort evidence, not future skip authority")
	}
	okCount := 0
	visibleDimensions := make([]MCPProbeDimension, 0, len(report.Dimensions))
	for _, dimension := range report.Dimensions {
		if !options.Verbose && dimension.State == "observed_ok" {
			okCount++
			continue
		}
		if !options.Verbose && report.Mode == "dry-run" && dimension.State == "not_probed" {
			continue
		}
		visibleDimensions = append(visibleDimensions, dimension)
	}
	if len(visibleDimensions) == 0 && okCount == 0 {
		return
	}
	fmt.Fprintln(output, "runtime readiness:")
	for _, dimension := range visibleDimensions {
		fmt.Fprintf(output, "  - dimension=%s state=%s", dimension.Dimension, dimension.State)
		if dimension.Reason != "" {
			fmt.Fprintf(output, " reason=%s", dimension.Reason)
		}
		if options.Verbose && dimension.Source != "" {
			fmt.Fprintf(output, " source=%s", dimension.Source)
		}
		if options.Verbose && dimension.Freshness != "" {
			fmt.Fprintf(output, " freshness=%s", dimension.Freshness)
		}
		if dimension.Detail != "" && (options.Verbose || dimension.isFailure) {
			fmt.Fprintf(output, " detail=%q", dimension.Detail)
		}
		fmt.Fprintln(output)
	}
	if okCount > 0 {
		fmt.Fprintf(output, "  observed ok: %d\n", okCount)
	}
}

// PrintMCPProbeJSON writes the structured explicit probe report.
func PrintMCPProbeJSON(output io.Writer, report MCPProbeReport) error {
	report.SchemaVersion = mcpProbeJSONSchemaVersion
	report.Command = "probe"
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func mcpProbeSubjectString(subject MCPSubject) string {
	parts := []string{subject.Kind, subject.Namespace, subject.Name}
	return strings.Join(parts, "/")
}
