package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/platformsupport"
)

func TestPreparedStatusRejectsMalformedArgumentsInRequestedFormat(t *testing.T) {
	for _, args := range [][]string{
		{"status", "--targte", "bad", "--json"},
		{"status", "--json", "--targte", "bad"},
		{"status", "unbound", "--json"},
		{"status", "--json", "--json=maybe"},
		{"status", "--manifest", "--", "--help", "--json"},
	} {
		var stdout, stderr bytes.Buffer
		code := RunWithOptions(args, RunOptions{Stdout: &stdout, Stderr: &stderr})
		var payload struct {
			Command string `json:"command"`
			Error   struct {
				Kind string `json:"kind"`
			} `json:"error"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil || code != 2 || stderr.Len() != 0 ||
			payload.Command != "status" || payload.Error.Kind != "usage" {
			t.Errorf("args=%q code=%d decode=%v payload=%#v stderr=%q", args, code, err, payload, stderr.String())
		}
	}
}

func TestPreparedStatusKeepsFormatLookingManifestAsData(t *testing.T) {
	_, cwd := manifestHintWorkspace(t)
	writeHintManifest(t, filepath.Join(cwd, "--json"))
	var stdout, stderr bytes.Buffer
	code := runWithoutTerminal([]string{"status", "--manifest", "--json"}, &stdout, &stderr)
	if code != 0 || stdout.Len() == 0 || stderr.Len() != 0 || json.Valid(stdout.Bytes()) {
		t.Fatalf("manifest data acquired format authority: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestPreparedPlatformRefusalPrecedesBindingAndPathResolution(t *testing.T) {
	unsupported, err := platformsupport.Lookup("windows", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", "invalid-relative-data")
	for _, test := range []struct {
		args []string
		json bool
	}{
		{[]string{"lock", "--unknown", "--json"}, true},
		{[]string{"lock", "--manifest", "--json"}, false},
	} {
		var stdout, stderr bytes.Buffer
		code := runCommand(test.args, &stdout, &stderr, RunOptions{}, commandOptions{
			context:           context.Background(),
			platformAdmission: unsupported,
		})
		if code != 1 {
			t.Fatalf("binding/path failure outranked platform admission: %d, %q", code, stderr.String())
		}
		if !test.json {
			if stdout.Len() != 0 || stderr.Len() == 0 {
				t.Fatalf("declared path value gained format authority: %q, %q", stdout.String(), stderr.String())
			}
			continue
		}
		var payload struct {
			Command string `json:"command"`
			Error   struct {
				Kind string `json:"kind"`
			} `json:"error"`
			Guidance []struct {
				Kind string   `json:"kind"`
				Argv []string `json:"argv"`
			} `json:"guidance"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil || stderr.Len() != 0 ||
			payload.Command != "lock" || payload.Error.Kind != "operation" || len(payload.Guidance) != 1 ||
			payload.Guidance[0].Kind != "command" || len(payload.Guidance[0].Argv) != 2 || payload.Guidance[0].Argv[1] != "doctor" {
			t.Fatalf("platform refusal lost its identity/advice: %v, %#v, %q", err, payload, stderr.String())
		}
	}
}

func TestPreparedFailureGuidanceCommandsPreserveManifestRequest(t *testing.T) {
	for _, command := range [][]string{{"status"}, {"lock", "--dry-run"}} {
		for _, destination := range []string{"explicit-cwd", "implicit-user", "explicit-user", "explicit-other"} {
			t.Run(flagCaseName(command)+destination, func(t *testing.T) {
				root, cwd := manifestHintWorkspace(t)
				selected := filepath.Join(cwd, "daem.toml")
				requested := selected
				switch destination {
				case "implicit-user", "explicit-user":
					selected = filepath.Join(root, "config", "daem", "daem.toml")
					requested = selected
					if destination == "implicit-user" {
						requested = ""
					}
				case "explicit-other":
					selected = filepath.Join(root, "other workspace", "environment.toml")
					requested = selected
				}
				if err := os.MkdirAll(filepath.Dir(selected), 0o700); err != nil {
					t.Fatal(err)
				}
				before := snapshotApplyInputFiles(t, root)
				args := slices.Clone(command)
				if requested != "" {
					args = append(args, "--manifest", requested)
				}
				args = append(args, "--json")
				var stdout, stderr bytes.Buffer
				if code := runWithoutTerminal(args, &stdout, &stderr); code != 1 {
					t.Fatalf("missing-manifest refusal code=%d stderr=%q", code, stderr.String())
				}
				refusalDiagnosticText(t, command[0], &stdout, &stderr, true)
				var failure struct {
					Guidance []struct {
						Kind string   `json:"kind"`
						Argv []string `json:"argv"`
					} `json:"guidance"`
				}
				if err := json.Unmarshal(stdout.Bytes(), &failure); err != nil {
					t.Fatal(err)
				}
				if len(failure.Guidance) != 1 || failure.Guidance[0].Kind != "command" {
					t.Fatalf("missing concrete init guidance: %#v", failure.Guidance)
				}
				argv := failure.Guidance[0].Argv
				if len(argv) < 3 || argv[0] != "daem" || argv[1] != "init" || !slices.Contains(argv, "--dry-run") {
					t.Fatalf("guidance is not the safe creation preview: %q", argv)
				}

				stdout.Reset()
				stderr.Reset()
				preview := append(slices.Clone(argv[1:]), "--json")
				if code := runWithoutTerminal(preview, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
					t.Fatalf("suggested preview failed: code=%d stderr=%q", code, stderr.String())
				}
				var result struct {
					ManifestPath string `json:"manifest_path"`
				}
				if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result.ManifestPath != selected {
					t.Fatalf("guidance selected %q, want %q", result.ManifestPath, selected)
				}
				if after := snapshotApplyInputFiles(t, root); !reflect.DeepEqual(before, after) {
					t.Fatal("failure or suggested preview changed the workspace")
				}
			})
		}
	}
}
