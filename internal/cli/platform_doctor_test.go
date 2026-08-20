package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/platformsupport"
	"github.com/isty2e/daem/test/testkit/doctorenv"
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
	doctorenv.WithFakeGit(t, "git version test")

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
			"skipped=0 unsupported=",
			"error platform",
			"unsupported file_set",
			"unsupported recovery",
			"unsupported cache",
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
				Status   string `json:"status"`
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
		if payload.SchemaVersion != contractversion.DoctorJSON ||
			payload.Command != "doctor" ||
			payload.CheckCount != len(payload.Checks) ||
			payload.CheckCount < 4 ||
			len(payload.Targets) != 1 ||
			payload.Targets[0] != "codex" {
			t.Fatalf("doctor JSON envelope = %#v", payload)
		}
		found := map[string]string{}
		for _, check := range payload.Checks {
			found[check.Name] = check.Status
			if check.Name == "platform" {
				if check.Status != "error" || !strings.Contains(check.Detail, "windows/amd64") || !strings.Contains(check.NextStep, "darwin/arm64, linux/amd64") {
					t.Fatalf("platform check = %#v", check)
				}
			}
		}
		if strings.Contains(stdout.String(), `"severity"`) {
			t.Fatalf("doctor JSON retained severity: %s", stdout.String())
		}
		for _, name := range []string{"platform", "file_set", "recovery", "cache"} {
			if _, ok := found[name]; !ok {
				t.Fatalf("doctor JSON omitted %s: %s", name, stdout.String())
			}
		}
		if found["platform"] != "error" || found["file_set"] != "unsupported" || found["recovery"] != "unsupported" {
			t.Fatalf("named remaining checks = %#v", found)
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
					"doctor: 2 checks (ok=0 warn=0 error=2 skipped=0 unsupported=0)",
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
			if payload.SchemaVersion != contractversion.DoctorJSON ||
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

func TestRunCommandDoctorDisclosesMacOSRuntimeFailureInHumanAndJSONOutput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	manifestPath := filepath.Join(home, "missing.toml")
	doctorenv.WithFakeGit(t, "git version test")

	tests := []struct {
		name     string
		options  commandOptions
		want     string
		wantNext string
	}{
		{
			name:     "below floor",
			options:  testMacOSCommandOptions(t, "25.9", 0),
			want:     "observed=25.9",
			wantNext: "upgrade macOS to 26.0 or newer",
		},
		{
			name:     "invalid observation",
			options:  testMacOSCommandOptions(t, "", platformsupport.RuntimeObservationInvalidOutput),
			want:     "reason=invalid-output",
			wantNext: "verify /usr/bin/sw_vers --productVersion, then rerun daem doctor",
		},
	}
	for _, test := range tests {
		for _, jsonOutput := range []bool{false, true} {
			name := test.name + "/human"
			args := []string{"doctor", "--manifest", manifestPath, "--target", "codex"}
			if jsonOutput {
				name = test.name + "/json"
				args = append(args, "--json")
			}
			t.Run(name, func(t *testing.T) {
				var stdout bytes.Buffer
				var stderr bytes.Buffer
				exitCode := runCommand(args, &stdout, &stderr, RunOptions{}, test.options)
				if exitCode != 1 || stderr.Len() != 0 {
					t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
				}
				for _, want := range []string{"darwin/arm64", "macOS 26.0 or newer", test.want, test.wantNext} {
					if !strings.Contains(stdout.String(), want) {
						t.Fatalf("stdout = %q, want %q", stdout.String(), want)
					}
				}
				if !jsonOutput && !strings.Contains(stdout.String(), "error platform") {
					t.Fatalf("human stdout = %q", stdout.String())
				}
				if !jsonOutput && !strings.Contains(stdout.String(), "unsupported file_set") {
					t.Fatalf("human stdout omitted remaining checks: %q", stdout.String())
				}
				if jsonOutput {
					var payload struct {
						CheckCount int  `json:"check_count"`
						HasErrors  bool `json:"has_errors"`
						Checks     []struct {
							Name   string `json:"name"`
							Status string `json:"status"`
						} `json:"checks"`
					}
					if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
						t.Fatalf("decode JSON: %v", err)
					}
					if !payload.HasErrors || payload.CheckCount != len(payload.Checks) || payload.CheckCount < 4 {
						t.Fatalf("JSON payload = %#v", payload)
					}
					foundPlatform := false
					for _, check := range payload.Checks {
						if check.Name == "platform" {
							foundPlatform = true
							if check.Status != "error" {
								t.Fatalf("platform check = %#v", check)
							}
						}
					}
					if !foundPlatform {
						t.Fatalf("JSON payload omitted platform: %#v", payload)
					}
				}
			})
		}
	}
}
