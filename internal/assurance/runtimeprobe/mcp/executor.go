package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/isty2e/daem/internal/assurance/runtimeprobe"
	"github.com/isty2e/daem/internal/subprocess"
)

// NewExecutor constructs an explicit MCP runtime probe executor using the
// supplied timeout or the package default when timeout is not positive.
func NewExecutor(timeout time.Duration) Executor {
	return newExecutor(executorOptions{Timeout: timeout})
}

func newExecutor(options executorOptions) Executor {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	outputLimit := options.OutputLimit
	if outputLimit <= 0 {
		outputLimit = defaultOutputLimit
	}
	lookupEnv := options.LookupEnv
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	runner := options.Runner
	if runner == nil {
		runner = defaultCommandRunner
	}
	protocolVersion := strings.TrimSpace(options.ProtocolVersion)
	if protocolVersion == "" {
		protocolVersion = defaultProtocolVersion
	}
	return Executor{
		timeout:         timeout,
		outputLimit:     outputLimit,
		lookupEnv:       lookupEnv,
		runner:          runner,
		protocolVersion: protocolVersion,
	}
}

// Probe launches the server from a freshly acquired descriptor-backed working
// directory and attempts MCP initialize for admitted stdio transports.
func (executor Executor) Probe(
	ctx context.Context,
	request ProbeRequest,
	bind subprocess.WorkingDirectoryBinder,
) ([]runtimeprobe.Fact, error) {
	executor = executor.withDefaults()
	if err := validateProbeRequest(request); err != nil {
		return nil, err
	}
	if request.Transport != TransportStdio {
		detail := fmt.Sprintf("transport %q is not admitted for launch/initialize probe", request.Transport)
		return []runtimeprobe.Fact{
			runtimeFact(runtimeprobe.DimensionLauncher, runtimeprobe.Unsupported, runtimeprobe.ReasonUnsupported, detail),
			runtimeFact(runtimeprobe.DimensionProtocolInitialize, runtimeprobe.Unsupported, runtimeprobe.ReasonUnsupported, detail),
			runtimeFact(runtimeprobe.DimensionEndpointHealth, runtimeprobe.Unsupported, runtimeprobe.ReasonUnsupported, detail),
			runtimeFact(runtimeprobe.DimensionAuthentication, runtimeprobe.Unsupported, runtimeprobe.ReasonUnsupported, detail),
			runtimeFact(runtimeprobe.DimensionToolInventory, runtimeprobe.Unsupported, runtimeprobe.ReasonUnsupported, detail),
		}, nil
	}

	env, secrets, missing := executor.resolveEnv(request.Env)
	if len(missing) != 0 {
		detail := "missing env refs: " + strings.Join(missing, ", ")
		facts := []runtimeprobe.Fact{
			runtimeFact(runtimeprobe.DimensionLauncher, runtimeprobe.Blocked, runtimeprobe.ReasonBlocked, detail),
		}
		facts = append(facts, stdioSupportFacts()...)
		return facts, nil
	}
	if bind == nil {
		return nil, fmt.Errorf("MCP probe working-directory binder is required")
	}
	binding, err := bind()
	if err != nil {
		return executor.workDirBlockedFacts(
			fmt.Errorf("acquire MCP probe working-directory authority: %w", err),
			secrets,
		), nil
	}
	if binding == nil {
		return nil, fmt.Errorf("acquire MCP probe working-directory authority: binding is required")
	}
	defer binding.Close()
	if err := binding.Validate(); err != nil {
		return executor.workDirBlockedFacts(
			fmt.Errorf("validate MCP probe working-directory authority before launch: %w", err),
			secrets,
		), nil
	}
	directory, err := binding.OpenDirectory()
	if err != nil {
		return executor.workDirBlockedFacts(
			fmt.Errorf("open MCP probe working-directory authority: %w", err),
			secrets,
		), nil
	}
	if err := subprocess.ValidateWorkingDirectory(directory); err != nil {
		if directory != nil {
			_ = directory.Close()
		}
		return executor.workDirBlockedFacts(
			fmt.Errorf("validate MCP probe working-directory descriptor: %w", err),
			secrets,
		), nil
	}
	defer directory.Close()

	runCtx, cancel := context.WithTimeout(ctx, executor.timeout)
	defer cancel()
	result := executor.runner(runCtx, commandRequest{
		Command:         request.Command,
		Args:            append([]string(nil), request.Args...),
		Env:             env,
		OutputLimit:     executor.outputLimit,
		ProtocolVersion: executor.protocolVersion,
		nativeWorkDir:   directory,
	})
	if !result.Started {
		result = result.withContextOutcome(runCtx.Err())
	}
	if err := binding.Validate(); err != nil {
		result.WorkDirAuthorityFailed = true
		result.Err = errors.Join(result.Err, fmt.Errorf("working-directory authority changed: %w", err))
	}

	capture := sanitizeCapture(result, secrets, executor.outputLimit)
	facts := factsFromCommandResult(result, capture)
	facts = append(facts, stdioSupportFacts()...)
	return facts, nil
}

