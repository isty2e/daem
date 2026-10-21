package diagnose

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/findings"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/subprocess"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/test/testkit/doctorenv"
)

func TestGitEnvironmentCheckClassifiesAttempts(t *testing.T) {
	version := subprocess.CommandResult{Started: true, HasExitCode: true, Stdout: "git version test"}
	cases := []struct {
		name         string
		version      subprocess.CommandResult
		help         subprocess.CommandResult
		want         findings.CheckStatus
		wantDetail   string
		wantAttempts int
	}{
		{
			name: "success", version: version,
			help:         subprocess.CommandResult{Started: true, HasExitCode: true, Stdout: "usage: git init"},
			want:         findings.CheckOK,
			wantDetail:   "git version test; object-format sha1",
			wantAttempts: 2,
		},
		{
			name: "usage exit", version: version,
			help: subprocess.CommandResult{
				Started: true, HasExitCode: true, ExitCode: 129,
				Stderr: "usage: git init", Err: errors.New("exit status 129"),
			},
			want:         findings.CheckOK,
			wantDetail:   "git version test; object-format sha1",
			wantAttempts: 2,
		},
		{
			name: "sha256 capability", version: version,
			help: subprocess.CommandResult{
				Started: true, HasExitCode: true, ExitCode: 129,
				Stderr: "usage: git init [--object-format=<format>]", Err: errors.New("exit status 129"),
			},
			want:         findings.CheckOK,
			wantDetail:   "git version test; object-format sha1,sha256",
			wantAttempts: 2,
		},
		{
			name: "unrelated help exit", version: version,
			help: subprocess.CommandResult{
				Started: true, HasExitCode: true, ExitCode: 127,
				Stderr: "sleep: not found", Err: errors.New("exit status 127"),
			},
			want:         findings.CheckError,
			wantDetail:   "git init -h failed: exit status 127",
			wantAttempts: 2,
		},
		{
			name:         "missing executable",
			version:      subprocess.CommandResult{MissingRunner: true, Err: exec.ErrNotFound},
			want:         findings.CheckError,
			wantDetail:   "git executable was not found in PATH",
			wantAttempts: 1,
		},
		{
			name: "version failure",
			version: subprocess.CommandResult{
				Started: true, HasExitCode: true, ExitCode: 7, Err: errors.New("exit status 7"),
			},
			want:         findings.CheckError,
			wantDetail:   "git --version failed: exit status 7",
			wantAttempts: 1,
		},
		{
			name:         "empty version",
			version:      subprocess.CommandResult{Started: true, HasExitCode: true},
			want:         findings.CheckError,
			wantDetail:   "git --version returned empty output",
			wantAttempts: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				attempts := 0
				runner := func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
					attempts++
					switch attempts {
					case 1:
						assertGitCheckRequest(t, request, "--version")
						return tc.version
					case 2:
						assertGitCheckRequest(t, request, "init", "-h")
						return tc.help
					default:
						t.Fatalf("unexpected attempt %d", attempts)
						return subprocess.CommandResult{}
					}
				}

				check := gitEnvironmentCheck(nil, 5*time.Second, runner)
				if check.Name != "git" || check.Status != tc.want || check.Detail != tc.wantDetail {
					t.Fatalf("check = %#v, want git %s %q", check, tc.want, tc.wantDetail)
				}
				if attempts != tc.wantAttempts {
					t.Fatalf("attempts = %d, want %d", attempts, tc.wantAttempts)
				}
			})
		})
	}
}

