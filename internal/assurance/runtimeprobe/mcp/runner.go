package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/isty2e/daem/internal/subprocess"
)

const (
	maxInitializeMessages = 32
	maxScannerTokenBytes  = 1024 * 1024
	stdinCloseGrace       = 500 * time.Millisecond
)

type jsonRPCMessage struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *jsonRPCError    `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type initializeResult struct {
	ProtocolVersion string `json:"protocolVersion"`
}

func defaultCommandRunner(ctx context.Context, request commandRequest) (result commandResult) {
	if request.nativeWorkDir == nil {
		return commandResult{
			WorkDirAuthorityFailed: true,
			Err:                    fmt.Errorf("descriptor-backed working directory is required"),
		}
	}
	path, err := exec.LookPath(request.Command)
	if err != nil {
		return commandResult{MissingRunner: true, Err: err}
	}
	cmd, err := subprocess.PrepareCommandInWorkingDirectory(ctx, path, request.Args, request.Env, request.nativeWorkDir)
	if err != nil {
		return commandResult{WorkDirAuthorityFailed: true, Err: err}
	}
	// Bind before allocating protocol pipes so a supervision failure cannot
	// retain unstarted stdin/stdout descriptors.
	group, err := subprocess.BindProcessGroup(cmd)
	if err != nil {
		return commandResult{Err: err}
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return commandResult{Err: err}
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		releaseUnstartedStdio(cmd, stdin)
		return commandResult{Err: err}
	}
	outputLimit := request.OutputLimit
	if outputLimit <= 0 {
		outputLimit = defaultOutputLimit
	}
	stderrBuffer := subprocess.NewBoundedBuffer(outputLimit)
	// Diagnostic stderr is exec-owned so Wait/WaitDelay observe copy EOF versus
	// forced pipe close. Parent-owned StderrPipe copies cannot tell those apart.
	cmd.Stderr = stderrBuffer
	cmd.WaitDelay = subprocess.InheritedOutputCloseWait

	if err := cmd.Start(); err != nil {
		return commandResult{Err: err}
	}
	result.Started = true

	closeProtocolStdio := sync.OnceFunc(func() {
		_ = stdin.Close()
		_ = stdout.Close()
	})
	interruptDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			closeProtocolStdio()
		case <-interruptDone:
		}
	}()
	defer func() {
		_ = stdin.Close()
		var waitErr error
		processExited := false
		if ctx.Err() == nil {
			timer := time.NewTimer(stdinCloseGrace)
			select {
			case <-group.WaitDone():
				processExited = true
			case <-timer.C:
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		var termination subprocess.ProcessTermination
		var terminationErr error
		if processExited {
			termination, terminationErr = group.ReapAfterLeaderExit()
		} else {
			termination, terminationErr = group.Terminate()
		}
		waitErr = group.Await(ctx, subprocess.InheritedOutputCloseWait)
		closeProtocolStdio()
		close(interruptDone)
		result.Stderr = stderrBuffer.String()
		if stderrCaptureIncomplete(stderrBuffer, waitErr, termination, terminationErr) {
			result.StderrTruncated = true
		}
		result = finalizeCommandResult(result, waitErr, terminationErr, ctx.Err())
	}()

	// Protocol pipes: Wait starts only after initialize scanning returns.
	// Cancellation unblocks the scanner by closing the parent stdin/stdout ends.
	if err := writeInitializeRequest(stdin, request.ProtocolVersion); err != nil {
		result.Err = err
		return result
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), maxScannerTokenBytes)
	for range maxInitializeMessages {
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				result.Err = fmt.Errorf("read initialize response: %w", err)
				return result
			}
			result.Err = fmt.Errorf("server stdout closed before initialize response")
			return result
		}
		message, ok, err := decodeInitializeResponse(scanner.Text())
		if err != nil {
			result.Err = err
			return result
		}
		if !ok {
			continue
		}
		if message.Error != nil {
			result.Err = fmt.Errorf("initialize error %d: %s", message.Error.Code, message.Error.Message)
			return result
		}
		if len(message.Result) == 0 {
			result.Err = fmt.Errorf("initialize response missing result")
			return result
		}
		var initialized initializeResult
		if err := json.Unmarshal(message.Result, &initialized); err != nil {
			result.Err = fmt.Errorf("decode initialize result: %w", err)
			return result
		}
		if strings.TrimSpace(initialized.ProtocolVersion) == "" {
			result.Err = fmt.Errorf("initialize response missing protocolVersion")
			return result
		}
		if err := writeInitializedNotificationUnlessCanceled(ctx, stdin); err != nil {
			result.Err = err
			return result
		}
		result.InitializeSucceeded = true
		return result
	}

	result.Err = fmt.Errorf("initialize response not received after %d messages", maxInitializeMessages)
	return result
}

func stderrCaptureIncomplete(
	buffer *subprocess.BoundedBuffer,
	waitErr error,
	termination subprocess.ProcessTermination,
	terminationErr error,
) bool {
	if buffer != nil && buffer.Truncated() {
		return true
	}
	return errors.Is(waitErr, exec.ErrWaitDelay) ||
		errors.Is(waitErr, subprocess.ErrProcessWaitAbandoned) ||
		termination.UnsignalableOccupancy() ||
		terminationErr != nil
}

func releaseUnstartedStdio(command *exec.Cmd, parents ...io.Closer) {
	for _, parent := range parents {
		if parent != nil {
			_ = parent.Close()
		}
	}
	if command == nil {
		return
	}
	for _, endpoint := range []any{command.Stdin, command.Stdout, command.Stderr} {
		if closer, ok := endpoint.(io.Closer); ok {
			_ = closer.Close()
		}
	}
}

func finalizeCommandResult(
	result commandResult,
	waitErr error,
	terminationErr error,
	contextErr error,
) commandResult {
	if terminationErr != nil {
		result.InitializeSucceeded = false
		result.Err = errors.Join(result.Err, fmt.Errorf("terminate MCP process group: %w", terminationErr))
	}
	if errors.Is(waitErr, subprocess.ErrProcessWaitAbandoned) {
		result.InitializeSucceeded = false
		result.Err = errors.Join(result.Err, waitErr)
	}
	if result.Err == nil && !result.InitializeSucceeded && waitErr != nil {
		result.Err = waitErr
	}
	return result.withContextOutcome(contextErr)
}

func writeInitializeRequest(writer io.Writer, protocolVersion string) error {
	if strings.TrimSpace(protocolVersion) == "" {
		protocolVersion = defaultProtocolVersion
	}
	request := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{},
			"clientInfo": map[string]string{
				"name":    "daem",
				"version": "0",
			},
		},
	}
	return json.NewEncoder(writer).Encode(request)
}

func writeInitializedNotification(writer io.Writer) error {
	notification := map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	}
	return json.NewEncoder(writer).Encode(notification)
}

func writeInitializedNotificationUnlessCanceled(ctx context.Context, writer io.Writer) error {
	select {
	case <-ctx.Done():
		if err := ctx.Err(); err != nil {
			return err
		}
		return context.Canceled
	default:
		return writeInitializedNotification(writer)
	}
}

func decodeInitializeResponse(line string) (jsonRPCMessage, bool, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return jsonRPCMessage{}, false, nil
	}
	var message jsonRPCMessage
	if err := json.Unmarshal([]byte(trimmed), &message); err != nil {
		return jsonRPCMessage{}, false, fmt.Errorf("decode MCP stdout message: %w", err)
	}
	if message.JSONRPC != "2.0" {
		return jsonRPCMessage{}, false, fmt.Errorf("MCP stdout message has jsonrpc %q", message.JSONRPC)
	}
	if message.ID == nil || !jsonRPCIDIsOne(*message.ID) {
		return message, false, nil
	}
	return message, true, nil
}

func jsonRPCIDIsOne(value json.RawMessage) bool {
	var number int
	if err := json.Unmarshal(value, &number); err == nil {
		return number == 1
	}
	var text string
	if err := json.Unmarshal(value, &text); err == nil {
		parsed, err := strconv.Atoi(text)
		return err == nil && parsed == 1
	}
	return false
}
