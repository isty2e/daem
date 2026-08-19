package mcp

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/runtimeprobe"
	"github.com/isty2e/daem/internal/subprocess"
)

func TestExecutorMapsSuccessfulLaunchAndInitialize(t *testing.T) {
	var gotRequest commandRequest
	executor := newExecutor(executorOptions{
		LookupEnv: func(name string) (string, bool) {
			if name == "HOST_TOKEN" {
				return "secret-value", true
			}
			return "", false
		},
		Runner: func(ctx context.Context, request commandRequest) commandResult {
			gotRequest = request
			return commandResult{Started: true, InitializeSucceeded: true}
		},
	})

	facts, err := executor.Probe(context.Background(), ProbeRequest{
		Transport: TransportStdio,
		Command:   "node",
		Args:      []string{"server.js"},
		Env:       map[string]string{"API_TOKEN": "HOST_TOKEN"},
	}, testProbeBinder(t))
	if err != nil {
		t.Fatalf("Probe returned error: %v", err)
	}
	observation := foldProbeFacts(t, facts)
	if observation.Launcher().State() != runtimeprobe.ObservedOK ||
		observation.ProtocolInitialize().State() != runtimeprobe.ObservedOK {
		t.Fatalf("runtime observation = %#v, want launcher and initialize observed_ok", observation)
	}
	assertStdioEndpointAuthClassification(t, observation)
	if gotRequest.Command != "node" || gotRequest.nativeWorkDir == nil {
		t.Fatalf("command request = %#v, want command and native workdir authority", gotRequest)
	}
	if !containsString(gotRequest.Env, "API_TOKEN=secret-value") {
		t.Fatalf("env = %#v, want server env name with host secret value", gotRequest.Env)
	}
	if gotRequest.ProtocolVersion != defaultProtocolVersion {
		t.Fatalf("protocol version = %q, want default", gotRequest.ProtocolVersion)
	}
}

func TestExecutorClassifiesCancellationAlongsideRunnerError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	executor := newExecutor(executorOptions{
		Runner: func(context.Context, commandRequest) commandResult {
			cancel()
			return commandResult{Started: true, Canceled: true, Err: errors.New("runner stopped")}
		},
	})

	facts, err := executor.Probe(ctx, ProbeRequest{
		Transport: TransportStdio,
		Command:   "test-mcp-server",
	}, testProbeBinder(t))
	if err != nil {
		t.Fatalf("Probe returned error: %v", err)
	}
	observation := foldProbeFacts(t, facts)
	detail := observation.ProtocolInitialize().SanitizedDetail()
	if !strings.Contains(detail, "probe canceled") || !strings.Contains(detail, "runner stopped") {
		t.Fatalf("protocol detail = %q, want cancellation and runner failure", detail)
	}
}

func TestExecutorMapsLaunchFailureWithoutProtocolFact(t *testing.T) {
	executor := newExecutor(executorOptions{
		Runner: func(ctx context.Context, request commandRequest) commandResult {
			return commandResult{MissingRunner: true, Err: errors.New("not found")}
		},
	})

	facts, err := executor.Probe(context.Background(), ProbeRequest{
		Transport: TransportStdio,
		Command:   "missing-daem-test",
	}, testProbeBinder(t))
	if err != nil {
		t.Fatalf("Probe returned error: %v", err)
	}
	observation := foldProbeFacts(t, facts)
	if observation.Launcher().State() != runtimeprobe.ObservedFailed ||
		observation.ProtocolInitialize().State() != runtimeprobe.NotProbed {
		t.Fatalf("runtime observation = %#v, want launch failure and not_probed protocol", observation)
	}
	assertStdioEndpointAuthClassification(t, observation)
	if !strings.Contains(observation.Launcher().SanitizedDetail(), "missing runner") {
		t.Fatalf("launcher detail = %q, want missing runner", observation.Launcher().SanitizedDetail())
	}
}