func TestGitEnvironmentCheckTimesOutStallingObjectFormatHelp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := time.Now()
		attempts := 0
		runner := func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			attempts++
			deadline, ok := ctx.Deadline()
			if !ok || !deadline.Equal(started.Add(5*time.Second)) {
				t.Fatalf("attempt %d deadline = %v/%t, want one shared five-second budget", attempts, deadline, ok)
			}
			switch attempts {
			case 1:
				assertGitCheckRequest(t, request, "--version")
				time.Sleep(4 * time.Second)
				return subprocess.CommandResult{Started: true, HasExitCode: true, Stdout: "git version test"}
			case 2:
				assertGitCheckRequest(t, request, "init", "-h")
				if remaining := time.Until(deadline); remaining != time.Second {
					t.Fatalf("help remaining budget = %s, want 1s", remaining)
				}
				<-ctx.Done()
				return subprocess.CommandResult{Started: true, TimedOut: true, Err: ctx.Err()}
			default:
				t.Fatalf("unexpected attempt %d", attempts)
				return subprocess.CommandResult{}
			}
		}

		check := gitEnvironmentCheck(t.Context(), 5*time.Second, runner)
		if attempts != 2 || check.Name != "git" || check.Status != findings.CheckError ||
			check.Detail != "git check timed out after 5s" {
			t.Fatalf("attempts/check = %d/%#v, want help attempt timeout", attempts, check)
		}
		if elapsed := time.Since(started); elapsed != 5*time.Second {
			t.Fatalf("assessment elapsed = %s, want total shared budget 5s", elapsed)
		}
	})
}

func TestGitCheckSeparatesOwnTimeoutFromCallerCancellation(t *testing.T) {
	for _, phase := range []string{"version", "help"} {
		for _, callerCancel := range []bool{false, true} {
			name := phase + "/timeout"
			if callerCancel {
				name = phase + "/caller cancellation"
			}
			t.Run(name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					if callerCancel {
						time.AfterFunc(time.Second, cancel)
					}
					attempts := 0
					runner := func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
						attempts++
						if attempts == 1 {
							assertGitCheckRequest(t, request, "--version")
							if phase == "help" {
								return subprocess.CommandResult{Started: true, HasExitCode: true, Stdout: "git version test"}
							}
						} else {
							assertGitCheckRequest(t, request, "init", "-h")
						}
						<-ctx.Done()
						return subprocess.CommandResult{
							Started: true, Err: ctx.Err(),
							TimedOut: errors.Is(ctx.Err(), context.DeadlineExceeded),
							Canceled: errors.Is(ctx.Err(), context.Canceled),
						}
					}

					check := gitEnvironmentCheck(ctx, 5*time.Second, runner)
					wantAttempts := 1
					if phase == "help" {
						wantAttempts = 2
					}
					wantDetail := "git check timed out after 5s"
					if callerCancel {
						wantDetail = "git check stopped by caller context: context canceled"
					}
					if attempts != wantAttempts || check.Name != "git" || check.Status != findings.CheckError || check.Detail != wantDetail {
						t.Fatalf("attempts/check = %d/%#v, want %d attempts and %q", attempts, check, wantAttempts, wantDetail)
					}
				})
			})
		}
	}
}

func TestGitEnvironmentCheckPreservesPreCanceledCaller(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	attempts := 0
	runner := func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
		attempts++
		assertGitCheckRequest(t, request, "--version")
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatal("runner did not receive caller cancellation")
		}
		return subprocess.CommandResult{Err: ctx.Err()}
	}

	check := gitEnvironmentCheck(ctx, 5*time.Second, runner)
	if attempts != 1 || check.Status != findings.CheckError ||
		check.Detail != "git check stopped by caller context: context canceled" {
		t.Fatalf("attempts/check = %d/%#v, want caller cancellation without help", attempts, check)
	}
}

func assertGitCheckRequest(t *testing.T, request subprocess.CommandRequest, args ...string) {
	t.Helper()
	if request.Command != "git" || !slices.Equal(request.Args, args) {
		t.Fatalf("command/args = %q/%q, want git %q", request.Command, request.Args, args)
	}
}

func TestGitObjectFormatCapabilityLabel(t *testing.T) {
	t.Parallel()

	if got := gitObjectFormatCapabilityLabel("usage: git init [--object-format=<format>]", false); got != "object-format sha1,sha256" {
		t.Fatalf("capable git label = %q", got)
	}
	if got := gitObjectFormatCapabilityLabel("usage: git init", false); got != "object-format sha1" {
		t.Fatalf("legacy git label = %q", got)
	}
}