func (executor Executor) workDirBlockedFacts(err error, secrets []string) []runtimeprobe.Fact {
	result := commandResult{
		WorkDirAuthorityFailed: true,
		Err:                    err,
	}
	capture := sanitizeCapture(result, secrets, executor.outputLimit)
	facts := factsFromCommandResult(result, capture)
	return append(facts, stdioSupportFacts()...)
}

func (executor Executor) withDefaults() Executor {
	return newExecutor(executorOptions{
		Timeout:         executor.timeout,
		OutputLimit:     executor.outputLimit,
		LookupEnv:       executor.lookupEnv,
		Runner:          executor.runner,
		ProtocolVersion: executor.protocolVersion,
	})
}

func (executor Executor) resolveEnv(envRefs map[string]string) ([]string, []string, []string) {
	env := subprocess.InheritedChildEnvironment()
	missing := make([]string, 0)
	serverNames := make([]string, 0, len(envRefs))
	for serverName := range envRefs {
		serverNames = append(serverNames, serverName)
	}
	sort.Strings(serverNames)

	for _, serverName := range serverNames {
		hostName := envRefs[serverName]
		value, ok := executor.lookupEnv(hostName)
		if !ok {
			missing = append(missing, hostName)
			continue
		}
		env = env.WithSecret(serverName, value)
	}
	sort.Strings(missing)
	return env.Entries(), env.SecretValues(), missing
}

func factsFromCommandResult(result commandResult, capture sanitizedCapture) []runtimeprobe.Fact {
	if !result.Started {
		state := runtimeprobe.ObservedFailed
		reason := runtimeprobe.ReasonObservedFailed
		if result.WorkDirAuthorityFailed {
			state = runtimeprobe.Blocked
			reason = runtimeprobe.ReasonBlocked
		}
		return []runtimeprobe.Fact{
			runtimeFact(
				runtimeprobe.DimensionLauncher,
				state,
				reason,
				commandFailureDetail(result, capture),
			),
		}
	}

	if result.WorkDirAuthorityFailed {
		return []runtimeprobe.Fact{
			runtimeFact(
				runtimeprobe.DimensionLauncher,
				runtimeprobe.ObservedFailed,
				runtimeprobe.ReasonObservedFailed,
				commandFailureDetail(result, capture),
			),
		}
	}

	facts := []runtimeprobe.Fact{
		runtimeFact(runtimeprobe.DimensionLauncher, runtimeprobe.ObservedOK, runtimeprobe.ReasonNone, ""),
	}
	if result.InitializeSucceeded {
		facts = append(facts, runtimeFact(runtimeprobe.DimensionProtocolInitialize, runtimeprobe.ObservedOK, runtimeprobe.ReasonNone, ""))
		return facts
	}
	facts = append(facts, runtimeFact(
		runtimeprobe.DimensionProtocolInitialize,
		runtimeprobe.ObservedFailed,
		runtimeprobe.ReasonObservedFailed,
		commandFailureDetail(result, capture),
	))
	return facts
}

func stdioSupportFacts() []runtimeprobe.Fact {
	return []runtimeprobe.Fact{
		runtimeFact(
			runtimeprobe.DimensionEndpointHealth,
			runtimeprobe.NotApplicable,
			runtimeprobe.ReasonNotApplicable,
			"stdio transport has no remote MCP endpoint to probe",
		),
		runtimeFact(
			runtimeprobe.DimensionAuthentication,
			runtimeprobe.Unsupported,
			runtimeprobe.ReasonUnsupported,
			"stdio authentication readiness has no generic admitted check; environment presence is not auth readiness",
		),
		runtimeFact(
			runtimeprobe.DimensionToolInventory,
			runtimeprobe.Unsupported,
			runtimeprobe.ReasonUnsupported,
			"tool inventory probing is not admitted without a redaction and permission contract",
		),
	}
}

func commandFailureDetail(result commandResult, capture sanitizedCapture) string {
	parts := make([]string, 0, 4)
	switch {
	case result.WorkDirAuthorityFailed:
		parts = append(parts, "working-directory authority failed")
	case result.MissingRunner:
		parts = append(parts, "missing runner")
	case result.TimedOut:
		parts = append(parts, "probe timed out")
	case result.Canceled:
		parts = append(parts, "probe canceled")
	}
	if capture.errorDetail != "" {
		parts = append(parts, "error: "+capture.errorDetail)
	}
	if capture.stderr != "" {
		parts = append(parts, "stderr: "+capture.stderr)
	}
	if capture.stdout != "" {
		parts = append(parts, "stdout: "+capture.stdout)
	}
	if capture.stderrTruncated {
		parts = append(parts, "stderr_truncated=true")
	}
	if capture.stdoutTruncated {
		parts = append(parts, "stdout_truncated=true")
	}
	if capture.redacted {
		parts = append(parts, "redacted=true")
	}
	if len(parts) == 0 {
		return "probe failed"
	}
	return strings.Join(parts, "; ")
}
