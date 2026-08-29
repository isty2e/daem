package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/platformsupport"
)

func TestRunCommandRejectsPlatformGatedFamiliesOnUnsupportedPlatform(t *testing.T) {
	admission := testPlatformAdmission(t, "windows", "amd64")
	manifestPath := filepath.Join(t.TempDir(), "daem.toml")

	for _, args := range platformGatedCommandExamples(manifestPath) {
		t.Run(args[0], func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := runCommand(args, &stdout, &stderr, RunOptions{}, commandOptions{
				context:           context.Background(),
				platformAdmission: admission,
			})
			if exitCode != 1 {
				t.Fatalf("exitCode = %d, want 1", exitCode)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			for _, want := range []string{
				args[0] + " failed: platform windows/amd64 is not an admitted daem product target",
				"verification=compile-only",
				"admitted=darwin/arm64,linux/amd64",
				"next: run daem doctor",
			} {
				if !strings.Contains(stderr.String(), want) {
					t.Fatalf("stderr = %q, want %q", stderr.String(), want)
				}
			}
		})
	}

	if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
		t.Fatalf("unsupported preflight touched manifest: %v", err)
	}
}

func TestRunCommandRejectsUnknownPlatformAcrossEveryGatedFamily(t *testing.T) {
	admission := testPlatformAdmission(t, "plan9", "amd64")
	for _, args := range platformGatedCommandExamples(filepath.Join(t.TempDir(), "daem.toml")) {
		t.Run(args[0], func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := runCommand(args, &stdout, &stderr, RunOptions{}, commandOptions{
				context:           context.Background(),
				platformAdmission: admission,
			})
			if exitCode != 1 || stdout.Len() != 0 {
				t.Fatalf("exitCode=%d stdout=%q, want 1 and empty", exitCode, stdout.String())
			}
			for _, want := range []string{"platform plan9/amd64", "verification=unverified"} {
				if !strings.Contains(stderr.String(), want) {
					t.Fatalf("stderr = %q, want %q", stderr.String(), want)
				}
			}
		})
	}
}

func TestRunCommandPreservesHelpOnUnsupportedPlatform(t *testing.T) {
	admission := testPlatformAdmission(t, "windows", "amd64")
	tests := [][]string{
		{"--help"},
		{"-h"},
		{"help"},
		{"help", "add", "skill"},
		{"add", "--help"},
		{"add", "help", "skill"},
		{"add", "skill", "--help"},
		{"apply", "--help"},
		{"import", "--help"},
		{"init", "--help"},
		{"lock", "--help"},
		{"outdated", "--help"},
		{"recover", "--help"},
		{"refresh", "--help"},
		{"refresh", "extension", "--help"},
		{"remove", "--help"},
		{"remove", "help", "skill"},
		{"remove", "skill", "--help"},
		{"unmanage", "--help"},
		{"unmanage", "extension", "--help"},
	}

	for _, args := range tests {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := runCommand(args, &stdout, &stderr, RunOptions{}, commandOptions{
				context:           context.Background(),
				platformAdmission: admission,
			})
			if exitCode != 0 {
				t.Fatalf("exitCode = %d, want 0; stderr=%q", exitCode, stderr.String())
			}
			if stdout.Len() == 0 {
				t.Fatal("help produced no stdout")
			}
			if strings.Contains(stderr.String(), "not an admitted") {
				t.Fatalf("help was blocked by platform preflight: %q", stderr.String())
			}
		})
	}
}

func TestRunCommandPreservesUnknownCommandDiagnosticsOnUnsupportedPlatform(t *testing.T) {
	admission := testPlatformAdmission(t, "windows", "amd64")
	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"unknown"}, want: `unknown command "unknown"`},
		{args: []string{"add", "unknown"}, want: `unknown add resource "unknown"`},
		{args: []string{"remove", "unknown"}, want: `unknown remove resource "unknown"`},
		{args: []string{"unmanage", "unknown"}, want: `unknown unmanage resource "unknown"`},
	}
	for _, test := range tests {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := runCommand(test.args, &stdout, &stderr, RunOptions{}, commandOptions{
			context:           context.Background(),
			platformAdmission: admission,
		})
		if exitCode != 2 || !strings.Contains(stderr.String(), test.want) {
			t.Fatalf("runCommand(%#v) = %d, stderr=%q; want usage error %q", test.args, exitCode, stderr.String(), test.want)
		}
		if strings.Contains(stderr.String(), "not an admitted") {
			t.Fatalf("unknown command was mislabeled as platform failure: %q", stderr.String())
		}
	}
}