func TestGitEnvironmentCheckCaptureDiagnostics(t *testing.T) {
	cases := []struct {
		name   string
		stdout string
		outCut bool
		errCut bool
		code   int
	}{
		{name: "complete success"},
		{name: "complete usage", code: 129},
		{name: "stdout incomplete", code: 129, outCut: true},
		{name: "stderr incomplete", code: 129, errCut: true},
		{name: "both incomplete", code: 129, outCut: true, errCut: true},
		{name: "normalized overflow", code: 129, outCut: true, stdout: strings.Repeat(" ", subprocess.DefaultCommandOutputLimit)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			help := subprocess.CommandResult{
				Started: true, HasExitCode: true, ExitCode: tc.code,
				Stdout: tc.stdout, StdoutTruncated: tc.outCut, StderrTruncated: tc.errCut,
			}
			if tc.code != 0 {
				help.Err = errors.New("cleanup-marker\n--token=help-probe-sensitive-marker")
			}
			check := gitEnvironmentHelpFixture(t, help)
			if check.Name != "git" || check.Status != findings.CheckError {
				t.Fatalf("check = %#v, want Git error", check)
			}
			class := "returned empty output"
			if tc.outCut || tc.errCut {
				class = "capture was truncated or incomplete"
			}
			reason := ""
			if tc.code != 0 {
				reason = string(subprocess.CommandReasonNonZeroExit)
			}
			for _, evidence := range []string{
				class,
				fmt.Sprintf("exit_code=%d", tc.code),
				"reason=" + strconv.Quote(reason),
				fmt.Sprintf("stdout_truncated=%t", tc.outCut),
				fmt.Sprintf("stderr_truncated=%t", tc.errCut),
			} {
				if !strings.Contains(check.Detail, evidence) {
					t.Errorf("detail %q omitted %q", check.Detail, evidence)
				}
			}
			if tc.code != 0 && !strings.Contains(check.Detail, "cleanup-marker") {
				t.Errorf("detail %q lost sanitized cleanup evidence", check.Detail)
			}
			detail := ""
			if help.Err != nil {
				detail = subprocess.NewCapturePolicy(nil, subprocess.DefaultCommandOutputLimit).Sanitize(help.Err.Error(), false).Text()
			}
			if !strings.Contains(check.Detail, "error_detail="+strconv.Quote(detail)) {
				t.Errorf("diagnostic lost sanitized detail %q: %q", detail, check.Detail)
			}
			if strings.Contains(check.Detail, "help-probe-sensitive-marker") || strings.Contains(check.Detail, "\n") {
				t.Fatal("diagnostic re-emitted a raw secret or unquoted multiline evidence")
			}
		})
	}
}

func TestGitEnvironmentCheckPartialCapabilityLabel(t *testing.T) {
	cases := []struct {
		name    string
		help    subprocess.CommandResult
		unknown bool
		sha256  bool
	}{
		{name: "complete negative", help: subprocess.CommandResult{Stdout: "usage: git init"}},
		{name: "complete positive", help: subprocess.CommandResult{Stderr: "usage: git init --object-format=<format>"}, sha256: true},
		{name: "stdout partial negative", help: subprocess.CommandResult{Stdout: "usage: git init", StdoutTruncated: true}, unknown: true},
		{name: "stderr partial negative", help: subprocess.CommandResult{Stderr: "usage: git init", StderrTruncated: true}, unknown: true},
		{name: "partial positive", help: subprocess.CommandResult{Stderr: "usage: git init --object-format=<format>", StderrTruncated: true}, sha256: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.help.Started = true
			tc.help.HasExitCode = true
			tc.help.ExitCode = 129
			tc.help.Err = errors.New("usage-exit-marker")
			check := gitEnvironmentHelpFixture(t, tc.help)
			if check.Status != findings.CheckOK {
				t.Fatalf("check = %#v, want retained OK status", check)
			}
			if strings.Contains(check.Detail, "object-format unknown") != tc.unknown {
				t.Fatalf("capability uncertainty = %q, want unknown=%t", check.Detail, tc.unknown)
			}
			if strings.Contains(check.Detail, "sha256") != tc.sha256 {
				t.Fatalf("capability evidence = %q, want sha256=%t", check.Detail, tc.sha256)
			}
			if tc.unknown {
				if strings.Contains(check.Detail, "sha1") || !strings.Contains(check.Detail, "truncated") {
					t.Fatalf("partial negative evidence was mislabelled: %q", check.Detail)
				}
			} else if !strings.Contains(check.Detail, "object-format sha1") {
				t.Fatalf("known capability label lost: %q", check.Detail)
			}
		})
	}
}

