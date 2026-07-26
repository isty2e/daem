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
		for _, want := range []string{"error platform", "windows/amd64", "verification=compile-only", "next=\"run daem on an admitted platform"} {
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
			HasErrors bool `json:"has_errors"`
			Checks    []struct {
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