func TestPlatformAdmissionClassifierCoversEveryFrozenRoute(t *testing.T) {
	gated := [][]string{
		{"apply"},
		{"import"},
		{"init"},
		{"lock"},
		{"outdated"},
		{"recover"},
		{"refresh"},
	}
	for _, subject := range []string{"extension", "instruction", "hook", "mcp-server", "skill", "skill-group"} {
		gated = append(gated, []string{"add", subject})
	}
	for _, subject := range []string{"extension", "instruction", "hook", "mcp-server", "skill"} {
		gated = append(gated, []string{"remove", subject})
	}
	gated = append(gated, []string{"unmanage", "extension"})
	for _, args := range gated {
		if !requiresPlatformAdmission(args) {
			t.Errorf("requiresPlatformAdmission(%#v) = false, want true", args)
		}
	}

	ungated := [][]string{
		nil,
		{"--version"},
		{"doctor"},
		{"help"},
		{"list"},
		{"probe", "mcp-server"},
		{"status"},
		{"version"},
		{"unknown"},
		{"add"},
		{"add", "unknown"},
		{"remove"},
		{"remove", "skill-group"},
		{"remove", "unknown"},
		{"unmanage"},
		{"unmanage", "unknown"},
	}
	for _, args := range ungated {
		if requiresPlatformAdmission(args) {
			t.Errorf("requiresPlatformAdmission(%#v) = true, want false", args)
		}
	}
}

func TestCommandAdmissionCatalogRoutesEveryRegisteredRoot(t *testing.T) {
	examples := map[string][]string{
		"--help":    {"--help"},
		"--version": {"--version"},
		"-h":        {"-h"},
		"add":       {"add", "--help"},
		"apply":     {"apply", "--help"},
		"doctor":    {"doctor", "--help"},
		"help":      {"help"},
		"import":    {"import", "--help"},
		"init":      {"init", "--help"},
		"list":      {"list", "--help"},
		"lock":      {"lock", "--help"},
		"outdated":  {"outdated", "--help"},
		"probe":     {"probe", "--help"},
		"recover":   {"recover", "--help"},
		"refresh":   {"refresh", "--help"},
		"remove":    {"remove", "--help"},
		"status":    {"status", "--help"},
		"unmanage":  {"unmanage", "--help"},
		"version":   {"version", "--help"},
	}
	if len(commandAdmissionCatalog) != len(examples) {
		t.Fatalf("command admission catalog has %d entries, want %d", len(commandAdmissionCatalog), len(examples))
	}
	for command, args := range examples {
		t.Run(command, func(t *testing.T) {
			if _, ok := commandAdmissionCatalog[command]; !ok {
				t.Fatalf("root command %q has no admission policy", command)
			}
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := runCommand(args, &stdout, &stderr, RunOptions{}, testAdmittedCommandOptions(t, "darwin", "arm64"))
			if exitCode != 0 {
				t.Fatalf("runCommand(%#v) = %d, stderr=%q", args, exitCode, stderr.String())
			}
			if strings.Contains(stderr.String(), "registered without an implementation") {
				t.Fatalf("catalog and dispatch disagree: %q", stderr.String())
			}
		})
	}
}

func TestPlatformPreflightUsesSupportPolicyNotBuildability(t *testing.T) {
	tests := []struct {
		goos             string
		goarch           string
		wantVerification string
	}{
		{goos: "darwin", goarch: "amd64", wantVerification: "compile-only"},
		{goos: "linux", goarch: "arm64", wantVerification: "compile-only"},
		{goos: "linux", goarch: "386", wantVerification: "compile-only"},
		{goos: "windows", goarch: "amd64", wantVerification: "compile-only"},
		{goos: "freebsd", goarch: "riscv64", wantVerification: "unverified"},
	}
	for _, test := range tests {
		t.Run(test.goos+"-"+test.goarch, func(t *testing.T) {
			var stderr bytes.Buffer
			exitCode, rejected := rejectUnsupportedPlatform(
				[]string{"lock"},
				commandOptions{
					context:           context.Background(),
					platformAdmission: testPlatformAdmission(t, test.goos, test.goarch),
				},
				&stderr,
			)
			if !rejected || exitCode != 1 {
				t.Fatalf("rejected=%t exitCode=%d, want true/1", rejected, exitCode)
			}
			if !strings.Contains(stderr.String(), "verification="+test.wantVerification) {
				t.Fatalf("stderr = %q, want verification=%s", stderr.String(), test.wantVerification)
			}
		})
	}

	for _, target := range [][2]string{{"darwin", "arm64"}, {"linux", "amd64"}} {
		var stderr bytes.Buffer
		exitCode, rejected := rejectUnsupportedPlatform(
			[]string{"lock"},
			testAdmittedCommandOptions(t, target[0], target[1]),
			&stderr,
		)
		if rejected || exitCode != 0 || stderr.Len() != 0 {
			t.Fatalf("admitted %s/%s rejected=%t exitCode=%d stderr=%q", target[0], target[1], rejected, exitCode, stderr.String())
		}
	}
}