func gitEnvironmentHelpFixture(t *testing.T, help subprocess.CommandResult) findings.Check {
	t.Helper()
	attempts := 0
	runner := func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
		attempts++
		if request.OutputLimit != subprocess.DefaultCommandOutputLimit {
			t.Fatal("doctor output limit changed")
		}
		if attempts == 1 {
			assertGitCheckRequest(t, request, "--version")
			return subprocess.CommandResult{Started: true, HasExitCode: true, Stdout: "git version fixture"}
		}
		if attempts != 2 {
			t.Fatalf("unexpected extra command attempt %d", attempts)
		}
		assertGitCheckRequest(t, request, "init", "-h")
		return help
	}
	check := gitEnvironmentCheck(context.Background(), time.Second, runner)
	if attempts != 2 {
		t.Fatalf("command attempts = %d, want version and help", attempts)
	}
	return check
}

func TestIndependentEnvironmentChecksNamesHostEffectWithoutProbing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	doctorenv.WithFakeGit(t, "git version test")
	cacheFile := filepath.Join(t.TempDir(), "cache-file")
	if err := os.WriteFile(cacheFile, []byte("not-a-directory\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	selection, err := targetselection.ForDiagnostics([]string{"codex"})
	if err != nil {
		t.Fatal(err)
	}
	checks := IndependentEnvironmentChecks(
		context.Background(),
		daempaths.Paths{CacheDir: cacheFile, ManifestRoot: t.TempDir()},
		true,
		selection,
		map[entity.Kind]struct{}{
			entity.KindInstructions: {},
			entity.KindSkill:        {},
			entity.KindHook:         {},
		},
		true,
	)
	byName := map[string]findings.Check{}
	for _, check := range checks {
		byName[check.Name] = check
		if strings.Contains(check.Name, "plugin_contribution") || strings.Contains(check.Name, "plugin_config") {
			t.Fatalf("independent checks observed Codex plugin tree: %#v", checks)
		}
	}
	if cache := byName["cache"]; cache.Status != findings.CheckUnsupported {
		t.Fatalf("cache = %#v, want unsupported without probing a non-directory", cache)
	}
	if git := byName["git"]; git.Status != findings.CheckUnsupported {
		t.Fatalf("git = %#v, want unsupported without executing PATH git", git)
	}
	if strings.Contains(byName["git"].Detail, "git version test") {
		t.Fatalf("git = %#v, want no executed version text", byName["git"])
	}
	if plugin := byName["codex_plugin"]; plugin.Status != findings.CheckUnsupported {
		t.Fatalf("codex_plugin = %#v, want unsupported parent", plugin)
	}
	if configDir := byName["target=codex config_dir"]; configDir.Status != findings.CheckUnsupported {
		t.Fatalf("config_dir = %#v, want unsupported", configDir)
	}
	if configFile := byName["target=codex config_file"]; configFile.Status != findings.CheckWarn ||
		!strings.Contains(configFile.Detail, "is missing") {
		t.Fatalf("config_file = %#v, want missing warning from bounded snapshot", configFile)
	}
}