func TestExecutorMapsInitializeFailureSeparatelyFromLaunch(t *testing.T) {
	executor := newExecutor(executorOptions{
		Runner: func(ctx context.Context, request commandRequest) commandResult {
			return commandResult{Started: true, Err: errors.New("initialize rejected")}
		},
	})

	facts, err := executor.Probe(context.Background(), ProbeRequest{
		Transport: TransportStdio,
		Command:   "node",
	}, testProbeBinder(t))
	if err != nil {
		t.Fatalf("Probe returned error: %v", err)
	}
	observation := foldProbeFacts(t, facts)
	if observation.Launcher().State() != runtimeprobe.ObservedOK ||
		observation.ProtocolInitialize().State() != runtimeprobe.ObservedFailed {
		t.Fatalf("runtime observation = %#v, want launch ok and initialize failure", observation)
	}
	assertStdioEndpointAuthClassification(t, observation)
}

func TestExecutorBlocksMissingEnvBeforeRunner(t *testing.T) {
	called := false
	executor := newExecutor(executorOptions{
		LookupEnv: func(name string) (string, bool) {
			return "", false
		},
		Runner: func(ctx context.Context, request commandRequest) commandResult {
			called = true
			return commandResult{Started: true, InitializeSucceeded: true}
		},
	})

	facts, err := executor.Probe(context.Background(), ProbeRequest{
		Transport: TransportStdio,
		Command:   "node",
		Env:       map[string]string{"API_TOKEN": "HOST_TOKEN"},
	}, testProbeBinder(t))
	if err != nil {
		t.Fatalf("Probe returned error: %v", err)
	}
	if called {
		t.Fatal("runner was called despite missing env ref")
	}
	observation := foldProbeFacts(t, facts)
	if observation.Launcher().State() != runtimeprobe.Blocked ||
		observation.Launcher().Reason() != runtimeprobe.ReasonBlocked ||
		observation.ProtocolInitialize().State() != runtimeprobe.NotProbed {
		t.Fatalf("runtime observation = %#v, want blocked launcher and not_probed protocol", observation)
	}
	assertStdioEndpointAuthClassification(t, observation)
}

func TestExecutorRejectsUnsupportedTransportWithoutRunner(t *testing.T) {
	called := false
	acquired := false
	executor := newExecutor(executorOptions{
		Runner: func(ctx context.Context, request commandRequest) commandResult {
			called = true
			return commandResult{}
		},
	})

	facts, err := executor.Probe(context.Background(), ProbeRequest{
		Transport: Transport("streamable-http"),
	}, func() (subprocess.WorkingDirectoryBinding, error) {
		acquired = true
		return nil, errors.New("must not acquire")
	})
	if err != nil {
		t.Fatalf("Probe returned error: %v", err)
	}
	if called {
		t.Fatal("runner was called for unsupported transport")
	}
	if acquired {
		t.Fatal("working-directory authority was acquired for unsupported transport")
	}
	observation := foldProbeFacts(t, facts)
	if observation.Launcher().State() != runtimeprobe.Unsupported ||
		observation.ProtocolInitialize().State() != runtimeprobe.Unsupported ||
		observation.EndpointHealth().State() != runtimeprobe.Unsupported ||
		observation.Authentication().State() != runtimeprobe.Unsupported ||
		observation.ToolInventory().State() != runtimeprobe.Unsupported {
		t.Fatalf("runtime observation = %#v, want unsupported launcher, protocol, endpoint, auth, and tool inventory", observation)
	}
}

func TestExecutorRejectsNULArgumentBeforeRunner(t *testing.T) {
	called := false
	executor := newExecutor(executorOptions{
		Runner: func(ctx context.Context, request commandRequest) commandResult {
			called = true
			return commandResult{Started: true, InitializeSucceeded: true}
		},
	})

	_, err := executor.Probe(context.Background(), ProbeRequest{
		Transport: TransportStdio,
		Command:   "node",
		Args:      []string{"server.js", "bad\x00arg"},
	}, testProbeBinder(t))
	if err == nil || !strings.Contains(err.Error(), "arg[1] must not contain NUL") {
		t.Fatalf("Probe error = %v, want NUL argument rejection", err)
	}
	if called {
		t.Fatal("runner was called after invalid NUL argument")
	}
}