func testAdmittedCommandOptions(t *testing.T, goos string, goarch string) commandOptions {
	t.Helper()
	admission := testPlatformAdmission(t, goos, goarch)
	var observation platformsupport.RuntimeObservation
	if minimum, required := admission.RuntimeRequirement(); required {
		var err error
		observation, err = platformsupport.NewRuntimeObservation(minimum)
		if err != nil {
			t.Fatal(err)
		}
	}
	return commandOptions{
		context:           context.Background(),
		platformAdmission: admission,
		platformObserver: func(context.Context) (platformsupport.RuntimeObservation, error) {
			return observation, nil
		},
	}
}

func TestPlatformPreflightRunsBeforePathResolutionAndHonorsOptionTerminator(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "relative-data-root")
	admission := testPlatformAdmission(t, "windows", "amd64")
	for _, args := range [][]string{
		{"lock", "--manifest", filepath.Join(t.TempDir(), "daem.toml")},
		{"lock", "--", "--help"},
	} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := runCommand(args, &stdout, &stderr, RunOptions{}, commandOptions{
			context:           context.Background(),
			platformAdmission: admission,
		})
		if exitCode != 1 || !strings.Contains(stderr.String(), "platform windows/amd64") {
			t.Fatalf("runCommand(%#v) = %d, stderr=%q; want platform rejection", args, exitCode, stderr.String())
		}
		if strings.Contains(stderr.String(), "XDG_DATA_HOME") {
			t.Fatalf("runCommand(%#v) reached path resolution: %q", args, stderr.String())
		}
	}
}

func TestPlatformPreflightRejectsMacOSBelowRuntimeFloorBeforeEffects(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "daem.toml")
	options := testMacOSCommandOptions(t, "25.9.9", platformsupport.RuntimeObservationNotObserved)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runCommand(
		[]string{"init", "--manifest", manifestPath},
		&stdout,
		&stderr,
		RunOptions{},
		options,
	)
	if exitCode != 1 || stdout.Len() != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	for _, want := range []string{
		"init failed: platform darwin/arm64 requires macOS 26.0 or newer",
		"observed=25.9.9",
		"verification=native-required",
		"next: run daem doctor",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
	if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
		t.Fatalf("below-floor preflight touched manifest: %v", err)
	}
}

func TestPlatformPreflightAppliesMacOSRuntimeFloorToEveryGatedFamily(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "daem.toml")
	for _, args := range platformGatedCommandExamples(manifestPath) {
		t.Run(args[0], func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := runCommand(
				args,
				&stdout,
				&stderr,
				RunOptions{},
				testMacOSCommandOptions(t, "25.9.9", platformsupport.RuntimeObservationNotObserved),
			)
			if exitCode != 1 || stdout.Len() != 0 {
				t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), "requires macOS 26.0 or newer") {
				t.Fatalf("stderr = %q", stderr.String())
			}
		})
	}
}

func TestPlatformPreflightRejectsUnmanageExtensionBeforeMetadataEffects(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		name := "write"
		if dryRun {
			name = "dry-run"
		}
		t.Run(name, func(t *testing.T) {
			root, manifestPath, manifestBefore, hostPath, hostBefore := writeCLIUnmanageFixture(t, "project")
			args := []string{"unmanage", "extension", "context7", "--manifest", manifestPath}
			if dryRun {
				args = append(args, "--dry-run")
			}

			options := testMacOSCommandOptions(t, "25.9.9", platformsupport.RuntimeObservationNotObserved)
			observe := options.platformObserver
			calls := 0
			options.platformObserver = func(ctx context.Context) (platformsupport.RuntimeObservation, error) {
				calls++
				return observe(ctx)
			}

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := runCommand(args, &stdout, &stderr, RunOptions{}, options)
			if exitCode != 1 || calls != 1 || stdout.Len() != 0 {
				t.Fatalf("exit=%d observer calls=%d stdout=%q stderr=%q", exitCode, calls, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), "requires macOS 26.0 or newer") {
				t.Fatalf("stderr = %q, want runtime-floor rejection", stderr.String())
			}
			assertCLIUnmanageFile(t, manifestPath, manifestBefore)
			assertCLIUnmanageFile(t, hostPath, hostBefore)
			if _, err := os.Stat(filepath.Join(root, "project", "daem.lock.toml")); !os.IsNotExist(err) {
				t.Fatalf("preflight touched lockfile: %v", err)
			}
		})
	}
}

