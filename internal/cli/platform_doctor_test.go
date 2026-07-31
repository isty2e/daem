package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCommandDoctorDisclosesUnsupportedPlatformInHumanAndJSONOutput(t *testing.T) {
	admission := testPlatformAdmission(t, "windows", "amd64")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	manifestPath := filepath.Join(home, "missing.toml")

	t.Run("human", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := runCommand(
			[]string{"doctor", "--manifest", manifestPath, "--target", "codex"},
			&stdout,
			&stderr,
			RunOptions{},
			commandOptions{context: context.Background(), platformAdmission: admission},
		)
		if exitCode != 1 {
			t.Fatalf("exitCode = %d, want 1; stderr=%q", exitCode, stderr.String())
		}
		for _, want := range []string{
			"targets: codex",
			"doctor: 1 checks (ok=0 warn=0 error=1)",
			"error platform",
			"windows/amd64",
			"verification=compile-only",
			"next=\"run daem on an admitted platform",
		} {
			if !strings.Contains(stdout.String(), want) {
				t.Fatalf("stdout = %q, want %q", stdout.String(), want)
			}
		}
		if stderr.Len() != 0 {
			t.Fatalf("stderr = %q, want empty", stderr.String())
		}
	})

	t.Run("json", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := runCommand(
			[]string{"doctor", "--manifest", manifestPath, "--target", "codex", "--json"},
			&stdout,
			&stderr,
			RunOptions{},
			commandOptions{context: context.Background(), platformAdmission: admission},
		)
		if exitCode != 1 {
			t.Fatalf("exitCode = %d, want 1; stderr=%q", exitCode, stderr.String())
		}
		var payload struct {
			SchemaVersion int      `json:"schema_version"`
			Command       string   `json:"command"`
			Targets       []string `json:"targets"`
			CheckCount    int      `json:"check_count"`
			HasErrors     bool     `json:"has_errors"`
			Checks        []struct {
				Severity string `json:"severity"`
				Name     string `json:"name"`
				Detail   string `json:"detail"`
				NextStep string `json:"next_step"`
			} `json:"checks"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
			t.Fatalf("decode doctor JSON: %v\n%s", err, stdout.String())
		}
		if !payload.HasErrors {
			t.Fatal("doctor JSON has_errors = false")
		}
		if payload.SchemaVersion != 1 ||
			payload.Command != "doctor" ||
			payload.CheckCount != 1 ||
			len(payload.Checks) != 1 ||
			len(payload.Targets) != 1 ||
			payload.Targets[0] != "codex" {
			t.Fatalf("doctor JSON envelope = %#v", payload)
		}
		found := false
		for _, check := range payload.Checks {
			if check.Name != "platform" {
				continue
			}
			found = true
			if check.Severity != "error" || !strings.Contains(check.Detail, "windows/amd64") || !strings.Contains(check.NextStep, "darwin/arm64, linux/amd64") {
				t.Fatalf("platform check = %#v", check)
			}
		}
		if !found {
			t.Fatalf("doctor JSON omitted platform check: %s", stdout.String())
		}
		if stderr.Len() != 0 {
			t.Fatalf("stderr = %q, want empty", stderr.String())
		}
	})
}

func TestRunCommandDoctorKeepsPathFailureAlongsideUnsupportedPlatform(t *testing.T) {
	admission := testPlatformAdmission(t, "windows", "amd64")
	t.Setenv("XDG_DATA_HOME", "relative-data-root")
	manifestPath := filepath.Join(t.TempDir(), "missing.toml")

	for _, jsonOutput := range []bool{false, true} {
		name := "human"
		args := []string{"doctor", "--manifest", manifestPath, "--target", "codex"}
		if jsonOutput {
			name = "json"
			args = append(args, "--json")
		}
		t.Run(name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := runCommand(
				args,
				&stdout,
				&stderr,
				RunOptions{},
				commandOptions{
					context:           context.Background(),
					platformAdmission: admission,
				},
			)
			if exitCode != 1 || stderr.Len() != 0 {
				t.Fatalf(
					"exitCode = %d stdout=%q stderr=%q",
					exitCode,
					stdout.String(),
					stderr.String(),
				)
			}
			for _, want := range []string{
				"windows/amd64",
				"XDG_DATA_HOME must be an absolute path",
			} {
				if !strings.Contains(stdout.String(), want) {
					t.Fatalf("stdout = %q, want %q", stdout.String(), want)
				}
			}
			if !jsonOutput {
				if !strings.Contains(
					stdout.String(),
					"doctor: 2 checks (ok=0 warn=0 error=2)",
				) {
					t.Fatalf("human output = %q, want two errors", stdout.String())
				}
				return
			}

			var payload struct {
				SchemaVersion int  `json:"schema_version"`
				CheckCount    int  `json:"check_count"`
				HasErrors     bool `json:"has_errors"`
				Checks        []struct {
					Name string `json:"name"`
				} `json:"checks"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
				t.Fatalf("decode doctor JSON: %v\n%s", err, stdout.String())
			}
			if payload.SchemaVersion != 1 ||
				payload.CheckCount != 2 ||
				!payload.HasErrors ||
				len(payload.Checks) != 2 ||
				payload.Checks[0].Name != "platform" ||
				payload.Checks[1].Name != "paths" {
				t.Fatalf("doctor JSON = %#v", payload)
			}
		})
	}
}