func TestExecutorSanitizesFailureDetails(t *testing.T) {
	executor := newExecutor(executorOptions{
		OutputLimit: 256,
		LookupEnv: func(name string) (string, bool) {
			if name == "HOST_TOKEN" {
				return "super-secret", true
			}
			return "", false
		},
		Runner: func(ctx context.Context, request commandRequest) commandResult {
			return commandResult{
				Started: true,
				Stdout:  `{"token":"super-secret"}`,
				Stderr:  "api_key=super-secret",
				Err:     errors.New("password=super-secret"),
			}
		},
	})

	facts, err := executor.Probe(context.Background(), ProbeRequest{
		Transport: TransportStdio,
		Command:   "node",
		Env:       map[string]string{"API_TOKEN": "HOST_TOKEN"},
	}, testProbeBinder(t))
	if err != nil {
		t.Fatalf("Probe returned error: %v", err)
	}
	observation := foldProbeFacts(t, facts)
	detail := observation.ProtocolInitialize().SanitizedDetail()
	if strings.Contains(detail, "super-secret") {
		t.Fatalf("detail = %q, leaked secret", detail)
	}
	if !strings.Contains(detail, "[REDACTED]") || !strings.Contains(detail, "redacted=true") {
		t.Fatalf("detail = %q, want redaction marker", detail)
	}
	if strings.Contains(observation.Authentication().SanitizedDetail(), "super-secret") ||
		strings.Contains(observation.EndpointHealth().SanitizedDetail(), "super-secret") ||
		strings.Contains(observation.ToolInventory().SanitizedDetail(), "super-secret") {
		t.Fatalf("support classification leaked secret: auth=%q endpoint=%q tools=%q", observation.Authentication().SanitizedDetail(), observation.EndpointHealth().SanitizedDetail(), observation.ToolInventory().SanitizedDetail())
	}
}

func TestExecutorRedactsSensitiveInheritedEnvironmentValue(t *testing.T) {
	const ambientSecret = "ambient-probe-secret-value"
	t.Setenv("DAEM_TEST_PROBE_SECRET", ambientSecret)
	executor := newExecutor(executorOptions{
		Runner: func(_ context.Context, request commandRequest) commandResult {
			if !containsString(request.Env, "DAEM_TEST_PROBE_SECRET="+ambientSecret) {
				t.Fatal("probe env did not inherit ambient secret")
			}
			return commandResult{
				Started: true,
				Stdout:  "server echoed " + ambientSecret,
				Err:     errors.New("initialize failed with " + ambientSecret),
			}
		},
	})

	facts, err := executor.Probe(context.Background(), ProbeRequest{
		Transport: TransportStdio,
		Command:   "ambient-probe-redaction-test",
	}, testProbeBinder(t))
	if err != nil {
		t.Fatalf("Probe returned error: %v", err)
	}
	observation := foldProbeFacts(t, facts)
	detail := observation.ProtocolInitialize().SanitizedDetail()
	if strings.Contains(detail, ambientSecret) {
		t.Fatalf("probe detail leaked inherited ambient secret in %q", detail)
	}
	if !strings.Contains(detail, "[REDACTED]") || !strings.Contains(detail, "redacted=true") {
		t.Fatalf("probe detail = %q, want inherited-secret redaction", detail)
	}
}

func TestExecutorRedactsQuotedKeysAndTruncatedSecretPrefixes(t *testing.T) {
	const secret = "super-secret"
	executor := newExecutor(executorOptions{
		OutputLimit: 256,
		LookupEnv: func(name string) (string, bool) {
			if name == "HOST_TOKEN" {
				return secret, true
			}
			return "", false
		},
		Runner: func(context.Context, commandRequest) commandResult {
			return commandResult{
				Started:         true,
				Stdout:          `{"token":"unlisted-secret"}`,
				Stderr:          "runner saw super-",
				StderrTruncated: true,
				Err:             errors.New(`password="quoted value"`),
			}
		},
	})

	facts, err := executor.Probe(context.Background(), ProbeRequest{
		Transport: TransportStdio,
		Command:   "redaction-boundary-test",
		Env:       map[string]string{"CHILD_TOKEN": "HOST_TOKEN"},
	}, testProbeBinder(t))
	if err != nil {
		t.Fatalf("Probe returned error: %v", err)
	}
	detail := foldProbeFacts(t, facts).ProtocolInitialize().SanitizedDetail()
	for _, forbidden := range []string{"unlisted-secret", "super-", "quoted value"} {
		if strings.Contains(detail, forbidden) {
			t.Fatalf("detail leaked %q in %q", forbidden, detail)
		}
	}
	for _, want := range []string{
		`stdout: {"token":[REDACTED]}`,
		"stderr: runner saw [REDACTED]",
		"error: password=[REDACTED]",
		"stderr_truncated=true",
		"redacted=true",
	} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail = %q, want %q", detail, want)
		}
	}
}

