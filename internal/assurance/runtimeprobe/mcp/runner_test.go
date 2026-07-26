package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDefaultCommandRunnerInitializesStdioServerAndCleansUp(t *testing.T) {
	markerPath := t.TempDir() + "/initialized"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result := defaultCommandRunner(ctx, commandRequestWithNativeWorkDir(t, commandRequest{
		Command: os.Args[0],
		Args: []string{
			"-test.run=^TestMCPProbeHelperProcess$",
		},
		Env: append(
			os.Environ(),
			"DAEM_MCPPROBE_HELPER=success",
			"DAEM_MCPPROBE_MARKER="+markerPath,
		),
		OutputLimit:     defaultOutputLimit,
		ProtocolVersion: defaultProtocolVersion,
	}))

	if !result.Started || !result.InitializeSucceeded || result.Err != nil {
		t.Fatalf("result = %#v, want started successful initialize", result)
	}
	content, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read helper marker: %v", err)
	}
	if !strings.Contains(string(content), "initialized") {
		t.Fatalf("marker = %q, want initialized", string(content))
	}
}

func TestDefaultCommandRunnerAcceptsNotificationBeforeInitializeResponse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result := defaultCommandRunner(ctx, commandRequestWithNativeWorkDir(t, commandRequest{
		Command: os.Args[0],
		Args: []string{
			"-test.run=^TestMCPProbeHelperProcess$",
		},
		Env: append(
			os.Environ(),
			"DAEM_MCPPROBE_HELPER=notification-first",
		),
		OutputLimit:     defaultOutputLimit,
		ProtocolVersion: defaultProtocolVersion,
	}))

	if !result.Started || !result.InitializeSucceeded || result.Err != nil {
		t.Fatalf("result = %#v, want success after pre-response notification", result)
	}
}

func TestDefaultCommandRunnerMapsInitializeErrorToFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result := defaultCommandRunner(ctx, commandRequestWithNativeWorkDir(t, commandRequest{
		Command: os.Args[0],
		Args: []string{
			"-test.run=^TestMCPProbeHelperProcess$",
		},
		Env: append(
			os.Environ(),
			"DAEM_MCPPROBE_HELPER=initialize-error",
		),
		OutputLimit:     defaultOutputLimit,
		ProtocolVersion: defaultProtocolVersion,
	}))

	if !result.Started || result.InitializeSucceeded || result.Err == nil ||
		!strings.Contains(result.Err.Error(), "initialize error") {
		t.Fatalf("result = %#v, want initialize error failure", result)
	}
}

func TestDefaultCommandRunnerRejectsInitializeResultWithoutProtocolVersion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result := defaultCommandRunner(ctx, commandRequestWithNativeWorkDir(t, commandRequest{
		Command: os.Args[0],
		Args: []string{
			"-test.run=^TestMCPProbeHelperProcess$",
		},
		Env: append(
			os.Environ(),
			"DAEM_MCPPROBE_HELPER=missing-protocol-version",
		),
		OutputLimit:     defaultOutputLimit,
		ProtocolVersion: defaultProtocolVersion,
	}))

	if !result.Started || result.InitializeSucceeded || result.Err == nil ||
		!strings.Contains(result.Err.Error(), "protocolVersion") {
		t.Fatalf("result = %#v, want missing protocolVersion failure", result)
	}
}

func TestDefaultCommandRunnerTimesOutAndCleansUp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()

	result := defaultCommandRunner(ctx, commandRequestWithNativeWorkDir(t, commandRequest{
		Command: os.Args[0],
		Args: []string{
			"-test.run=^TestMCPProbeHelperProcess$",
		},
		Env: append(
			os.Environ(),
			"DAEM_MCPPROBE_HELPER=hang",
		),
		OutputLimit:     defaultOutputLimit,
		ProtocolVersion: defaultProtocolVersion,
	}))
	elapsed := time.Since(started)

	if !result.Started || !result.TimedOut || result.InitializeSucceeded {
		t.Fatalf("result = %#v, want started timeout before initialize", result)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("cleanup took %s, want bounded timeout cleanup", elapsed)
	}
}

func TestDefaultCommandRunnerFailsClosedWithoutNativeWorkingDirectory(t *testing.T) {
	result := defaultCommandRunner(context.Background(), commandRequest{
		Command: os.Args[0],
	})

	if result.Started || !result.WorkDirAuthorityFailed || result.Err == nil ||
		!strings.Contains(result.Err.Error(), "descriptor-backed working directory is required") {
		t.Fatalf("result = %#v, want prelaunch working-directory authority failure", result)
	}
}

func TestFinalizeCommandResultRevokesInitializeSuccessOnCleanupFailure(t *testing.T) {
	cleanupErr := errors.New("descendant survived")
	result := finalizeCommandResult(
		commandResult{Started: true, InitializeSucceeded: true},
		errors.New("leader exit"),
		cleanupErr,
		nil,
	)

	if result.InitializeSucceeded || result.Err == nil ||
		!strings.Contains(result.Err.Error(), "terminate MCP process tree") ||
		!errors.Is(result.Err, cleanupErr) {
		t.Fatalf("result = %#v, want cleanup failure to revoke initialize success", result)
	}
}

func TestInitializedNotificationRefusesAlreadyCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bytes.Buffer

	err := writeInitializedNotificationUnlessCanceled(ctx, &output)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
	if output.Len() != 0 {
		t.Fatalf("output = %q, want no initialized notification", output.String())
	}
}

func TestInitializedNotificationMayLinearizeBeforeConcurrentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	writer := &cancelingBuffer{cancel: cancel}

	err := writeInitializedNotificationUnlessCanceled(ctx, writer)
	if err != nil {
		t.Fatalf("write initialized notification: %v", err)
	}
	if ctx.Err() == nil {
		t.Fatal("context remained active, want cancellation during transmission")
	}
	if !strings.Contains(writer.String(), "notifications/initialized") {
		t.Fatalf("output = %q, want completed initialized notification", writer.String())
	}
}

type cancelingBuffer struct {
	bytes.Buffer
	cancel context.CancelFunc
}

func (writer *cancelingBuffer) Write(content []byte) (int, error) {
	writer.cancel()
	return writer.Buffer.Write(content)
}

func commandRequestWithNativeWorkDir(t *testing.T, request commandRequest) commandRequest {
	t.Helper()
	root := t.TempDir()
	directory, err := os.Open(root)
	if err != nil {
		t.Fatalf("open native working directory: %v", err)
	}
	t.Cleanup(func() {
		_ = directory.Close()
	})
	request.nativeWorkDir = directory
	return request
}

func TestMCPProbeHelperProcess(t *testing.T) {
	mode := os.Getenv("DAEM_MCPPROBE_HELPER")
	if mode == "" {
		return
	}
	switch mode {
	case "success":
		runSuccessfulMCPProbeHelper(false)
	case "success-hang":
		runSuccessfulMCPProbeHelper(true)
	case "notification-first":
		runNotificationFirstMCPProbeHelper()
	case "initialize-error":
		runInitializeErrorMCPProbeHelper()
	case "missing-protocol-version":
		runMissingProtocolVersionMCPProbeHelper()
	case "hang":
		time.Sleep(10 * time.Second)
		os.Exit(0)
	default:
		os.Exit(3)
	}
}

func runSuccessfulMCPProbeHelper(keepRunning bool) {
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		os.Exit(4)
	}
	var request struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Method  string `json:"method"`
		Params  struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"params"`
	}
	if err := json.Unmarshal([]byte(line), &request); err != nil ||
		request.JSONRPC != "2.0" ||
		request.ID != 1 ||
		request.Method != "initialize" ||
		request.Params.ProtocolVersion == "" {
		os.Exit(5)
	}
	fmt.Printf(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":%q,"capabilities":{},"serverInfo":{"name":"test","version":"1"}}}`+"\n", request.Params.ProtocolVersion)
	notification, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(notification, "notifications/initialized") {
		os.Exit(6)
	}
	if marker := os.Getenv("DAEM_MCPPROBE_MARKER"); marker != "" {
		if err := os.WriteFile(marker, []byte("initialized\n"), 0o600); err != nil {
			os.Exit(7)
		}
	}
	if keepRunning {
		for {
			time.Sleep(time.Hour)
		}
	}
	os.Exit(0)
}

func runNotificationFirstMCPProbeHelper() {
	reader := bufio.NewReader(os.Stdin)
	readInitializeRequestFromReaderOrExit(reader)
	fmt.Println(`{"jsonrpc":"2.0","method":"notifications/message","params":{"level":"info","logger":"test","data":"booting"}}`)
	fmt.Println(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-11-25","capabilities":{},"serverInfo":{"name":"test","version":"1"}}}`)
	notification, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(notification, "notifications/initialized") {
		os.Exit(10)
	}
	os.Exit(0)
}

func runInitializeErrorMCPProbeHelper() {
	readInitializeRequestOrExit()
	fmt.Println(`{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"bad initialize"}}`)
	os.Exit(0)
}

func runMissingProtocolVersionMCPProbeHelper() {
	readInitializeRequestOrExit()
	fmt.Println(`{"jsonrpc":"2.0","id":1,"result":{"capabilities":{},"serverInfo":{"name":"test","version":"1"}}}`)
	os.Exit(0)
}

func readInitializeRequestOrExit() {
	reader := bufio.NewReader(os.Stdin)
	readInitializeRequestFromReaderOrExit(reader)
}

func readInitializeRequestFromReaderOrExit(reader *bufio.Reader) {
	line, err := reader.ReadString('\n')
	if err != nil {
		os.Exit(8)
	}
	var request struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Method  string `json:"method"`
	}
	if err := json.Unmarshal([]byte(line), &request); err != nil ||
		request.JSONRPC != "2.0" ||
		request.ID != 1 ||
		request.Method != "initialize" {
		os.Exit(9)
	}
}
