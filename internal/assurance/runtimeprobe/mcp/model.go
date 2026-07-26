package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/isty2e/daem/internal/assurance/runtimeprobe"
)

const (
	DefaultTimeout         = 30 * time.Second
	defaultOutputLimit     = 8192
	defaultProtocolVersion = "2025-11-25"
)

// Transport identifies the runtime communication mechanism this probe can use.
type Transport string

const (
	TransportStdio Transport = "stdio"
)

// envLookup resolves one host env reference at the execution boundary.
type envLookup func(name string) (string, bool)

// commandRunner executes a prepared MCP stdio command request.
type commandRunner func(ctx context.Context, request commandRequest) commandResult

// ProbeRequest is the effectful runtime probe request for one locked MCP server.
type ProbeRequest struct {
	Transport Transport
	Command   string
	Args      []string
	Env       map[string]string
}

// commandRequest is passed to the low-level process runner after env lookup.
type commandRequest struct {
	Command         string
	Args            []string
	Env             []string
	OutputLimit     int
	ProtocolVersion string
	nativeWorkDir   *os.File
}

// commandResult is the process/protocol result before redaction and fact mapping.
type commandResult struct {
	Started                bool
	InitializeSucceeded    bool
	Stdout                 string
	Stderr                 string
	StdoutTruncated        bool
	StderrTruncated        bool
	TimedOut               bool
	Canceled               bool
	MissingRunner          bool
	WorkDirAuthorityFailed bool
	Err                    error
}

func (result commandResult) withContextOutcome(err error) commandResult {
	if err == nil || result.InitializeSucceeded {
		return result
	}
	if errors.Is(err, context.DeadlineExceeded) {
		result.TimedOut = true
		if result.Err == nil {
			result.Err = err
		}
		return result
	}
	if errors.Is(err, context.Canceled) && !result.TimedOut {
		result.Canceled = true
		if result.Err == nil {
			result.Err = err
		}
	}
	return result
}

// executorOptions configures explicit MCP probe execution.
type executorOptions struct {
	Timeout         time.Duration
	OutputLimit     int
	LookupEnv       envLookup
	Runner          commandRunner
	ProtocolVersion string
}

// Executor launches and initializes MCP servers under explicit user authority.
type Executor struct {
	timeout         time.Duration
	outputLimit     int
	lookupEnv       envLookup
	runner          commandRunner
	protocolVersion string
}

type sanitizedCapture struct {
	stdout          string
	stderr          string
	stdoutTruncated bool
	stderrTruncated bool
	redacted        bool
	errorDetail     string
}

func validateProbeRequest(request ProbeRequest) error {
	if strings.TrimSpace(string(request.Transport)) == "" {
		return fmt.Errorf("MCP probe transport is required")
	}
	if request.Transport != TransportStdio {
		return nil
	}
	if strings.TrimSpace(request.Command) == "" {
		return fmt.Errorf("MCP probe command is required")
	}
	for index, arg := range request.Args {
		if strings.Contains(arg, "\x00") {
			return fmt.Errorf("MCP probe arg[%d] must not contain NUL", index)
		}
	}
	for serverEnv, hostEnv := range request.Env {
		if strings.TrimSpace(serverEnv) == "" || strings.TrimSpace(serverEnv) != serverEnv {
			return fmt.Errorf("MCP probe server env name %q is invalid", serverEnv)
		}
		if strings.TrimSpace(hostEnv) == "" || strings.TrimSpace(hostEnv) != hostEnv {
			return fmt.Errorf("MCP probe host env reference for %q is invalid", serverEnv)
		}
	}
	return nil
}

func runtimeFact(
	dimension runtimeprobe.Dimension,
	state runtimeprobe.Readiness,
	reason runtimeprobe.ReasonCode,
	detail string,
) runtimeprobe.Fact {
	return runtimeprobe.Fact{
		Dimension:       dimension,
		State:           state,
		Reason:          reason,
		Source:          runtimeprobe.SourceExplicit,
		Freshness:       runtimeprobe.FreshnessCurrent,
		SanitizedDetail: detail,
	}
}
