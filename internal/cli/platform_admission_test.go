package cli

import (
	"bytes"
	"context"
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
	}
	for _, args := range ungated {
		if requiresPlatformAdmission(args) {
			t.Errorf("requiresPlatformAdmission(%#v) = true, want false", args)
		}
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
				testPlatformAdmission(t, test.goos, test.goarch),
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
			testPlatformAdmission(t, target[0], target[1]),
			&stderr,
		)
		if rejected || exitCode != 0 || stderr.Len() != 0 {
			t.Fatalf("admitted %s/%s rejected=%t exitCode=%d stderr=%q", target[0], target[1], rejected, exitCode, stderr.String())
		}
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
	}
}
