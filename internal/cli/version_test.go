package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/buildidentity"
	"github.com/isty2e/daem/internal/platformsupport"
)

const cliVersionTestRevision = "0123456789abcdef0123456789abcdef01234567"

func TestRunVersionHumanAndAliasAreEquivalent(t *testing.T) {
	identity := cliVersionTestIdentity(t)
	var commandOutput bytes.Buffer
	var aliasOutput bytes.Buffer
	var stderr bytes.Buffer
	invocation := commandOptions{context: context.Background(), buildIdentity: identity}

	if exitCode := runCommand([]string{"version"}, &commandOutput, &stderr, RunOptions{}, invocation); exitCode != 0 {
		t.Fatalf("version exit = %d, stderr=%q", exitCode, stderr.String())
	}
	if exitCode := runCommand([]string{"--version"}, &aliasOutput, &stderr, RunOptions{}, invocation); exitCode != 0 {
		t.Fatalf("--version exit = %d, stderr=%q", exitCode, stderr.String())
	}
	if commandOutput.String() != aliasOutput.String() {
		t.Fatalf("version=%q --version=%q", commandOutput.String(), aliasOutput.String())
	}
	for _, want := range []string{"version=v1.2.3", "revision=" + cliVersionTestRevision, "source=clean", "go=go1.26.5", "target=linux/amd64"} {
		if !strings.Contains(commandOutput.String(), want) {
			t.Fatalf("output = %q, want %q", commandOutput.String(), want)
		}
	}
}

func TestRunVersionJSONHasExactPublicFields(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runCommand(
		[]string{"version", "--json"},
		&stdout,
		&stderr,
		RunOptions{},
		commandOptions{context: context.Background(), buildIdentity: cliVersionTestIdentity(t)},
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	wantKeys := []string{"schema_version", "version", "revision", "revision_time", "source_state", "vcs", "go_version", "goos", "goarch"}
	if len(payload) != len(wantKeys) {
		t.Fatalf("payload = %#v", payload)
	}
	for _, key := range wantKeys {
		if _, ok := payload[key]; !ok {
			t.Fatalf("payload omitted %q: %#v", key, payload)
		}
	}
}

func TestVersionHelpRoutesArePortable(t *testing.T) {
	unsupported, err := platformsupport.Lookup("windows", "amd64")
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}
	invocation := commandOptions{
		context:           context.Background(),
		platformAdmission: unsupported,
		buildIdentity:     cliVersionTestIdentity(t),
	}
	for _, args := range [][]string{{"version", "--help"}, {"version", "-h"}, {"help", "version"}} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := runCommand(args, &stdout, &stderr, RunOptions{}, invocation)
		if exitCode != 0 || stderr.Len() != 0 {
			t.Fatalf("runCommand(%#v) = %d stderr=%q", args, exitCode, stderr.String())
		}
		if !strings.Contains(stdout.String(), "daem version [--json]") {
			t.Fatalf("runCommand(%#v) output=%q", args, stdout.String())
		}
	}
}

func TestVersionRunsOnUnsupportedPlatformWithoutWorkspaceEnvironment(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "relative-and-invalid")
	t.Setenv("XDG_DATA_HOME", "relative-and-invalid")
	unsupported, err := platformsupport.Lookup("windows", "amd64")
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}
	for _, args := range [][]string{{"version"}, {"version", "--json"}, {"--version"}} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := runCommand(args, &stdout, &stderr, RunOptions{}, commandOptions{
			context:           context.Background(),
			platformAdmission: unsupported,
			buildIdentity:     cliVersionTestIdentity(t),
		})
		if exitCode != 0 || stdout.Len() == 0 || stderr.Len() != 0 {
			t.Fatalf("runCommand(%#v) = %d stdout=%q stderr=%q", args, exitCode, stdout.String(), stderr.String())
		}
	}
}

func TestVersionRejectsEveryUnadmittedArgument(t *testing.T) {
	tests := [][]string{
		{"version", "--verbose"},
		{"version", "--json", "extra"},
		{"version", "help"},
		{"--version", "--json"},
		{"--version", "extra"},
	}
	for _, args := range tests {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := runCommand(args, &stdout, &stderr, RunOptions{}, commandOptions{buildIdentity: cliVersionTestIdentity(t)})
		if exitCode != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "unexpected argument") {
			t.Fatalf("runCommand(%#v) = %d stdout=%q stderr=%q", args, exitCode, stdout.String(), stderr.String())
		}
	}
}

func TestVersionReportsOutputFailure(t *testing.T) {
	want := errors.New("stdout closed")
	for _, args := range [][]string{{"version"}, {"version", "--json"}, {"--version"}} {
		var stderr bytes.Buffer
		exitCode := runCommand(args, errorWriter{err: want}, &stderr, RunOptions{}, commandOptions{buildIdentity: cliVersionTestIdentity(t)})
		if exitCode != 1 || !strings.Contains(stderr.String(), "version failed: stdout closed") {
			t.Fatalf("runCommand(%#v) = %d stderr=%q", args, exitCode, stderr.String())
		}
	}
}

func TestRunVersionDoesNotConsultManifestSelection(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "relative-and-invalid")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := RunWithOptions([]string{"version", "--json"}, RunOptions{Stdout: &stdout, Stderr: &stderr}); exitCode != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
}

func cliVersionTestIdentity(t *testing.T) buildidentity.Identity {
	t.Helper()
	identity, err := buildidentity.FromBuildInfo(debug.BuildInfo{
		Path:      buildidentity.MainPackagePath,
		GoVersion: "go1.26.5",
		Main:      debug.Module{Path: buildidentity.MainModulePath, Version: "v1.2.3"},
		Settings: []debug.BuildSetting{
			{Key: "vcs", Value: "git"},
			{Key: "vcs.revision", Value: cliVersionTestRevision},
			{Key: "vcs.time", Value: "2026-07-01T02:03:04Z"},
			{Key: "vcs.modified", Value: "false"},
			{Key: "GOOS", Value: "linux"},
			{Key: "GOARCH", Value: "amd64"},
			{Key: "CGO_ENABLED", Value: "0"},
		},
	})
	if err != nil {
		t.Fatalf("build test identity: %v", err)
	}
	return identity
}