func TestPlatformPreflightRejectsUnknownMacOSRuntimeForEveryFailureReason(t *testing.T) {
	for _, reason := range []platformsupport.RuntimeObservationReason{
		platformsupport.RuntimeObservationCommandFailed,
		platformsupport.RuntimeObservationInvalidOutput,
		platformsupport.RuntimeObservationTimedOut,
	} {
		t.Run(reason.String(), func(t *testing.T) {
			options := testMacOSCommandOptions(t, "", reason)
			var stderr bytes.Buffer
			exitCode, rejected := rejectUnsupportedPlatform([]string{"lock"}, options, &stderr)
			if !rejected || exitCode != 1 {
				t.Fatalf("rejected=%t exit=%d stderr=%q", rejected, exitCode, stderr.String())
			}
			if !strings.Contains(stderr.String(), "reason="+reason.String()) {
				t.Fatalf("stderr = %q, want reason %s", stderr.String(), reason)
			}
		})
	}
}

func TestPlatformAssessmentRunsOnlyForCommandsThatConsumeIt(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantExit int
	}{
		{name: "root help", args: []string{"help"}, wantExit: 0},
		{name: "gated help", args: []string{"apply", "--help"}, wantExit: 0},
		{name: "doctor help", args: []string{"doctor", "--help"}, wantExit: 0},
		{name: "doctor usage error", args: []string{"doctor", "--unknown"}, wantExit: 2},
		{name: "version", args: []string{"version"}, wantExit: 0},
		{name: "version alias", args: []string{"--version"}, wantExit: 0},
		{name: "unknown command", args: []string{"unknown"}, wantExit: 2},
		{name: "unknown unmanage subject", args: []string{"unmanage", "unknown"}, wantExit: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := RunWithOptions(test.args, RunOptions{
				Stdout: &stdout,
				Stderr: &stderr,
				PlatformObserver: func(context.Context) (platformsupport.RuntimeObservation, error) {
					calls++
					return platformsupport.RuntimeObservation{}, errors.New("must not observe")
				},
			})
			if exitCode != test.wantExit || calls != 0 {
				t.Fatalf("exit=%d want=%d calls=%d stdout=%q stderr=%q", exitCode, test.wantExit, calls, stdout.String(), stderr.String())
			}
		})
	}
}

func testMacOSCommandOptions(
	t *testing.T,
	versionValue string,
	failure platformsupport.RuntimeObservationReason,
) commandOptions {
	t.Helper()
	admission := testPlatformAdmission(t, "darwin", "arm64")
	var observation platformsupport.RuntimeObservation
	var err error
	if versionValue != "" {
		version, parseErr := platformsupport.ParseMacOSProductVersion(versionValue)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		observation, err = platformsupport.NewRuntimeObservation(version)
	} else {
		observation, err = platformsupport.NewRuntimeObservationFailure(failure)
	}
	if err != nil {
		t.Fatal(err)
	}
	return commandOptions{
		context:           context.Background(),
		platformAdmission: admission,
		platformObserver: func(context.Context) (platformsupport.RuntimeObservation, error) {
			return observation, nil
		},
	}
}

func testPlatformAdmission(t *testing.T, goos string, goarch string) platformsupport.Admission {
	t.Helper()
	admission, err := platformsupport.Lookup(goos, goarch)
	if err != nil {
		t.Fatalf("Lookup(%q, %q) returned error: %v", goos, goarch, err)
	}
	return admission
}

func platformGatedCommandExamples(manifestPath string) [][]string {
	return [][]string{
		{"add", "skill", "example", "./skill", "--manifest", manifestPath},
		{"apply", "--yes", "--manifest", manifestPath},
		{"import", "--target", "codex", "--manifest", manifestPath},
		{"init", "--manifest", manifestPath},
		{"lock", "--manifest", manifestPath},
		{"outdated", "--manifest", manifestPath},
		{"recover", "--yes", "--manifest", manifestPath},
		{"refresh", "extension", "example", "--yes", "--manifest", manifestPath},
		{"remove", "skill", "example", "--manifest", manifestPath},
		{"unmanage", "extension", "example", "--manifest", manifestPath},
	}
}