func TestSanitizeCaptureRedactsSecretSplitAcrossRunnerWrites(t *testing.T) {
	buffer := subprocess.NewBoundedBuffer(64)
	for _, chunk := range []string{"runner saw ", "super-", "secret"} {
		if _, err := buffer.Write([]byte(chunk)); err != nil {
			t.Fatalf("Write(%q): %v", chunk, err)
		}
	}

	capture := sanitizeCapture(commandResult{
		Stderr:          buffer.String(),
		StderrTruncated: buffer.Truncated(),
	}, []string{"super-secret"}, 64)
	if capture.stderr != "runner saw [REDACTED]" || !capture.redacted || capture.stderrTruncated {
		t.Fatalf("capture = %#v", capture)
	}
}

func TestExecutorDoesNotAcquireWorkingDirectoryBeforeEnvPreflight(t *testing.T) {
	acquired := false
	called := false
	executor := newExecutor(executorOptions{
		LookupEnv: func(string) (string, bool) {
			return "", false
		},
		Runner: func(context.Context, commandRequest) commandResult {
			called = true
			return commandResult{}
		},
	})

	facts, err := executor.Probe(context.Background(), ProbeRequest{
		Transport: TransportStdio,
		Command:   "node",
		Env:       map[string]string{"API_TOKEN": "MISSING_TOKEN"},
	}, func() (subprocess.WorkingDirectoryBinding, error) {
		acquired = true
		return nil, errors.New("must not acquire")
	})
	if err != nil {
		t.Fatalf("Probe returned error: %v", err)
	}
	if acquired || called {
		t.Fatalf("acquired=%t called=%t, want no authority or runner effects", acquired, called)
	}
	if got := foldProbeFacts(t, facts).Launcher().State(); got != runtimeprobe.Blocked {
		t.Fatalf("launcher state = %s, want blocked", got)
	}
}

func TestExecutorFailsClosedWithoutWorkingDirectoryAuthority(t *testing.T) {
	called := false
	executor := newExecutor(executorOptions{
		Runner: func(context.Context, commandRequest) commandResult {
			called = true
			return commandResult{}
		},
	})

	_, err := executor.Probe(context.Background(), ProbeRequest{
		Transport: TransportStdio,
		Command:   "node",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "binder is required") {
		t.Fatalf("Probe error = %v, want missing binder rejection", err)
	}
	if called {
		t.Fatal("runner was called without working-directory authority")
	}

	_, err = executor.Probe(context.Background(), ProbeRequest{
		Transport: TransportStdio,
		Command:   "node",
	}, func() (subprocess.WorkingDirectoryBinding, error) {
		return nil, nil
	})
	if err == nil || !strings.Contains(err.Error(), "binding is required") {
		t.Fatalf("Probe error = %v, want nil binding rejection", err)
	}
}

func TestExecutorRejectsInvalidWorkingDirectoryAuthorityBeforeRunner(t *testing.T) {
	called := false
	binding := testProbeBinding{
		validate: func() error {
			return errors.New("selected root changed")
		},
	}
	executor := newExecutor(executorOptions{
		Runner: func(context.Context, commandRequest) commandResult {
			called = true
			return commandResult{}
		},
	})

	facts, err := executor.Probe(context.Background(), ProbeRequest{
		Transport: TransportStdio,
		Command:   "node",
	}, func() (subprocess.WorkingDirectoryBinding, error) {
		return &binding, nil
	})
	if err != nil {
		t.Fatalf("Probe returned error: %v", err)
	}
	observation := foldProbeFacts(t, facts)
	if observation.Launcher().State() != runtimeprobe.Blocked ||
		observation.Launcher().Reason() != runtimeprobe.ReasonBlocked ||
		!strings.Contains(observation.Launcher().SanitizedDetail(), "before launch") {
		t.Fatalf("runtime observation = %#v, want prelaunch authority blocker", observation)
	}
	if called {
		t.Fatal("runner was called after prelaunch authority rejection")
	}
	if binding.closeCount != 1 {
		t.Fatalf("binding close count = %d, want 1", binding.closeCount)
	}
}

func TestExecutorRejectsNilWorkingDirectoryDescriptorWithoutPanic(t *testing.T) {
	called := false
	binding := testProbeBinding{nilDirectory: true}
	executor := newExecutor(executorOptions{
		Runner: func(context.Context, commandRequest) commandResult {
			called = true
			return commandResult{}
		},
	})

	facts, err := executor.Probe(context.Background(), ProbeRequest{
		Transport: TransportStdio,
		Command:   "node",
	}, func() (subprocess.WorkingDirectoryBinding, error) {
		return &binding, nil
	})
	if err != nil {
		t.Fatalf("Probe returned error: %v", err)
	}
	observation := foldProbeFacts(t, facts)
	if observation.Launcher().State() != runtimeprobe.Blocked ||
		!strings.Contains(observation.Launcher().SanitizedDetail(), "descriptor is required") {
		t.Fatalf("runtime observation = %#v, want nil descriptor blocker", observation)
	}
	if called {
		t.Fatal("runner was called with a nil working-directory descriptor")
	}
}

func TestExecutorSanitizesWorkingDirectoryAcquisitionBlocker(t *testing.T) {
	const secret = "root-secret"
	executor := newExecutor(executorOptions{
		LookupEnv: func(name string) (string, bool) {
			return secret, name == "HOST_TOKEN"
		},
	})

	facts, err := executor.Probe(context.Background(), ProbeRequest{
		Transport: TransportStdio,
		Command:   "node",
		Env:       map[string]string{"API_TOKEN": "HOST_TOKEN"},
	}, func() (subprocess.WorkingDirectoryBinding, error) {
		return nil, errors.New("capture failed near " + secret)
	})
	if err != nil {
		t.Fatalf("Probe returned error: %v", err)
	}
	observation := foldProbeFacts(t, facts)
	detail := observation.Launcher().SanitizedDetail()
	if observation.Launcher().State() != runtimeprobe.Blocked ||
		strings.Contains(detail, secret) ||
		!strings.Contains(detail, "[REDACTED]") {
		t.Fatalf("launcher = %#v, want redacted authority blocker", observation.Launcher())
	}
}

func TestExecutorInvalidatesSuccessWhenWorkingDirectoryAuthorityChanges(t *testing.T) {
	root := t.TempDir()
	directory, err := os.Open(root)
	if err != nil {
		t.Fatalf("open working directory: %v", err)
	}
	validateCalls := 0
	binding := testProbeBinding{
		directory: directory,
		validate: func() error {
			validateCalls++
			if validateCalls > 1 {
				return errors.New("selected root changed")
			}
			return nil
		},
	}
	executor := newExecutor(executorOptions{
		Runner: func(context.Context, commandRequest) commandResult {
			return commandResult{Started: true, InitializeSucceeded: true}
		},
	})

	facts, err := executor.Probe(context.Background(), ProbeRequest{
		Transport: TransportStdio,
		Command:   "node",
	}, func() (subprocess.WorkingDirectoryBinding, error) {
		return &binding, nil
	})
	if err != nil {
		t.Fatalf("Probe returned error: %v", err)
	}
	observation := foldProbeFacts(t, facts)
	if observation.Launcher().State() != runtimeprobe.ObservedFailed ||
		observation.ProtocolInitialize().State() != runtimeprobe.NotProbed {
		t.Fatalf("runtime observation = %#v, want invalidated launch and no protocol success", observation)
	}
	if !strings.Contains(observation.Launcher().SanitizedDetail(), "working-directory authority failed") {
		t.Fatalf("launcher detail = %q, want authority failure", observation.Launcher().SanitizedDetail())
	}
	if binding.closeCount != 1 {
		t.Fatalf("binding close count = %d, want 1", binding.closeCount)
	}
}

func TestExecutorDoesNotReclassifyStartedFailureAfterProbeTimeout(t *testing.T) {
	executor := newExecutor(executorOptions{
		Timeout: time.Millisecond,
		Runner: func(ctx context.Context, request commandRequest) commandResult {
			<-ctx.Done()
			return commandResult{Started: true, Err: errors.New("exit status 17")}
		},
	})

	facts, err := executor.Probe(context.Background(), ProbeRequest{
		Transport: TransportStdio,
		Command:   "node",
	}, testProbeBinder(t))
	if err != nil {
		t.Fatalf("Probe returned error: %v", err)
	}
	observation := foldProbeFacts(t, facts)
	detail := observation.ProtocolInitialize().SanitizedDetail()
	if strings.Contains(detail, "probe timed out") {
		t.Fatalf("detail = %q, want started failure preserved through post-wait timeout", detail)
	}
	if !strings.Contains(detail, "exit status 17") {
		t.Fatalf("detail = %q, want runner exit error", detail)
	}
}

func TestExecutorSanitizesTimeoutFailureDetails(t *testing.T) {
	executor := newExecutor(executorOptions{
		OutputLimit: 256,
		LookupEnv: func(name string) (string, bool) {
			if name == "HOST_TOKEN" {
				return "super-secret", true
			}
			return "", false
		},
		Runner: func(ctx context.Context, request commandRequest) commandResult {
			return commandResult{
				Started:  true,
				TimedOut: true,
				Stderr:   "token=super-secret",
				Err:      errors.New("password=super-secret"),
			}
		},
	})

	facts, err := executor.Probe(context.Background(), ProbeRequest{
		Transport: TransportStdio,
		Command:   "node",
		Env:       map[string]string{"API_TOKEN": "HOST_TOKEN"},
	}, testProbeBinder(t))
	if err != nil {
		t.Fatalf("Probe returned error: %v", err)
	}
	observation := foldProbeFacts(t, facts)
	detail := observation.ProtocolInitialize().SanitizedDetail()
	if strings.Contains(detail, "super-secret") {
		t.Fatalf("detail = %q, leaked secret", detail)
	}
	for _, want := range []string{"probe timed out", "token=[REDACTED]", "password=[REDACTED]", "redacted=true"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail = %q, want %q", detail, want)
		}
	}
}

func foldProbeFacts(t *testing.T, facts []runtimeprobe.Fact) runtimeprobe.Observation {
	t.Helper()
	observation, err := runtimeprobe.FoldFacts(facts)
	if err != nil {
		t.Fatalf("FoldFacts returned error: %v", err)
	}
	return observation
}

func assertStdioEndpointAuthClassification(t *testing.T, observation runtimeprobe.Observation) {
	t.Helper()
	if observation.EndpointHealth().State() != runtimeprobe.NotApplicable ||
		observation.EndpointHealth().Reason() != runtimeprobe.ReasonNotApplicable ||
		!strings.Contains(observation.EndpointHealth().SanitizedDetail(), "stdio transport") {
		t.Fatalf("endpoint health = %#v, want stdio not_applicable support classification", observation.EndpointHealth())
	}
	if observation.Authentication().State() != runtimeprobe.Unsupported ||
		observation.Authentication().Reason() != runtimeprobe.ReasonUnsupported ||
		!strings.Contains(observation.Authentication().SanitizedDetail(), "environment presence is not auth readiness") {
		t.Fatalf("authentication = %#v, want stdio unsupported auth classification", observation.Authentication())
	}
	if observation.ToolInventory().State() != runtimeprobe.Unsupported ||
		observation.ToolInventory().Reason() != runtimeprobe.ReasonUnsupported ||
		!strings.Contains(observation.ToolInventory().SanitizedDetail(), "redaction and permission contract") {
		t.Fatalf("tool inventory = %#v, want unsupported inventory classification", observation.ToolInventory())
	}
}

func containsString(values []string, want string) bool {
	return slices.Contains(values, want)
}

type testProbeBinding struct {
	directory    *os.File
	validate     func() error
	nilDirectory bool
	closeCount   int
}

func testProbeBinder(t *testing.T) subprocess.WorkingDirectoryBinder {
	t.Helper()
	root := t.TempDir()
	return func() (subprocess.WorkingDirectoryBinding, error) {
		directory, err := os.Open(root)
		if err != nil {
			return nil, err
		}
		return &testProbeBinding{directory: directory}, nil
	}
}

func (binding *testProbeBinding) Validate() error {
	if binding.validate != nil {
		return binding.validate()
	}
	return nil
}

func (binding *testProbeBinding) OpenDirectory() (*os.File, error) {
	if binding.nilDirectory {
		return nil, nil
	}
	if binding.directory == nil {
		return nil, errors.New("test working directory is unavailable")
	}
	directory := binding.directory
	binding.directory = nil
	return directory, nil
}

func (binding *testProbeBinding) Close() error {
	binding.closeCount++
	if binding.directory == nil {
		return nil
	}
	err := binding.directory.Close()
	binding.directory = nil
	return err
}
